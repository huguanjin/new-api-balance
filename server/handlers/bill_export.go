package handlers

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"balanceserver/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	billExportDataDir  = "data/bill_exports"
	billExportJobTTL   = 24 * time.Hour
	billExportRunLimit = 10 * time.Minute
)

// openMySQLWithTimeout opens a MySQL connection with short dial/read/write
// timeouts. The site's DSN is arbitrary user input (potentially pointing at
// an unreachable host), and database/sql's default is to block on TCP dial
// until the OS-level timeout - which is far longer than any HTTP proxy or
// load balancer in front of this server will wait, producing a confusing
// 504 upstream instead of a clear error from this handler.
func openMySQLWithTimeout(dsn string) (*sql.DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("解析 DSN 失败: %w", err)
	}
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 60 * time.Second
	cfg.WriteTimeout = 60 * time.Second
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector), nil
}

func ensureBillExportDir() error {
	return os.MkdirAll(billExportDataDir, 0o755)
}

// CreateCustomerBillExportJobHandler enqueues an async export: it validates
// params, records a job document, kicks off the MySQL query + CSV write in a
// background goroutine, and returns immediately so the frontend doesn't have
// to hold a connection open for a slow upstream query.
func CreateCustomerBillExportJobHandler(c *gin.Context) {
	var req struct {
		UpstreamSiteID string `json:"upstreamSiteId"`
		Username       string `json:"username"`
		UserID         string `json:"userId"`
		StartTS        int64  `json:"startTs"`
		EndTS          int64  `json:"endTs"`
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

	username := strings.TrimSpace(req.Username)
	userIDStr := strings.TrimSpace(req.UserID)
	if username == "" && userIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供用户名或用户ID"})
		return
	}
	var userID int64
	if userIDStr != "" {
		userID, err = strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "userId 格式错误"})
			return
		}
	}

	if req.StartTS == 0 || req.EndTS == 0 || req.EndTS < req.StartTS {
		c.JSON(http.StatusBadRequest, gin.H{"error": "时间范围无效"})
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
	job := models.BillExportJob{
		UpstreamSiteID: siteID,
		Username:       username,
		UserID:         userID,
		StartTS:        req.StartTS,
		EndTS:          req.EndTS,
		Status:         "pending",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	res, err := BillExportJobCol.InsertOne(ctx, job)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建导出任务失败"})
		return
	}
	job.ID = res.InsertedID.(primitive.ObjectID)

	go runCustomerBillExportJob(job.ID, site.SqlDsn)

	c.JSON(http.StatusOK, job)
}

func runCustomerBillExportJob(jobID primitive.ObjectID, dsn string) {
	ctx, cancel := context.WithTimeout(context.Background(), billExportRunLimit)
	defer cancel()

	var job models.BillExportJob
	if err := BillExportJobCol.FindOne(ctx, bson.M{"_id": jobID}).Decode(&job); err != nil {
		log.Printf("[bill-export] job %s not found: %v", jobID.Hex(), err)
		return
	}

	failJob := func(errMsg string) {
		BillExportJobCol.UpdateByID(ctx, jobID, bson.M{"$set": bson.M{
			"status": "failed", "error": errMsg, "updated_at": time.Now(),
		}})
	}

	BillExportJobCol.UpdateByID(ctx, jobID, bson.M{"$set": bson.M{
		"status": "running", "updated_at": time.Now(),
	}})

	db, err := openMySQLWithTimeout(dsn)
	if err != nil {
		failJob(fmt.Sprintf("打开数据库连接失败: %v", err))
		return
	}
	defer db.Close()

	query := "SELECT id, user_id, username, type, created_at, model_name, `group`, " +
		"prompt_tokens, completion_tokens, quota, is_stream, request_id, other " +
		"FROM logs WHERE type = 2 AND created_at >= ? AND created_at <= ?"
	args := []interface{}{job.StartTS, job.EndTS}
	if job.Username != "" {
		query += " AND username = ?"
		args = append(args, job.Username)
	} else {
		query += " AND user_id = ?"
		args = append(args, job.UserID)
	}
	query += " ORDER BY id"

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		failJob(fmt.Sprintf("查询账单失败: %v", err))
		return
	}
	defer rows.Close()

	if err := ensureBillExportDir(); err != nil {
		failJob(fmt.Sprintf("创建导出目录失败: %v", err))
		return
	}

	nameHint := job.Username
	if nameHint == "" {
		nameHint = strconv.FormatInt(job.UserID, 10)
	}
	fileName := fmt.Sprintf("账单-%s-%s.csv", nameHint, time.Now().Format("20060102-150405"))
	filePath := filepath.Join(billExportDataDir, jobID.Hex()+".csv")

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
	header := []string{"记录id", "用户id", "用户名", "类型", "创建时间", "模型名称", "分组",
		"输入", "输出", "费用(USD)", "is_stream", "request_id", "other"}
	if err := w.Write(header); err != nil {
		failJob(fmt.Sprintf("写入导出文件失败: %v", err))
		return
	}

	rowCount := 0
	for rows.Next() {
		var (
			id, userID, createdAt, quota           int64
			logType, promptTokens, completionTokens int
			username, modelName, group, requestID, other string
			isStream                                bool
		)
		if err := rows.Scan(&id, &userID, &username, &logType, &createdAt, &modelName, &group,
			&promptTokens, &completionTokens, &quota, &isStream, &requestID, &other); err != nil {
			failJob(fmt.Sprintf("解析账单数据失败: %v", err))
			return
		}
		record := []string{
			strconv.FormatInt(id, 10),
			strconv.FormatInt(userID, 10),
			username,
			strconv.Itoa(logType),
			time.Unix(createdAt, 0).In(cstZone).Format("2006-01-02 15:04:05"),
			modelName,
			group,
			strconv.Itoa(promptTokens),
			strconv.Itoa(completionTokens),
			strconv.FormatFloat(float64(quota)/500000, 'f', 4, 64),
			strconv.FormatBool(isStream),
			requestID,
			other,
		}
		if err := w.Write(record); err != nil {
			failJob(fmt.Sprintf("写入导出文件失败: %v", err))
			return
		}
		rowCount++
	}
	if err := rows.Err(); err != nil {
		failJob(fmt.Sprintf("读取账单数据失败: %v", err))
		return
	}
	w.Flush()
	if err := w.Error(); err != nil {
		failJob(fmt.Sprintf("写入导出文件失败: %v", err))
		return
	}

	BillExportJobCol.UpdateByID(ctx, jobID, bson.M{"$set": bson.M{
		"status": "completed", "error": "", "file_path": filePath, "file_name": fileName,
		"row_count": rowCount, "updated_at": time.Now(),
	}})
	log.Printf("[bill-export] job %s completed: %d rows -> %s", jobID.Hex(), rowCount, filePath)
}

func ListCustomerBillExportJobsHandler(c *gin.Context) {
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
	cursor, err := BillExportJobCol.Find(ctx, bson.M{"upstream_site_id": siteID}, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询任务列表失败"})
		return
	}
	defer cursor.Close(ctx)

	jobs := make([]models.BillExportJob, 0)
	if err := cursor.All(ctx, &jobs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "解析任务列表失败"})
		return
	}
	c.JSON(http.StatusOK, jobs)
}

func GetCustomerBillExportJobHandler(c *gin.Context) {
	jobID, err := primitive.ObjectIDFromHex(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务 ID"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var job models.BillExportJob
	if err := BillExportJobCol.FindOne(ctx, bson.M{"_id": jobID}).Decode(&job); err != nil {
		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询任务失败"})
		return
	}
	c.JSON(http.StatusOK, job)
}

func DownloadCustomerBillExportJobHandler(c *gin.Context) {
	jobID, err := primitive.ObjectIDFromHex(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务 ID"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var job models.BillExportJob
	if err := BillExportJobCol.FindOne(ctx, bson.M{"_id": jobID}).Decode(&job); err != nil {
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

	c.FileAttachment(job.FilePath, job.FileName)
}

// StartBillExportCleanupScheduler periodically removes exported CSV files
// and job records older than billExportJobTTL, since these are meant to be
// temporary (24h) downloads, not permanent storage.
func StartBillExportCleanupScheduler() {
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()

		cleanupExpiredBillExportJobs()
		for range ticker.C {
			cleanupExpiredBillExportJobs()
		}
	}()
}

func cleanupExpiredBillExportJobs() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cutoff := time.Now().Add(-billExportJobTTL)
	cursor, err := BillExportJobCol.Find(ctx, bson.M{"created_at": bson.M{"$lt": cutoff}})
	if err != nil {
		log.Printf("[bill-export] cleanup query failed: %v", err)
		return
	}
	defer cursor.Close(ctx)

	var expired []models.BillExportJob
	if err := cursor.All(ctx, &expired); err != nil {
		log.Printf("[bill-export] cleanup decode failed: %v", err)
		return
	}
	if len(expired) == 0 {
		return
	}

	var ids []primitive.ObjectID
	for _, job := range expired {
		if job.FilePath != "" {
			if err := os.Remove(job.FilePath); err != nil && !os.IsNotExist(err) {
				log.Printf("[bill-export] failed to remove file %s: %v", job.FilePath, err)
			}
		}
		ids = append(ids, job.ID)
	}
	if _, err := BillExportJobCol.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": ids}}); err != nil {
		log.Printf("[bill-export] cleanup delete failed: %v", err)
		return
	}
	log.Printf("[bill-export] cleaned up %d expired job(s)", len(expired))
}
