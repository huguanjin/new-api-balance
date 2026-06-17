package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func QueryUpstreamLogsHandler(c *gin.Context) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	site, err := loadUpstreamSite(ctx, siteID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "上游站点不存在"})
		return
	}

	siteURL := strings.TrimSpace(site.URL)
	if siteURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "站点 URL 未配置"})
		return
	}
	parsed, err := url.Parse(siteURL)
	if err != nil || parsed.Host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "站点 URL 无效"})
		return
	}
	baseURL := fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)

	token := strings.TrimSpace(site.Token)
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "站点 Token 未配置"})
		return
	}

	params := []string{}
	for _, key := range []string{
		"p", "page_size",
		"start_timestamp", "end_timestamp",
		"model_name", "username", "token_name",
		"channel", "content", "keyword", "type",
	} {
		if v := c.Query(key); v != "" {
			params = append(params, fmt.Sprintf("%s=%s", key, v))
		}
	}

	targetURL := baseURL + "/api/log/"
	if len(params) > 0 {
		targetURL += "?" + strings.Join(params, "&")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "构建请求失败"})
		return
	}
	req.Header.Set("Authorization", normalizeBearerToken(token))
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	if userID := strings.TrimSpace(site.UserID); userID != "" {
		req.Header.Set("New-Api-User", userID)
	}

	resp, err := channelAvailabilityHTTPClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("请求上游站点失败: %v", err)})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "读取上游响应失败"})
		return
	}

	if resp.StatusCode != http.StatusOK {
		c.JSON(resp.StatusCode, gin.H{
			"error":          fmt.Sprintf("上游站点返回错误 (HTTP %d)", resp.StatusCode),
			"upstream_body":  string(body),
		})
		return
	}

	c.Data(http.StatusOK, "application/json", body)
}
