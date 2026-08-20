package handlers

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"balanceserver/models"

	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	customSqlExportDataDir    = "data/custom_sql_exports"
	customSqlExportJobTTL     = 24 * time.Hour
	customSqlExportRunLimit   = 30 * time.Minute
	customSqlExportMaxLen     = 20000
	customSqlDownloadTokenTTL = 60 * time.Second
)

// customSqlDownloadTokens holds short-lived, single-use download tokens.
// Browser-native navigation (a plain <a href> click) is what lets large
// export files stream straight to disk instead of being buffered fully in
// memory by axios/blob, but native navigation can't carry an Authorization
// header - so a normal authenticated request exchanges the job for a token
// here, and the actual file transfer is authorized by that token instead.
var (
	customSqlDownloadTokens   = map[string]customSqlDownloadTokenEntry{}
	customSqlDownloadTokensMu sync.Mutex
)

type customSqlDownloadTokenEntry struct {
	JobID     primitive.ObjectID
	Format    string
	ExpiresAt time.Time
}

func issueCustomSqlDownloadToken(jobID primitive.ObjectID, format string) (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)

	customSqlDownloadTokensMu.Lock()
	defer customSqlDownloadTokensMu.Unlock()
	now := time.Now()
	for k, v := range customSqlDownloadTokens {
		if now.After(v.ExpiresAt) {
			delete(customSqlDownloadTokens, k)
		}
	}
	customSqlDownloadTokens[token] = customSqlDownloadTokenEntry{
		JobID: jobID, Format: format, ExpiresAt: now.Add(customSqlDownloadTokenTTL),
	}
	return token, nil
}

func consumeCustomSqlDownloadToken(token string) (customSqlDownloadTokenEntry, bool) {
	customSqlDownloadTokensMu.Lock()
	defer customSqlDownloadTokensMu.Unlock()
	entry, ok := customSqlDownloadTokens[token]
	if !ok {
		return customSqlDownloadTokenEntry{}, false
	}
	delete(customSqlDownloadTokens, token)
	if time.Now().After(entry.ExpiresAt) {
		return customSqlDownloadTokenEntry{}, false
	}
	return entry, true
}

// customSqlDeniedKeywords blocks statements that write, alter schema, manage
// sessions/transactions, or read/write files on the DB server. This is a
// denylist, not a real SQL parser - it is meant to catch mistakes (e.g. a
// pasted DELETE) on a single-admin internal tool, not to sandbox a hostile
// operator with valid login credentials.
var customSqlDeniedKeywordsRe = regexp.MustCompile(`(?i)\b(insert|update|delete|replace|merge|drop|alter|create|truncate|grant|revoke|call|exec|execute|set|lock|unlock|use|prepare|deallocate|handler|rename|optimize|repair|flush|kill|shutdown|load|benchmark|sleep)\b|into\s+outfile|into\s+dumpfile|load_file\s*\(`)

var (
	leadingLineCommentRe  = regexp.MustCompile(`^\s*--[^\n]*\n?`)
	leadingBlockCommentRe = regexp.MustCompile(`^\s*/\*.*?\*/`)
)

// sanitizeReadOnlyQuery validates that raw is a single, read-only SELECT (or
// WITH ... SELECT) statement and returns the cleaned query (trailing ';'
// stripped) ready to execute.
func sanitizeReadOnlyQuery(raw string) (string, error) {
	q := strings.TrimSpace(raw)
	if q == "" {
		return "", fmt.Errorf("请输入 SQL 查询语句")
	}
	if len(q) > customSqlExportMaxLen {
		return "", fmt.Errorf("SQL 语句过长，最多 %d 字符", customSqlExportMaxLen)
	}

	// Strip a single trailing ';' (and surrounding whitespace) before
	// checking for stacked statements.
	trimmed := strings.TrimSpace(q)
	trimmed = strings.TrimSuffix(trimmed, ";")
	trimmed = strings.TrimSpace(trimmed)
	if strings.Contains(trimmed, ";") {
		return "", fmt.Errorf("不支持多条语句，请仅提交单条 SELECT 查询")
	}

	// Determine the "real" first keyword after stripping any leading
	// comments an attacker might use to disguise the statement type.
	head := trimmed
	for {
		if loc := leadingLineCommentRe.FindStringIndex(head); loc != nil {
			head = head[loc[1]:]
			continue
		}
		if loc := leadingBlockCommentRe.FindStringIndex(head); loc != nil {
			head = head[loc[1]:]
			continue
		}
		break
	}
	head = strings.TrimSpace(head)
	upperHead := strings.ToUpper(head)
	if !strings.HasPrefix(upperHead, "SELECT") && !strings.HasPrefix(upperHead, "WITH") {
		return "", fmt.Errorf("仅支持 SELECT 查询语句")
	}

	if customSqlDeniedKeywordsRe.MatchString(trimmed) {
		return "", fmt.Errorf("查询语句包含不允许的关键字，仅支持只读 SELECT 查询")
	}

	return trimmed, nil
}

func ensureCustomSqlExportDir() error {
	return os.MkdirAll(customSqlExportDataDir, 0o755)
}

// openMySQLForExport is like openMySQLWithTimeout but with a much longer
// read timeout: custom queries can scan millions of rows with no LIMIT, so a
// 60s read timeout (fine for the indexed, bounded bill-export queries) would
// abort large-but-legitimate exports before they finish.
func openMySQLForExport(dsn string) (*sql.DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("解析 DSN 失败: %w", err)
	}
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = customSqlExportRunLimit
	cfg.WriteTimeout = 60 * time.Second
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector), nil
}

// CreateCustomSqlExportJobHandler validates the submitted SQL, records a
// pending job, and kicks off execution in a background goroutine so the
// frontend doesn't hold a connection open for a slow/arbitrary query.
func CreateCustomSqlExportJobHandler(c *gin.Context) {
	var req struct {
		UpstreamSiteID string `json:"upstreamSiteId"`
		SQL            string `json:"sql"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}

	siteID, err := primitive.ObjectIDFromHex(strings.TrimSpace(req.UpstreamSiteID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 upstreamSiteId"})
		return
	}

	cleanedSQL, err := sanitizeReadOnlyQuery(req.SQL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	site, err := loadUpstreamSite(ctx, siteID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "上游站点不存在"})
		return
	}
	if strings.TrimSpace(site.SqlDsn) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该站点未配置 MySQL 连接字符串"})
		return
	}

	now := time.Now()
	job := models.CustomSqlExportJob{
		UpstreamSiteID: siteID,
		SQL:            cleanedSQL,
		Status:         "pending",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	res, err := CustomSqlExportJobCol.InsertOne(ctx, job)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建导出任务失败"})
		return
	}
	job.ID = res.InsertedID.(primitive.ObjectID)

	go runCustomSqlExportJob(job.ID, site.SqlDsn, cleanedSQL)

	c.JSON(http.StatusOK, job)
}

func runCustomSqlExportJob(jobID primitive.ObjectID, dsn string, query string) {
	ctx, cancel := context.WithTimeout(context.Background(), customSqlExportRunLimit)
	defer cancel()

	failJob := func(errMsg string) {
		CustomSqlExportJobCol.UpdateByID(ctx, jobID, bson.M{"$set": bson.M{
			"status": "failed", "error": errMsg, "updated_at": time.Now(),
		}})
	}

	CustomSqlExportJobCol.UpdateByID(ctx, jobID, bson.M{"$set": bson.M{
		"status": "running", "updated_at": time.Now(),
	}})

	db, err := openMySQLForExport(dsn)
	if err != nil {
		failJob(fmt.Sprintf("打开数据库连接失败: %v", err))
		return
	}
	defer db.Close()

	// Grab a single physical connection so the READ ONLY session setting
	// is guaranteed to apply to the connection the query actually runs on
	// (a pooled *sql.DB could otherwise hand the query to a different,
	// unconfigured connection).
	conn, err := db.Conn(ctx)
	if err != nil {
		failJob(fmt.Sprintf("获取数据库连接失败: %v", err))
		return
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "SET SESSION TRANSACTION READ ONLY"); err != nil {
		failJob(fmt.Sprintf("无法将连接设置为只读，为安全起见已取消执行: %v", err))
		return
	}

	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		failJob(fmt.Sprintf("执行查询失败: %v", err))
		return
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		failJob(fmt.Sprintf("读取结果列失败: %v", err))
		return
	}

	if err := ensureCustomSqlExportDir(); err != nil {
		failJob(fmt.Sprintf("创建导出目录失败: %v", err))
		return
	}

	fileName := fmt.Sprintf("自定义查询-%s.csv", time.Now().Format("20060102-150405"))
	filePath := filepath.Join(customSqlExportDataDir, jobID.Hex()+".csv")

	f, err := os.Create(filePath)
	if err != nil {
		failJob(fmt.Sprintf("创建导出文件失败: %v", err))
		return
	}
	defer f.Close()

	// UTF-8 BOM so the CSV opens correctly in Excel on Windows.
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		failJob(fmt.Sprintf("写入导出文件失败: %v", err))
		return
	}

	w := csv.NewWriter(f)
	if err := w.Write(cols); err != nil {
		failJob(fmt.Sprintf("写入导出文件失败: %v", err))
		return
	}

	values := make([]sql.RawBytes, len(cols))
	scanArgs := make([]interface{}, len(cols))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	rowCount := 0
	record := make([]string, len(cols))
	for rows.Next() {
		if err := rows.Scan(scanArgs...); err != nil {
			failJob(fmt.Sprintf("解析查询结果失败: %v", err))
			return
		}
		for i, v := range values {
			if v == nil {
				record[i] = ""
			} else {
				record[i] = string(v)
			}
		}
		if err := w.Write(record); err != nil {
			failJob(fmt.Sprintf("写入导出文件失败: %v", err))
			return
		}
		rowCount++
	}
	if err := rows.Err(); err != nil {
		failJob(fmt.Sprintf("读取查询结果失败: %v", err))
		return
	}
	w.Flush()
	if err := w.Error(); err != nil {
		failJob(fmt.Sprintf("写入导出文件失败: %v", err))
		return
	}

	CustomSqlExportJobCol.UpdateByID(ctx, jobID, bson.M{"$set": bson.M{
		"status": "completed", "error": "", "file_path": filePath, "file_name": fileName,
		"row_count": rowCount, "updated_at": time.Now(),
	}})
	log.Printf("[custom-sql-export] job %s completed: %d rows -> %s", jobID.Hex(), rowCount, filePath)
}

func ListCustomSqlExportJobsHandler(c *gin.Context) {
	siteIDStr := strings.TrimSpace(c.Query("upstreamSiteId"))
	if siteIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 upstreamSiteId 参数"})
		return
	}
	siteID, err := primitive.ObjectIDFromHex(siteIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 upstreamSiteId"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(20)
	cursor, err := CustomSqlExportJobCol.Find(ctx, bson.M{"upstream_site_id": siteID}, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询任务列表失败"})
		return
	}
	defer cursor.Close(ctx)

	jobs := make([]models.CustomSqlExportJob, 0)
	if err := cursor.All(ctx, &jobs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "解析任务列表失败"})
		return
	}
	c.JSON(http.StatusOK, jobs)
}

func GetCustomSqlExportJobHandler(c *gin.Context) {
	jobID, err := primitive.ObjectIDFromHex(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务 ID"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var job models.CustomSqlExportJob
	if err := CustomSqlExportJobCol.FindOne(ctx, bson.M{"_id": jobID}).Decode(&job); err != nil {
		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询任务失败"})
		return
	}
	c.JSON(http.StatusOK, job)
}

// CreateCustomSqlExportDownloadTokenHandler exchanges a completed job for a
// short-lived, single-use download token. Called from an authenticated XHR
// request; the returned token is then used in a plain, unauthenticated GET
// so the browser can navigate/stream the file directly to disk.
func CreateCustomSqlExportDownloadTokenHandler(c *gin.Context) {
	jobID, err := primitive.ObjectIDFromHex(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务 ID"})
		return
	}

	format := strings.ToLower(c.Query("format"))
	if format != "zip" {
		format = "csv"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var job models.CustomSqlExportJob
	if err := CustomSqlExportJobCol.FindOne(ctx, bson.M{"_id": jobID}).Decode(&job); err != nil {
		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询任务失败"})
		return
	}
	if job.Status != "completed" || job.FilePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "任务尚未完成"})
		return
	}
	if _, err := os.Stat(job.FilePath); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "文件已过期或不存在"})
		return
	}

	token, err := issueCustomSqlDownloadToken(jobID, format)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成下载令牌失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "expiresIn": int(customSqlDownloadTokenTTL.Seconds())})
}

// DownloadCustomSqlExportJobHandler serves the exported CSV gzip-compressed
// (a plain text/csv shrinks a lot under gzip, which keeps large exports
// under whatever size/time limit sits in front of this server), or - when
// the token was issued with format=zip - as a zip archive built on the fly
// so only the raw CSV is ever kept on disk. Authorization comes from a
// single-use token (see CreateCustomSqlExportDownloadTokenHandler) rather
// than the normal JWT header, since this endpoint is hit by a plain browser
// navigation (<a href>) so the file streams straight to disk instead of
// being buffered fully in memory as a blob.
func DownloadCustomSqlExportJobHandler(c *gin.Context) {
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少下载令牌"})
		return
	}
	entry, ok := consumeCustomSqlDownloadToken(token)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "下载令牌无效或已过期，请重新点击下载"})
		return
	}
	jobID := entry.JobID

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var job models.CustomSqlExportJob
	if err := CustomSqlExportJobCol.FindOne(ctx, bson.M{"_id": jobID}).Decode(&job); err != nil {
		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询任务失败"})
		return
	}
	if job.Status != "completed" || job.FilePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "任务尚未完成"})
		return
	}

	f, err := os.Open(job.FilePath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "文件已过期或不存在"})
		return
	}
	defer f.Close()

	if entry.Format == "zip" {
		zipName := strings.TrimSuffix(job.FileName, ".csv") + ".zip"
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, zipName))

		zw := zip.NewWriter(c.Writer)
		zipEntry, err := zw.Create(job.FileName)
		if err != nil {
			log.Printf("[custom-sql-export] zip create entry failed for job %s: %v", jobID.Hex(), err)
			return
		}
		if _, err := io.Copy(zipEntry, f); err != nil {
			log.Printf("[custom-sql-export] zip write failed for job %s: %v", jobID.Hex(), err)
			return
		}
		if err := zw.Close(); err != nil {
			log.Printf("[custom-sql-export] zip close failed for job %s: %v", jobID.Hex(), err)
		}
		return
	}

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, job.FileName))
	c.Header("Content-Encoding", "gzip")

	gz := gzip.NewWriter(c.Writer)
	if _, err := io.Copy(gz, f); err != nil {
		log.Printf("[custom-sql-export] gzip write failed for job %s: %v", jobID.Hex(), err)
		return
	}
	if err := gz.Close(); err != nil {
		log.Printf("[custom-sql-export] gzip close failed for job %s: %v", jobID.Hex(), err)
	}
}

// StartCustomSqlExportCleanupScheduler periodically removes exported CSV
// files and job records older than customSqlExportJobTTL, mirroring
// StartBillExportCleanupScheduler's 24h retention policy.
func StartCustomSqlExportCleanupScheduler() {
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()

		cleanupExpiredCustomSqlExportJobs()
		for range ticker.C {
			cleanupExpiredCustomSqlExportJobs()
		}
	}()
}

func cleanupExpiredCustomSqlExportJobs() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cutoff := time.Now().Add(-customSqlExportJobTTL)
	cursor, err := CustomSqlExportJobCol.Find(ctx, bson.M{"created_at": bson.M{"$lt": cutoff}})
	if err != nil {
		log.Printf("[custom-sql-export] cleanup query failed: %v", err)
		return
	}
	defer cursor.Close(ctx)

	var expired []models.CustomSqlExportJob
	if err := cursor.All(ctx, &expired); err != nil {
		log.Printf("[custom-sql-export] cleanup decode failed: %v", err)
		return
	}
	if len(expired) == 0 {
		return
	}

	var ids []primitive.ObjectID
	for _, job := range expired {
		if job.FilePath != "" {
			if err := os.Remove(job.FilePath); err != nil && !os.IsNotExist(err) {
				log.Printf("[custom-sql-export] failed to remove file %s: %v", job.FilePath, err)
			}
		}
		ids = append(ids, job.ID)
	}
	if _, err := CustomSqlExportJobCol.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": ids}}); err != nil {
		log.Printf("[custom-sql-export] cleanup delete failed: %v", err)
		return
	}
	log.Printf("[custom-sql-export] cleaned up %d expired job(s)", len(expired))
}
