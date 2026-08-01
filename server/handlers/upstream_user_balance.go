package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	userBalanceStatsTimeout = 5 * time.Minute
	userBalancePageSize     = 100
	quotaPerUnit            = 500000.0
)

type upstreamUserItem struct {
	ID              int    `json:"id"`
	Username        string `json:"username"`
	DisplayName     string `json:"display_name"`
	Email           string `json:"email"`
	Role            int    `json:"role"`
	Status          int    `json:"status"`
	Quota           int64  `json:"quota"`
	UsedQuota       int64  `json:"used_quota"`
	RequestCount    int    `json:"request_count"`
	Group           string `json:"group"`
	AffCode         string `json:"aff_code"`
	AffCount        int    `json:"aff_count"`
	AffQuota        int64  `json:"aff_quota"`
	AffHistoryQuota int64  `json:"aff_history_quota"`
	InviterID       int    `json:"inviter_id"`
	CreatedAt       int64  `json:"created_at"`
	LastLoginAt     int64  `json:"last_login_at"`
}

type upstreamUserListResponse struct {
	Data struct {
		Items []upstreamUserItem `json:"items"`
		Total int                `json:"total"`
	} `json:"data"`
	Success bool `json:"success"`
}

type UserBalanceItem struct {
	ID           int     `json:"id"`
	Username     string  `json:"username"`
	DisplayName  string  `json:"displayName"`
	Email        string  `json:"email"`
	Role         int     `json:"role"`
	Status       int     `json:"status"`
	Group        string  `json:"group"`
	Balance      float64 `json:"balance"`
	UsedBalance  float64 `json:"usedBalance"`
	RequestCount int     `json:"requestCount"`
	AffCode      string  `json:"affCode"`
	AffCount     int     `json:"affCount"`
	AffBalance   float64 `json:"affBalance"`
	InviterID    int     `json:"inviterId"`
	CreatedAt    int64   `json:"createdAt"`
	LastLoginAt  int64   `json:"lastLoginAt"`
}

type userBalanceStatsResponse struct {
	SiteName             string            `json:"siteName"`
	TotalUsers           int               `json:"totalUsers"`
	TotalBalance         float64           `json:"totalBalance"`
	TotalUsedBalance     float64           `json:"totalUsedBalance"`
	PositiveBalanceUsers int               `json:"positiveBalanceUsers"`
	Users                []UserBalanceItem `json:"users"`
	ElapsedMs            int64             `json:"elapsedMs"`
}

func fetchUpstreamUserPage(ctx context.Context, pageURL, token, userID string) ([]upstreamUserItem, error) {
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
		return nil, fmt.Errorf("请求上游用户列表失败: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("读取上游用户响应失败: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("上游返回 HTTP %d: %s", response.StatusCode, compactBody(body))
	}

	var payload upstreamUserListResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析上游用户响应失败: %w", err)
	}
	return payload.Data.Items, nil
}

func fetchAllUpstreamUsers(ctx context.Context, baseURL, token, userID string) ([]upstreamUserItem, error) {
	all := make([]upstreamUserItem, 0, 200)

	for page := 1; page < 2000; page++ {
		pageURL := fmt.Sprintf("%s/api/user/?p=%d&page_size=%d", baseURL, page, userBalancePageSize)
		items, err := fetchUpstreamUserPage(ctx, pageURL, token, userID)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if len(items) == 0 || len(items) < userBalancePageSize {
			break
		}
	}

	return all, nil
}

func QueryUserBalanceStatsHandler(c *gin.Context) {
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

	ctx, cancel := context.WithTimeout(context.Background(), userBalanceStatsTimeout)
	defer cancel()

	cred, err := resolveCredentialsFromSite(ctx, siteID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	site, _ := loadUpstreamSite(ctx, siteID)

	start := time.Now()
	items, err := fetchAllUpstreamUsers(ctx, cred.BaseURL, cred.Token, cred.UserID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	resp := userBalanceStatsResponse{
		SiteName: site.Name,
		Users:    make([]UserBalanceItem, 0, len(items)),
	}

	for _, item := range items {
		if item.Status != 1 {
			continue
		}

		balance := float64(item.Quota) / quotaPerUnit
		usedBalance := float64(item.UsedQuota) / quotaPerUnit
		affBalance := float64(item.AffQuota) / quotaPerUnit

		resp.Users = append(resp.Users, UserBalanceItem{
			ID:           item.ID,
			Username:     item.Username,
			DisplayName:  item.DisplayName,
			Email:        item.Email,
			Role:         item.Role,
			Status:       item.Status,
			Group:        item.Group,
			Balance:      balance,
			UsedBalance:  usedBalance,
			RequestCount: item.RequestCount,
			AffCode:      item.AffCode,
			AffCount:     item.AffCount,
			AffBalance:   affBalance,
			InviterID:    item.InviterID,
			CreatedAt:    item.CreatedAt,
			LastLoginAt:  item.LastLoginAt,
		})

		resp.TotalBalance += balance
		resp.TotalUsedBalance += usedBalance
		if item.Quota > 0 {
			resp.PositiveBalanceUsers++
		}
	}
	resp.TotalUsers = len(resp.Users)

	sort.Slice(resp.Users, func(i, j int) bool {
		return resp.Users[i].Balance > resp.Users[j].Balance
	})

	resp.ElapsedMs = time.Since(start).Milliseconds()

	c.JSON(http.StatusOK, resp)
}
