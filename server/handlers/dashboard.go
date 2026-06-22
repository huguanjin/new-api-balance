package handlers

import (
	"context"
	"fmt"
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

func computeSiteStats(ctx context.Context, site models.UpstreamSite, date string) (*models.SiteDailyStats, error) {
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

	const (
		sliceDuration    int64 = 5 * 60
		maxConcurrency         = 20
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
	sem := make(chan struct{}, maxConcurrency)
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
				items, err := fetchLogPage(ctx, baseURL, token, userID, p, logStatsPageSize, slStart, slEnd, "")
				if err != nil {
					return
				}
				atomic.AddInt64(&totalPages, 1)
				if len(items) == 0 {
					break
				}
				var filtered []upstreamLogItem
				for i := range items {
					if items[i].Type == 2 || items[i].Type == 5 {
						filtered = append(filtered, items[i])
					}
				}
				if len(filtered) > 0 {
					itemsCh <- filtered
				}
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

	var totalQuota int64
	var successCount, errorCount, totalCount int
	modelQuota := map[string]int64{}
	channelQuota := map[string]int64{}
	userQuota := map[string]int64{}
	errorModelCount := map[string]int{}

	for items := range itemsCh {
		for _, item := range items {
			totalCount++
			totalQuota += item.Quota

			modelName := item.ModelName
			if modelName == "" {
				modelName = "(unknown)"
			}
			channelName := item.ChannelName
			if channelName == "" {
				channelName = fmt.Sprintf("渠道#%d", item.Channel)
			}
			username := item.Username
			if username == "" {
				username = "(anonymous)"
			}

			modelQuota[modelName] += item.Quota
			channelQuota[channelName] += item.Quota
			userQuota[username] += item.Quota

			if item.Type == 2 {
				successCount++
			} else {
				errorCount++
				errorModelCount[modelName]++
			}
		}
	}

	return &models.SiteDailyStats{
		UpstreamSiteID:    site.ID,
		SiteName:          site.Name,
		Date:              date,
		TotalQuota:        totalQuota,
		SuccessCount:      successCount,
		ErrorCount:        errorCount,
		TotalCount:        totalCount,
		ModelRanking:      buildQuotaRanking(modelQuota, 10),
		ChannelRanking:    buildQuotaRanking(channelQuota, 10),
		UserRanking:       buildQuotaRanking(userQuota, 10),
		ErrorModelRanking: buildCountRanking(errorModelCount, 10),
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
		c.JSON(http.StatusOK, models.DashboardConfig{StartDate: todayDateCST()})
		return
	}
	c.JSON(http.StatusOK, config)
}

func SaveDashboardConfigHandler(c *gin.Context) {
	var req struct {
		StartDate string `json:"startDate"`
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := models.DashboardConfig{
		ID:        "dashboard",
		StartDate: req.StartDate,
		UpdatedAt: time.Now(),
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
			stats, err := computeSiteStats(ctx, site, date)
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
