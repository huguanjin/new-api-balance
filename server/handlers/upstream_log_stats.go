package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	logStatsPageSize    = 100
	logStatsConcurrency = 5
	logStatsTimeout     = 5 * time.Minute
)

var logStatsHTTPClient = &http.Client{Timeout: 2 * time.Minute}

type upstreamLogResponse struct {
	Data struct {
		Items      []upstreamLogItem `json:"items"`
		TotalCount int               `json:"total_count"`
		Total      int               `json:"total"`
	} `json:"data"`
	Success bool `json:"success"`
}

type upstreamLogItem struct {
	ID          int64  `json:"id"`
	Type        int    `json:"type"`
	CreatedAt   int64  `json:"created_at"`
	Username    string `json:"username"`
	TokenName   string `json:"token_name"`
	ModelName   string `json:"model_name"`
	Quota       int64  `json:"quota"`
	Channel     int    `json:"channel"`
	ChannelName string `json:"channel_name"`
	Group       string `json:"group"`
	Content     string `json:"content"`
	UseTime     int    `json:"use_time"`
}

type logGroupStats struct {
	Group        string           `json:"group"`
	SuccessCount int              `json:"successCount"`
	ErrorCount   int              `json:"errorCount"`
	TotalCount   int              `json:"totalCount"`
	SuccessRate  float64          `json:"successRate"`
	TotalQuota   int64            `json:"totalQuota"`
	AvgUseTime   float64          `json:"avgUseTime"`
	Models       []logModelStats  `json:"models"`
	modelMap     map[string]*logModelStats
	totalUseTime int64
}

type logModelStats struct {
	ModelName    string  `json:"modelName"`
	SuccessCount int     `json:"successCount"`
	ErrorCount   int     `json:"errorCount"`
	TotalCount   int     `json:"totalCount"`
	SuccessRate  float64 `json:"successRate"`
	AvgUseTime   float64 `json:"avgUseTime"`
	totalUseTime int64
}

type errorCodeStats struct {
	StatusCode string `json:"statusCode"`
	Count      int    `json:"count"`
	Percent    float64 `json:"percent"`
}

var statusCodeRe = regexp.MustCompile(`status_code=(\d{3})`)

type logStatsResponse struct {
	Groups       []logGroupStats  `json:"groups"`
	ErrorCodes   []errorCodeStats `json:"errorCodes"`
	TotalRecords int              `json:"totalRecords"`
	SuccessTotal int              `json:"successTotal"`
	ErrorTotal   int              `json:"errorTotal"`
	OverallRate  float64          `json:"overallRate"`
	GroupCount   int              `json:"groupCount"`
	FetchedPages int              `json:"fetchedPages"`
	ElapsedMs    int64            `json:"elapsedMs"`
	Warnings     []string         `json:"warnings,omitempty"`
}

func fetchUpstreamLogPage(ctx context.Context, baseURL, token, userID string,
	page, pageSize, logType int, startTS, endTS, group string) ([]upstreamLogItem, int, error) {

	targetURL := fmt.Sprintf("%s/api/log/?p=%d&page_size=%d&type=%d&start_timestamp=%s&end_timestamp=%s",
		baseURL, page, pageSize, logType, startTS, endTS)
	if group != "" {
		targetURL += "&group=" + url.QueryEscape(group)
	}

	const maxRetries = 3
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			delay := time.Duration(attempt) * 2 * time.Second
			log.Printf("[upstream-log-stats] type=%d page %d: retry %d/%d after %v", logType, page, attempt, maxRetries, delay)
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(delay):
			}
		}

		reqCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, targetURL, nil)
		if err != nil {
			cancel()
			return nil, 0, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", normalizeBearerToken(token))
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36")
		req.Header.Set("Accept", "application/json, text/plain, */*")
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
		if userID != "" {
			req.Header.Set("New-Api-User", userID)
		}

		resp, err := logStatsHTTPClient.Do(req)
		if err != nil {
			cancel()
			lastErr = fmt.Errorf("http request: %w", err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()

		if err != nil {
			lastErr = fmt.Errorf("read body: %w", err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("upstream HTTP %d", resp.StatusCode)
			continue
		}

		var result upstreamLogResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, 0, fmt.Errorf("json parse: %w", err)
		}

		totalCount := result.Data.TotalCount
		if totalCount == 0 {
			totalCount = result.Data.Total
		}

		return result.Data.Items, totalCount, nil
	}

	return nil, 0, lastErr
}

func QueryUpstreamLogStatsHandler(c *gin.Context) {
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

	startTS := strings.TrimSpace(c.Query("start_timestamp"))
	endTS := strings.TrimSpace(c.Query("end_timestamp"))
	if startTS == "" || endTS == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少时间范围参数"})
		return
	}

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
	groupFilter := strings.TrimSpace(c.Query("group"))

	startTime := time.Now()
	itemsCh := make(chan []upstreamLogItem, 100)
	var wg sync.WaitGroup
	var fetchErrors []string
	var mu sync.Mutex
	var totalFetchedPages int64
	sem := make(chan struct{}, logStatsConcurrency)

	for _, logType := range []int{2, 5} {
		wg.Add(1)
		go func(lt int) {
			defer wg.Done()

			sem <- struct{}{}
			items, totalCount, err := fetchUpstreamLogPage(ctx, baseURL, token, userID, 1, logStatsPageSize, lt, startTS, endTS, groupFilter)
			<-sem
			if err != nil {
				mu.Lock()
				fetchErrors = append(fetchErrors, fmt.Sprintf("type=%d page 1: %v", lt, err))
				mu.Unlock()
				log.Printf("[upstream-log-stats] type=%d page 1 error: %v", lt, err)
				return
			}
			atomic.AddInt64(&totalFetchedPages, 1)
			log.Printf("[upstream-log-stats] type=%d page 1: got %d items, totalCount=%d", lt, len(items), totalCount)
			if len(items) > 0 {
				itemsCh <- items
			}

			effectivePageSize := len(items)
			if effectivePageSize == 0 || totalCount <= effectivePageSize {
				return
			}

			totalPages := (totalCount + effectivePageSize - 1) / effectivePageSize
			log.Printf("[upstream-log-stats] type=%d total_count=%d effectivePageSize=%d pages=%d", lt, totalCount, effectivePageSize, totalPages)

			var innerWg sync.WaitGroup
			for p := 2; p <= totalPages; p++ {
				innerWg.Add(1)
				go func(page int) {
					defer innerWg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()

					pageItems, _, err := fetchUpstreamLogPage(ctx, baseURL, token, userID, page, logStatsPageSize, lt, startTS, endTS, groupFilter)
					if err != nil {
						mu.Lock()
						fetchErrors = append(fetchErrors, fmt.Sprintf("type=%d page %d: %v", lt, page, err))
						mu.Unlock()
						return
					}
					atomic.AddInt64(&totalFetchedPages, 1)
					if len(pageItems) > 0 {
						itemsCh <- pageItems
					}
				}(p)
			}
			innerWg.Wait()
		}(logType)
	}

	go func() {
		wg.Wait()
		close(itemsCh)
	}()

	groupMap := make(map[string]*logGroupStats)
	errorCodeMap := make(map[string]int)
	for items := range itemsCh {
		for _, item := range items {
			g := item.Group
			if g == "" {
				g = "(empty)"
			}
			gs, ok := groupMap[g]
			if !ok {
				gs = &logGroupStats{Group: g, modelMap: make(map[string]*logModelStats)}
				groupMap[g] = gs
			}
			gs.TotalCount++
			gs.TotalQuota += item.Quota
			gs.totalUseTime += int64(item.UseTime)
			if item.Type == 2 {
				gs.SuccessCount++
			} else {
				gs.ErrorCount++
				if m := statusCodeRe.FindStringSubmatch(item.Content); len(m) > 1 {
					errorCodeMap[m[1]]++
				} else {
					errorCodeMap["unknown"]++
				}
			}

			mn := item.ModelName
			if mn == "" {
				mn = "(unknown)"
			}
			ms, ok := gs.modelMap[mn]
			if !ok {
				ms = &logModelStats{ModelName: mn}
				gs.modelMap[mn] = ms
			}
			ms.TotalCount++
			ms.totalUseTime += int64(item.UseTime)
			if item.Type == 2 {
				ms.SuccessCount++
			} else {
				ms.ErrorCount++
			}
		}
	}

	groups := make([]logGroupStats, 0, len(groupMap))
	totalRecords, successTotal, errorTotal := 0, 0, 0
	for _, gs := range groupMap {
		if gs.TotalCount > 0 {
			gs.SuccessRate = float64(gs.SuccessCount) / float64(gs.TotalCount) * 100
			gs.AvgUseTime = float64(gs.totalUseTime) / float64(gs.TotalCount)
		}
		gs.Models = make([]logModelStats, 0, len(gs.modelMap))
		for _, ms := range gs.modelMap {
			if ms.TotalCount > 0 {
				ms.SuccessRate = float64(ms.SuccessCount) / float64(ms.TotalCount) * 100
				ms.AvgUseTime = float64(ms.totalUseTime) / float64(ms.TotalCount)
			}
			gs.Models = append(gs.Models, *ms)
		}
		sort.Slice(gs.Models, func(i, j int) bool {
			return gs.Models[i].SuccessRate < gs.Models[j].SuccessRate
		})
		gs.modelMap = nil
		groups = append(groups, *gs)
		totalRecords += gs.TotalCount
		successTotal += gs.SuccessCount
		errorTotal += gs.ErrorCount
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].SuccessRate < groups[j].SuccessRate
	})

	overallRate := 0.0
	if totalRecords > 0 {
		overallRate = float64(successTotal) / float64(totalRecords) * 100
	}

	errorCodes := make([]errorCodeStats, 0, len(errorCodeMap))
	for code, count := range errorCodeMap {
		pct := 0.0
		if errorTotal > 0 {
			pct = float64(count) / float64(errorTotal) * 100
		}
		errorCodes = append(errorCodes, errorCodeStats{StatusCode: code, Count: count, Percent: pct})
	}
	sort.Slice(errorCodes, func(i, j int) bool {
		return errorCodes[i].Count > errorCodes[j].Count
	})

	var warnings []string
	if len(fetchErrors) > 0 {
		limit := len(fetchErrors)
		if limit > 10 {
			limit = 10
		}
		warnings = fetchErrors[:limit]
		log.Printf("[upstream-log-stats] %d fetch errors, first: %s", len(fetchErrors), fetchErrors[0])
	}

	elapsed := time.Since(startTime).Milliseconds()
	log.Printf("[upstream-log-stats] done: %d records, %d groups, %d pages fetched in %dms",
		totalRecords, len(groups), totalFetchedPages, elapsed)

	c.JSON(http.StatusOK, logStatsResponse{
		Groups:       groups,
		ErrorCodes:   errorCodes,
		TotalRecords: totalRecords,
		SuccessTotal: successTotal,
		ErrorTotal:   errorTotal,
		OverallRate:  overallRate,
		GroupCount:   len(groups),
		FetchedPages: int(totalFetchedPages),
		ElapsedMs:    elapsed,
		Warnings:     warnings,
	})
}

func QueryUpstreamGroupsHandler(c *gin.Context) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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

	targetURL := baseURL + "/api/group/"
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

	resp, err := logStatsHTTPClient.Do(req)
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
			"error":         fmt.Sprintf("上游站点返回错误 (HTTP %d)", resp.StatusCode),
			"upstream_body": string(body),
		})
		return
	}

	c.Data(http.StatusOK, "application/json", body)
}
