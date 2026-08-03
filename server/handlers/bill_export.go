package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const billExportTimeout = 2 * time.Minute

type billLogItem struct {
	ID               int64  `json:"id"`
	UserID           int64  `json:"user_id"`
	Username         string `json:"username"`
	Type             int    `json:"type"`
	CreatedAt        int64  `json:"created_at"`
	ModelName        string `json:"model_name"`
	Group            string `json:"group"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	Quota            int64  `json:"quota"`
	IsStream         bool   `json:"is_stream"`
	RequestID        string `json:"request_id"`
	Other            string `json:"other"`
}

// ExportCustomerBillHandler queries the upstream new-api instance's own MySQL
// `logs` table directly (via the site's configured SqlDsn) rather than the
// upstream HTTP API, since the log-export HTTP endpoint has no way to return
// a clean per-customer bill and paging around it for a full month is slow.
func ExportCustomerBillHandler(c *gin.Context) {
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

	username := strings.TrimSpace(c.Query("username"))
	userIDStr := strings.TrimSpace(c.Query("user_id"))
	if username == "" && userIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供用户名或用户ID"})
		return
	}
	var userID int64
	if userIDStr != "" {
		userID, err = strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "user_id 格式错误"})
			return
		}
	}

	startTSStr := strings.TrimSpace(c.Query("start_timestamp"))
	endTSStr := strings.TrimSpace(c.Query("end_timestamp"))
	if startTSStr == "" || endTSStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少时间范围参数"})
		return
	}
	startTS, err := strconv.ParseInt(startTSStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start_timestamp 格式错误"})
		return
	}
	endTS, err := strconv.ParseInt(endTSStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "end_timestamp 格式错误"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), billExportTimeout)
	defer cancel()

	site, err := loadUpstreamSite(ctx, siteID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "上游站点不存在"})
		return
	}
	dsn := strings.TrimSpace(site.SqlDsn)
	if dsn == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该站点未配置 MySQL 连接字符串"})
		return
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "打开数据库连接失败"})
		return
	}
	defer db.Close()

	query := "SELECT id, user_id, username, type, created_at, model_name, `group`, " +
		"prompt_tokens, completion_tokens, quota, is_stream, request_id, other " +
		"FROM logs WHERE type = 2 AND created_at >= ? AND created_at <= ?"
	args := []interface{}{startTS, endTS}
	if username != "" {
		query += " AND username = ?"
		args = append(args, username)
	} else {
		query += " AND user_id = ?"
		args = append(args, userID)
	}
	query += " ORDER BY id"

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("查询账单失败: %v", err)})
		return
	}
	defer rows.Close()

	items := make([]billLogItem, 0)
	for rows.Next() {
		var item billLogItem
		if err := rows.Scan(&item.ID, &item.UserID, &item.Username, &item.Type, &item.CreatedAt,
			&item.ModelName, &item.Group, &item.PromptTokens, &item.CompletionTokens,
			&item.Quota, &item.IsStream, &item.RequestID, &item.Other); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("解析账单数据失败: %v", err)})
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("读取账单数据失败: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"total": len(items),
	})
}
