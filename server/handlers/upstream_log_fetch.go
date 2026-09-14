package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const logStatsPageSize = 100

var logStatsHTTPClient = &http.Client{
	Timeout: 2 * time.Minute,
	Transport: &http.Transport{
		DisableKeepAlives: true,
	},
}

type upstreamLogResponse struct {
	Data struct {
		Items      []upstreamLogItem `json:"items"`
		TotalCount int               `json:"total_count"`
		Total      int               `json:"total"`
	} `json:"data"`
	Success bool `json:"success"`
}

type upstreamLogItem struct {
	ID               int64  `json:"id"`
	Type             int    `json:"type"`
	CreatedAt        int64  `json:"created_at"`
	Username         string `json:"username"`
	TokenName        string `json:"token_name"`
	ModelName        string `json:"model_name"`
	Quota            int64  `json:"quota"`
	Channel          int    `json:"channel"`
	ChannelName      string `json:"channel_name"`
	Group            string `json:"group"`
	Content          string `json:"content"`
	UseTime          int    `json:"use_time"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	RequestID        string `json:"request_id"`
	IsStream         bool   `json:"is_stream"`
}

func fetchLogPage(ctx context.Context, baseURL, token, userID string,
	page, pageSize int, startTS, endTS, group string, logType ...int) ([]upstreamLogItem, error) {

	targetURL := fmt.Sprintf("%s/api/log/?p=%d&page_size=%d&start_timestamp=%s&end_timestamp=%s",
		baseURL, page, pageSize, startTS, endTS)
	if len(logType) > 0 && logType[0] > 0 {
		targetURL += "&type=" + strconv.Itoa(logType[0])
	}
	if group != "" {
		targetURL += "&group=" + url.QueryEscape(group)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
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
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var result upstreamLogResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("json parse: %w", err)
	}

	return result.Data.Items, nil
}
