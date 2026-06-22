package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"balanceserver/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type logStatResponse struct {
	Data struct {
		Quota int64 `json:"quota"`
		RPM   int   `json:"rpm"`
		TPM   int64 `json:"tpm"`
	} `json:"data"`
	Message string `json:"message"`
	Success bool   `json:"success"`
}

func fetchLogStat(ctx context.Context, baseURL, token, userID string, startTS, endTS int64) (int64, error) {
	return fetchLogStatFiltered(ctx, baseURL, token, userID, startTS, endTS, 2, "", "", "")
}

func fetchLogStatWithModel(ctx context.Context, baseURL, token, userID string, startTS, endTS int64, modelName string) (int64, error) {
	return fetchLogStatFiltered(ctx, baseURL, token, userID, startTS, endTS, 0, modelName, "", "")
}

func fetchLogStatWithChannel(ctx context.Context, baseURL, token, userID string, startTS, endTS int64, channelID string) (int64, error) {
	return fetchLogStatFiltered(ctx, baseURL, token, userID, startTS, endTS, 0, "", channelID, "")
}

func fetchLogStatWithUsername(ctx context.Context, baseURL, token, userID string, startTS, endTS int64, username string) (int64, error) {
	return fetchLogStatFiltered(ctx, baseURL, token, userID, startTS, endTS, 0, "", "", username)
}

func fetchLogStatFiltered(ctx context.Context, baseURL, token, userID string, startTS, endTS int64, logType int, modelName, channelID, username string) (int64, error) {
	targetURL := fmt.Sprintf("%s/api/log/stat?type=%d&username=%s&token_name=&model_name=%s&start_timestamp=%d&end_timestamp=%d&channel=%s&group=",
		baseURL, logType, url.QueryEscape(username), url.QueryEscape(modelName), startTS, endTS, url.QueryEscape(channelID))

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
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
		return 0, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("upstream HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read body: %w", err)
	}

	var result logStatResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("json parse: %w", err)
	}

	if !result.Success {
		return 0, fmt.Errorf("upstream stat failed: %s", result.Message)
	}

	return result.Data.Quota, nil
}

var cstZone = time.FixedZone("CST", 8*3600)

func todayDateCST() string {
	return time.Now().In(cstZone).Format("2006-01-02")
}

func yesterdayDateCST() string {
	return time.Now().In(cstZone).AddDate(0, 0, -1).Format("2006-01-02")
}

func dateToTimestamps(date string) (int64, int64, error) {
	t, err := time.ParseInLocation("2006-01-02", date, cstZone)
	if err != nil {
		return 0, 0, err
	}
	return t.Unix(), t.Add(24 * time.Hour).Unix(), nil
}

func refineRankingViaStat(ctx context.Context, baseURL, token, userID string, startTS, endTS int64, candidates []models.DashboardRankItem, statFn func(context.Context, string, string, string, int64, int64, string) (int64, error), label string, concurrency int) []models.DashboardRankItem {
	type result struct {
		Name  string
		Quota int64
	}

	sem := make(chan struct{}, concurrency)
	resultsCh := make(chan result, len(candidates))
	var wg sync.WaitGroup

	for _, c := range candidates {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			quota, err := statFn(ctx, baseURL, token, userID, startTS, endTS, name)
			if err != nil {
				log.Printf("[dashboard] %s stat(%s) error: %v", label, name, err)
				return
			}
			resultsCh <- result{Name: name, Quota: quota}
		}(c.Name)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	quotaMap := map[string]int64{}
	for r := range resultsCh {
		if r.Quota > 0 {
			quotaMap[r.Name] = r.Quota
		}
	}

	if len(quotaMap) == 0 {
		return nil
	}
	return buildQuotaRanking(quotaMap, 10)
}

func computeSiteStats(ctx context.Context, site models.UpstreamSite, date string, concurrency int) (*models.SiteDailyStats, error) {
	startTS, endTS, err := dateToTimestamps(date)
	if err != nil {
		return nil, fmt.Errorf("日期格式错误: %w", err)
	}

	siteURL := strings.TrimSpace(site.URL)
	if siteURL == "" {
		return nil, fmt.Errorf("站点 URL 未配置")
	}
	parsed, err := url.Parse(siteURL)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("站点 URL 无效")
	}
	baseURL := fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)

	token := strings.TrimSpace(site.Token)
	if token == "" {
		return nil, fmt.Errorf("站点 Token 未配置")
	}
	userID := strings.TrimSpace(site.UserID)

	statQuota, err := fetchLogStat(ctx, baseURL, token, userID, startTS, endTS)
	if err != nil {
		log.Printf("[dashboard] fetchLogStat failed for site=%s date=%s: %v, falling back to pagination", site.Name, date, err)
	}

	const (
		sliceDuration    int64 = 5 * 60
		maxPagesPerSlice       = 10
	)

	type timeSlice struct{ start, end int64 }
	var slices []timeSlice
	for s := startTS; s < endTS; s += sliceDuration {
		e := s + sliceDuration
		if e > endTS {
			e = endTS
		}
		slices = append(slices, timeSlice{s, e})
	}

	itemsCh := make(chan []upstreamLogItem, len(slices))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var totalPages int64

	for _, sl := range slices {
		wg.Add(1)
		go func(s timeSlice) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			slStart := strconv.FormatInt(s.start, 10)
			slEnd := strconv.FormatInt(s.end, 10)

			for p := 1; p <= maxPagesPerSlice; p++ {
				items, err := fetchLogPage(ctx, baseURL, token, userID, p, logStatsPageSize, slStart, slEnd, "", 2)
				if err != nil {
					return
				}
				atomic.AddInt64(&totalPages, 1)
				if len(items) == 0 {
					break
				}
				itemsCh <- items
				if len(items) < logStatsPageSize {
					break
				}
			}
		}(sl)
	}

	go func() {
		wg.Wait()
		close(itemsCh)
	}()

	var paginatedQuota int64
	var totalCount int
	paginatedModelQuota := map[string]int64{}
	paginatedChannelQuota := map[int]int64{}
	channelIDToName := map[int]string{}
	paginatedUserQuota := map[string]int64{}

	for items := range itemsCh {
		for _, item := range items {
			totalCount++
			paginatedQuota += item.Quota

			modelName := item.ModelName
			if modelName == "" {
				modelName = "(unknown)"
			}
			username := item.Username
			if username == "" {
				username = "(anonymous)"
			}

			paginatedModelQuota[modelName] += item.Quota
			paginatedChannelQuota[item.Channel] += item.Quota
			if item.ChannelName != "" {
				channelIDToName[item.Channel] = item.ChannelName
			}
			paginatedUserQuota[username] += item.Quota
		}
	}

	totalQuota := statQuota
	if totalQuota == 0 {
		totalQuota = paginatedQuota
	}

	// 模型排行：先用分页数据筛出前20名候选，再用 /api/log/stat 获取精确值
	roughModelTop := buildQuotaRanking(paginatedModelQuota, 20)
	log.Printf("[dashboard] 模型排行: 分页筛出 %d 个候选模型，用 /api/log/stat 获取精确值", len(roughModelTop))
	finalModelRanking := refineRankingViaStat(ctx, baseURL, token, userID, startTS, endTS, roughModelTop, fetchLogStatWithModel, "模型排行", concurrency)
	if finalModelRanking == nil {
		finalModelRanking = buildQuotaRanking(paginatedModelQuota, 10)
		log.Printf("[dashboard] 模型排行: stat 全部失败，使用分页统计(fallback)")
	} else {
		log.Printf("[dashboard] 模型排行: 使用 /api/log/stat 精确值, 共 %d 个模型", len(finalModelRanking))
		for i, item := range finalModelRanking {
			log.Printf("[dashboard]   #%d %s quota=%d (¥%.2f)", i+1, item.Name, item.Quota, float64(item.Quota)/500000)
		}
	}

	// 渠道排行：先用分页数据筛出前20名渠道，再用 /api/log/stat 获取精确值
	type channelCandidate struct {
		ID    int
		Name  string
		Quota int64
	}
	var channelCandidates []channelCandidate
	for chID, quota := range paginatedChannelQuota {
		name := channelIDToName[chID]
		if name == "" {
			name = fmt.Sprintf("渠道#%d", chID)
		}
		channelCandidates = append(channelCandidates, channelCandidate{ID: chID, Name: name, Quota: quota})
	}
	sort.Slice(channelCandidates, func(i, j int) bool {
		return channelCandidates[i].Quota > channelCandidates[j].Quota
	})
	if len(channelCandidates) > 20 {
		channelCandidates = channelCandidates[:20]
	}
	log.Printf("[dashboard] 渠道排行: 分页筛出 %d 个候选渠道，用 /api/log/stat 获取精确值", len(channelCandidates))

	refinedChannelQuota := map[string]int64{}
	channelStatOK := false
	{
		type chResult struct {
			Name  string
			Quota int64
		}
		chResultsCh := make(chan chResult, len(channelCandidates))
		chSem := make(chan struct{}, concurrency)
		var chWg sync.WaitGroup
		for _, cc := range channelCandidates {
			chWg.Add(1)
			go func(id int, name string) {
				defer chWg.Done()
				chSem <- struct{}{}
				defer func() { <-chSem }()

				chID := strconv.Itoa(id)
				quota, err := fetchLogStatWithChannel(ctx, baseURL, token, userID, startTS, endTS, chID)
				if err != nil {
					log.Printf("[dashboard] 渠道排行 stat(channel=%d/%s) error: %v", id, name, err)
					return
				}
				if quota > 0 {
					chResultsCh <- chResult{Name: name, Quota: quota}
				}
			}(cc.ID, cc.Name)
		}
		go func() {
			chWg.Wait()
			close(chResultsCh)
		}()
		for r := range chResultsCh {
			refinedChannelQuota[r.Name] = r.Quota
			channelStatOK = true
		}
	}

	var finalChannelRanking []models.DashboardRankItem
	if channelStatOK {
		finalChannelRanking = buildQuotaRanking(refinedChannelQuota, 10)
		log.Printf("[dashboard] 渠道排行: 使用 /api/log/stat 精确值, 共 %d 个渠道", len(finalChannelRanking))
	} else {
		fallbackMap := map[string]int64{}
		for chID, quota := range paginatedChannelQuota {
			name := channelIDToName[chID]
			if name == "" {
				name = fmt.Sprintf("渠道#%d", chID)
			}
			fallbackMap[name] = quota
		}
		finalChannelRanking = buildQuotaRanking(fallbackMap, 10)
		log.Printf("[dashboard] 渠道排行: stat 全部失败，使用分页统计(fallback)")
	}

	// 用户排行：先用分页数据筛出前20名候选，再用 /api/log/stat 获取精确值
	roughUserTop := buildQuotaRanking(paginatedUserQuota, 20)
	log.Printf("[dashboard] 用户排行: 分页筛出 %d 个候选用户，用 /api/log/stat 获取精确值", len(roughUserTop))
	finalUserRanking := refineRankingViaStat(ctx, baseURL, token, userID, startTS, endTS, roughUserTop, fetchLogStatWithUsername, "用户排行", concurrency)
	if finalUserRanking == nil {
		finalUserRanking = buildQuotaRanking(paginatedUserQuota, 10)
		log.Printf("[dashboard] 用户排行: stat 全部失败，使用分页统计(fallback)")
	} else {
		log.Printf("[dashboard] 用户排行: 使用 /api/log/stat 精确值, 共 %d 个用户", len(finalUserRanking))
	}

	// 错误模型排行：分页拉取 type=5 日志，统计每个模型的错误次数
	log.Printf("[dashboard] 错误模型排行: 开始拉取 type=5 日志")
	errorItemsCh := make(chan []upstreamLogItem, len(slices))
	errorSem := make(chan struct{}, concurrency)
	var errorWg sync.WaitGroup

	for _, sl := range slices {
		errorWg.Add(1)
		go func(s timeSlice) {
			defer errorWg.Done()
			errorSem <- struct{}{}
			defer func() { <-errorSem }()

			slStart := strconv.FormatInt(s.start, 10)
			slEnd := strconv.FormatInt(s.end, 10)

			for p := 1; p <= maxPagesPerSlice; p++ {
				items, err := fetchLogPage(ctx, baseURL, token, userID, p, logStatsPageSize, slStart, slEnd, "", 5)
				if err != nil {
					return
				}
				if len(items) == 0 {
					break
				}
				errorItemsCh <- items
				if len(items) < logStatsPageSize {
					break
				}
			}
		}(sl)
	}

	go func() {
		errorWg.Wait()
		close(errorItemsCh)
	}()

	errorModelCount := map[string]int{}
	var totalErrorCount int
	for items := range errorItemsCh {
		for _, item := range items {
			totalErrorCount++
			mn := item.ModelName
			if mn == "" {
				mn = "(unknown)"
			}
			errorModelCount[mn]++
		}
	}
	finalErrorModelRanking := buildCountRanking(errorModelCount, 10)
	log.Printf("[dashboard] 错误模型排行: 共 %d 条错误日志, %d 个模型, 取前 %d", totalErrorCount, len(errorModelCount), len(finalErrorModelRanking))

	return &models.SiteDailyStats{
		UpstreamSiteID:    site.ID,
		SiteName:          site.Name,
		Date:              date,
		TotalQuota:        totalQuota,
		SuccessCount:      totalCount,
		ErrorCount:        totalErrorCount,
		TotalCount:        totalCount + totalErrorCount,
		ModelRanking:      finalModelRanking,
		ChannelRanking:    finalChannelRanking,
		UserRanking:       finalUserRanking,
		ErrorModelRanking: finalErrorModelRanking,
		ComputedAt:        time.Now(),
	}, nil
}

func buildQuotaRanking(m map[string]int64, limit int) []models.DashboardRankItem {
	items := make([]models.DashboardRankItem, 0, len(m))
	for name, quota := range m {
		items = append(items, models.DashboardRankItem{Name: name, Quota: quota})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Quota > items[j].Quota
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func buildCountRanking(m map[string]int, limit int) []models.DashboardRankItem {
	items := make([]models.DashboardRankItem, 0, len(m))
	for name, count := range m {
		items = append(items, models.DashboardRankItem{Name: name, Count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Count > items[j].Count
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func upsertSiteDailyStats(ctx context.Context, stats *models.SiteDailyStats) error {
	filter := bson.M{
		"upstream_site_id": stats.UpstreamSiteID,
		"date":             stats.Date,
	}
	update := bson.M{"$set": stats}
	opts := options.Update().SetUpsert(true)
	_, err := SiteDailyStatsCol.UpdateOne(ctx, filter, update, opts)
	return err
}

func loadSiteDailyStats(ctx context.Context, date string) ([]models.SiteDailyStats, error) {
	cursor, err := SiteDailyStatsCol.Find(ctx, bson.M{"date": date})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var stats []models.SiteDailyStats
	if err := cursor.All(ctx, &stats); err != nil {
		return nil, err
	}
	return stats, nil
}

func GetDashboardConfigHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var config models.DashboardConfig
	err := DashboardConfigCol.FindOne(ctx, bson.M{"_id": "dashboard"}).Decode(&config)
	if err != nil {
		c.JSON(http.StatusOK, models.DashboardConfig{StartDate: todayDateCST(), Concurrency: 5})
		return
	}
	if config.Concurrency <= 0 {
		config.Concurrency = 5
	}
	c.JSON(http.StatusOK, config)
}

func SaveDashboardConfigHandler(c *gin.Context) {
	var req struct {
		StartDate   string `json:"startDate"`
		Concurrency int    `json:"concurrency"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if strings.TrimSpace(req.StartDate) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "开始日期不能为空"})
		return
	}
	if _, _, err := dateToTimestamps(req.StartDate); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "日期格式错误，请使用 YYYY-MM-DD"})
		return
	}
	if req.Concurrency <= 0 {
		req.Concurrency = 5
	}
	if req.Concurrency > 50 {
		req.Concurrency = 50
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := models.DashboardConfig{
		ID:          "dashboard",
		StartDate:   req.StartDate,
		Concurrency: req.Concurrency,
		UpdatedAt:   time.Now(),
	}
	opts := options.Update().SetUpsert(true)
	_, err := DashboardConfigCol.UpdateOne(ctx, bson.M{"_id": "dashboard"}, bson.M{"$set": config}, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置失败"})
		return
	}
	c.JSON(http.StatusOK, config)
}

func GetDashboardStatsHandler(c *gin.Context) {
	date := strings.TrimSpace(c.Query("date"))
	if date == "" {
		date = todayDateCST()
	}

	yesterday := ""
	t, err := time.ParseInLocation("2006-01-02", date, cstZone)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "日期格式错误"})
		return
	}
	yesterday = t.AddDate(0, 0, -1).Format("2006-01-02")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	todayStats, err := loadSiteDailyStats(ctx, date)
	if err != nil {
		todayStats = []models.SiteDailyStats{}
	}
	yesterdayStats, err := loadSiteDailyStats(ctx, yesterday)
	if err != nil {
		yesterdayStats = []models.SiteDailyStats{}
	}

	if todayStats == nil {
		todayStats = []models.SiteDailyStats{}
	}
	if yesterdayStats == nil {
		yesterdayStats = []models.SiteDailyStats{}
	}

	c.JSON(http.StatusOK, gin.H{
		"date":           date,
		"yesterday":      yesterday,
		"todayStats":     todayStats,
		"yesterdayStats":  yesterdayStats,
	})
}

func ComputeDashboardStatsHandler(c *gin.Context) {
	var req struct {
		Date      string `json:"date"`
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
		SiteID    string `json:"siteId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}

	var dates []string
	if req.Date != "" {
		if _, _, err := dateToTimestamps(req.Date); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "日期格式错误"})
			return
		}
		dates = append(dates, req.Date)
	} else if req.StartDate != "" {
		startT, err := time.ParseInLocation("2006-01-02", req.StartDate, cstZone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "开始日期格式错误"})
			return
		}
		endDate := req.EndDate
		if endDate == "" {
			endDate = todayDateCST()
		}
		endT, err := time.ParseInLocation("2006-01-02", endDate, cstZone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "结束日期格式错误"})
			return
		}
		if endT.Before(startT) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "结束日期不能早于开始日期"})
			return
		}
		for d := startT; !d.After(endT); d = d.AddDate(0, 0, 1) {
			dates = append(dates, d.Format("2006-01-02"))
		}
		if len(dates) > 90 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "日期范围不能超过90天"})
			return
		}
	} else {
		dates = append(dates, todayDateCST())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// 加载并发配置
	concurrency := 5
	var dashConfig models.DashboardConfig
	if err := DashboardConfigCol.FindOne(ctx, bson.M{"_id": "dashboard"}).Decode(&dashConfig); err == nil && dashConfig.Concurrency > 0 {
		concurrency = dashConfig.Concurrency
	}
	log.Printf("[dashboard] using concurrency=%d", concurrency)

	var sites []models.UpstreamSite
	if req.SiteID != "" {
		siteOID, err := primitive.ObjectIDFromHex(req.SiteID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的站点 ID"})
			return
		}
		site, err := loadUpstreamSite(ctx, siteOID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "站点不存在"})
			return
		}
		sites = append(sites, site)
	} else {
		siteCursor, err := UpstreamSiteCol.Find(ctx, bson.M{})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询站点失败"})
			return
		}
		if err := siteCursor.All(ctx, &sites); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "解析站点数据失败"})
			return
		}
		siteCursor.Close(ctx)
	}

	if len(sites) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "没有配置上游站点", "computed": 0})
		return
	}

	var allStats []models.SiteDailyStats
	var computeErrors []string
	totalComputed := 0

	for _, date := range dates {
		for _, site := range sites {
			log.Printf("[dashboard] computing stats for site=%s date=%s", site.Name, date)
			stats, err := computeSiteStats(ctx, site, date, concurrency)
			if err != nil {
				errMsg := fmt.Sprintf("站点 %s 日期 %s: %v", site.Name, date, err)
				computeErrors = append(computeErrors, errMsg)
				log.Printf("[dashboard] error: %s", errMsg)
				continue
			}

			if err := upsertSiteDailyStats(ctx, stats); err != nil {
				errMsg := fmt.Sprintf("站点 %s 日期 %s 存储失败: %v", site.Name, date, err)
				computeErrors = append(computeErrors, errMsg)
				log.Printf("[dashboard] error: %s", errMsg)
				continue
			}

			allStats = append(allStats, *stats)
			totalComputed++
			log.Printf("[dashboard] done: site=%s date=%s quota=%d records=%d",
				site.Name, date, stats.TotalQuota, stats.TotalCount)
		}
	}

	resp := gin.H{
		"computed": totalComputed,
		"stats":    allStats,
	}
	if len(computeErrors) > 0 {
		resp["errors"] = computeErrors
	}
	c.JSON(http.StatusOK, resp)
}
