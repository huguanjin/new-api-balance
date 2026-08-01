package handlers

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"balanceserver/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func loadKeyCustomerConfig(ctx context.Context, siteID primitive.ObjectID) (models.KeyCustomerConfig, error) {
	var config models.KeyCustomerConfig
	err := KeyCustomerConfigCol.FindOne(ctx, bson.M{"upstream_site_id": siteID}).Decode(&config)
	if err == mongo.ErrNoDocuments {
		return models.KeyCustomerConfig{UserIDs: []int{}}, nil
	}
	if err != nil {
		return config, err
	}
	if config.UserIDs == nil {
		config.UserIDs = []int{}
	}
	return config, nil
}

func loadKeyCustomerUserIDs(ctx context.Context, siteID primitive.ObjectID) ([]int, error) {
	config, err := loadKeyCustomerConfig(ctx, siteID)
	if err != nil {
		return nil, err
	}
	return config.UserIDs, nil
}

func GetKeyCustomersHandler(c *gin.Context) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := loadKeyCustomerConfig(ctx, siteID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询重点客户列表失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"userIds": config.UserIDs, "warningThreshold": config.WarningThreshold})
}

type setKeyCustomerWarningThresholdRequest struct {
	UpstreamSiteID   string  `json:"upstreamSiteId"`
	WarningThreshold float64 `json:"warningThreshold"`
}

func SetKeyCustomerWarningThresholdHandler(c *gin.Context) {
	var req setKeyCustomerWarningThresholdRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
		return
	}
	if req.WarningThreshold < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "预警值不能为负数"})
		return
	}

	siteID, err := primitive.ObjectIDFromHex(strings.TrimSpace(req.UpstreamSiteID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的站点 ID"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = KeyCustomerConfigCol.UpdateOne(
		ctx,
		bson.M{"upstream_site_id": siteID},
		bson.M{"$set": bson.M{
			"upstream_site_id":  siteID,
			"warning_threshold": req.WarningThreshold,
			"updated_at":        time.Now(),
		}},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新预警值失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"warningThreshold": req.WarningThreshold})
}

type toggleKeyCustomerRequest struct {
	UpstreamSiteID string `json:"upstreamSiteId"`
	UserID         int    `json:"userId"`
	Marked         bool   `json:"marked"`
}

func ToggleKeyCustomerHandler(c *gin.Context) {
	var req toggleKeyCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
		return
	}

	siteID, err := primitive.ObjectIDFromHex(strings.TrimSpace(req.UpstreamSiteID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的站点 ID"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	update := bson.M{
		"$set": bson.M{"upstream_site_id": siteID, "updated_at": time.Now()},
	}
	if req.Marked {
		update["$addToSet"] = bson.M{"user_ids": req.UserID}
	} else {
		update["$pull"] = bson.M{"user_ids": req.UserID}
	}

	_, err = KeyCustomerConfigCol.UpdateOne(
		ctx,
		bson.M{"upstream_site_id": siteID},
		update,
		options.Update().SetUpsert(true),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新重点客户标记失败"})
		return
	}

	userIDs, err := loadKeyCustomerUserIDs(ctx, siteID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询重点客户列表失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"userIds": userIDs})
}

func QueryKeyCustomerBalanceHandler(c *gin.Context) {
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

	keyUserIDs, err := loadKeyCustomerUserIDs(ctx, siteID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询重点客户列表失败"})
		return
	}
	if len(keyUserIDs) == 0 {
		c.JSON(http.StatusOK, userBalanceStatsResponse{Users: []UserBalanceItem{}})
		return
	}
	keySet := make(map[int]bool, len(keyUserIDs))
	for _, id := range keyUserIDs {
		keySet[id] = true
	}

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
		Users:    make([]UserBalanceItem, 0, len(keyUserIDs)),
	}

	for _, item := range items {
		if item.Status != 1 || !keySet[item.ID] {
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
