package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"balanceserver/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	codexUsageURL       = "https://chatgpt.com/backend-api/wham/usage"
	codexDefaultTimeout = 30
	codexUserAgent      = "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal"
	codexMaxWorkers     = 10
)

type codexAuthFile struct {
	AuthIndex string `json:"auth_index"`
	Status    string `json:"status"`
	Disabled  bool   `json:"disabled"`
}

type codexRateWindow struct {
	UsedPercent        float64 `json:"usedPercent"`
	RemainingPercent   float64 `json:"remainingPercent"`
	LimitWindowSeconds int     `json:"limitWindowSeconds"`
	ResetAfterSeconds  int     `json:"resetAfterSeconds"`
	ResetAt            string  `json:"resetAt"`
}

type codexAccountUsage struct {
	AuthIndex  string           `json:"authIndex"`
	AuthStatus string           `json:"authStatus"`
	Disabled   bool             `json:"disabled"`
	PlanType   string           `json:"planType"`
	Primary    *codexRateWindow `json:"primary"`
	Secondary  *codexRateWindow `json:"secondary"`
	Error      string           `json:"error,omitempty"`
}

func GetCodexConfigHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var config models.CodexConfig
	err := CodexConfigCol.FindOne(ctx, bson.M{"_id": "default"}).Decode(&config)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"baseUrl":    "",
			"adminToken": "",
			"timeout":    codexDefaultTimeout,
		})
		return
	}

	c.JSON(http.StatusOK, config)
}

func SaveCodexConfigHandler(c *gin.Context) {
	var req struct {
		BaseURL    string `json:"baseUrl"`
		AdminToken string `json:"adminToken"`
		Timeout    int    `json:"timeout"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if req.BaseURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "baseUrl is required"})
		return
	}

	if req.Timeout <= 0 {
		req.Timeout = codexDefaultTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := models.CodexConfig{
		ID:         "default",
		BaseURL:    req.BaseURL,
		AdminToken: req.AdminToken,
		Timeout:    req.Timeout,
		UpdatedAt:  time.Now(),
	}

	opts := options.Replace().SetUpsert(true)
	_, err := CodexConfigCol.ReplaceOne(ctx, bson.M{"_id": "default"}, config, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save config"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func QueryCodexBalanceHandler(c *gin.Context) {
	var req struct {
		IncludeDisabled bool `json:"includeDisabled"`
	}
	c.ShouldBindJSON(&req)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var config models.CodexConfig
	err := CodexConfigCol.FindOne(ctx, bson.M{"_id": "default"}).Decode(&config)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先配置Codex服务信息"})
		return
	}

	timeout := time.Duration(config.Timeout) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(codexDefaultTimeout) * time.Second
	}

	authFiles, err := fetchCodexAuthFiles(config.BaseURL, config.AdminToken, timeout)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("获取账号列表失败: %v", err)})
		return
	}

	if !req.IncludeDisabled {
		filtered := make([]codexAuthFile, 0, len(authFiles))
		for _, f := range authFiles {
			if !f.Disabled {
				filtered = append(filtered, f)
			}
		}
		authFiles = filtered
	}

	if len(authFiles) == 0 {
		c.JSON(http.StatusOK, gin.H{"accounts": []codexAccountUsage{}, "total": 0})
		return
	}

	results := fetchCodexUsageConcurrent(authFiles, config.BaseURL, config.AdminToken, timeout)

	successCount := 0
	for _, r := range results {
		if r.Error == "" {
			successCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"accounts":     results,
		"total":        len(results),
		"successCount": successCount,
	})
}

func fetchCodexAuthFiles(baseURL, adminToken string, timeout time.Duration) ([]codexAuthFile, error) {
	url := fmt.Sprintf("%s/v0/management/auth-files", baseURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var payload struct {
		Files []struct {
			AuthIndex string `json:"auth_index"`
			Status    string `json:"status"`
			Disabled  bool   `json:"disabled"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	result := make([]codexAuthFile, 0, len(payload.Files))
	for _, f := range payload.Files {
		if f.AuthIndex == "" {
			continue
		}
		result = append(result, codexAuthFile{
			AuthIndex: f.AuthIndex,
			Status:    f.Status,
			Disabled:  f.Disabled,
		})
	}
	return result, nil
}

func fetchCodexUsageConcurrent(authFiles []codexAuthFile, baseURL, adminToken string, timeout time.Duration) []codexAccountUsage {
	results := make([]codexAccountUsage, len(authFiles))
	sem := make(chan struct{}, codexMaxWorkers)
	var wg sync.WaitGroup

	for i, af := range authFiles {
		wg.Add(1)
		go func(idx int, file codexAuthFile) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = fetchSingleCodexUsage(file, baseURL, adminToken, timeout)
		}(i, af)
	}

	wg.Wait()
	return results
}

func fetchSingleCodexUsage(af codexAuthFile, baseURL, adminToken string, timeout time.Duration) codexAccountUsage {
	usage := codexAccountUsage{
		AuthIndex:  af.AuthIndex,
		AuthStatus: af.Status,
		Disabled:   af.Disabled,
	}

	reqBody := map[string]interface{}{
		"authIndex": af.AuthIndex,
		"method":    "GET",
		"url":       codexUsageURL,
		"header": map[string]string{
			"Authorization": "Bearer $TOKEN$",
			"Content-Type":  "application/json",
			"User-Agent":    codexUserAgent,
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("%s/v0/management/api-call", baseURL)
	req, err := http.NewRequest("POST", url, io.NopCloser(
		io.Reader(jsonReader(bodyBytes)),
	))
	if err != nil {
		usage.Error = err.Error()
		return usage
	}
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		usage.Error = fmt.Sprintf("请求失败: %v", err)
		return usage
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		usage.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
		return usage
	}

	var proxyResp struct {
		StatusCode int             `json:"status_code"`
		Body       json.RawMessage `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&proxyResp); err != nil {
		usage.Error = fmt.Sprintf("解析代理响应失败: %v", err)
		return usage
	}

	if proxyResp.StatusCode != 200 {
		usage.Error = fmt.Sprintf("上游返回 %d: %s", proxyResp.StatusCode, truncate(string(proxyResp.Body), 200))
		return usage
	}

	var usageBody string
	if err := json.Unmarshal(proxyResp.Body, &usageBody); err == nil {
		// body is a JSON string, need to parse again
		var usagePayload map[string]interface{}
		if err := json.Unmarshal([]byte(usageBody), &usagePayload); err != nil {
			usage.Error = fmt.Sprintf("解析用量数据失败: %v", err)
			return usage
		}
		parseCodexUsagePayload(&usage, usagePayload)
	} else {
		// body is already a JSON object
		var usagePayload map[string]interface{}
		if err := json.Unmarshal(proxyResp.Body, &usagePayload); err != nil {
			usage.Error = fmt.Sprintf("解析用量数据失败: %v", err)
			return usage
		}
		parseCodexUsagePayload(&usage, usagePayload)
	}

	return usage
}

func parseCodexUsagePayload(usage *codexAccountUsage, payload map[string]interface{}) {
	if pt, ok := payload["plan_type"]; ok && pt != nil {
		usage.PlanType = fmt.Sprintf("%v", pt)
	}

	rateLimit, ok := payload["rate_limit"].(map[string]interface{})
	if !ok {
		return
	}

	now := time.Now()
	if pw, ok := rateLimit["primary_window"].(map[string]interface{}); ok {
		usage.Primary = parseCodexRateWindow(pw, now)
	}
	if sw, ok := rateLimit["secondary_window"].(map[string]interface{}); ok {
		usage.Secondary = parseCodexRateWindow(sw, now)
	}
}

func parseCodexRateWindow(raw map[string]interface{}, now time.Time) *codexRateWindow {
	usedPercent := toFloat64(raw["used_percent"])
	if usedPercent < 0 {
		usedPercent = 0
	}
	if usedPercent > 100 {
		usedPercent = 100
	}

	limitWindowSeconds := toInt(raw["limit_window_seconds"])
	resetAfterSeconds := toInt(raw["reset_after_seconds"])
	if resetAfterSeconds < 0 {
		resetAfterSeconds = 0
	}

	resetAt := now.Add(time.Duration(resetAfterSeconds) * time.Second)

	return &codexRateWindow{
		UsedPercent:        usedPercent,
		RemainingPercent:   max(0, 100-usedPercent),
		LimitWindowSeconds: limitWindowSeconds,
		ResetAfterSeconds:  resetAfterSeconds,
		ResetAt:            resetAt.Format("2006-01-02 15:04:05"),
	}
}

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case json.Number:
		f, _ := val.Float64()
		return f
	default:
		return 0
	}
}

func toInt(v interface{}) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case int64:
		return int(val)
	case json.Number:
		i, _ := val.Int64()
		return int(i)
	default:
		return 0
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

type bytesReader struct {
	data []byte
	pos  int
}

func (r *bytesReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func jsonReader(data []byte) io.Reader {
	return &bytesReader{data: data}
}
