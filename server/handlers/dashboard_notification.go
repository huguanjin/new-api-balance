package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"balanceserver/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const dashboardNotificationConfigID = "dashboard_notification"

func loadDashboardNotificationConfig(ctx context.Context) (models.DashboardNotificationConfig, error) {
	var config models.DashboardNotificationConfig
	err := DashboardNotificationConfigCol.FindOne(ctx, bson.M{"_id": dashboardNotificationConfigID}).Decode(&config)
	if err == mongo.ErrNoDocuments {
		return models.DashboardNotificationConfig{
			NotificationType: "feishu",
			PushTime:         "08:00",
			TopN:             10,
		}, nil
	}
	return config, err
}

func GetDashboardNotificationConfigHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config, err := loadDashboardNotificationConfig(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "加载配置失败"})
		return
	}
	c.JSON(http.StatusOK, config)
}

func SaveDashboardNotificationConfigHandler(c *gin.Context) {
	var req models.DashboardNotificationConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}

	req.NotificationType = strings.TrimSpace(req.NotificationType)
	if req.NotificationType == "" {
		req.NotificationType = "feishu"
	}
	if req.NotificationType != "feishu" && req.NotificationType != "wework" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "通知方式必须为 feishu 或 wework"})
		return
	}

	if req.Enabled {
		if req.NotificationType == "feishu" {
			if err := validateWebhookURL(req.WebhookURL, "飞书 Webhook URL"); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		} else {
			if err := validateWebhookURL(req.WeworkWebhookURL, "企业微信 Webhook URL"); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		}
	}

	pushTime := strings.TrimSpace(req.PushTime)
	if pushTime == "" {
		pushTime = "08:00"
	}
	if _, ok := parseClockMinutes(pushTime); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "推送时间格式错误，请使用 HH:mm"})
		return
	}
	req.PushTime = pushTime

	if req.TopN <= 0 {
		req.TopN = 10
	}
	if req.TopN > 20 {
		req.TopN = 20
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	existing, _ := loadDashboardNotificationConfig(ctx)
	req.ID = dashboardNotificationConfigID
	req.LastAttemptAt = existing.LastAttemptAt
	req.LastSentAt = existing.LastSentAt
	req.LastSentDate = existing.LastSentDate
	req.LastError = existing.LastError
	req.UpdatedAt = time.Now()

	opts := options.Update().SetUpsert(true)
	_, err := DashboardNotificationConfigCol.UpdateOne(ctx, bson.M{"_id": dashboardNotificationConfigID}, bson.M{"$set": req}, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置失败"})
		return
	}
	c.JSON(http.StatusOK, req)
}

func TestDashboardNotificationHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	config, err := loadDashboardNotificationConfig(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "加载配置失败"})
		return
	}

	now := formatNotificationTime(notificationNow())
	if config.NotificationType == "wework" {
		message := fmt.Sprintf("#### 战绩推送测试\n> 发送时间：%s\n> 状态：<font color=\"info\">Webhook 配置正常</font>", now)
		if err := sendWeworkMarkdownNotification(ctx, config.WeworkWebhookURL, message); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		card := map[string]interface{}{
			"config": map[string]interface{}{"wide_screen_mode": true},
			"header": map[string]interface{}{
				"template": "green",
				"title":    map[string]string{"tag": "plain_text", "content": "战绩推送测试"},
			},
			"elements": []interface{}{
				map[string]interface{}{
					"tag": "div",
					"fields": []interface{}{
						feishuField("发送时间", now),
						feishuField("状态", "<font color=\"green\">Webhook 配置正常</font>"),
					},
				},
			},
		}
		if err := sendFeishuCardNotification(ctx, config.WebhookURL, card, config.SignKey); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "测试通知发送成功"})
}

func SendDashboardNotificationNowHandler(c *gin.Context) {
	var req struct {
		Date string `json:"date"`
	}
	_ = c.ShouldBindJSON(&req)

	date := strings.TrimSpace(req.Date)
	if date == "" {
		date = yesterdayDateCST()
	}
	if _, _, err := dateToTimestamps(date); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "日期格式错误，请使用 YYYY-MM-DD"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	config, err := loadDashboardNotificationConfig(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "加载配置失败"})
		return
	}

	stats, err := loadSiteDailyStats(ctx, date)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "加载统计数据失败"})
		return
	}

	if len(stats) == 0 && config.AutoCompute {
		log.Printf("[dashboard-notify] no stats for %s, auto computing...", date)
		if computeErr := autoComputeDashboardStats(ctx, date); computeErr != nil {
			log.Printf("[dashboard-notify] auto compute error: %v", computeErr)
		}
		stats, _ = loadSiteDailyStats(ctx, date)
	}

	if len(stats) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("日期 %s 暂无统计数据", date)})
		return
	}

	topN := config.TopN
	if topN <= 0 {
		topN = 10
	}

	sendErr := sendDashboardStatsNotification(ctx, config, stats, date, topN)
	recordDashboardNotificationAttempt(sendErr, date)

	if sendErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": sendErr.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("日期 %s 的战绩已推送", date)})
}

func sendDashboardStatsNotification(ctx context.Context, config models.DashboardNotificationConfig, stats []models.SiteDailyStats, date string, topN int) error {
	if config.NotificationType == "wework" {
		message := buildWeworkDashboardStatsMarkdown(stats, date, topN)
		return sendWeworkMarkdownNotification(ctx, config.WeworkWebhookURL, message)
	}
	card := buildFeishuDashboardStatsCard(stats, date, topN)
	return sendFeishuCardNotification(ctx, config.WebhookURL, card, config.SignKey)
}

func formatQuotaRMB(quota int64) string {
	val := float64(quota) / 500000.0
	if val >= 1 {
		return fmt.Sprintf("¥%.2f", val)
	}
	if val > 0 {
		return fmt.Sprintf("¥%.4f", val)
	}
	return "¥0.00"
}

type mergedRankings struct {
	ModelRanking      []models.DashboardRankItem
	ChannelRanking    []models.DashboardRankItem
	UserRanking       []models.DashboardRankItem
	ErrorModelRanking []models.DashboardRankItem
}

func mergeDashboardRankings(stats []models.SiteDailyStats, topN int) mergedRankings {
	modelMap := map[string]int64{}
	channelMap := map[string]int64{}
	userMap := map[string]int64{}
	errorModelMap := map[string]int{}

	for _, s := range stats {
		for _, item := range s.ModelRanking {
			modelMap[item.Name] += item.Quota
		}
		for _, item := range s.ChannelRanking {
			channelMap[item.Name] += item.Quota
		}
		for _, item := range s.UserRanking {
			userMap[item.Name] += item.Quota
		}
		for _, item := range s.ErrorModelRanking {
			errorModelMap[item.Name] += item.Count
		}
	}

	return mergedRankings{
		ModelRanking:      topQuotaRanking(modelMap, topN),
		ChannelRanking:    topQuotaRanking(channelMap, topN),
		UserRanking:       topQuotaRanking(userMap, topN),
		ErrorModelRanking: topCountRanking(errorModelMap, topN),
	}
}

func topQuotaRanking(m map[string]int64, limit int) []models.DashboardRankItem {
	items := make([]models.DashboardRankItem, 0, len(m))
	for name, quota := range m {
		items = append(items, models.DashboardRankItem{Name: name, Quota: quota})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Quota > items[j].Quota })
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func topCountRanking(m map[string]int, limit int) []models.DashboardRankItem {
	items := make([]models.DashboardRankItem, 0, len(m))
	for name, count := range m {
		items = append(items, models.DashboardRankItem{Name: name, Count: count})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Count > items[j].Count })
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func buildFeishuDashboardStatsCard(stats []models.SiteDailyStats, date string, topN int) map[string]interface{} {
	elements := []interface{}{}

	var totalQuota int64
	var totalSuccess, totalError, totalCount int
	for _, s := range stats {
		totalQuota += s.TotalQuota
		totalSuccess += s.SuccessCount
		totalError += s.ErrorCount
		totalCount += s.TotalCount
	}

	if len(stats) > 1 {
		elements = append(elements, map[string]interface{}{
			"tag": "div",
			"fields": []interface{}{
				feishuField("总消费", formatQuotaRMB(totalQuota)),
				feishuField("站点数", fmt.Sprintf("%d", len(stats))),
				feishuField("成功/失败/总数", fmt.Sprintf("%d / %d / %d", totalSuccess, totalError, totalCount)),
			},
		})
		elements = append(elements, map[string]interface{}{"tag": "hr"})
	}

	for _, s := range stats {
		elements = append(elements, map[string]interface{}{
			"tag": "div",
			"fields": []interface{}{
				feishuField("站点", s.SiteName),
				feishuField("消费", formatQuotaRMB(s.TotalQuota)),
				feishuField("成功/失败/总数", fmt.Sprintf("%d / %d / %d", s.SuccessCount, s.ErrorCount, s.TotalCount)),
			},
		})
	}

	merged := mergeDashboardRankings(stats, topN)

	if len(merged.ModelRanking) > 0 {
		elements = append(elements, map[string]interface{}{"tag": "hr"})
		elements = append(elements, map[string]interface{}{
			"tag":  "div",
			"text": feishuMarkdown("**📊 模型排行**"),
		})
		for i, item := range merged.ModelRanking {
			elements = append(elements, map[string]interface{}{
				"tag":  "div",
				"text": feishuMarkdown(fmt.Sprintf("%d. %s — %s", i+1, item.Name, formatQuotaRMB(item.Quota))),
			})
		}
	}

	if len(merged.ChannelRanking) > 0 {
		elements = append(elements, map[string]interface{}{"tag": "hr"})
		elements = append(elements, map[string]interface{}{
			"tag":  "div",
			"text": feishuMarkdown("**📡 渠道排行**"),
		})
		for i, item := range merged.ChannelRanking {
			elements = append(elements, map[string]interface{}{
				"tag":  "div",
				"text": feishuMarkdown(fmt.Sprintf("%d. %s — %s", i+1, item.Name, formatQuotaRMB(item.Quota))),
			})
		}
	}

	if len(merged.UserRanking) > 0 {
		elements = append(elements, map[string]interface{}{"tag": "hr"})
		elements = append(elements, map[string]interface{}{
			"tag":  "div",
			"text": feishuMarkdown("**👤 用户排行**"),
		})
		for i, item := range merged.UserRanking {
			elements = append(elements, map[string]interface{}{
				"tag":  "div",
				"text": feishuMarkdown(fmt.Sprintf("%d. %s — %s", i+1, item.Name, formatQuotaRMB(item.Quota))),
			})
		}
	}

	if len(merged.ErrorModelRanking) > 0 {
		elements = append(elements, map[string]interface{}{"tag": "hr"})
		elements = append(elements, map[string]interface{}{
			"tag":  "div",
			"text": feishuMarkdown("**⚠️ 错误模型排行**"),
		})
		for i, item := range merged.ErrorModelRanking {
			elements = append(elements, map[string]interface{}{
				"tag":  "div",
				"text": feishuMarkdown(fmt.Sprintf("%d. %s — %d 次", i+1, item.Name, item.Count)),
			})
		}
	}

	return map[string]interface{}{
		"config": map[string]interface{}{"wide_screen_mode": true},
		"header": map[string]interface{}{
			"template": "blue",
			"title":    map[string]string{"tag": "plain_text", "content": fmt.Sprintf("战绩统计 — %s", date)},
		},
		"elements": elements,
	}
}

func buildWeworkDashboardStatsMarkdown(stats []models.SiteDailyStats, date string, topN int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("#### 战绩统计 — %s\n", date))

	var totalQuota int64
	var totalSuccess, totalError, totalCount int
	for _, s := range stats {
		totalQuota += s.TotalQuota
		totalSuccess += s.SuccessCount
		totalError += s.ErrorCount
		totalCount += s.TotalCount
	}

	if len(stats) > 1 {
		b.WriteString(fmt.Sprintf("> **总计**\n"))
		b.WriteString(fmt.Sprintf("> 总消费：<font color=\"info\">%s</font>\n", formatQuotaRMB(totalQuota)))
		b.WriteString(fmt.Sprintf("> 站点数：%d\n", len(stats)))
		b.WriteString(fmt.Sprintf("> 成功/失败/总数：%d / %d / %d\n\n", totalSuccess, totalError, totalCount))
	}

	for _, s := range stats {
		b.WriteString(fmt.Sprintf("> **%s**\n", s.SiteName))
		b.WriteString(fmt.Sprintf("> 消费：<font color=\"info\">%s</font>\n", formatQuotaRMB(s.TotalQuota)))
		b.WriteString(fmt.Sprintf("> 成功/失败/总数：%d / %d / %d\n", s.SuccessCount, s.ErrorCount, s.TotalCount))
	}

	merged := mergeDashboardRankings(stats, topN)

	if len(merged.ModelRanking) > 0 {
		b.WriteString("\n**模型排行**\n")
		for i, item := range merged.ModelRanking {
			b.WriteString(fmt.Sprintf("%d. %s — %s\n", i+1, item.Name, formatQuotaRMB(item.Quota)))
		}
	}

	if len(merged.ChannelRanking) > 0 {
		b.WriteString("\n**渠道排行**\n")
		for i, item := range merged.ChannelRanking {
			b.WriteString(fmt.Sprintf("%d. %s — %s\n", i+1, item.Name, formatQuotaRMB(item.Quota)))
		}
	}

	if len(merged.UserRanking) > 0 {
		b.WriteString("\n**用户排行**\n")
		for i, item := range merged.UserRanking {
			b.WriteString(fmt.Sprintf("%d. %s — %s\n", i+1, item.Name, formatQuotaRMB(item.Quota)))
		}
	}

	if len(merged.ErrorModelRanking) > 0 {
		b.WriteString("\n**错误模型排行**\n")
		for i, item := range merged.ErrorModelRanking {
			b.WriteString(fmt.Sprintf("%d. %s — %d 次\n", i+1, item.Name, item.Count))
		}
	}

	return b.String()
}

func autoComputeDashboardStats(ctx context.Context, date string) error {
	concurrency := 5
	var dashConfig models.DashboardConfig
	if err := DashboardConfigCol.FindOne(ctx, bson.M{"_id": "dashboard"}).Decode(&dashConfig); err == nil && dashConfig.Concurrency > 0 {
		concurrency = dashConfig.Concurrency
	}

	siteCursor, err := UpstreamSiteCol.Find(ctx, bson.M{})
	if err != nil {
		return fmt.Errorf("查询站点失败: %w", err)
	}
	var sites []models.UpstreamSite
	if err := siteCursor.All(ctx, &sites); err != nil {
		return fmt.Errorf("解析站点失败: %w", err)
	}
	siteCursor.Close(ctx)

	for _, site := range sites {
		log.Printf("[dashboard-notify] auto computing site=%s date=%s", site.Name, date)
		stats, err := computeSiteStats(ctx, site, date, concurrency)
		if err != nil {
			log.Printf("[dashboard-notify] compute error site=%s: %v", site.Name, err)
			continue
		}
		if err := upsertSiteDailyStats(ctx, stats); err != nil {
			log.Printf("[dashboard-notify] upsert error site=%s: %v", site.Name, err)
		}
	}
	return nil
}

func recordDashboardNotificationAttempt(sendErr error, date string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	now := time.Now()
	update := bson.M{
		"last_attempt_at": now,
		"updated_at":      now,
	}
	if sendErr != nil {
		update["last_error"] = sendErr.Error()
	} else {
		update["last_sent_at"] = now
		update["last_sent_date"] = date
		update["last_error"] = ""
	}

	opts := options.Update().SetUpsert(true)
	if _, err := DashboardNotificationConfigCol.UpdateOne(ctx, bson.M{"_id": dashboardNotificationConfigID}, bson.M{"$set": update}, opts); err != nil {
		log.Printf("[dashboard-notify] failed to record attempt: %v", err)
	}
}

func StartDashboardNotificationScheduler() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			runScheduledDashboardNotification()
		}
	}()
	log.Println("[dashboard-notify] scheduler started")
}

func runScheduledDashboardNotification() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	config, err := loadDashboardNotificationConfig(ctx)
	if err != nil || !config.Enabled {
		return
	}

	pushMinute, ok := parseClockMinutes(config.PushTime)
	if !ok {
		return
	}

	now := time.Now().In(cstZone)
	currentMinute := now.Hour()*60 + now.Minute()
	if currentMinute != pushMinute {
		return
	}

	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	if config.LastSentDate == yesterday {
		return
	}

	log.Printf("[dashboard-notify] triggering push for date=%s", yesterday)

	stats, err := loadSiteDailyStats(ctx, yesterday)
	if err != nil {
		log.Printf("[dashboard-notify] load stats error: %v", err)
		recordDashboardNotificationAttempt(err, yesterday)
		return
	}

	if len(stats) == 0 && config.AutoCompute {
		log.Printf("[dashboard-notify] no stats for %s, auto computing...", yesterday)
		if computeErr := autoComputeDashboardStats(ctx, yesterday); computeErr != nil {
			log.Printf("[dashboard-notify] auto compute error: %v", computeErr)
		}
		stats, _ = loadSiteDailyStats(ctx, yesterday)
	}

	if len(stats) == 0 {
		log.Printf("[dashboard-notify] no stats for %s, skipping", yesterday)
		recordDashboardNotificationAttempt(fmt.Errorf("日期 %s 无统计数据", yesterday), yesterday)
		return
	}

	topN := config.TopN
	if topN <= 0 {
		topN = 10
	}

	sendErr := sendDashboardStatsNotification(ctx, config, stats, yesterday, topN)
	recordDashboardNotificationAttempt(sendErr, yesterday)

	if sendErr != nil {
		log.Printf("[dashboard-notify] send error: %v", sendErr)
	} else {
		log.Printf("[dashboard-notify] push sent for date=%s", yesterday)
	}
}
