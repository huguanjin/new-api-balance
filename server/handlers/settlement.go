package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

const settlementQueryTimeout = 3 * time.Minute

// beijingLayout is the wire format the frontend sends for the picked range.
// The strings are wall-clock times the operator saw in the UI, which are
// Beijing times; parsing them in cstZone is what turns "2026-09-10 03:00:00"
// into 1788980400 rather than the same string read as UTC.
const beijingLayout = "2006-01-02 15:04:05"

func parseBeijingTime(raw string, label string) (int64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, fmt.Errorf("%s不能为空", label)
	}
	if ts, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return ts, nil
	}
	t, err := time.ParseInLocation(beijingLayout, trimmed, cstZone)
	if err != nil {
		return 0, fmt.Errorf("%s格式错误，应为 YYYY-MM-DD HH:mm:ss", label)
	}
	return t.Unix(), nil
}

// SettlementQueryHandler answers "how much did this user consume between
// these two Beijing times" by connecting straight to the upstream site's
// MySQL - the same DSN the upstream site config already carries - and
// summing the logs table.
//
// Unlike the bill export this runs synchronously: SUM(quota) is a single
// aggregate row, so there is no result set to stream and nothing for the
// frontend to poll. The dial/read timeouts come from openMySQLForExport so
// an unreachable DSN still fails with a clear error instead of hanging.
func SettlementQueryHandler(c *gin.Context) {
	var req struct {
		UpstreamSiteID string `json:"upstreamSiteId"`
		Identity       string `json:"identity"`
		StartTime      string `json:"startTime"`
		EndTime        string `json:"endTime"`
		LogType        *int   `json:"logType"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}

	siteID, err := primitive.ObjectIDFromHex(strings.TrimSpace(req.UpstreamSiteID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "上游站点无效"})
		return
	}

	identity := strings.TrimSpace(req.Identity)
	if identity == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入用户名或用户 ID"})
		return
	}

	startTs, err := parseBeijingTime(req.StartTime, "开始时间")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	endTs, err := parseBeijingTime(req.EndTime, "结束时间")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if endTs < startTs {
		c.JSON(http.StatusBadRequest, gin.H{"error": "结束时间不能早于开始时间"})
		return
	}

	logType := 2
	if req.LogType != nil {
		logType = *req.LogType
	}

	ctx := c.Request.Context()

	site, err := loadUpstreamSite(ctx, siteID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{"error": "上游站点不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "加载上游站点失败"})
		return
	}
	dsn := strings.TrimSpace(site.SqlDsn)
	if dsn == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该上游站点未配置 MySQL DSN"})
		return
	}

	query := "SELECT COALESCE(SUM(quota), 0), COUNT(*) FROM logs " +
		"WHERE type = ? AND created_at BETWEEN ? AND ? AND (username = ? OR user_id = ?)"

	db, err := openMySQLForExport(dsn)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer db.Close()

	queryCtx, cancel := context.WithTimeout(ctx, settlementQueryTimeout)
	defer cancel()

	var quotaSum int64
	var rowCount int64
	if err := db.QueryRowContext(queryCtx, query, logType, startTs, endTs, identity, identity).
		Scan(&quotaSum, &rowCount); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("查询失败: %v", err),
		})
		return
	}

	amount := float64(quotaSum) / 500000.0

	c.JSON(http.StatusOK, gin.H{
		"siteId":   site.ID.Hex(),
		"siteName": site.Name,
		"identity": identity,
		"logType":  logType,
		"startTs":  startTs,
		"endTs":    endTs,
		"startAt":  time.Unix(startTs, 0).In(cstZone).Format(beijingLayout),
		"endAt":    time.Unix(endTs, 0).In(cstZone).Format(beijingLayout),
		"quotaSum": quotaSum,
		"rowCount": rowCount,
		"amount":   amount,
		"sql": fmt.Sprintf(
			"SELECT SUM(quota)/500000\nFROM logs\nWHERE type = %d\n"+
				"  AND (username = '%s' OR user_id = '%s')\n"+
				"  AND created_at BETWEEN %d AND %d",
			logType, identity, identity, startTs, endTs),
	})
}
