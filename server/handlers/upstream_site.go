package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"balanceserver/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func ListUpstreamSitesHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cursor, err := UpstreamSiteCol.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}}))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询上游站点失败"})
		return
	}
	defer cursor.Close(ctx)

	var sites []models.UpstreamSite
	if err := cursor.All(ctx, &sites); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "解析上游站点数据失败"})
		return
	}
	if sites == nil {
		sites = []models.UpstreamSite{}
	}
	c.JSON(http.StatusOK, sites)
}

func CreateUpstreamSiteHandler(c *gin.Context) {
	var req struct {
		Name            string `json:"name"`
		URL             string `json:"url"`
		Token           string `json:"token"`
		UserID          string `json:"userId"`
		SkipStatusCodes []int  `json:"skipStatusCodes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "站点名称不能为空"})
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "站点地址不能为空"})
		return
	}

	now := time.Now()
	site := models.UpstreamSite{
		Name:            strings.TrimSpace(req.Name),
		URL:             strings.TrimSpace(req.URL),
		Token:           strings.TrimSpace(req.Token),
		UserID:          strings.TrimSpace(req.UserID),
		SkipStatusCodes: req.SkipStatusCodes,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := UpstreamSiteCol.InsertOne(ctx, site)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建上游站点失败"})
		return
	}
	site.ID = result.InsertedID.(primitive.ObjectID)
	c.JSON(http.StatusOK, site)
}

func UpdateUpstreamSiteHandler(c *gin.Context) {
	idStr := strings.TrimSpace(c.Param("id"))
	objectID, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的站点 ID"})
		return
	}

	var req struct {
		Name            string `json:"name"`
		URL             string `json:"url"`
		Token           string `json:"token"`
		UserID          string `json:"userId"`
		SkipStatusCodes []int  `json:"skipStatusCodes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "站点名称不能为空"})
		return
	}

	now := time.Now()
	update := bson.M{
		"name":              strings.TrimSpace(req.Name),
		"url":               strings.TrimSpace(req.URL),
		"token":             strings.TrimSpace(req.Token),
		"userId":            strings.TrimSpace(req.UserID),
		"skip_status_codes": req.SkipStatusCodes,
		"updated_at":        now,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := UpstreamSiteCol.UpdateOne(ctx, bson.M{"_id": objectID}, bson.M{"$set": update})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新上游站点失败"})
		return
	}
	if result.MatchedCount == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "站点不存在"})
		return
	}

	var site models.UpstreamSite
	_ = UpstreamSiteCol.FindOne(ctx, bson.M{"_id": objectID}).Decode(&site)
	c.JSON(http.StatusOK, site)
}

func DeleteUpstreamSiteHandler(c *gin.Context) {
	idStr := strings.TrimSpace(c.Param("id"))
	objectID, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的站点 ID"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := UpstreamSiteCol.DeleteOne(ctx, bson.M{"_id": objectID})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除上游站点失败"})
		return
	}
	if result.DeletedCount == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "站点不存在"})
		return
	}

	channelResult, _ := UpstreamChannelCol.DeleteMany(ctx, bson.M{"upstream_site_id": objectID})
	notifyResult, _ := ChannelAvailabilityNotifyCol.DeleteMany(ctx, bson.M{"upstream_site_id": objectID})

	c.JSON(http.StatusOK, gin.H{
		"message":         "站点已删除",
		"channelsDeleted": channelResult.DeletedCount,
		"notifyDeleted":   notifyResult.DeletedCount,
	})
}

func loadUpstreamSite(ctx context.Context, id primitive.ObjectID) (models.UpstreamSite, error) {
	var site models.UpstreamSite
	err := UpstreamSiteCol.FindOne(ctx, bson.M{"_id": id}).Decode(&site)
	if err != nil {
		return site, err
	}
	return site, nil
}

func resolveCredentialsFromSite(ctx context.Context, siteID primitive.ObjectID) (availabilityCredentials, error) {
	site, err := loadUpstreamSite(ctx, siteID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return availabilityCredentials{}, fmt.Errorf("上游站点不存在")
		}
		return availabilityCredentials{}, fmt.Errorf("加载上游站点失败")
	}

	baseURL := ""
	if u := strings.TrimSpace(site.URL); u != "" {
		base, err := extractChannelAPIBaseURL(u)
		if err == nil {
			baseURL = base
		}
	}
	if baseURL == "" {
		return availabilityCredentials{}, fmt.Errorf("站点 URL 无效")
	}

	token := strings.TrimSpace(site.Token)
	if token == "" {
		return availabilityCredentials{}, fmt.Errorf("站点 Token 未配置")
	}

	return availabilityCredentials{
		BaseURL:         baseURL,
		Token:           token,
		UserID:          strings.TrimSpace(site.UserID),
		SkipStatusCodes: site.SkipStatusCodes,
	}, nil
}

func resolveCredentialsFromChannel(ctx context.Context, channel models.UpstreamChannel) (availabilityCredentials, error) {
	if channel.UpstreamSiteID.IsZero() {
		return availabilityCredentials{}, fmt.Errorf("渠道未关联上游站点")
	}
	return resolveCredentialsFromSite(ctx, channel.UpstreamSiteID)
}

func migrateToUpstreamSites(ctx context.Context) error {
	count, err := UpstreamSiteCol.CountDocuments(ctx, bson.M{})
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	db := MongoClient.Database(UpstreamSiteCol.Database().Name())
	oldAvailCol := db.Collection("channel_availability_configs")
	oldImportCol := db.Collection("channel_import_configs")

	var availConfig struct {
		URL             string `bson:"url"`
		Token           string `bson:"token"`
		UserID          string `bson:"userId"`
		SkipStatusCodes []int  `bson:"skip_status_codes"`
	}
	_ = oldAvailCol.FindOne(ctx, bson.M{"_id": "channel_availability"}).Decode(&availConfig)

	var importConfig struct {
		URL    string `bson:"url"`
		Token  string `bson:"token"`
		UserID string `bson:"userId"`
	}
	_ = oldImportCol.FindOne(ctx, bson.M{"_id": "default_channel_import"}).Decode(&importConfig)

	siteURL := strings.TrimSpace(availConfig.URL)
	if siteURL == "" {
		siteURL = strings.TrimSpace(importConfig.URL)
	}
	siteToken := strings.TrimSpace(availConfig.Token)
	if siteToken == "" {
		siteToken = strings.TrimSpace(importConfig.Token)
	}
	siteUserID := strings.TrimSpace(availConfig.UserID)
	if siteUserID == "" {
		siteUserID = strings.TrimSpace(importConfig.UserID)
	}

	if siteURL == "" && siteToken == "" {
		log.Println("[migration] no old configs found, skipping upstream site migration")
		return nil
	}

	now := time.Now()
	site := models.UpstreamSite{
		Name:            "默认站点",
		URL:             siteURL,
		Token:           siteToken,
		UserID:          siteUserID,
		SkipStatusCodes: availConfig.SkipStatusCodes,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	result, err := UpstreamSiteCol.InsertOne(ctx, site)
	if err != nil {
		return fmt.Errorf("insert migrated site: %w", err)
	}
	siteID := result.InsertedID.(primitive.ObjectID)
	log.Printf("[migration] created upstream site '%s' with ID %s", site.Name, siteID.Hex())

	updateResult, _ := UpstreamChannelCol.UpdateMany(ctx,
		bson.M{"upstream_site_id": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"upstream_site_id": siteID}},
	)
	if updateResult != nil && updateResult.ModifiedCount > 0 {
		log.Printf("[migration] tagged %d upstream channels with site ID", updateResult.ModifiedCount)
	}

	var oldNotifyDoc bson.M
	err = ChannelAvailabilityNotifyCol.FindOne(ctx, bson.M{"_id": "channel_availability_notify"}).Decode(&oldNotifyDoc)
	if err == nil {
		_, _ = ChannelAvailabilityNotifyCol.DeleteOne(ctx, bson.M{"_id": "channel_availability_notify"})
		delete(oldNotifyDoc, "_id")
		oldNotifyDoc["upstream_site_id"] = siteID
		_, err = ChannelAvailabilityNotifyCol.InsertOne(ctx, oldNotifyDoc)
		if err != nil {
			log.Printf("[migration] failed to migrate notify config: %v", err)
		} else {
			log.Println("[migration] migrated channel availability notify config")
		}
	}

	return nil
}
