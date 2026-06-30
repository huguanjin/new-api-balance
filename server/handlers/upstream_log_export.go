package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const exportPageSize = 100

func fetchExportPage(ctx context.Context, baseURL, token, userID string,
	page, pageSize int, startTS, endTS int64, username string) ([]upstreamLogItem, int, error) {

	targetURL := fmt.Sprintf("%s/api/log/?p=%d&page_size=%d&start_timestamp=%d&end_timestamp=%d&type=2",
		baseURL, page, pageSize, startTS, endTS)
	if username != "" {
		targetURL += "&username=" + url.QueryEscape(username)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", normalizeBearerToken(token))
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	if userID != "" {
		req.Header.Set("New-Api-User", userID)
	}

	resp, err := channelAvailabilityHTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("upstream HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read body: %w", err)
	}

	var result upstreamLogResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, 0, fmt.Errorf("json parse: %w", err)
	}

	total := result.Data.TotalCount
	if total == 0 {
		total = result.Data.Total
	}
	return result.Data.Items, total, nil
}

func ExportUpstreamLogsHandler(c *gin.Context) {
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

	username := strings.TrimSpace(c.Query("username"))

	ctx, cancel := context.WithTimeout(context.Background(), logStatsTimeout)
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
	userID := strings.TrimSpace(site.UserID)

	startTime := time.Now()
	log.Printf("[upstream-log-export] start, username=%q, range=[%d, %d]", username, startTS, endTS)

	var allItems []upstreamLogItem
	cursorEndTS := endTS
	totalPages := 0
	seenIDs := make(map[int64]bool)

	for cursorEndTS > startTS {
		for page := 1; ; page++ {
			items, _, err := fetchExportPage(ctx, baseURL, token, userID, page, exportPageSize, startTS, cursorEndTS, username)
			if err != nil {
				log.Printf("[upstream-log-export] fetch error at cursorEnd=%d page=%d: %v", cursorEndTS, page, err)
				c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("拉取数据失败: %v", err)})
				return
			}
			totalPages++

			if len(items) == 0 {
				cursorEndTS = startTS
				break
			}

			for i := range items {
				if !seenIDs[items[i].ID] {
					seenIDs[items[i].ID] = true
					allItems = append(allItems, items[i])
				}
			}

			if len(items) < exportPageSize {
				cursorEndTS = startTS
				break
			}

			if page >= 8 {
				minTS := items[0].CreatedAt
				for _, it := range items[1:] {
					if it.CreatedAt < minTS {
						minTS = it.CreatedAt
					}
				}
				if minTS < cursorEndTS {
					cursorEndTS = minTS
				} else {
					cursorEndTS = minTS - 1
				}
				log.Printf("[upstream-log-export] cursor moved: endTS=%d, collected=%d",
					cursorEndTS, len(allItems))
				break
			}
		}

		if totalPages%10 == 0 {
			log.Printf("[upstream-log-export] progress: %d records, %d pages, cursorEnd=%d",
				len(allItems), totalPages, cursorEndTS)
		}
	}

	elapsed := time.Since(startTime).Milliseconds()
	log.Printf("[upstream-log-export] done: %d records, %d pages in %dms", len(allItems), totalPages, elapsed)

	c.JSON(http.StatusOK, gin.H{
		"items":   allItems,
		"total":   len(allItems),
		"elapsed": elapsed,
	})
}
