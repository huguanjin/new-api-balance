package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"balanceserver/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	channelTestConcurrency = 10
	channelTestTimeout     = 30 * time.Second
)

var channelAvailabilityHTTPClient = &http.Client{Timeout: channelTestTimeout}

type upstreamChannelItem struct {
	ID               int    `json:"id"`
	Type             int    `json:"type"`
	Status           int    `json:"status"`
	Name             string `json:"name"`
	Weight           int    `json:"weight"`
	CreatedTime      int64  `json:"created_time"`
	TestTime         int64  `json:"test_time"`
	ResponseTime     int    `json:"response_time"`
	BaseURL          string `json:"base_url"`
	Balance          float64 `json:"balance"`
	Models           string `json:"models"`
	Group            string `json:"group"`
	UsedQuota        int64  `json:"used_quota"`
	ModelMapping     string `json:"model_mapping"`
	Priority         int    `json:"priority"`
	AutoBan          int    `json:"auto_ban"`
	Tag              string `json:"tag"`
	TestModel        string `json:"test_model"`
}

type upstreamChannelListResponse struct {
	Data struct {
		Items []upstreamChannelItem `json:"items"`
	} `json:"data"`
	Items []upstreamChannelItem `json:"items"`
}

type channelTestResult struct {
	ChannelID    int                     `json:"channelId"`
	Name         string                  `json:"name"`
	TestModel    string                  `json:"testModel"`
	Success      bool                    `json:"success"`
	ResponseTime int                     `json:"responseTime"`
	Error        string                  `json:"error,omitempty"`
	ModelResults []channelModelTestDetail `json:"modelResults,omitempty"`
}

type channelModelTestDetail struct {
	Model        string `json:"model"`
	Success      bool   `json:"success"`
	ResponseTime int    `json:"responseTime"`
	Error        string `json:"error,omitempty"`
}

func FetchUpstreamChannelsHandler(c *gin.Context) {
	var req struct {
		UpstreamSiteID string `json:"upstreamSiteId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	siteID, err := primitive.ObjectIDFromHex(strings.TrimSpace(req.UpstreamSiteID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择上游站点"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cred, err := resolveCredentialsFromSite(ctx, siteID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	channelURL := cred.BaseURL + "/api/channel/"
	channels, err := fetchAllUpstreamChannelItems(ctx, channelURL, cred.Token, cred.UserID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	if err := saveUpstreamChannels(ctx, channels, siteID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save upstream channels"})
		return
	}

	stored, err := listUpstreamChannels(ctx, siteID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load saved channels"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  fmt.Sprintf("成功获取 %d 个上游渠道", len(stored)),
		"count":    len(stored),
		"channels": stored,
	})
}

func GetUpstreamChannelsHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	siteIDStr := strings.TrimSpace(c.Query("upstreamSiteId"))
	if siteIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择上游站点"})
		return
	}
	siteID, err := primitive.ObjectIDFromHex(siteIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的站点 ID"})
		return
	}

	channels, err := listUpstreamChannels(ctx, siteID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load upstream channels"})
		return
	}

	c.JSON(http.StatusOK, channels)
}

func TestChannelAvailabilityHandler(c *gin.Context) {
	var req struct {
		UpstreamSiteID string `json:"upstreamSiteId"`
		ChannelIDs     []int  `json:"channelIds"`
		TestModel      string `json:"testModel"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}
	siteID, err := primitive.ObjectIDFromHex(strings.TrimSpace(req.UpstreamSiteID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择上游站点"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cred, err := resolveCredentialsFromSite(ctx, siteID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	filter := bson.M{"upstream_site_id": siteID}
	if len(req.ChannelIDs) > 0 {
		filter["channelId"] = bson.M{"$in": req.ChannelIDs}
	}

	cursor, err := UpstreamChannelCol.Find(ctx, filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load channels"})
		return
	}
	defer cursor.Close(ctx)

	var channels []models.UpstreamChannel
	if err := cursor.All(ctx, &channels); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to decode channels"})
		return
	}

	if len(channels) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"message": "没有需要测试的渠道",
			"results": []channelTestResult{},
		})
		return
	}

	testModel := strings.TrimSpace(req.TestModel)
	runID := fmt.Sprintf("test-%d", time.Now().UnixNano())
	cleanTestResults(ctx, "")
	if err := batchTestChannels(ctx, channels, cred.BaseURL, cred.Token, cred.UserID, testModel, runID, cred.SkipStatusCodes, false); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "测试结果写入失败"})
		return
	}

	classified, err := classifyTestResults(ctx, runID, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取测试结果失败"})
		return
	}
	defer cleanTestResults(ctx, runID)

	successCount := classified.SuccessCount
	failCount := len(classified.All) - successCount

	c.JSON(http.StatusOK, gin.H{
		"message":      fmt.Sprintf("测试完成：成功 %d，失败 %d，共 %d 个渠道", successCount, failCount, len(classified.All)),
		"total":        len(classified.All),
		"success_count": successCount,
		"fail_count":    failCount,
		"results":      classified.All,
	})
}

func TestSingleChannelAvailabilityHandler(c *gin.Context) {
	channelIDStr := strings.TrimSpace(c.Param("channelId"))
	channelID, err := strconv.Atoi(channelIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid channel ID"})
		return
	}

	var req struct {
		TestModel string `json:"testModel"`
	}
	if c.Request.Body != nil {
		_ = c.ShouldBindJSON(&req)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var channel models.UpstreamChannel
	if err := UpstreamChannelCol.FindOne(ctx, bson.M{"channelId": channelID}).Decode(&channel); err != nil {
		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load channel"})
		return
	}

	cred, err := resolveCredentialsFromChannel(ctx, channel)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	testModel := strings.TrimSpace(req.TestModel)
	result := testOneChannel(ctx, channel, cred.BaseURL, cred.Token, cred.UserID, testModel, cred.SkipStatusCodes, false)

	now := time.Now()
	update := bson.M{
		"test_time":     now.Unix(),
		"response_time": result.ResponseTime,
		"test_model":    result.TestModel,
		"tested_at":     now,
	}
	if result.Success {
		update["test_result"] = "success"
		update["test_error"] = ""
	} else {
		update["test_result"] = "failed"
		update["test_error"] = result.Error
	}
	_, _ = UpstreamChannelCol.UpdateOne(ctx, bson.M{"_id": channel.ID}, bson.M{"$set": update})

	c.JSON(http.StatusOK, result)
}

type modelTestResult struct {
	Model        string `json:"model"`
	Success      bool   `json:"success"`
	ResponseTime int    `json:"responseTime"`
	Error        string `json:"error,omitempty"`
	Message      string `json:"message,omitempty"`
}

func TestChannelModelsHandler(c *gin.Context) {
	channelIDStr := strings.TrimSpace(c.Param("channelId"))
	channelID, err := strconv.Atoi(channelIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid channel ID"})
		return
	}

	var req struct {
		Models []string `json:"models"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}
	if len(req.Models) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择至少一个模型"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var channel models.UpstreamChannel
	if err := UpstreamChannelCol.FindOne(ctx, bson.M{"channelId": channelID}).Decode(&channel); err != nil {
		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{"error": "渠道不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询渠道失败"})
		return
	}

	cred, err := resolveCredentialsFromChannel(ctx, channel)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	results := make([]modelTestResult, len(req.Models))
	sem := make(chan struct{}, channelTestConcurrency)
	var wg sync.WaitGroup

	for i, model := range req.Models {
		wg.Add(1)
		go func(index int, modelName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[index] = testOneModel(ctx, channelID, modelName, cred)
		}(i, strings.TrimSpace(model))
	}
	wg.Wait()

	successCount := 0
	failCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
		} else {
			failCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"channelId":     channelID,
		"total":         len(results),
		"success_count": successCount,
		"fail_count":    failCount,
		"results":       results,
	})
}

func SaveChannelCustomTestModelsHandler(c *gin.Context) {
	channelIDStr := strings.TrimSpace(c.Param("channelId"))
	channelID, err := strconv.Atoi(channelIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid channel ID"})
		return
	}

	var req struct {
		Models []string `json:"models"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	cleaned := make([]string, 0, len(req.Models))
	for _, m := range req.Models {
		m = strings.TrimSpace(m)
		if m != "" {
			cleaned = append(cleaned, m)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := UpstreamChannelCol.UpdateOne(ctx, bson.M{"channelId": channelID}, bson.M{"$set": bson.M{"custom_test_models": cleaned}})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败"})
		return
	}
	if result.MatchedCount == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "渠道不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"channelId": channelID, "customTestModels": cleaned})
}

func testOneModel(ctx context.Context, channelID int, model string, cred availabilityCredentials) modelTestResult {
	result := modelTestResult{Model: model}

	testURL := fmt.Sprintf("%s/api/channel/test/%d?model=%s", cred.BaseURL, channelID, url.QueryEscape(model))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		result.Error = "创建请求失败"
		return result
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", normalizeBearerToken(cred.Token))
	if strings.TrimSpace(cred.UserID) != "" {
		request.Header.Set("New-Api-User", strings.TrimSpace(cred.UserID))
	}

	start := time.Now()
	response, err := channelAvailabilityHTTPClient.Do(request)
	elapsed := time.Since(start)
	result.ResponseTime = int(elapsed.Milliseconds())

	if err != nil {
		result.Error = fmt.Sprintf("请求失败: %s", err.Error())
		return result
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		result.Error = "读取响应失败"
		return result
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		result.Error = fmt.Sprintf("HTTP %d: %s", response.StatusCode, compactBody(body))
		return result
	}

	var payload struct {
		Success bool    `json:"success"`
		Message string  `json:"message"`
		Time    float64 `json:"time"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		result.Error = "解析响应失败"
		return result
	}

	if payload.Success {
		result.Success = true
		if payload.Time > 0 {
			result.ResponseTime = int(payload.Time * 1000)
		}
	} else {
		errMsg := payload.Message
		if errMsg == "" {
			errMsg = "测试未通过"
		}
		if isSkippedStatusCode(errMsg, cred.SkipStatusCodes) {
			result.Success = true
			result.Error = errMsg + " (已过滤)"
		} else {
			result.Error = errMsg
		}
	}
	result.Message = payload.Message
	return result
}

type availabilityCredentials struct {
	BaseURL         string
	Token           string
	UserID          string
	SkipStatusCodes []int
}

type channelStatusChangeResult struct {
	ChannelID int    `json:"channelId"`
	Name      string `json:"name"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

func DeleteUpstreamChannelsHandler(c *gin.Context) {
	var req struct {
		ChannelIDs []int `json:"channelIds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}
	if len(req.ChannelIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择至少一个渠道"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"channelId": bson.M{"$in": req.ChannelIDs}}
	result, err := UpstreamChannelCol.DeleteMany(ctx, filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除渠道失败"})
		return
	}

	removeChannelIDsFromNotifyConfig(ctx, req.ChannelIDs)

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("成功删除 %d 个渠道", result.DeletedCount),
		"deleted": result.DeletedCount,
	})
}

func removeChannelIDsFromNotifyConfig(ctx context.Context, idsToRemove []int) {
	removeSet := make(map[int]struct{}, len(idsToRemove))
	for _, id := range idsToRemove {
		removeSet[id] = struct{}{}
	}
	cursor, err := ChannelAvailabilityNotifyCol.Find(ctx, bson.M{})
	if err != nil {
		return
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var config models.ChannelAvailabilityNotifyConfig
		if err := cursor.Decode(&config); err != nil {
			continue
		}
		changed := false
		for gi, group := range config.MonitoringGroups {
			if len(group.ChannelIDs) == 0 {
				continue
			}
			cleaned := make([]int, 0, len(group.ChannelIDs))
			for _, id := range group.ChannelIDs {
				if _, ok := removeSet[id]; !ok {
					cleaned = append(cleaned, id)
				}
			}
			if len(cleaned) != len(group.ChannelIDs) {
				config.MonitoringGroups[gi].ChannelIDs = cleaned
				changed = true
			}
		}
		if changed {
			_, _ = ChannelAvailabilityNotifyCol.UpdateOne(ctx, bson.M{"_id": config.ID}, bson.M{"$set": bson.M{"monitoring_groups": config.MonitoringGroups}})
		}
	}
}

func BatchUpdateChannelStatusHandler(c *gin.Context) {
	var req struct {
		UpstreamSiteID string `json:"upstreamSiteId"`
		ChannelIDs     []int  `json:"channelIds"`
		Status         int    `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}
	if len(req.ChannelIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择至少一个渠道"})
		return
	}
	if req.Status != 1 && req.Status != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status 仅支持 1(启用) 或 2(禁用)"})
		return
	}
	siteID, err := primitive.ObjectIDFromHex(strings.TrimSpace(req.UpstreamSiteID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择上游站点"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cred, err := resolveCredentialsFromSite(ctx, siteID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	filter := bson.M{"channelId": bson.M{"$in": req.ChannelIDs}}
	cursor, err := UpstreamChannelCol.Find(ctx, filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询渠道失败"})
		return
	}
	defer cursor.Close(ctx)

	var channels []models.UpstreamChannel
	if err := cursor.All(ctx, &channels); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "解析渠道数据失败"})
		return
	}

	if len(channels) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "没有匹配的渠道", "results": []channelStatusChangeResult{}})
		return
	}

	results := make([]channelStatusChangeResult, len(channels))
	sem := make(chan struct{}, channelTestConcurrency)
	var wg sync.WaitGroup

	for i, ch := range channels {
		wg.Add(1)
		go func(index int, channel models.UpstreamChannel) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			r := updateOneChannelStatus(ctx, channel.ChannelID, channel.Name, req.Status, cred)
			results[index] = r

			if r.Success {
				updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, _ = UpstreamChannelCol.UpdateOne(updateCtx, bson.M{"channelId": channel.ChannelID}, bson.M{"$set": bson.M{"status": req.Status}})
			}
		}(i, ch)
	}
	wg.Wait()

	successCount := 0
	failCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
		} else {
			failCount++
		}
	}

	action := "启用"
	if req.Status == 2 {
		action = "禁用"
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       fmt.Sprintf("%s完成：成功 %d，失败 %d", action, successCount, failCount),
		"total":         len(results),
		"success_count": successCount,
		"fail_count":    failCount,
		"results":       results,
	})
}

func updateOneChannelStatus(ctx context.Context, channelID int, name string, status int, cred availabilityCredentials) channelStatusChangeResult {
	result := channelStatusChangeResult{
		ChannelID: channelID,
		Name:      name,
	}

	payload, _ := json.Marshal(map[string]interface{}{"id": channelID, "status": status})
	apiURL := cred.BaseURL + "/api/channel/"

	request, err := http.NewRequestWithContext(ctx, http.MethodPut, apiURL, bytes.NewReader(payload))
	if err != nil {
		result.Error = "创建请求失败"
		return result
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", normalizeBearerToken(cred.Token))
	if strings.TrimSpace(cred.UserID) != "" {
		request.Header.Set("New-Api-User", strings.TrimSpace(cred.UserID))
	}

	response, err := channelAvailabilityHTTPClient.Do(request)
	if err != nil {
		result.Error = fmt.Sprintf("请求失败: %s", err.Error())
		return result
	}
	defer response.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		result.Error = fmt.Sprintf("HTTP %d: %s", response.StatusCode, compactBody(body))
		return result
	}

	var respPayload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &respPayload); err == nil && !respPayload.Success && respPayload.Message != "" {
		result.Error = respPayload.Message
		return result
	}

	result.Success = true
	return result
}

func FetchUpstreamGroupsHandler(c *gin.Context) {
	siteIDStr := strings.TrimSpace(c.Query("upstreamSiteId"))
	if siteIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择上游站点"})
		return
	}
	siteID, err := primitive.ObjectIDFromHex(siteIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的站点 ID"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cred, err := resolveCredentialsFromSite(ctx, siteID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	groupURL := cred.BaseURL + "/api/group/"
	groups, err := fetchUpstreamGroups(ctx, groupURL, cred.Token, cred.UserID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"groups": groups,
	})
}

func fetchUpstreamGroups(ctx context.Context, groupURL, token, userID string) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, groupURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", normalizeBearerToken(token))
	if strings.TrimSpace(userID) != "" {
		request.Header.Set("New-Api-User", strings.TrimSpace(userID))
	}

	response, err := channelAvailabilityHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("请求上游分组列表失败: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("读取上游分组响应失败: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("上游返回 HTTP %d: %s", response.StatusCode, compactBody(body))
	}

	var payload struct {
		Data    []string `json:"data"`
		Success bool     `json:"success"`
		Message string   `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析上游分组响应失败: %w", err)
	}
	if payload.Data == nil {
		payload.Data = []string{}
	}
	return payload.Data, nil
}

func validateChannelAvailabilityURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("渠道接口 URL 必须是完整的 http:// 或 https:// 地址")
	}
	return nil
}

func extractChannelAPIBaseURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("URL 不能为空")
	}
	if !strings.HasPrefix(strings.ToLower(rawURL), "http://") && !strings.HasPrefix(strings.ToLower(rawURL), "https://") {
		rawURL = "https://" + rawURL
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("无效的 URL")
	}

	return fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host), nil
}

func fetchAllUpstreamChannelItems(ctx context.Context, channelURL, token, userID string) ([]upstreamChannelItem, error) {
	parsed, err := url.Parse(channelURL)
	if err != nil {
		return nil, fmt.Errorf("invalid channel url: %w", err)
	}

	query := parsed.Query()
	if !query.Has("page_size") {
		query.Set("page_size", "100")
	}

	seen := make(map[int]struct{}, 200)
	all := make([]upstreamChannelItem, 0, 200)
	pageSize := positiveIntQuery(query, "page_size", 100)

	for page := 1; page < 1000; page++ {
		query.Set("p", strconv.Itoa(page))
		parsed.RawQuery = query.Encode()

		items, err := fetchUpstreamChannelItemPage(ctx, parsed.String(), token, userID)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if _, dup := seen[item.ID]; !dup {
				seen[item.ID] = struct{}{}
				all = append(all, item)
			}
		}
		if len(items) == 0 || len(items) < pageSize {
			break
		}
	}

	return all, nil
}

func fetchUpstreamChannelItemPage(ctx context.Context, pageURL, token, userID string) ([]upstreamChannelItem, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", normalizeBearerToken(token))
	if strings.TrimSpace(userID) != "" {
		request.Header.Set("New-Api-User", strings.TrimSpace(userID))
	}

	response, err := channelAvailabilityHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("请求上游渠道列表失败: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("读取上游渠道响应失败: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("上游返回 HTTP %d: %s", response.StatusCode, compactBody(body))
	}

	var payload upstreamChannelListResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析上游渠道响应失败: %w", err)
	}
	if len(payload.Data.Items) > 0 {
		return payload.Data.Items, nil
	}
	if len(payload.Items) > 0 {
		return payload.Items, nil
	}
	return []upstreamChannelItem{}, nil
}

func saveUpstreamChannels(ctx context.Context, items []upstreamChannelItem, siteID primitive.ObjectID) error {
	existing, err := listUpstreamChannels(ctx, siteID)
	if err != nil {
		return err
	}
	preserve := make(map[int]models.UpstreamChannel, len(existing))
	for _, ch := range existing {
		preserve[ch.ChannelID] = ch
	}

	if _, err := UpstreamChannelCol.DeleteMany(ctx, bson.M{"upstream_site_id": siteID}); err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	now := time.Now()
	docs := make([]interface{}, 0, len(items))
	for _, item := range items {
		doc := models.UpstreamChannel{
			UpstreamSiteID: siteID,
			ChannelID:    item.ID,
			Type:         item.Type,
			Status:       item.Status,
			Name:         strings.TrimSpace(item.Name),
			Weight:       item.Weight,
			CreatedTime:  item.CreatedTime,
			TestTime:     item.TestTime,
			ResponseTime: item.ResponseTime,
			BaseURL:      strings.TrimSpace(item.BaseURL),
			Balance:      item.Balance,
			Models:       item.Models,
			Group:        item.Group,
			UsedQuota:    item.UsedQuota,
			ModelMapping: item.ModelMapping,
			Priority:     item.Priority,
			AutoBan:      item.AutoBan,
			Tag:          strings.TrimSpace(item.Tag),
			TestModel:    strings.TrimSpace(item.TestModel),
			FetchedAt:    now,
		}
		if old, ok := preserve[item.ID]; ok {
			doc.CustomTestModels = old.CustomTestModels
			doc.TestResult = old.TestResult
			doc.TestError = old.TestError
			doc.TestedAt = old.TestedAt
		}
		docs = append(docs, doc)
	}

	_, err = UpstreamChannelCol.InsertMany(ctx, docs)
	return err
}

func listUpstreamChannels(ctx context.Context, siteID primitive.ObjectID) ([]models.UpstreamChannel, error) {
	filter := bson.M{"upstream_site_id": siteID}
	cursor, err := UpstreamChannelCol.Find(
		ctx,
		filter,
		options.Find().SetSort(bson.D{{Key: "channelId", Value: 1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var channels []models.UpstreamChannel
	if err := cursor.All(ctx, &channels); err != nil {
		return nil, err
	}
	if channels == nil {
		channels = []models.UpstreamChannel{}
	}
	return channels, nil
}

func batchTestChannels(ctx context.Context, channels []models.UpstreamChannel, baseURL, token, userID, testModel, runID string, skipStatusCodes []int, anyModelSuccess bool) error {
	sem := make(chan struct{}, channelTestConcurrency)
	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once

	for _, ch := range channels {
		wg.Add(1)
		go func(channel models.UpstreamChannel) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result := testOneChannel(ctx, channel, baseURL, token, userID, testModel, skipStatusCodes, anyModelSuccess)

			now := time.Now()
			update := bson.M{
				"test_time":     now.Unix(),
				"response_time": result.ResponseTime,
				"test_model":    result.TestModel,
				"tested_at":     now,
			}
			if result.Success {
				update["test_result"] = "success"
				update["test_error"] = ""
			} else {
				update["test_result"] = "failed"
				update["test_error"] = result.Error
			}
			updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = UpstreamChannelCol.UpdateOne(updateCtx, bson.M{"_id": channel.ID}, bson.M{"$set": update})

			if runID != "" {
				var modelDetails []models.ChannelTestResultDetail
				for _, mr := range result.ModelResults {
					modelDetails = append(modelDetails, models.ChannelTestResultDetail{
						Model:        mr.Model,
						Success:      mr.Success,
						ResponseTime: mr.ResponseTime,
						Error:        mr.Error,
					})
				}
				doc := models.ChannelTestResult{
					RunID:        runID,
					ChannelID:    result.ChannelID,
					Name:         result.Name,
					TestModel:    result.TestModel,
					Success:      result.Success,
					ResponseTime: result.ResponseTime,
					Error:        result.Error,
					ModelResults: modelDetails,
					Status:       channel.Status,
					TestedAt:     now,
				}
				insertCtx, insertCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer insertCancel()
				if _, err := ChannelTestResultCol.InsertOne(insertCtx, doc); err != nil {
					errOnce.Do(func() { firstErr = err })
				}
			}
		}(ch)
	}

	wg.Wait()
	return firstErr
}

func cleanTestResults(ctx context.Context, runID string) {
	filter := bson.M{}
	if runID != "" {
		filter["run_id"] = runID
	}
	_, _ = ChannelTestResultCol.DeleteMany(ctx, filter)
}

func loadTestResults(ctx context.Context, runID string) ([]models.ChannelTestResult, error) {
	cursor, err := ChannelTestResultCol.Find(ctx, bson.M{"run_id": runID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var results []models.ChannelTestResult
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

type classifiedResults struct {
	All             []channelTestResult
	EnabledFailed   []channelTestResult
	DisabledSuccess []channelTestResult
	EnabledSlow     []channelTestResult
	SuccessCount    int
}

func classifyTestResults(ctx context.Context, runID string, slowThresholdMs int) (*classifiedResults, error) {
	docs, err := loadTestResults(ctx, runID)
	if err != nil {
		return nil, err
	}
	cr := &classifiedResults{}
	for _, doc := range docs {
		r := channelTestResult{
			ChannelID:    doc.ChannelID,
			Name:         doc.Name,
			TestModel:    doc.TestModel,
			Success:      doc.Success,
			ResponseTime: doc.ResponseTime,
			Error:        doc.Error,
		}
		for _, mr := range doc.ModelResults {
			r.ModelResults = append(r.ModelResults, channelModelTestDetail{
				Model:        mr.Model,
				Success:      mr.Success,
				ResponseTime: mr.ResponseTime,
				Error:        mr.Error,
			})
		}
		cr.All = append(cr.All, r)
		if r.Success {
			cr.SuccessCount++
		}
		if doc.Status == 1 && !r.Success {
			cr.EnabledFailed = append(cr.EnabledFailed, r)
		} else if doc.Status == 2 && r.Success {
			cr.DisabledSuccess = append(cr.DisabledSuccess, r)
		} else if doc.Status == 1 && r.Success && slowThresholdMs > 0 && r.ResponseTime > slowThresholdMs {
			cr.EnabledSlow = append(cr.EnabledSlow, r)
		}
	}
	return cr, nil
}

func testOneChannel(ctx context.Context, channel models.UpstreamChannel, baseURL, token, userID, testModel string, skipStatusCodes []int, anyModelSuccess bool) channelTestResult {
	result := channelTestResult{
		ChannelID: channel.ChannelID,
		Name:      channel.Name,
	}

	var modelsToTest []string
	if testModel != "" {
		modelsToTest = []string{testModel}
	} else if len(channel.CustomTestModels) > 0 {
		modelsToTest = channel.CustomTestModels
	} else if channel.TestModel != "" {
		modelsToTest = []string{channel.TestModel}
	} else {
		parts := strings.Split(channel.Models, ",")
		for _, m := range parts {
			m = strings.TrimSpace(m)
			if m != "" {
				modelsToTest = []string{m}
				break
			}
		}
	}
	if len(modelsToTest) == 0 {
		modelsToTest = []string{"gpt-3.5-turbo"}
	}

	result.TestModel = strings.Join(modelsToTest, ", ")

	if len(modelsToTest) == 1 {
		detail := testSingleModel(ctx, channel.ChannelID, modelsToTest[0], baseURL, token, userID, skipStatusCodes)
		result.Success = detail.Success
		result.ResponseTime = detail.ResponseTime
		result.Error = detail.Error
		return result
	}

	details := make([]channelModelTestDetail, len(modelsToTest))
	hasSuccess := false
	allSuccess := true
	maxResponseTime := 0
	var failErrors []string
	for i, m := range modelsToTest {
		details[i] = testSingleModel(ctx, channel.ChannelID, m, baseURL, token, userID, skipStatusCodes)
		if details[i].Success {
			hasSuccess = true
		} else {
			allSuccess = false
			failErrors = append(failErrors, fmt.Sprintf("%s: %s", m, details[i].Error))
		}
		if details[i].ResponseTime > maxResponseTime {
			maxResponseTime = details[i].ResponseTime
		}
	}

	result.ModelResults = details
	if anyModelSuccess {
		result.Success = hasSuccess
	} else {
		result.Success = allSuccess
	}
	result.ResponseTime = maxResponseTime
	if !result.Success {
		result.Error = strings.Join(failErrors, "; ")
	}
	return result
}

func testSingleModel(ctx context.Context, channelID int, model, baseURL, token, userID string, skipStatusCodes []int) channelModelTestDetail {
	result := channelModelTestDetail{Model: model}

	testURL := fmt.Sprintf("%s/api/channel/test/%d?model=%s", baseURL, channelID, url.QueryEscape(model))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		result.Error = "创建请求失败"
		return result
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", normalizeBearerToken(token))
	if strings.TrimSpace(userID) != "" {
		request.Header.Set("New-Api-User", strings.TrimSpace(userID))
	}

	start := time.Now()
	response, err := channelAvailabilityHTTPClient.Do(request)
	elapsed := time.Since(start)
	result.ResponseTime = int(elapsed.Milliseconds())

	if err != nil {
		result.Error = fmt.Sprintf("请求失败: %s", err.Error())
		return result
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		result.Error = "读取响应失败"
		return result
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		result.Error = fmt.Sprintf("HTTP %d: %s", response.StatusCode, compactBody(body))
		return result
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		result.Error = "解析响应失败"
		return result
	}

	if success, ok := payload["success"].(bool); ok && success {
		result.Success = true
		if respTime, ok := payload["time"].(float64); ok {
			result.ResponseTime = int(respTime * 1000)
		}
	} else {
		errMsg := "测试未通过"
		if msg, ok := payload["message"].(string); ok && msg != "" {
			errMsg = msg
		}
		if isSkippedStatusCode(errMsg, skipStatusCodes) {
			result.Success = true
			result.Error = errMsg + " (已过滤)"
		} else {
			result.Error = errMsg
		}
	}

	return result
}

var statusCodePattern = regexp.MustCompile(`(?i)status.?code\D*(\d{3})`)

func isSkippedStatusCode(errMsg string, skipCodes []int) bool {
	if len(skipCodes) == 0 {
		return false
	}
	matches := statusCodePattern.FindStringSubmatch(errMsg)
	if len(matches) < 2 {
		return false
	}
	code, err := strconv.Atoi(matches[1])
	if err != nil {
		return false
	}
	for _, sc := range skipCodes {
		if sc == code {
			return true
		}
	}
	return false
}

func GetChannelAvailabilityNotifyConfigHandler(c *gin.Context) {
	siteIDStr := strings.TrimSpace(c.Query("upstreamSiteId"))
	if siteIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择上游站点"})
		return
	}
	siteID, err := primitive.ObjectIDFromHex(siteIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的站点 ID"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	config, err := loadChannelAvailabilityNotifyConfig(ctx, siteID)
	if err != nil {
		log.Printf("[channel-availability] load notify config error for site %s: %v", siteIDStr, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to load notify config: %v", err)})
		return
	}
	c.JSON(http.StatusOK, config)
}

const channelAvailabilityGlobalNotifyConfigID = "channel_availability_global_notify"

func loadChannelAvailabilityGlobalNotifyConfig(ctx context.Context) (models.ChannelAvailabilityGlobalNotifyConfig, bool, error) {
	var config models.ChannelAvailabilityGlobalNotifyConfig
	err := ChannelAvailabilityGlobalNotifyCol.FindOne(ctx, bson.M{"_id": channelAvailabilityGlobalNotifyConfigID}).Decode(&config)
	if err == mongo.ErrNoDocuments {
		return config, false, nil
	}
	if err != nil {
		return config, false, err
	}
	return config, true, nil
}

func GetChannelAvailabilityGlobalNotifyConfigHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	config, _, err := loadChannelAvailabilityGlobalNotifyConfig(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load global notify config"})
		return
	}
	c.JSON(http.StatusOK, config)
}

func SaveChannelAvailabilityGlobalNotifyConfigHandler(c *gin.Context) {
	var req struct {
		NotificationType string                        `json:"notificationType"`
		WebhookURL       string                        `json:"webhookUrl"`
		SignKey          string                        `json:"signKey"`
		WeworkWebhookURL string                        `json:"weworkWebhookUrl"`
		AlwaysNotify     bool                          `json:"alwaysNotify"`
		Schedules        []models.NotificationSchedule `json:"schedules"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req.Schedules = normalizeNotificationSchedules(req.Schedules)
	if err := validateNotificationSchedules(req.Schedules); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	now := time.Now()
	update := bson.M{
		"notification_type":  strings.TrimSpace(req.NotificationType),
		"webhook_url":        strings.TrimSpace(req.WebhookURL),
		"sign_key":           strings.TrimSpace(req.SignKey),
		"wework_webhook_url": strings.TrimSpace(req.WeworkWebhookURL),
		"always_notify":      req.AlwaysNotify,
		"schedules":          req.Schedules,
		"updated_at":         now,
	}
	opts := options.Update().SetUpsert(true)
	if _, err := ChannelAvailabilityGlobalNotifyCol.UpdateOne(ctx, bson.M{"_id": channelAvailabilityGlobalNotifyConfigID}, bson.M{"$set": update}, opts); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save global notify config"})
		return
	}
	config, _, _ := loadChannelAvailabilityGlobalNotifyConfig(ctx)
	c.JSON(http.StatusOK, config)
}

func SaveChannelAvailabilityNotifyConfigHandler(c *gin.Context) {
	var req struct {
		UpstreamSiteID   string                        `json:"upstreamSiteId"`
		Enabled          bool                          `json:"enabled"`
		NotificationType string                        `json:"notificationType"`
		WebhookURL       string                        `json:"webhookUrl"`
		SignKey          string                        `json:"signKey"`
		WeworkWebhookURL string                        `json:"weworkWebhookUrl"`
		RefreshChannels  *bool                         `json:"refreshChannels"`
		MonitoringGroups []struct {
			Name            string                        `json:"name"`
			ChannelIDs      []int                         `json:"channelIds"`
			StatusFilter    int                           `json:"statusFilter"`
			AutoToggle      bool                          `json:"autoToggle"`
			AnyModelSuccess bool                          `json:"anyModelSuccess"`
			SlowThresholdMs int                           `json:"slowThresholdMs"`
			SkipStatusCodes []int                         `json:"skipStatusCodes"`
			Schedules       []models.NotificationSchedule `json:"schedules"`
		} `json:"monitoringGroups"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}
	siteID, err := primitive.ObjectIDFromHex(strings.TrimSpace(req.UpstreamSiteID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择上游站点"})
		return
	}
	if len(req.MonitoringGroups) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请至少配置一个监测组"})
		return
	}
	var groups []models.MonitoringGroup
	for i, g := range req.MonitoringGroups {
		name := strings.TrimSpace(g.Name)
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("监测组 %d 名称不能为空", i+1)})
			return
		}
		if len(g.ChannelIDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("监测组「%s」至少需要一个生效渠道 ID", name)})
			return
		}
		schedules := normalizeNotificationSchedules(g.Schedules)
		if err := validateNotificationSchedules(schedules); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("监测组「%s」推送计划错误: %s", name, err.Error())})
			return
		}
		groups = append(groups, models.MonitoringGroup{
			Name:            name,
			ChannelIDs:      g.ChannelIDs,
			StatusFilter:    g.StatusFilter,
			AutoToggle:      g.AutoToggle,
			AnyModelSuccess: g.AnyModelSuccess,
			SlowThresholdMs: g.SlowThresholdMs,
			SkipStatusCodes: g.SkipStatusCodes,
			Schedules:       schedules,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	existing, _ := loadChannelAvailabilityNotifyConfig(ctx, siteID)
	for i := range groups {
		for _, old := range existing.MonitoringGroups {
			if old.Name == groups[i].Name && old.LastAttemptAt != nil {
				groups[i].LastAttemptAt = old.LastAttemptAt
				break
			}
		}
	}

	now := time.Now()
	update := bson.M{
		"upstream_site_id":   siteID,
		"enabled":            req.Enabled,
		"notification_type":  strings.TrimSpace(req.NotificationType),
		"webhook_url":        strings.TrimSpace(req.WebhookURL),
		"sign_key":           strings.TrimSpace(req.SignKey),
		"wework_webhook_url": strings.TrimSpace(req.WeworkWebhookURL),
		"refresh_channels":   req.RefreshChannels,
		"monitoring_groups":  groups,
		"updated_at":         now,
	}
	unset := bson.M{
		"channel_ids":      "",
		"schedules":        "",
		"last_attempt_at":  "",
		"status_filter":    "",
		"auto_toggle":      "",
		"slow_threshold_ms": "",
	}
	opts := options.Update().SetUpsert(true)
	if _, err := ChannelAvailabilityNotifyCol.UpdateOne(ctx, bson.M{"upstream_site_id": siteID}, bson.M{"$set": update, "$unset": unset}, opts); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save notify config"})
		return
	}
	config, _ := loadChannelAvailabilityNotifyConfig(ctx, siteID)
	config.UpdatedAt = now
	c.JSON(http.StatusOK, config)
}

func TestChannelAvailabilityNotifyHandler(c *gin.Context) {
	var req struct {
		UpstreamSiteID string `json:"upstreamSiteId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	siteID, err := primitive.ObjectIDFromHex(strings.TrimSpace(req.UpstreamSiteID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择上游站点"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	config, err := loadChannelAvailabilityNotifyConfig(ctx, siteID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load notify config"})
		return
	}
	nType, webhookURL, signKey, err := resolveNotifyWebhook(ctx, config)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	siteName := ""
	if site, sErr := loadUpstreamSite(ctx, siteID); sErr == nil {
		siteName = site.Name
	}
	msg := "【渠道可用性推送测试】\n这是一条测试消息，收到此消息说明推送配置正常。"
	if siteName != "" {
		msg = fmt.Sprintf("【渠道可用性推送测试 - %s】\n这是一条测试消息，收到此消息说明推送配置正常。", siteName)
	}
	if err := sendAvailabilityNotification(ctx, nType, webhookURL, signKey, msg); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("推送失败: %s", err.Error())})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "测试推送成功"})
}

func TestChannelAvailabilityGlobalNotifyHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	globalConfig, found, err := loadChannelAvailabilityGlobalNotifyConfig(ctx)
	if err != nil || !found {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未配置全局推送机器人"})
		return
	}
	nType := globalConfig.NotificationType
	if nType == "" {
		nType = "feishu"
	}
	var webhookURL, signKey string
	if nType == "feishu" {
		webhookURL = strings.TrimSpace(globalConfig.WebhookURL)
		signKey = strings.TrimSpace(globalConfig.SignKey)
	} else {
		webhookURL = strings.TrimSpace(globalConfig.WeworkWebhookURL)
	}
	if webhookURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "全局推送机器人 Webhook URL 未配置"})
		return
	}
	msg := "【渠道可用性推送测试 - 全局配置】\n这是一条测试消息，收到此消息说明全局推送配置正常。"
	if err := sendAvailabilityNotification(ctx, nType, webhookURL, signKey, msg); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("推送失败: %s", err.Error())})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "测试推送成功"})
}

type globalSiteNotifyResult struct {
	SiteName        string
	GroupName       string
	Config          models.ChannelAvailabilityNotifyConfig
	AutoToggle      bool
	SlowThresholdMs int
	All             []channelTestResult
	EnabledFailed   []channelTestResult
	DisabledSuccess []channelTestResult
	EnabledSlow     []channelTestResult
	MissingIDs      []int
	ToggleResults   []channelStatusChangeResult
	SuccessCount    int
	Error           string
}

func RunChannelAvailabilityGlobalNotifyHandler(c *gin.Context) {
	valCtx, valCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer valCancel()

	globalConfig, _, _ := loadChannelAvailabilityGlobalNotifyConfig(valCtx)
	alwaysNotify := globalConfig.AlwaysNotify

	cursor, err := ChannelAvailabilityNotifyCol.Find(valCtx, bson.M{"enabled": true})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询站点配置失败"})
		return
	}

	var configs []models.ChannelAvailabilityNotifyConfig
	if err := cursor.All(valCtx, &configs); err != nil {
		cursor.Close(valCtx)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取站点配置失败"})
		return
	}
	cursor.Close(valCtx)

	if len(configs) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "没有已启用的站点推送配置", "results": []struct{}{}})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": fmt.Sprintf("全局推送任务已开始执行（%d 个站点），完成后将发送通知", len(configs))})

	go runChannelAvailabilityGlobalNotifyAsync(configs, alwaysNotify)
}

func runChannelAvailabilityGlobalNotifyAsync(configs []models.ChannelAvailabilityNotifyConfig, alwaysNotify bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var siteNotifyResults []globalSiteNotifyResult
	cleanTestResults(ctx, "")

	for _, config := range configs {
		if config.UpstreamSiteID.IsZero() {
			continue
		}
		if len(config.MonitoringGroups) == 0 && len(config.ChannelIDs) > 0 {
			config.MonitoringGroups = []models.MonitoringGroup{{
				Name:            "默认组",
				ChannelIDs:      config.ChannelIDs,
				StatusFilter:    config.StatusFilter,
				AutoToggle:      config.AutoToggle,
				SlowThresholdMs: config.SlowThresholdMs,
				Schedules:       config.Schedules,
				LastAttemptAt:   config.LastAttemptAt,
			}}
		}

		siteID := config.UpstreamSiteID
		siteName := ""
		if site, sErr := loadUpstreamSite(ctx, siteID); sErr == nil {
			siteName = site.Name
		}
		if siteName == "" {
			siteName = siteID.Hex()
		}

		cred, credErr := resolveCredentialsFromSite(ctx, siteID)
		if credErr != nil {
			log.Printf("[global-notify-run] 解析凭据失败 site=%s: %s", siteID.Hex(), credErr.Error())
			continue
		}

		shouldRefresh := config.RefreshChannels == nil || *config.RefreshChannels
		if shouldRefresh {
			channelURL := cred.BaseURL + "/api/channel/"
			freshItems, fetchErr := fetchAllUpstreamChannelItems(ctx, channelURL, cred.Token, cred.UserID)
			if fetchErr != nil {
				log.Printf("[global-notify-run] 拉取渠道失败 site=%s: %s", siteID.Hex(), fetchErr.Error())
				continue
			}
			if err := saveUpstreamChannels(ctx, freshItems, siteID); err != nil {
				log.Printf("[global-notify-run] 保存渠道失败 site=%s: %s", siteID.Hex(), err.Error())
				continue
			}
		}

		storedChannels, scErr := listUpstreamChannels(ctx, siteID)
		if scErr != nil {
			log.Printf("[global-notify-run] 读取渠道失败 site=%s: %s", siteID.Hex(), scErr.Error())
			continue
		}
		upstreamIDSet := make(map[int]bool, len(storedChannels))
		for _, ch := range storedChannels {
			upstreamIDSet[ch.ChannelID] = true
		}

		for gi, group := range config.MonitoringGroups {
			if len(group.ChannelIDs) == 0 {
				continue
			}
			var missingIDs []int
			var validIDs []int
			for _, id := range group.ChannelIDs {
				if upstreamIDSet[id] {
					validIDs = append(validIDs, id)
				} else {
					missingIDs = append(missingIDs, id)
				}
			}

			filter := bson.M{"channelId": bson.M{"$in": validIDs}, "upstream_site_id": siteID}
			if group.StatusFilter == 1 {
				filter["status"] = 1
			} else if group.StatusFilter == 2 {
				filter["status"] = 2
			}
			channelCursor, chErr := UpstreamChannelCol.Find(ctx, filter)
			if chErr != nil {
				log.Printf("[global-notify-run] 查询渠道失败 group=%s: %s", group.Name, chErr.Error())
				continue
			}
			var targetChannels []models.UpstreamChannel
			if err := channelCursor.All(ctx, &targetChannels); err != nil {
				channelCursor.Close(ctx)
				log.Printf("[global-notify-run] 解析渠道失败 group=%s: %s", group.Name, err.Error())
				continue
			}
			channelCursor.Close(ctx)

			if len(targetChannels) == 0 {
				continue
			}

			skipCodes := resolveGroupSkipStatusCodes(group, cred)
			runID := fmt.Sprintf("global-%d-%s-%d", time.Now().UnixNano(), siteID.Hex(), gi)
			if err := batchTestChannels(ctx, targetChannels, cred.BaseURL, cred.Token, cred.UserID, "", runID, skipCodes, group.AnyModelSuccess); err != nil {
				log.Printf("[global-notify-run] 测试结果写入失败 group=%s: %s", group.Name, err.Error())
				continue
			}

			classified, classErr := classifyTestResults(ctx, runID, group.SlowThresholdMs)
			if classErr != nil {
				cleanTestResults(ctx, runID)
				log.Printf("[global-notify-run] 读取测试结果失败 group=%s: %s", group.Name, classErr.Error())
				continue
			}

			enabledFailed := classified.EnabledFailed
			disabledSuccess := classified.DisabledSuccess
			enabledSlow := classified.EnabledSlow

			var toggleResults []channelStatusChangeResult
			if group.AutoToggle && (len(enabledFailed) > 0 || len(disabledSuccess) > 0 || len(enabledSlow) > 0) {
				toggleResults = autoToggleChannels(ctx, enabledFailed, disabledSuccess, cred)
				for _, r := range enabledSlow {
					tr := updateOneChannelStatus(ctx, r.ChannelID, r.Name, 2, cred)
					toggleResults = append(toggleResults, tr)
				}
			}

			cleanTestResults(ctx, runID)
			nowTs := time.Now()
			_, _ = ChannelAvailabilityNotifyCol.UpdateOne(ctx, bson.M{"_id": config.ID}, bson.M{"$set": bson.M{
				fmt.Sprintf("monitoring_groups.%d.last_attempt_at", gi): nowTs,
			}})

			hasChanges := len(enabledFailed) > 0 || len(disabledSuccess) > 0 || len(enabledSlow) > 0
			if hasChanges || alwaysNotify {
				siteNotifyResults = append(siteNotifyResults, globalSiteNotifyResult{
					SiteName:        siteName,
					GroupName:       group.Name,
					Config:          config,
					AutoToggle:      group.AutoToggle,
					SlowThresholdMs: group.SlowThresholdMs,
					All:             classified.All,
					EnabledFailed:   enabledFailed,
					DisabledSuccess: disabledSuccess,
					EnabledSlow:     enabledSlow,
					MissingIDs:      missingIDs,
					ToggleResults:   toggleResults,
					SuccessCount:    classified.SuccessCount,
				})
			}
		}
	}

	if len(siteNotifyResults) > 0 {
		card := buildGlobalAvailabilityNotifyCard(siteNotifyResults)
		msg := buildGlobalAvailabilityNotifyMessage(siteNotifyResults)
		nType, webhookURL, signKey, resolveErr := resolveGlobalNotifyWebhook(ctx)
		if resolveErr == nil {
			if err := sendAvailabilityCardNotification(ctx, nType, webhookURL, signKey, card, msg); err != nil {
				log.Printf("[global-notify-run] 推送通知失败: %v", err)
			}
		} else {
			log.Printf("[global-notify-run] 解析 webhook 失败: %v", resolveErr)
		}
	}
	log.Printf("[global-notify-run] 完成 sites=%d", len(configs))
}

func resolveGlobalNotifyWebhook(ctx context.Context) (string, string, string, error) {
	globalConfig, found, err := loadChannelAvailabilityGlobalNotifyConfig(ctx)
	if err == nil && found {
		nType := globalConfig.NotificationType
		if nType == "" {
			nType = "feishu"
		}
		var webhookURL, signKey string
		if nType == "feishu" {
			webhookURL = strings.TrimSpace(globalConfig.WebhookURL)
			signKey = strings.TrimSpace(globalConfig.SignKey)
		} else {
			webhookURL = strings.TrimSpace(globalConfig.WeworkWebhookURL)
		}
		if webhookURL != "" {
			return nType, webhookURL, signKey, nil
		}
	}
	balanceConfig, found, loadErr := loadNotificationConfig(ctx)
	if loadErr != nil || !found {
		return "", "", "", fmt.Errorf("未配置全局推送机器人")
	}
	nType := balanceConfig.NotificationType
	if nType == "" {
		nType = "feishu"
	}
	var webhookURL, signKey string
	if nType == "feishu" {
		webhookURL = strings.TrimSpace(balanceConfig.WebhookURL)
		signKey = strings.TrimSpace(balanceConfig.SignKey)
	} else {
		webhookURL = strings.TrimSpace(balanceConfig.WeworkWebhookURL)
	}
	if webhookURL == "" {
		return "", "", "", fmt.Errorf("未配置推送机器人 Webhook URL")
	}
	return nType, webhookURL, signKey, nil
}

func buildGlobalAvailabilityNotifyMessage(sites []globalSiteNotifyResult) string {
	var b strings.Builder
	b.WriteString("【渠道可用性测试通知 - 全局汇总】\n")

	for i, site := range sites {
		if i > 0 {
			b.WriteString("\n────────────────────────\n")
		}
		successCount := site.SuccessCount
		totalCount := len(site.All)
		displayName := site.SiteName
		if site.GroupName != "" {
			displayName = site.SiteName + " / " + site.GroupName
		}
		b.WriteString(fmt.Sprintf("\n📍 %s\n", displayName))
		b.WriteString(fmt.Sprintf("共测试 %d 个渠道，成功 %d，失败 %d\n", totalCount, successCount, totalCount-successCount))

		toggleResultMap := make(map[int]bool, len(site.ToggleResults))
		for _, tr := range site.ToggleResults {
			toggleResultMap[tr.ChannelID] = tr.Success
		}

		if len(site.DisabledSuccess) > 0 {
			b.WriteString(fmt.Sprintf("\n🟢 以下已禁用渠道测试通过（%d个）：\n", len(site.DisabledSuccess)))
			for _, r := range site.DisabledSuccess {
				action := "建议：可启用"
				if site.AutoToggle {
					if toggleResultMap[r.ChannelID] {
						action = "已自动启用 ✅"
					} else {
						action = "自动启用失败 ❗"
					}
				}
				b.WriteString(fmt.Sprintf("  ✅ %s (ID:%d) | 测试模型: %s | 响应: %dms | %s\n", r.Name, r.ChannelID, r.TestModel, r.ResponseTime, action))
				writeModelResultDetails(&b, r)
			}
		}

		if len(site.EnabledFailed) > 0 {
			b.WriteString(fmt.Sprintf("\n🔴 以下已启用渠道测试失败（%d个）：\n", len(site.EnabledFailed)))
			for _, r := range site.EnabledFailed {
				errMsg := r.Error
				if len(r.ModelResults) == 0 && len(errMsg) > 80 {
					errMsg = errMsg[:80] + "..."
				}
				action := "建议：临时停用"
				if site.AutoToggle {
					if toggleResultMap[r.ChannelID] {
						action = "已自动停用 ✅"
					} else {
						action = "自动停用失败 ❗"
					}
				}
				if len(r.ModelResults) > 0 {
					b.WriteString(fmt.Sprintf("  ❌ %s (ID:%d) | 测试模型: %s | %s\n", r.Name, r.ChannelID, r.TestModel, action))
				} else {
					b.WriteString(fmt.Sprintf("  ❌ %s (ID:%d) | 测试模型: %s | 错误: %s | %s\n", r.Name, r.ChannelID, r.TestModel, errMsg, action))
				}
				writeModelResultDetails(&b, r)
			}
		}

		if len(site.EnabledSlow) > 0 {
			b.WriteString(fmt.Sprintf("\n🟡 以下已启用渠道响应超时（%d个，阈值 %dms）：\n", len(site.EnabledSlow), site.SlowThresholdMs))
			for _, r := range site.EnabledSlow {
				action := "建议：临时停用"
				if site.AutoToggle {
					if toggleResultMap[r.ChannelID] {
						action = "已自动停用 ✅"
					} else {
						action = "自动停用失败 ❗"
					}
				}
				b.WriteString(fmt.Sprintf("  ⏱️ %s (ID:%d) | 测试模型: %s | 响应: %dms | %s\n", r.Name, r.ChannelID, r.TestModel, r.ResponseTime, action))
				writeModelResultDetails(&b, r)
			}
		}

		if len(site.MissingIDs) > 0 {
			b.WriteString(fmt.Sprintf("\n⚠️ 以下配置的渠道 ID 在上游已不存在：%v\n", site.MissingIDs))
		}

		if len(site.DisabledSuccess) == 0 && len(site.EnabledFailed) == 0 && len(site.EnabledSlow) == 0 {
			b.WriteString("\n✅ 所有渠道运行正常\n")
		}
	}

	return b.String()
}

func RunChannelAvailabilityNotifyHandler(c *gin.Context) {
	var req struct {
		UpstreamSiteID string `json:"upstreamSiteId"`
		GroupIndex     *int   `json:"groupIndex"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	siteID, err := primitive.ObjectIDFromHex(strings.TrimSpace(req.UpstreamSiteID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择上游站点"})
		return
	}

	valCtx, valCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer valCancel()
	notifyConfig, err := loadChannelAvailabilityNotifyConfig(valCtx, siteID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load notify config"})
		return
	}
	if len(notifyConfig.MonitoringGroups) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先配置监测组"})
		return
	}
	nType, webhookURL, signKey, err := resolveNotifyWebhook(valCtx, notifyConfig)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cred, err := resolveCredentialsFromSite(valCtx, siteID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	siteName := ""
	if site, sErr := loadUpstreamSite(valCtx, siteID); sErr == nil {
		siteName = site.Name
	}

	var groupsToRun []models.MonitoringGroup
	var groupIndices []int
	if req.GroupIndex != nil {
		gi := *req.GroupIndex
		if gi < 0 || gi >= len(notifyConfig.MonitoringGroups) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的监测组索引"})
			return
		}
		groupsToRun = []models.MonitoringGroup{notifyConfig.MonitoringGroups[gi]}
		groupIndices = []int{gi}
	} else {
		groupsToRun = notifyConfig.MonitoringGroups
		for i := range groupsToRun {
			groupIndices = append(groupIndices, i)
		}
	}

	c.JSON(http.StatusAccepted, gin.H{"message": fmt.Sprintf("推送任务已开始执行（%d 个监测组），完成后将发送通知", len(groupsToRun))})

	go runChannelAvailabilityNotifyAsync(notifyConfig, siteID, siteName, nType, webhookURL, signKey, cred, groupsToRun, groupIndices)
}

func runChannelAvailabilityNotifyAsync(notifyConfig models.ChannelAvailabilityNotifyConfig, siteID primitive.ObjectID, siteName, nType, webhookURL, signKey string, cred availabilityCredentials, groupsToRun []models.MonitoringGroup, groupIndices []int) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	shouldRefresh := notifyConfig.RefreshChannels == nil || *notifyConfig.RefreshChannels
	if shouldRefresh {
		channelURL := cred.BaseURL + "/api/channel/"
		freshItems, fetchErr := fetchAllUpstreamChannelItems(ctx, channelURL, cred.Token, cred.UserID)
		if fetchErr != nil {
			log.Printf("[notify-run] 拉取渠道失败 site=%s: %s", siteID.Hex(), fetchErr.Error())
			return
		}
		if err := saveUpstreamChannels(ctx, freshItems, siteID); err != nil {
			log.Printf("[notify-run] 保存渠道失败 site=%s: %s", siteID.Hex(), err.Error())
			return
		}
	}

	storedChannels, err := listUpstreamChannels(ctx, siteID)
	if err != nil {
		log.Printf("[notify-run] 读取渠道失败 site=%s: %s", siteID.Hex(), err.Error())
		return
	}
	upstreamIDSet := make(map[int]bool, len(storedChannels))
	for _, ch := range storedChannels {
		upstreamIDSet[ch.ChannelID] = true
	}

	cleanTestResults(ctx, "")

	for idx, group := range groupsToRun {
		gi := groupIndices[idx]
		if len(group.ChannelIDs) == 0 {
			continue
		}

		var missingIDs []int
		var validIDs []int
		for _, id := range group.ChannelIDs {
			if upstreamIDSet[id] {
				validIDs = append(validIDs, id)
			} else {
				missingIDs = append(missingIDs, id)
			}
		}

		filter := bson.M{"channelId": bson.M{"$in": validIDs}, "upstream_site_id": siteID}
		if group.StatusFilter == 1 {
			filter["status"] = 1
		} else if group.StatusFilter == 2 {
			filter["status"] = 2
		}
		cursor, chErr := UpstreamChannelCol.Find(ctx, filter)
		if chErr != nil {
			log.Printf("[notify-run] 查询渠道失败 group=%s: %s", group.Name, chErr.Error())
			continue
		}
		var targetChannels []models.UpstreamChannel
		if err := cursor.All(ctx, &targetChannels); err != nil {
			cursor.Close(ctx)
			log.Printf("[notify-run] 解析渠道失败 group=%s: %s", group.Name, err.Error())
			continue
		}
		cursor.Close(ctx)

		if len(targetChannels) == 0 {
			continue
		}

		displayName := siteName
		if group.Name != "" {
			displayName = siteName + " / " + group.Name
		}

		skipCodes := resolveGroupSkipStatusCodes(group, cred)
		runID := fmt.Sprintf("notify-%d-%d", time.Now().UnixNano(), gi)
		if err := batchTestChannels(ctx, targetChannels, cred.BaseURL, cred.Token, cred.UserID, "", runID, skipCodes, group.AnyModelSuccess); err != nil {
			log.Printf("[notify-run] 测试结果写入失败 group=%s: %s", group.Name, err.Error())
			continue
		}

		classified, classErr := classifyTestResults(ctx, runID, group.SlowThresholdMs)
		if classErr != nil {
			cleanTestResults(ctx, runID)
			log.Printf("[notify-run] 读取测试结果失败 group=%s: %s", group.Name, classErr.Error())
			continue
		}

		enabledFailed := classified.EnabledFailed
		disabledSuccess := classified.DisabledSuccess
		enabledSlow := classified.EnabledSlow

		var toggleResults []channelStatusChangeResult
		if group.AutoToggle && (len(enabledFailed) > 0 || len(disabledSuccess) > 0 || len(enabledSlow) > 0) {
			toggleResults = autoToggleChannels(ctx, enabledFailed, disabledSuccess, cred)
			for _, r := range enabledSlow {
				tr := updateOneChannelStatus(ctx, r.ChannelID, r.Name, 2, cred)
				toggleResults = append(toggleResults, tr)
			}
		}

		if len(enabledFailed) > 0 || len(disabledSuccess) > 0 || len(enabledSlow) > 0 {
			card := buildAvailabilityNotifyCard(displayName, classified.All, enabledFailed, disabledSuccess, enabledSlow, missingIDs, group.AutoToggle, group.SlowThresholdMs, toggleResults)
			msg := buildAvailabilityNotifyMessage(displayName, classified.All, enabledFailed, disabledSuccess, enabledSlow, missingIDs, group.AutoToggle, group.SlowThresholdMs, toggleResults)
			if err := sendAvailabilityCardNotification(ctx, nType, webhookURL, signKey, card, msg); err != nil {
				log.Printf("[notify-run] 推送通知失败 group=%s: %s", group.Name, err.Error())
			}
		}

		cleanTestResults(ctx, runID)
		nowTs := time.Now()
		_, _ = ChannelAvailabilityNotifyCol.UpdateOne(ctx, bson.M{"_id": notifyConfig.ID}, bson.M{"$set": bson.M{
			fmt.Sprintf("monitoring_groups.%d.last_attempt_at", gi): nowTs,
		}})
	}
	log.Printf("[notify-run] 完成 site=%s groups=%d", siteID.Hex(), len(groupsToRun))
}

func loadChannelAvailabilityNotifyConfig(ctx context.Context, siteID primitive.ObjectID) (models.ChannelAvailabilityNotifyConfig, error) {
	var config models.ChannelAvailabilityNotifyConfig
	err := ChannelAvailabilityNotifyCol.FindOne(ctx, bson.M{"upstream_site_id": siteID}).Decode(&config)
	if err == mongo.ErrNoDocuments {
		return models.ChannelAvailabilityNotifyConfig{
			UpstreamSiteID:   siteID,
			NotificationType: "feishu",
		}, nil
	}
	if err != nil {
		log.Printf("[channel-availability] decode notify config error for site %s: %v, attempting raw decode", siteID.Hex(), err)
		var raw bson.M
		if rawErr := ChannelAvailabilityNotifyCol.FindOne(ctx, bson.M{"upstream_site_id": siteID}).Decode(&raw); rawErr == nil {
			config = rebuildNotifyConfigFromRaw(raw, siteID)
			log.Printf("[channel-availability] recovered config from raw document for site %s", siteID.Hex())
			return config, nil
		}
		return config, err
	}
	if len(config.MonitoringGroups) == 0 && len(config.ChannelIDs) > 0 {
		config.MonitoringGroups = []models.MonitoringGroup{{
			Name:            "默认组",
			ChannelIDs:      config.ChannelIDs,
			StatusFilter:    config.StatusFilter,
			AutoToggle:      config.AutoToggle,
			SlowThresholdMs: config.SlowThresholdMs,
			Schedules:       config.Schedules,
			LastAttemptAt:   config.LastAttemptAt,
		}}
		config.ChannelIDs = nil
		config.Schedules = nil
		config.LastAttemptAt = nil
		_, _ = ChannelAvailabilityNotifyCol.UpdateOne(ctx, bson.M{"_id": config.ID}, bson.M{
			"$set":   bson.M{"monitoring_groups": config.MonitoringGroups},
			"$unset": bson.M{"channel_ids": "", "schedules": "", "last_attempt_at": "", "status_filter": "", "auto_toggle": "", "slow_threshold_ms": ""},
		})
	}
	return config, nil
}

func rebuildNotifyConfigFromRaw(raw bson.M, siteID primitive.ObjectID) models.ChannelAvailabilityNotifyConfig {
	config := models.ChannelAvailabilityNotifyConfig{
		UpstreamSiteID:   siteID,
		NotificationType: "feishu",
	}
	if id, ok := raw["_id"].(primitive.ObjectID); ok {
		config.ID = id
	}
	if v, ok := raw["enabled"].(bool); ok {
		config.Enabled = v
	}
	if v, ok := raw["notification_type"].(string); ok {
		config.NotificationType = v
	}
	if v, ok := raw["webhook_url"].(string); ok {
		config.WebhookURL = v
	}
	if v, ok := raw["sign_key"].(string); ok {
		config.SignKey = v
	}
	if v, ok := raw["wework_webhook_url"].(string); ok {
		config.WeworkWebhookURL = v
	}
	if v, ok := raw["refresh_channels"].(bool); ok {
		config.RefreshChannels = &v
	}

	var channelIDs []int
	if arr, ok := raw["channel_ids"].(primitive.A); ok {
		for _, item := range arr {
			switch v := item.(type) {
			case int32:
				channelIDs = append(channelIDs, int(v))
			case int64:
				channelIDs = append(channelIDs, int(v))
			case float64:
				channelIDs = append(channelIDs, int(v))
			}
		}
	}

	if len(channelIDs) > 0 {
		autoToggle := false
		if v, ok := raw["auto_toggle"].(bool); ok {
			autoToggle = v
		}
		statusFilter := 0
		if v, ok := raw["status_filter"].(int32); ok {
			statusFilter = int(v)
		} else if v, ok := raw["status_filter"].(int64); ok {
			statusFilter = int(v)
		}
		slowThresholdMs := 0
		if v, ok := raw["slow_threshold_ms"].(int32); ok {
			slowThresholdMs = int(v)
		} else if v, ok := raw["slow_threshold_ms"].(int64); ok {
			slowThresholdMs = int(v)
		}
		config.MonitoringGroups = []models.MonitoringGroup{{
			Name:            "默认组",
			ChannelIDs:      channelIDs,
			StatusFilter:    statusFilter,
			AutoToggle:      autoToggle,
			SlowThresholdMs: slowThresholdMs,
		}}
	}

	log.Printf("[channel-availability] raw doc fields: %v", rawDocKeys(raw))
	return config
}

func rawDocKeys(doc bson.M) []string {
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, fmt.Sprintf("%s(%T)", k, doc[k]))
	}
	return keys
}

func resolveNotifyWebhook(ctx context.Context, config models.ChannelAvailabilityNotifyConfig) (string, string, string, error) {
	nType := config.NotificationType
	if nType == "" {
		nType = "feishu"
	}
	var webhookURL, signKey string
	if nType == "feishu" {
		webhookURL = strings.TrimSpace(config.WebhookURL)
		signKey = strings.TrimSpace(config.SignKey)
	} else {
		webhookURL = strings.TrimSpace(config.WeworkWebhookURL)
	}
	if webhookURL != "" {
		return nType, webhookURL, signKey, nil
	}
	globalConfig, found, loadErr := loadChannelAvailabilityGlobalNotifyConfig(ctx)
	if loadErr == nil && found {
		gType := globalConfig.NotificationType
		if gType == "" {
			gType = "feishu"
		}
		if gType == "feishu" {
			webhookURL = strings.TrimSpace(globalConfig.WebhookURL)
			signKey = strings.TrimSpace(globalConfig.SignKey)
		} else {
			webhookURL = strings.TrimSpace(globalConfig.WeworkWebhookURL)
		}
		if webhookURL != "" {
			return gType, webhookURL, signKey, nil
		}
	}
	balanceConfig, found, loadErr := loadNotificationConfig(ctx)
	if loadErr != nil || !found {
		return "", "", "", fmt.Errorf("未配置推送机器人，且全局配置和余额通知中也未配置")
	}
	nType = balanceConfig.NotificationType
	if nType == "" {
		nType = "feishu"
	}
	if nType == "feishu" {
		webhookURL = strings.TrimSpace(balanceConfig.WebhookURL)
		signKey = strings.TrimSpace(balanceConfig.SignKey)
	} else {
		webhookURL = strings.TrimSpace(balanceConfig.WeworkWebhookURL)
	}
	if webhookURL == "" {
		return "", "", "", fmt.Errorf("未配置推送机器人 Webhook URL")
	}
	return nType, webhookURL, signKey, nil
}

func sendAvailabilityNotification(ctx context.Context, nType, webhookURL, signKey, message string) error {
	if nType == "wework" {
		return sendWeworkMarkdownNotification(ctx, webhookURL, message)
	}
	return sendFeishuNotification(ctx, webhookURL, message, signKey)
}

func sendAvailabilityCardNotification(ctx context.Context, nType, webhookURL, signKey string, card map[string]interface{}, markdownFallback string) error {
	if nType == "wework" {
		return sendWeworkMarkdownNotification(ctx, webhookURL, markdownFallback)
	}
	return sendFeishuCardNotification(ctx, webhookURL, card, signKey)
}

func buildChannelLine(r channelTestResult, autoToggle bool, toggleMap map[int]bool) string {
	var b strings.Builder
	if r.Success {
		b.WriteString(fmt.Sprintf("✅ **%s** (ID:%d)\n", r.Name, r.ChannelID))
		b.WriteString(fmt.Sprintf("模型: %s | 响应: %dms", r.TestModel, r.ResponseTime))
	} else {
		b.WriteString(fmt.Sprintf("❌ **%s** (ID:%d)\n", r.Name, r.ChannelID))
		b.WriteString(fmt.Sprintf("模型: %s", r.TestModel))
		if len(r.ModelResults) == 0 && r.Error != "" {
			errMsg := r.Error
			if len(errMsg) > 80 {
				errMsg = errMsg[:80] + "..."
			}
			b.WriteString(fmt.Sprintf("\n错误: %s", errMsg))
		}
	}
	if autoToggle {
		if success, ok := toggleMap[r.ChannelID]; ok {
			if success {
				b.WriteString(" | ✅ 已自动处理")
			} else {
				b.WriteString(" | ❗ 自动处理失败")
			}
		}
	}
	if len(r.ModelResults) > 1 {
		for _, mr := range r.ModelResults {
			if mr.Success {
				b.WriteString(fmt.Sprintf("\n　　✅ %s %dms", mr.Model, mr.ResponseTime))
			} else {
				errMsg := mr.Error
				if len(errMsg) > 50 {
					errMsg = errMsg[:50] + "..."
				}
				b.WriteString(fmt.Sprintf("\n　　❌ %s %s", mr.Model, errMsg))
			}
		}
	}
	return b.String()
}

func buildSlowChannelLine(r channelTestResult, autoToggle bool, toggleMap map[int]bool) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("⏱️ **%s** (ID:%d)\n", r.Name, r.ChannelID))
	b.WriteString(fmt.Sprintf("模型: %s | 响应: <font color='red'>%dms</font>", r.TestModel, r.ResponseTime))
	if autoToggle {
		if success, ok := toggleMap[r.ChannelID]; ok {
			if success {
				b.WriteString(" | ✅ 已自动停用")
			} else {
				b.WriteString(" | ❗ 自动停用失败")
			}
		}
	}
	if len(r.ModelResults) > 1 {
		for _, mr := range r.ModelResults {
			if mr.Success {
				b.WriteString(fmt.Sprintf("\n　　✅ %s %dms", mr.Model, mr.ResponseTime))
			} else {
				errMsg := mr.Error
				if len(errMsg) > 50 {
					errMsg = errMsg[:50] + "..."
				}
				b.WriteString(fmt.Sprintf("\n　　❌ %s %s", mr.Model, errMsg))
			}
		}
	}
	return b.String()
}

func buildAvailabilityNotifyCard(siteName string, allResults []channelTestResult, enabledFailed, disabledSuccess, enabledSlow []channelTestResult, missingIDs []int, autoToggle bool, slowThresholdMs int, toggleResults []channelStatusChangeResult) map[string]interface{} {
	successCount := 0
	for _, r := range allResults {
		if r.Success {
			successCount++
		}
	}
	failCount := len(allResults) - successCount

	template := "green"
	if len(enabledSlow) > 0 {
		template = "orange"
	}
	if len(enabledFailed) > 0 {
		template = "red"
	}

	title := "渠道可用性测试通知"
	if siteName != "" {
		title = fmt.Sprintf("渠道可用性测试通知 - %s", siteName)
	}

	toggleMap := make(map[int]bool, len(toggleResults))
	for _, tr := range toggleResults {
		toggleMap[tr.ChannelID] = tr.Success
	}

	elements := []interface{}{
		map[string]interface{}{
			"tag": "div",
			"fields": []interface{}{
				feishuField("测试渠道", strconv.Itoa(len(allResults))),
				feishuField("测试时间", time.Now().Format("01-02 15:04")),
				feishuField("成功", fmt.Sprintf("<font color='green'>%d</font>", successCount)),
				feishuField("失败", fmt.Sprintf("<font color='red'>%d</font>", failCount)),
			},
		},
	}

	if len(enabledFailed) > 0 {
		elements = append(elements, map[string]interface{}{"tag": "hr"})
		elements = append(elements, map[string]interface{}{
			"tag":  "div",
			"text": feishuMarkdown(fmt.Sprintf("🔴 **已启用渠道测试失败（%d个）**", len(enabledFailed))),
		})
		for _, r := range enabledFailed {
			elements = append(elements, map[string]interface{}{
				"tag":  "div",
				"text": feishuMarkdown(buildChannelLine(r, autoToggle, toggleMap)),
			})
		}
	}

	if len(disabledSuccess) > 0 {
		elements = append(elements, map[string]interface{}{"tag": "hr"})
		elements = append(elements, map[string]interface{}{
			"tag":  "div",
			"text": feishuMarkdown(fmt.Sprintf("🟢 **已禁用渠道测试通过（%d个）**", len(disabledSuccess))),
		})
		for _, r := range disabledSuccess {
			elements = append(elements, map[string]interface{}{
				"tag":  "div",
				"text": feishuMarkdown(buildChannelLine(r, autoToggle, toggleMap)),
			})
		}
	}

	if len(enabledSlow) > 0 {
		elements = append(elements, map[string]interface{}{"tag": "hr"})
		elements = append(elements, map[string]interface{}{
			"tag":  "div",
			"text": feishuMarkdown(fmt.Sprintf("🟡 **已启用渠道响应超时（%d个，阈值 %dms）**", len(enabledSlow), slowThresholdMs)),
		})
		for _, r := range enabledSlow {
			elements = append(elements, map[string]interface{}{
				"tag":  "div",
				"text": feishuMarkdown(buildSlowChannelLine(r, autoToggle, toggleMap)),
			})
		}
	}

	if len(missingIDs) > 0 {
		elements = append(elements, map[string]interface{}{"tag": "hr"})
		elements = append(elements, map[string]interface{}{
			"tag":  "div",
			"text": feishuMarkdown(fmt.Sprintf("⚠️ 以下配置的渠道 ID 在上游已不存在：%v", missingIDs)),
		})
	}

	if len(enabledFailed) == 0 && len(disabledSuccess) == 0 && len(enabledSlow) == 0 {
		elements = append(elements, map[string]interface{}{"tag": "hr"})
		elements = append(elements, map[string]interface{}{
			"tag":  "div",
			"text": feishuMarkdown("✅ 所有渠道状态正常，无需调整"),
		})
	}

	return map[string]interface{}{
		"config": map[string]interface{}{"wide_screen_mode": true},
		"header": map[string]interface{}{
			"template": template,
			"title":    map[string]string{"tag": "plain_text", "content": title},
		},
		"elements": elements,
	}
}

func buildGlobalAvailabilityNotifyCard(sites []globalSiteNotifyResult) map[string]interface{} {
	template := "green"
	totalTested := 0
	totalSuccess := 0
	for _, site := range sites {
		totalTested += len(site.All)
		totalSuccess += site.SuccessCount
		if len(site.EnabledFailed) > 0 {
			template = "red"
		} else if len(site.EnabledSlow) > 0 && template != "red" {
			template = "orange"
		}
	}

	elements := []interface{}{
		map[string]interface{}{
			"tag": "div",
			"fields": []interface{}{
				feishuField("站点数", strconv.Itoa(len(sites))),
				feishuField("测试时间", time.Now().Format("01-02 15:04")),
				feishuField("渠道总数", strconv.Itoa(totalTested)),
				feishuField("总成功/失败", fmt.Sprintf("<font color='green'>%d</font> / <font color='red'>%d</font>", totalSuccess, totalTested-totalSuccess)),
			},
		},
	}

	for _, site := range sites {
		elements = append(elements, map[string]interface{}{"tag": "hr"})

		successCount := site.SuccessCount
		totalCount := len(site.All)
		failCount := totalCount - successCount

		displayName := site.SiteName
		if site.GroupName != "" {
			displayName = site.SiteName + " / " + site.GroupName
		}
		siteHeader := fmt.Sprintf("📍 **%s** — 测试 %d 个，成功 %d，失败 %d", displayName, totalCount, successCount, failCount)
		elements = append(elements, map[string]interface{}{
			"tag":  "div",
			"text": feishuMarkdown(siteHeader),
		})

		toggleMap := make(map[int]bool, len(site.ToggleResults))
		for _, tr := range site.ToggleResults {
			toggleMap[tr.ChannelID] = tr.Success
		}

		if len(site.EnabledFailed) > 0 {
			elements = append(elements, map[string]interface{}{
				"tag":  "div",
				"text": feishuMarkdown(fmt.Sprintf("🔴 **启用但失败（%d个）**", len(site.EnabledFailed))),
			})
			for _, r := range site.EnabledFailed {
				elements = append(elements, map[string]interface{}{
					"tag":  "div",
					"text": feishuMarkdown(buildChannelLine(r, site.AutoToggle, toggleMap)),
				})
			}
		}

		if len(site.DisabledSuccess) > 0 {
			elements = append(elements, map[string]interface{}{
				"tag":  "div",
				"text": feishuMarkdown(fmt.Sprintf("🟢 **禁用但通过（%d个）**", len(site.DisabledSuccess))),
			})
			for _, r := range site.DisabledSuccess {
				elements = append(elements, map[string]interface{}{
					"tag":  "div",
					"text": feishuMarkdown(buildChannelLine(r, site.AutoToggle, toggleMap)),
				})
			}
		}

		if len(site.EnabledSlow) > 0 {
			elements = append(elements, map[string]interface{}{
				"tag":  "div",
				"text": feishuMarkdown(fmt.Sprintf("🟡 **响应超时（%d个，阈值 %dms）**", len(site.EnabledSlow), site.SlowThresholdMs)),
			})
			for _, r := range site.EnabledSlow {
				elements = append(elements, map[string]interface{}{
					"tag":  "div",
					"text": feishuMarkdown(buildSlowChannelLine(r, site.AutoToggle, toggleMap)),
				})
			}
		}

		if len(site.MissingIDs) > 0 {
			elements = append(elements, map[string]interface{}{
				"tag":  "div",
				"text": feishuMarkdown(fmt.Sprintf("⚠️ 渠道 ID 已不存在：%v", site.MissingIDs)),
			})
		}

		if len(site.EnabledFailed) == 0 && len(site.DisabledSuccess) == 0 && len(site.EnabledSlow) == 0 {
			elements = append(elements, map[string]interface{}{
				"tag":  "div",
				"text": feishuMarkdown("✅ 所有渠道运行正常"),
			})
		}
	}

	return map[string]interface{}{
		"config": map[string]interface{}{"wide_screen_mode": true},
		"header": map[string]interface{}{
			"template": template,
			"title":    map[string]string{"tag": "plain_text", "content": "渠道可用性测试通知 - 全局汇总"},
		},
		"elements": elements,
	}
}

func autoToggleChannels(ctx context.Context, enabledFailed, disabledSuccess []channelTestResult, cred availabilityCredentials) []channelStatusChangeResult {
	var results []channelStatusChangeResult
	for _, r := range enabledFailed {
		tr := updateOneChannelStatus(ctx, r.ChannelID, r.Name, 2, cred)
		results = append(results, tr)
	}
	for _, r := range disabledSuccess {
		tr := updateOneChannelStatus(ctx, r.ChannelID, r.Name, 1, cred)
		results = append(results, tr)
	}
	return results
}

func writeModelResultDetails(b *strings.Builder, r channelTestResult) {
	if len(r.ModelResults) <= 1 {
		return
	}
	for _, mr := range r.ModelResults {
		if mr.Success {
			b.WriteString(fmt.Sprintf("      ✅ %s | 响应: %dms\n", mr.Model, mr.ResponseTime))
		} else {
			errMsg := mr.Error
			if len(errMsg) > 60 {
				errMsg = errMsg[:60] + "..."
			}
			b.WriteString(fmt.Sprintf("      ❌ %s | 错误: %s\n", mr.Model, errMsg))
		}
	}
}

func buildAvailabilityNotifyMessage(siteName string, allResults []channelTestResult, enabledFailed, disabledSuccess, enabledSlow []channelTestResult, missingIDs []int, autoToggle bool, slowThresholdMs int, toggleResults []channelStatusChangeResult) string {
	var b strings.Builder
	successCount := 0
	for _, r := range allResults {
		if r.Success {
			successCount++
		}
	}
	if siteName != "" {
		b.WriteString(fmt.Sprintf("【渠道可用性测试通知 - %s】\n", siteName))
	} else {
		b.WriteString("【渠道可用性测试通知】\n")
	}
	b.WriteString(fmt.Sprintf("共测试 %d 个渠道，成功 %d，失败 %d\n", len(allResults), successCount, len(allResults)-successCount))

	toggleResultMap := make(map[int]bool, len(toggleResults))
	for _, tr := range toggleResults {
		toggleResultMap[tr.ChannelID] = tr.Success
	}

	if len(disabledSuccess) > 0 {
		b.WriteString(fmt.Sprintf("\n🟢 以下已禁用渠道测试通过（%d个）：\n", len(disabledSuccess)))
		for _, r := range disabledSuccess {
			action := "建议：可启用"
			if autoToggle {
				if toggleResultMap[r.ChannelID] {
					action = "已自动启用 ✅"
				} else {
					action = "自动启用失败 ❗"
				}
			}
			b.WriteString(fmt.Sprintf("  ✅ %s (ID:%d) | 测试模型: %s | 响应: %dms | %s\n", r.Name, r.ChannelID, r.TestModel, r.ResponseTime, action))
			writeModelResultDetails(&b, r)
		}
	}

	if len(enabledFailed) > 0 {
		b.WriteString(fmt.Sprintf("\n🔴 以下已启用渠道测试失败（%d个）：\n", len(enabledFailed)))
		for _, r := range enabledFailed {
			errMsg := r.Error
			if len(r.ModelResults) == 0 && len(errMsg) > 80 {
				errMsg = errMsg[:80] + "..."
			}
			action := "建议：临时停用"
			if autoToggle {
				if toggleResultMap[r.ChannelID] {
					action = "已自动停用 ✅"
				} else {
					action = "自动停用失败 ❗"
				}
			}
			if len(r.ModelResults) > 0 {
				b.WriteString(fmt.Sprintf("  ❌ %s (ID:%d) | 测试模型: %s | %s\n", r.Name, r.ChannelID, r.TestModel, action))
			} else {
				b.WriteString(fmt.Sprintf("  ❌ %s (ID:%d) | 测试模型: %s | 错误: %s | %s\n", r.Name, r.ChannelID, r.TestModel, errMsg, action))
			}
			writeModelResultDetails(&b, r)
		}
	}

	if len(enabledSlow) > 0 {
		b.WriteString(fmt.Sprintf("\n🟡 以下已启用渠道响应超时（%d个，阈值 %dms）：\n", len(enabledSlow), slowThresholdMs))
		for _, r := range enabledSlow {
			action := "建议：临时停用"
			if autoToggle {
				if toggleResultMap[r.ChannelID] {
					action = "已自动停用 ✅"
				} else {
					action = "自动停用失败 ❗"
				}
			}
			b.WriteString(fmt.Sprintf("  ⏱️ %s (ID:%d) | 测试模型: %s | 响应: %dms | %s\n", r.Name, r.ChannelID, r.TestModel, r.ResponseTime, action))
			writeModelResultDetails(&b, r)
		}
	}

	if len(missingIDs) > 0 {
		b.WriteString(fmt.Sprintf("\n⚠️ 以下配置的渠道 ID 在上游已不存在：%v\n", missingIDs))
	}

	if len(enabledFailed) == 0 && len(disabledSuccess) == 0 && len(enabledSlow) == 0 {
		b.WriteString("\n所有渠道状态正常，无需调整。\n")
	}

	return b.String()
}

func StartChannelAvailabilityScheduler() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			runScheduledChannelAvailabilityNotify()
		}
	}()
}

func runScheduledChannelAvailabilityNotify() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	globalConfig, globalFound, _ := loadChannelAvailabilityGlobalNotifyConfig(ctx)

	cursor, err := ChannelAvailabilityNotifyCol.Find(ctx, bson.M{"enabled": true})
	if err != nil {
		return
	}
	defer cursor.Close(ctx)

	cleanTestResults(ctx, "")

	type globalGroupEntry struct {
		config     models.ChannelAvailabilityNotifyConfig
		groupIndex int
	}
	var globalScheduleGroups []globalGroupEntry

	for cursor.Next(ctx) {
		var config models.ChannelAvailabilityNotifyConfig
		if err := cursor.Decode(&config); err != nil {
			continue
		}
		if config.UpstreamSiteID.IsZero() {
			continue
		}
		if len(config.MonitoringGroups) == 0 && len(config.ChannelIDs) > 0 {
			config.MonitoringGroups = []models.MonitoringGroup{{
				Name:            "默认组",
				ChannelIDs:      config.ChannelIDs,
				StatusFilter:    config.StatusFilter,
				AutoToggle:      config.AutoToggle,
				SlowThresholdMs: config.SlowThresholdMs,
				Schedules:       config.Schedules,
				LastAttemptAt:   config.LastAttemptAt,
			}}
		}
		for gi, group := range config.MonitoringGroups {
			if len(group.ChannelIDs) == 0 {
				continue
			}
			if len(group.Schedules) > 0 {
				runScheduledNotifyForGroup(ctx, config, group, gi)
			} else if globalFound && len(globalConfig.Schedules) > 0 {
				globalScheduleGroups = append(globalScheduleGroups, globalGroupEntry{config: config, groupIndex: gi})
			}
		}
	}

	if len(globalScheduleGroups) == 0 {
		return
	}

	now := time.Now()
	schedule, scheduleStart, ok := activeNotificationSchedule(globalConfig.Schedules, now)
	if !ok {
		return
	}

	var siteNotifyResults []globalSiteNotifyResult
	for _, entry := range globalScheduleGroups {
		group := entry.config.MonitoringGroups[entry.groupIndex]
		if group.LastAttemptAt != nil {
			if group.LastAttemptAt.After(scheduleStart) || group.LastAttemptAt.Equal(scheduleStart) {
				elapsed := now.Sub(*group.LastAttemptAt)
				if elapsed < time.Duration(schedule.IntervalMinutes)*time.Minute {
					continue
				}
			}
		}

		result := runAndCollectGroupResult(ctx, entry.config, group, globalConfig.AlwaysNotify)
		if result != nil {
			siteNotifyResults = append(siteNotifyResults, *result)
		}
		nowTs := time.Now()
		_, _ = ChannelAvailabilityNotifyCol.UpdateOne(ctx, bson.M{"_id": entry.config.ID}, bson.M{"$set": bson.M{
			fmt.Sprintf("monitoring_groups.%d.last_attempt_at", entry.groupIndex): nowTs,
		}})
	}

	if len(siteNotifyResults) > 0 {
		card := buildGlobalAvailabilityNotifyCard(siteNotifyResults)
		msg := buildGlobalAvailabilityNotifyMessage(siteNotifyResults)
		nType, webhookURL, signKey, resolveErr := resolveGlobalNotifyWebhook(ctx)
		if resolveErr == nil {
			if err := sendAvailabilityCardNotification(ctx, nType, webhookURL, signKey, card, msg); err != nil {
				log.Printf("[channel-availability-scheduler] send combined notification error: %v", err)
			}
		}
	}
}

func resolveGroupSkipStatusCodes(group models.MonitoringGroup, cred availabilityCredentials) []int {
	if len(group.SkipStatusCodes) > 0 {
		return group.SkipStatusCodes
	}
	return cred.SkipStatusCodes
}

func runAndCollectGroupResult(ctx context.Context, config models.ChannelAvailabilityNotifyConfig, group models.MonitoringGroup, alwaysNotify bool) *globalSiteNotifyResult {
	siteID := config.UpstreamSiteID
	siteName := ""
	if site, sErr := loadUpstreamSite(ctx, siteID); sErr == nil {
		siteName = site.Name
	}
	if siteName == "" {
		siteName = siteID.Hex()
	}

	cred, credErr := resolveCredentialsFromSite(ctx, siteID)
	if credErr != nil {
		return nil
	}

	shouldRefresh := config.RefreshChannels == nil || *config.RefreshChannels
	if shouldRefresh {
		channelURL := cred.BaseURL + "/api/channel/"
		freshItems, fetchErr := fetchAllUpstreamChannelItems(ctx, channelURL, cred.Token, cred.UserID)
		if fetchErr != nil {
			log.Printf("[channel-availability-scheduler] site %s group %s fetch channels error: %v", siteID.Hex(), group.Name, fetchErr)
			return nil
		}
		if err := saveUpstreamChannels(ctx, freshItems, siteID); err != nil {
			log.Printf("[channel-availability-scheduler] site %s group %s save channels error: %v", siteID.Hex(), group.Name, err)
			return nil
		}
	}

	storedChannels, err := listUpstreamChannels(ctx, siteID)
	if err != nil {
		return nil
	}
	upstreamIDSet := make(map[int]bool, len(storedChannels))
	for _, ch := range storedChannels {
		upstreamIDSet[ch.ChannelID] = true
	}
	var missingIDs []int
	var validIDs []int
	for _, id := range group.ChannelIDs {
		if upstreamIDSet[id] {
			validIDs = append(validIDs, id)
		} else {
			missingIDs = append(missingIDs, id)
		}
	}

	filter := bson.M{"channelId": bson.M{"$in": validIDs}, "upstream_site_id": siteID}
	if group.StatusFilter == 1 {
		filter["status"] = 1
	} else if group.StatusFilter == 2 {
		filter["status"] = 2
	}
	channelCursor, err := UpstreamChannelCol.Find(ctx, filter)
	if err != nil {
		return nil
	}
	defer channelCursor.Close(ctx)
	var targetChannels []models.UpstreamChannel
	if err := channelCursor.All(ctx, &targetChannels); err != nil {
		return nil
	}
	if len(targetChannels) == 0 {
		return nil
	}

	skipCodes := resolveGroupSkipStatusCodes(group, cred)
	runID := fmt.Sprintf("sched-%d-%s", time.Now().UnixNano(), siteID.Hex())
	if err := batchTestChannels(ctx, targetChannels, cred.BaseURL, cred.Token, cred.UserID, "", runID, skipCodes, group.AnyModelSuccess); err != nil {
		log.Printf("[channel-availability-scheduler] site %s group %s write test results error: %v", siteID.Hex(), group.Name, err)
		return nil
	}

	classified, err := classifyTestResults(ctx, runID, group.SlowThresholdMs)
	if err != nil {
		log.Printf("[channel-availability-scheduler] site %s group %s classify test results error: %v", siteID.Hex(), group.Name, err)
		cleanTestResults(ctx, runID)
		return nil
	}

	enabledFailed := classified.EnabledFailed
	disabledSuccess := classified.DisabledSuccess
	enabledSlow := classified.EnabledSlow

	var toggleResults []channelStatusChangeResult
	if group.AutoToggle && (len(enabledFailed) > 0 || len(disabledSuccess) > 0 || len(enabledSlow) > 0) {
		toggleResults = autoToggleChannels(ctx, enabledFailed, disabledSuccess, cred)
		for _, r := range enabledSlow {
			tr := updateOneChannelStatus(ctx, r.ChannelID, r.Name, 2, cred)
			toggleResults = append(toggleResults, tr)
		}
	}

	cleanTestResults(ctx, runID)

	if !alwaysNotify && len(enabledFailed) == 0 && len(disabledSuccess) == 0 && len(enabledSlow) == 0 {
		return nil
	}

	return &globalSiteNotifyResult{
		SiteName:        siteName,
		GroupName:       group.Name,
		Config:          config,
		AutoToggle:      group.AutoToggle,
		SlowThresholdMs: group.SlowThresholdMs,
		All:             classified.All,
		EnabledFailed:   enabledFailed,
		DisabledSuccess: disabledSuccess,
		EnabledSlow:     enabledSlow,
		MissingIDs:      missingIDs,
		ToggleResults:   toggleResults,
		SuccessCount:    classified.SuccessCount,
	}
}

func runScheduledNotifyForGroup(ctx context.Context, config models.ChannelAvailabilityNotifyConfig, group models.MonitoringGroup, groupIndex int) {
	now := time.Now()
	schedule, scheduleStart, ok := activeNotificationSchedule(group.Schedules, now)
	if !ok {
		return
	}
	if group.LastAttemptAt != nil {
		if group.LastAttemptAt.After(scheduleStart) || group.LastAttemptAt.Equal(scheduleStart) {
			elapsed := now.Sub(*group.LastAttemptAt)
			if elapsed < time.Duration(schedule.IntervalMinutes)*time.Minute {
				return
			}
		}
	}

	siteID := config.UpstreamSiteID
	nType, webhookURL, signKey, resolveErr := resolveNotifyWebhook(ctx, config)
	if resolveErr != nil {
		return
	}
	cred, credErr := resolveCredentialsFromSite(ctx, siteID)
	if credErr != nil {
		return
	}
	siteName := ""
	if site, sErr := loadUpstreamSite(ctx, siteID); sErr == nil {
		siteName = site.Name
	}

	shouldRefresh := config.RefreshChannels == nil || *config.RefreshChannels
	if shouldRefresh {
		channelURL := cred.BaseURL + "/api/channel/"
		freshItems, fetchErr := fetchAllUpstreamChannelItems(ctx, channelURL, cred.Token, cred.UserID)
		if fetchErr != nil {
			log.Printf("[channel-availability-scheduler] site %s group %s fetch channels error: %v", siteID.Hex(), group.Name, fetchErr)
			return
		}
		if err := saveUpstreamChannels(ctx, freshItems, siteID); err != nil {
			log.Printf("[channel-availability-scheduler] site %s group %s save channels error: %v", siteID.Hex(), group.Name, err)
			return
		}
	}

	storedChannels, err := listUpstreamChannels(ctx, siteID)
	if err != nil {
		return
	}
	upstreamIDSet := make(map[int]bool, len(storedChannels))
	for _, ch := range storedChannels {
		upstreamIDSet[ch.ChannelID] = true
	}
	var missingIDs []int
	var validIDs []int
	for _, id := range group.ChannelIDs {
		if upstreamIDSet[id] {
			validIDs = append(validIDs, id)
		} else {
			missingIDs = append(missingIDs, id)
		}
	}

	filter := bson.M{"channelId": bson.M{"$in": validIDs}, "upstream_site_id": siteID}
	if group.StatusFilter == 1 {
		filter["status"] = 1
	} else if group.StatusFilter == 2 {
		filter["status"] = 2
	}
	channelCursor, err := UpstreamChannelCol.Find(ctx, filter)
	if err != nil {
		return
	}
	defer channelCursor.Close(ctx)
	var targetChannels []models.UpstreamChannel
	if err := channelCursor.All(ctx, &targetChannels); err != nil {
		return
	}
	if len(targetChannels) == 0 {
		return
	}

	displayName := siteName
	if group.Name != "" {
		displayName = siteName + " / " + group.Name
	}

	skipCodes := resolveGroupSkipStatusCodes(group, cred)
	runID := fmt.Sprintf("sched-%d", time.Now().UnixNano())
	if err := batchTestChannels(ctx, targetChannels, cred.BaseURL, cred.Token, cred.UserID, "", runID, skipCodes, group.AnyModelSuccess); err != nil {
		log.Printf("[channel-availability-scheduler] site %s group %s write test results error: %v", siteID.Hex(), group.Name, err)
		return
	}

	classified, err := classifyTestResults(ctx, runID, group.SlowThresholdMs)
	if err != nil {
		log.Printf("[channel-availability-scheduler] site %s group %s classify test results error: %v", siteID.Hex(), group.Name, err)
		return
	}

	enabledFailed := classified.EnabledFailed
	disabledSuccess := classified.DisabledSuccess
	enabledSlow := classified.EnabledSlow

	var toggleResults []channelStatusChangeResult
	if group.AutoToggle && (len(enabledFailed) > 0 || len(disabledSuccess) > 0 || len(enabledSlow) > 0) {
		toggleResults = autoToggleChannels(ctx, enabledFailed, disabledSuccess, cred)
		for _, r := range enabledSlow {
			tr := updateOneChannelStatus(ctx, r.ChannelID, r.Name, 2, cred)
			toggleResults = append(toggleResults, tr)
		}
	}

	if len(enabledFailed) > 0 || len(disabledSuccess) > 0 || len(enabledSlow) > 0 {
		card := buildAvailabilityNotifyCard(displayName, classified.All, enabledFailed, disabledSuccess, enabledSlow, missingIDs, group.AutoToggle, group.SlowThresholdMs, toggleResults)
		msg := buildAvailabilityNotifyMessage(displayName, classified.All, enabledFailed, disabledSuccess, enabledSlow, missingIDs, group.AutoToggle, group.SlowThresholdMs, toggleResults)
		if err := sendAvailabilityCardNotification(ctx, nType, webhookURL, signKey, card, msg); err != nil {
			log.Printf("[channel-availability-scheduler] site %s group %s send notification error: %v", siteID.Hex(), group.Name, err)
		}
	}

	cleanTestResults(ctx, runID)

	nowTs := time.Now()
	_, _ = ChannelAvailabilityNotifyCol.UpdateOne(ctx, bson.M{"_id": config.ID}, bson.M{"$set": bson.M{
		fmt.Sprintf("monitoring_groups.%d.last_attempt_at", groupIndex): nowTs,
	}})
}
