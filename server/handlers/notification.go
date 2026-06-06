package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"balanceserver/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	notificationConfigID              = "balance_notification"
	defaultNotificationIntervalMinute = 60
	maxNotificationIntervalMinute     = 10080
	siteAdapterQingshan               = "qingshan"
	siteAdapterEPhone                 = "ephone"
)

var (
	notificationHTTPClient   = &http.Client{Timeout: 15 * time.Second}
	notificationRunMu        sync.Mutex
	errNoNotificationTargets = errors.New("没有符合推送条件的渠道，本次未发送")
)

type balanceResult struct {
	ChannelID int     `json:"channelId"`
	Name      string  `json:"name"`
	URL       string  `json:"url"`
	Quota     float64 `json:"quota"`
	Balance   float64 `json:"balance"`
	UsedQuota float64 `json:"used_quota,omitempty"`
	UsedUSD   float64 `json:"used_usd,omitempty"`
	OK        bool    `json:"ok"`
	Error     string  `json:"error,omitempty"`
}

type balanceNotificationSummary struct {
	TotalSites     int             `json:"total_sites"`
	SuccessCount   int             `json:"success_count"`
	FailedCount    int             `json:"failed_count"`
	TotalBalance   float64         `json:"total_balance"`
	MatchedCount   int             `json:"matched_count"`
	MatchedBalance float64         `json:"matched_balance"`
	Threshold      float64         `json:"threshold"`
	Results        []balanceResult `json:"results"`
}

type notificationConfigRequest struct {
	Enabled          bool    `json:"enabled"`
	NotificationType string  `json:"notification_type"`
	WebhookURL       string  `json:"webhook_url"`
	SignKey          string  `json:"sign_key"`
	WeworkWebhookURL string  `json:"wework_webhook_url"`
	IntervalMinutes  int     `json:"interval_minutes"`
	BalanceThreshold float64 `json:"balance_threshold"`
}

func GetNotificationConfigHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config, _, err := loadNotificationConfig(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load notification config"})
		return
	}

	c.JSON(http.StatusOK, config)
}

func SaveNotificationConfigHandler(c *gin.Context) {
	var req notificationConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	config := models.NotificationConfig{
		Enabled:          req.Enabled,
		NotificationType: req.NotificationType,
		WebhookURL:       req.WebhookURL,
		SignKey:          req.SignKey,
		WeworkWebhookURL: req.WeworkWebhookURL,
		IntervalMinutes:  req.IntervalMinutes,
		BalanceThreshold: req.BalanceThreshold,
	}
	normalizeNotificationConfig(&config)
	if err := validateNotificationConfig(config, config.Enabled); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	existing, _, err := loadNotificationConfig(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load notification config"})
		return
	}

	now := time.Now()
	update := bson.M{
		"enabled":            config.Enabled,
		"notification_type":  config.NotificationType,
		"webhook_url":        config.WebhookURL,
		"sign_key":           config.SignKey,
		"wework_webhook_url": config.WeworkWebhookURL,
		"interval_minutes":   config.IntervalMinutes,
		"balance_threshold":  config.BalanceThreshold,
		"last_attempt_at":    existing.LastAttemptAt,
		"last_sent_at":       existing.LastSentAt,
		"last_error":         existing.LastError,
		"updated_at":         now,
	}

	opts := options.Update().SetUpsert(true)
	if _, err := NotificationConfigCol.UpdateOne(ctx, bson.M{"_id": notificationConfigID}, bson.M{"$set": update}, opts); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save notification config"})
		return
	}

	config.ID = notificationConfigID
	config.LastAttemptAt = existing.LastAttemptAt
	config.LastSentAt = existing.LastSentAt
	config.LastError = existing.LastError
	config.UpdatedAt = now
	c.JSON(http.StatusOK, config)
}

func TestNotificationHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	config, _, err := loadNotificationConfig(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Failed to load notification config"})
		return
	}
	if err := validateNotificationConfig(config, true); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	if err := sendTestNotification(ctx, config); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "通知测试成功"})
}

func SendBalanceNotificationNowHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	config, _, err := loadNotificationConfig(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Failed to load notification config"})
		return
	}
	if err := validateNotificationConfig(config, true); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	summary, err := sendBalanceNotification(ctx, config)
	recordNotificationAttempt(err)
	if errors.Is(err, errNoNotificationTargets) {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": err.Error(),
			"summary": summary,
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
			"summary": summary,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "余额通知已发送",
		"summary": summary,
	})
}

func StartNotificationScheduler() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			runScheduledBalanceNotification()
		}
	}()
}

func runScheduledBalanceNotification() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	config, found, err := loadNotificationConfig(ctx)
	if err != nil {
		log.Printf("Failed to load notification config: %v", err)
		return
	}
	if !found || !config.Enabled {
		return
	}
	if !notificationDue(config, time.Now()) {
		return
	}
	if err := validateNotificationConfig(config, true); err != nil {
		recordNotificationAttempt(err)
		log.Printf("Notification config is invalid: %v", err)
		return
	}

	_, err = sendBalanceNotification(ctx, config)
	recordNotificationAttempt(err)
	if err != nil && !errors.Is(err, errNoNotificationTargets) {
		log.Printf("Failed to send scheduled balance notification: %v", err)
	}
}

func notificationDue(config models.NotificationConfig, now time.Time) bool {
	if config.LastAttemptAt == nil {
		return true
	}
	interval := time.Duration(config.IntervalMinutes) * time.Minute
	return now.Sub(*config.LastAttemptAt) >= interval
}

func sendBalanceNotification(ctx context.Context, config models.NotificationConfig) (balanceNotificationSummary, error) {
	notificationRunMu.Lock()
	defer notificationRunMu.Unlock()

	results, err := queryAllSiteBalances(ctx)
	if err != nil {
		return balanceNotificationSummary{}, err
	}

	summary := summarizeBalanceResults(results, config.BalanceThreshold)
	if len(summary.Results) == 0 {
		return summary, errNoNotificationTargets
	}
	if err := sendBalanceSummaryNotification(ctx, config, summary); err != nil {
		return summary, err
	}

	return summary, nil
}

func queryAllSiteBalances(ctx context.Context) ([]balanceResult, error) {
	cursor, err := SiteCol.Find(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch sites: %w", err)
	}
	defer cursor.Close(ctx)

	var sites []models.Site
	if err := cursor.All(ctx, &sites); err != nil {
		return nil, fmt.Errorf("failed to decode sites: %w", err)
	}

	results := make([]balanceResult, 0, len(sites))
	for _, site := range sites {
		if !siteEligibleForBalanceNotification(site) {
			continue
		}
		results = append(results, querySiteBalance(ctx, site))
	}

	return results, nil
}

func siteEligibleForBalanceNotification(site models.Site) bool {
	return site.Status == 1 && strings.TrimSpace(site.Token) != "" && strings.TrimSpace(site.UserID) != ""
}

func querySiteBalance(ctx context.Context, site models.Site) balanceResult {
	result := balanceResult{
		ChannelID: site.ChannelID,
		Name:      strings.TrimSpace(site.Name),
		URL:       strings.TrimSpace(site.URL),
	}
	if result.Name == "" {
		result.Name = result.URL
	}

	endpoint, err := siteBalanceEndpoint(site)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		result.Error = "failed to create request"
		return result
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "NewApiBalance/1.0")
	if auth := normalizeBearerToken(site.Token); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if userID := strings.TrimSpace(site.UserID); userID != "" {
		req.Header.Set("New-Api-User", userID)
	}

	resp, err := notificationHTTPClient.Do(req)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		result.Error = "failed to read response"
		return result
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Error = fmt.Sprintf("http status %d: %s", resp.StatusCode, compactBody(body))
		return result
	}

	quota, usedQuota, err := parseBalancePayload(body, siteAdapter(site))
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Quota = quota
	result.Balance = quotaToUSD(quota)
	result.UsedQuota = usedQuota
	result.UsedUSD = quotaToUSD(usedQuota)
	result.OK = true
	return result
}

func siteBalanceEndpoint(site models.Site) (string, error) {
	normalized, err := normalizeSiteURLForRequest(site.URL)
	if err != nil {
		return "", err
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", err
	}
	if strings.TrimRight(parsed.Path, "/") == "/api/user/self" {
		return normalized, nil
	}
	return strings.TrimRight(normalized, "/") + "/api/user/self", nil
}

func siteAdapter(site models.Site) string {
	return strings.ToLower(strings.TrimSpace(site.Adapter))
}

func parseBalancePayload(body []byte, adapter string) (float64, float64, error) {
	var payload map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return 0, 0, fmt.Errorf("invalid json response: %w", err)
	}

	data := payload
	if nested, ok := payload["data"].(map[string]interface{}); ok {
		data = nested
	}

	if adapter == siteAdapterEPhone {
		balance, ok := numberFromMap(data, "balance")
		if !ok {
			return 0, 0, errors.New("response does not contain balance")
		}
		return balanceToQuota(balance * 7), 0, nil
	}

	quota, ok := numberFromMap(data, "quota")
	if !ok {
		return 0, 0, errors.New("response does not contain quota")
	}

	usedQuota, _ := numberFromMap(data, "used_quota")
	return quota, usedQuota, nil
}

func numberFromMap(data map[string]interface{}, key string) (float64, bool) {
	value, ok := data[key]
	if !ok || value == nil {
		return 0, false
	}

	switch v := value.(type) {
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func summarizeBalanceResults(results []balanceResult, threshold float64) balanceNotificationSummary {
	summary := balanceNotificationSummary{
		TotalSites: len(results),
		Threshold:  threshold,
	}
	for _, result := range results {
		if result.OK {
			summary.SuccessCount++
			summary.TotalBalance += result.Balance
		} else {
			summary.FailedCount++
		}

		if result.OK && (threshold <= 0 || result.Balance < threshold) {
			summary.Results = append(summary.Results, result)
			summary.MatchedCount++
			summary.MatchedBalance += result.Balance
		}
	}
	return summary
}

func buildBalanceNotificationMessage(summary balanceNotificationSummary) string {
	var builder strings.Builder
	builder.WriteString("New API 渠道余额通知\n\n")
	builder.WriteString(fmt.Sprintf("查询时间: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	builder.WriteString(fmt.Sprintf("站点总数: %d\n", summary.TotalSites))
	builder.WriteString(fmt.Sprintf("查询成功: %d\n", summary.SuccessCount))
	builder.WriteString(fmt.Sprintf("查询失败: %d\n", summary.FailedCount))
	builder.WriteString(fmt.Sprintf("总余额: %.4f USD\n\n", summary.TotalBalance))

	if len(summary.Results) == 0 {
		builder.WriteString("暂无站点配置。")
		return builder.String()
	}

	builder.WriteString("明细:\n")
	for _, result := range summary.Results {
		name := result.Name
		if name == "" {
			name = result.URL
		}
		if result.OK {
			builder.WriteString(fmt.Sprintf("- 渠道ID：%s\n  渠道名称：%s\n  url：%s\n  余额：%.4f USD\n", formatChannelID(result.ChannelID), name, result.URL, result.Balance))
		} else {
			builder.WriteString(fmt.Sprintf("- %s: 查询失败，%s\n", name, result.Error))
		}
	}

	return builder.String()
}

func sendBalanceSummaryNotification(ctx context.Context, config models.NotificationConfig, summary balanceNotificationSummary) error {
	if config.NotificationType == "wework" {
		return sendWeworkMarkdownNotification(ctx, config.WeworkWebhookURL, buildWeworkBalanceMarkdownMessage(summary))
	}
	return sendFeishuCardNotification(ctx, config.WebhookURL, buildFeishuBalanceCard(summary), config.SignKey)
}

func sendTestNotification(ctx context.Context, config models.NotificationConfig) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	if config.NotificationType == "wework" {
		message := fmt.Sprintf("#### New API Balance 通知测试\n> 发送时间：%s\n> 状态：<font color=\"info\">Webhook 配置正常</font>", now)
		return sendWeworkMarkdownNotification(ctx, config.WeworkWebhookURL, message)
	}

	card := map[string]interface{}{
		"config": map[string]interface{}{
			"wide_screen_mode": true,
		},
		"header": map[string]interface{}{
			"template": "green",
			"title": map[string]string{
				"tag":     "plain_text",
				"content": "New API Balance 通知测试",
			},
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
	return sendFeishuCardNotification(ctx, config.WebhookURL, card, config.SignKey)
}

func sendNotification(ctx context.Context, config models.NotificationConfig, message string) error {
	if config.NotificationType == "wework" {
		return sendWeworkNotification(ctx, config.WeworkWebhookURL, message)
	}
	return sendFeishuNotification(ctx, config.WebhookURL, message, config.SignKey)
}

func buildFeishuBalanceCard(summary balanceNotificationSummary) map[string]interface{} {
	template := "green"
	if summary.FailedCount > 0 {
		template = "orange"
	}
	if summary.MatchedBalance < 0 {
		template = "red"
	}

	fields := []interface{}{
		feishuField("查询时间", time.Now().Format("2006-01-02 15:04:05")),
		feishuField("站点总数", strconv.Itoa(summary.TotalSites)),
		feishuField("查询成功", strconv.Itoa(summary.SuccessCount)),
		feishuField("查询失败", strconv.Itoa(summary.FailedCount)),
		feishuField("推送条数", strconv.Itoa(summary.MatchedCount)),
		feishuField("推送余额合计", fmt.Sprintf("<font color=\"%s\">%s</font>", balanceColor(summary.MatchedBalance), formatUSD(summary.MatchedBalance))),
	}
	if summary.Threshold > 0 {
		fields = append(fields, feishuField("低余额阈值", fmt.Sprintf("%s 以下", formatUSD(summary.Threshold))))
	}

	elements := []interface{}{
		map[string]interface{}{
			"tag":    "div",
			"fields": fields,
		},
		map[string]interface{}{"tag": "hr"},
	}

	if len(summary.Results) == 0 {
		elements = append(elements, map[string]interface{}{
			"tag":  "div",
			"text": feishuMarkdown("暂无站点配置。"),
		})
	} else {
		elements = append(elements, map[string]interface{}{
			"tag":  "div",
			"text": feishuMarkdown("**渠道明细**"),
		})
		for index, result := range summary.Results {
			elements = append(elements, map[string]interface{}{
				"tag":  "div",
				"text": feishuMarkdown(buildFeishuResultLine(index+1, result)),
			})
		}
	}

	return map[string]interface{}{
		"config": map[string]interface{}{
			"wide_screen_mode": true,
		},
		"header": map[string]interface{}{
			"template": template,
			"title": map[string]string{
				"tag":     "plain_text",
				"content": balanceNotificationTitle(summary),
			},
		},
		"elements": elements,
	}
}

func feishuField(label, value string) map[string]interface{} {
	return map[string]interface{}{
		"is_short": true,
		"text":     feishuMarkdown(fmt.Sprintf("**%s**\n%s", label, value)),
	}
}

func feishuMarkdown(content string) map[string]string {
	return map[string]string{
		"tag":     "lark_md",
		"content": content,
	}
}

func buildFeishuResultLine(index int, result balanceResult) string {
	name := result.Name
	if name == "" {
		name = result.URL
	}
	name = strings.ReplaceAll(name, "\n", " ")

	if !result.OK {
		return fmt.Sprintf(
			"**%d. %s**\n<font color=\"red\">查询失败</font>：%s",
			index,
			name,
			strings.ReplaceAll(result.Error, "\n", " "),
		)
	}

	return fmt.Sprintf(
		"**%d. 渠道明细**\n渠道ID：%s\n渠道名称：%s\nurl：%s\n余额：<font color=\"%s\">%s</font>",
		index,
		formatChannelID(result.ChannelID),
		name,
		result.URL,
		balanceColor(result.Balance),
		formatUSD(result.Balance),
	)
}

func buildWeworkBalanceMarkdownMessage(summary balanceNotificationSummary) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("#### %s\n", balanceNotificationTitle(summary)))
	builder.WriteString(fmt.Sprintf("> 查询时间：%s\n", time.Now().Format("2006-01-02 15:04:05")))
	builder.WriteString(fmt.Sprintf("> 站点总数：<font color=\"comment\">%d</font>\n", summary.TotalSites))
	builder.WriteString(fmt.Sprintf("> 查询成功：<font color=\"info\">%d</font>\n", summary.SuccessCount))
	builder.WriteString(fmt.Sprintf("> 查询失败：<font color=\"warning\">%d</font>\n", summary.FailedCount))
	builder.WriteString(fmt.Sprintf("> 推送条数：<font color=\"comment\">%d</font>\n", summary.MatchedCount))
	builder.WriteString(fmt.Sprintf("> 推送余额合计：<font color=\"%s\">%s</font>\n", weworkBalanceColor(summary.MatchedBalance), formatUSD(summary.MatchedBalance)))
	if summary.Threshold > 0 {
		builder.WriteString(fmt.Sprintf("> 低余额阈值：<font color=\"warning\">%s 以下</font>\n", formatUSD(summary.Threshold)))
	}
	builder.WriteString("\n")

	if len(summary.Results) == 0 {
		builder.WriteString("暂无站点配置。")
		return builder.String()
	}

	builder.WriteString("**渠道明细**\n")
	for index, result := range summary.Results {
		name := result.Name
		if name == "" {
			name = result.URL
		}
		name = strings.ReplaceAll(name, "\n", " ")
		if !result.OK {
			builder.WriteString(fmt.Sprintf("%d. **%s**\n", index+1, name))
			builder.WriteString(fmt.Sprintf("   <font color=\"warning\">查询失败</font>：%s\n", strings.ReplaceAll(result.Error, "\n", " ")))
			continue
		}
		builder.WriteString(fmt.Sprintf("%d. **渠道明细**\n", index+1))
		builder.WriteString(fmt.Sprintf("   渠道ID：%s\n", formatChannelID(result.ChannelID)))
		builder.WriteString(fmt.Sprintf("   渠道名称：%s\n", name))
		builder.WriteString(fmt.Sprintf("   url：%s\n", result.URL))
		builder.WriteString(fmt.Sprintf(
			"   余额：<font color=\"%s\">%s</font>\n",
			weworkBalanceColor(result.Balance),
			formatUSD(result.Balance),
		))
	}

	return builder.String()
}

func sendFeishuNotification(ctx context.Context, webhookURL, message, signKey string) error {
	payload := map[string]interface{}{
		"msg_type": "text",
		"content":  map[string]string{"text": message},
	}

	if strings.TrimSpace(signKey) != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		payload["timestamp"] = timestamp
		payload["sign"] = genFeishuSign(timestamp, strings.TrimSpace(signKey))
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	var response struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := postWebhookJSON(ctx, webhookURL, body, &response); err != nil {
		return err
	}
	if response.Code != 0 {
		return fmt.Errorf("飞书通知发送失败: code=%d, msg=%s", response.Code, response.Msg)
	}
	return nil
}

func sendFeishuCardNotification(ctx context.Context, webhookURL string, card map[string]interface{}, signKey string) error {
	payload := map[string]interface{}{
		"msg_type": "interactive",
		"card":     card,
	}

	if strings.TrimSpace(signKey) != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		payload["timestamp"] = timestamp
		payload["sign"] = genFeishuSign(timestamp, strings.TrimSpace(signKey))
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	var response struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := postWebhookJSON(ctx, webhookURL, body, &response); err != nil {
		return err
	}
	if response.Code != 0 {
		return fmt.Errorf("飞书通知发送失败: code=%d, msg=%s", response.Code, response.Msg)
	}
	return nil
}

func sendWeworkNotification(ctx context.Context, webhookURL, message string) error {
	payload := map[string]interface{}{
		"msgtype": "text",
		"text":    map[string]string{"content": message},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	var response struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := postWebhookJSON(ctx, webhookURL, body, &response); err != nil {
		return err
	}
	if response.ErrCode != 0 {
		return fmt.Errorf("企业微信通知发送失败: errcode=%d, errmsg=%s", response.ErrCode, response.ErrMsg)
	}
	return nil
}

func sendWeworkMarkdownNotification(ctx context.Context, webhookURL, message string) error {
	payload := map[string]interface{}{
		"msgtype":  "markdown",
		"markdown": map[string]string{"content": message},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	var response struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := postWebhookJSON(ctx, webhookURL, body, &response); err != nil {
		return err
	}
	if response.ErrCode != 0 {
		return fmt.Errorf("企业微信通知发送失败: errcode=%d, errmsg=%s", response.ErrCode, response.ErrMsg)
	}
	return nil
}

func postWebhookJSON(ctx context.Context, webhookURL string, body []byte, responsePayload interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(webhookURL), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := notificationHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook http status %d: %s", resp.StatusCode, compactBody(respBody))
	}
	if err := json.Unmarshal(respBody, responsePayload); err != nil {
		return fmt.Errorf("invalid webhook response: %w", err)
	}
	return nil
}

func genFeishuSign(timestamp, signKey string) string {
	stringToSign := fmt.Sprintf("%s\n%s", timestamp, signKey)
	mac := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func loadNotificationConfig(ctx context.Context) (models.NotificationConfig, bool, error) {
	config := defaultNotificationConfig()
	err := NotificationConfigCol.FindOne(ctx, bson.M{"_id": notificationConfigID}).Decode(&config)
	if err == mongo.ErrNoDocuments {
		return config, false, nil
	}
	if err != nil {
		return config, false, err
	}
	normalizeNotificationConfig(&config)
	return config, true, nil
}

func defaultNotificationConfig() models.NotificationConfig {
	return models.NotificationConfig{
		ID:               notificationConfigID,
		NotificationType: "feishu",
		IntervalMinutes:  defaultNotificationIntervalMinute,
		BalanceThreshold: 0,
	}
}

func normalizeNotificationConfig(config *models.NotificationConfig) {
	config.NotificationType = strings.ToLower(strings.TrimSpace(config.NotificationType))
	if config.NotificationType != "wework" {
		config.NotificationType = "feishu"
	}

	config.WebhookURL = strings.TrimSpace(config.WebhookURL)
	config.SignKey = strings.TrimSpace(config.SignKey)
	config.WeworkWebhookURL = strings.TrimSpace(config.WeworkWebhookURL)

	if config.IntervalMinutes <= 0 {
		config.IntervalMinutes = defaultNotificationIntervalMinute
	}
	if config.IntervalMinutes > maxNotificationIntervalMinute {
		config.IntervalMinutes = maxNotificationIntervalMinute
	}
	if config.BalanceThreshold < 0 {
		config.BalanceThreshold = 0
	}
}

func validateNotificationConfig(config models.NotificationConfig, requireWebhook bool) error {
	if !requireWebhook {
		return nil
	}
	if config.NotificationType == "wework" {
		return validateWebhookURL(config.WeworkWebhookURL, "企业微信 Webhook URL")
	}
	return validateWebhookURL(config.WebhookURL, "飞书 Webhook URL")
}

func validateWebhookURL(rawURL, label string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return fmt.Errorf("请先填写%s", label)
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("%s必须是完整的 http:// 或 https:// URL", label)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s必须以 http:// 或 https:// 开头", label)
	}
	return nil
}

func recordNotificationAttempt(sendErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	now := time.Now()
	update := bson.M{
		"last_attempt_at": now,
		"updated_at":      now,
	}
	if errors.Is(sendErr, errNoNotificationTargets) {
		update["last_error"] = ""
	} else if sendErr != nil {
		update["last_error"] = sendErr.Error()
	} else {
		update["last_sent_at"] = now
		update["last_error"] = ""
	}

	if _, err := NotificationConfigCol.UpdateOne(ctx, bson.M{"_id": notificationConfigID}, bson.M{"$set": update}); err != nil {
		log.Printf("Failed to update notification attempt status: %v", err)
	}
}

func normalizeSiteURLForRequest(value string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(value), "/")
	if trimmed == "" {
		return "", errors.New("site url is empty")
	}
	if !strings.HasPrefix(strings.ToLower(trimmed), "http://") && !strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		trimmed = "https://" + trimmed
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return "", errors.New("site url is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("site url must use http or https")
	}

	return trimmed, nil
}

func quotaToUSD(quota float64) float64 {
	return quota / 500000
}

func balanceToQuota(balance float64) float64 {
	return balance * 500000
}

func balanceNotificationTitle(summary balanceNotificationSummary) string {
	if summary.Threshold > 0 {
		return "New API 低余额渠道通知"
	}
	return "New API 渠道余额通知"
}

func balanceColor(balance float64) string {
	if balance < 0 {
		return "red"
	}
	if balance < 10 {
		return "orange"
	}
	return "green"
}

func weworkBalanceColor(balance float64) string {
	if balance < 0 {
		return "warning"
	}
	if balance < 10 {
		return "warning"
	}
	return "info"
}

func formatUSD(value float64) string {
	return fmt.Sprintf("%s USD", formatFloatWithCommas(value, 2))
}

func formatQuota(value float64) string {
	return formatFloatWithCommas(value, 0)
}

func formatChannelID(channelID int) string {
	if channelID <= 0 {
		return "-"
	}
	return strconv.Itoa(channelID)
}

func formatFloatWithCommas(value float64, precision int) string {
	formatted := fmt.Sprintf("%.*f", precision, value)
	sign := ""
	if strings.HasPrefix(formatted, "-") {
		sign = "-"
		formatted = strings.TrimPrefix(formatted, "-")
	}

	parts := strings.SplitN(formatted, ".", 2)
	integer := parts[0]
	for i := len(integer) - 3; i > 0; i -= 3 {
		integer = integer[:i] + "," + integer[i:]
	}
	if len(parts) == 2 {
		return sign + integer + "." + parts[1]
	}
	return sign + integer
}

func compactBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 200 {
		return text[:200] + "..."
	}
	if text == "" {
		return "empty response"
	}
	return text
}
