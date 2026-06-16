package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"balanceserver/models"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/crypto/bcrypt"
)

var jwtKey = []byte("your_secret_key_change_me_in_production")

const channelImportConfigID = "default_channel_import" // kept for migration only

type Claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"oldPassword" binding:"required"`
	NewPassword string `json:"newPassword" binding:"required"`
}

type ImportChannelsRequest struct {
	UpstreamSiteID string `json:"upstreamSiteId" binding:"required"`
}

type QueryBalanceRequest struct {
	ID string `json:"id" binding:"required"`
}

type upstreamChannel struct {
	ID      int    `json:"id"`
	Status  int    `json:"status"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
}

type upstreamChannelResponse struct {
	Data struct {
		Items []upstreamChannel `json:"items"`
	} `json:"data"`
	Items []upstreamChannel `json:"items"`
}

type existingSiteIndex struct {
	ByURL       map[string]models.Site
	ByChannelID map[int]models.Site
	ByName      map[string]models.Site
}

type channelImportResult struct {
	Sites             []models.Site
	DuplicateURLCount int
	InvalidURLCount   int
}

func LoginHandler(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var user models.User
	err := UserCol.FindOne(ctx, bson.M{"username": req.Username}).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username or password"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username or password"})
		return
	}

	// Generate JWT
	expirationTime := time.Now().Add(24 * time.Hour)
	claims := &Claims{
		Username: user.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": tokenString})
}

func ChangePasswordHandler(c *gin.Context) {
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写完整的密码信息"})
		return
	}

	if len(req.NewPassword) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码长度不能少于6位"})
		return
	}

	username, _ := c.Get("username")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var user models.User
	err := UserCol.FindOne(ctx, bson.M{"username": username}).Decode(&user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "用户信息查询失败"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "原密码错误"})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密失败"})
		return
	}

	_, err = UserCol.UpdateOne(ctx, bson.M{"username": username}, bson.M{"$set": bson.M{"password": string(hashedPassword)}})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码更新失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "密码修改成功"})
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			c.Abort()
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization format"})
			c.Abort()
			return
		}

		tokenString := parts[1]
		claims := &Claims{}

		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			return jwtKey, nil
		})

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		c.Set("username", claims.Username)
		c.Next()
	}
}

func GetSitesHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cursor, err := SiteCol.Find(ctx, bson.M{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch sites"})
		return
	}
	defer cursor.Close(ctx)

	var sites []models.Site
	if err = cursor.All(ctx, &sites); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to decode sites"})
		return
	}

	if sites == nil {
		sites = make([]models.Site, 0)
	}

	c.JSON(http.StatusOK, sites)
}

func SaveSitesHandler(c *gin.Context) {
	var sites []models.Site
	if err := c.ShouldBindJSON(&sites); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := preserveModelDetectionForSites(ctx, sites); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to preserve model detection config"})
		return
	}

	// Clear existing sites
	_, err := SiteCol.DeleteMany(ctx, bson.M{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear old sites"})
		return
	}

	// Insert new sites if not empty
	if len(sites) > 0 {
		var interfaceSlice []interface{}
		for _, site := range sites {
			interfaceSlice = append(interfaceSlice, site)
		}

		_, err = SiteCol.InsertMany(ctx, interfaceSlice)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save sites"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Sites saved successfully"})
}

func QueryBalanceHandler(c *gin.Context) {
	var req QueryBalanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}
	objectID, err := primitive.ObjectIDFromHex(strings.TrimSpace(req.ID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid site id"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var site models.Site
	if err := SiteCol.FindOne(ctx, bson.M{"_id": objectID}).Decode(&site); err != nil {
		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{"error": "Site not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load site"})
		return
	}
	if strings.TrimSpace(site.Token) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token is required"})
		return
	}

	result := querySiteBalance(ctx, site)
	c.JSON(http.StatusOK, gin.H{
		"ok":      result.OK,
		"balance": result.Balance,
		"error":   result.Error,
	})
}

func ImportChannelsHandler(c *gin.Context) {
	var req ImportChannelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择上游站点"})
		return
	}

	siteID, err := primitive.ObjectIDFromHex(strings.TrimSpace(req.UpstreamSiteID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的站点 ID"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	site, err := loadUpstreamSite(ctx, siteID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "上游站点不存在"})
		return
	}

	siteURL := strings.TrimSpace(site.URL)
	siteToken := strings.TrimSpace(site.Token)
	siteUserID := strings.TrimSpace(site.UserID)
	if siteURL == "" || siteToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "站点 URL 或 Token 未配置"})
		return
	}

	channels, err := fetchUpstreamChannels(ctx, siteURL, siteToken, siteUserID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	existingIndex, err := existingSitesIndex(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch existing sites"})
		return
	}

	importResult := channelsToSites(channels, existingIndex)
	if err := replaceSites(ctx, importResult.Sites); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save imported sites"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":             "Channels imported successfully",
		"source_count":        len(channels),
		"imported_count":      len(importResult.Sites),
		"duplicate_url_count": importResult.DuplicateURLCount,
		"invalid_url_count":   importResult.InvalidURLCount,
	})
}

func fetchUpstreamChannels(ctx context.Context, channelURL, token, userID string) ([]upstreamChannel, error) {
	parsed, err := url.Parse(channelURL)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream url: %w", err)
	}

	query := parsed.Query()
	shouldPaginate := query.Has("p") || query.Has("page_size")
	if !shouldPaginate {
		return fetchUpstreamChannelPage(ctx, channelURL, token, userID)
	}

	page := positiveIntQuery(query, "p", 1)
	pageSize := positiveIntQuery(query, "page_size", 100)
	channels := make([]upstreamChannel, 0, pageSize)

	for currentPage := page; currentPage < page+1000; currentPage++ {
		query.Set("p", strconv.Itoa(currentPage))
		parsed.RawQuery = query.Encode()

		items, err := fetchUpstreamChannelPage(ctx, parsed.String(), token, userID)
		if err != nil {
			return nil, err
		}
		channels = append(channels, items...)
		if len(items) == 0 || len(items) < pageSize {
			break
		}
	}

	return channels, nil
}

func fetchUpstreamChannelPage(ctx context.Context, channelURL, token, userID string) ([]upstreamChannel, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, channelURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create upstream request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", normalizeBearerToken(token))
	if strings.TrimSpace(userID) != "" {
		request.Header.Set("new-api-user", strings.TrimSpace(userID))
	}

	response, err := notificationHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("failed to request upstream channels: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read upstream response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream returned http status %d: %s", response.StatusCode, compactBody(body))
	}

	var payload upstreamChannelResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse upstream response: %w", err)
	}
	if len(payload.Data.Items) > 0 {
		return payload.Data.Items, nil
	}
	if len(payload.Items) > 0 {
		return payload.Items, nil
	}
	return []upstreamChannel{}, nil
}

func positiveIntQuery(query url.Values, key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(query.Get(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func existingSitesIndex(ctx context.Context) (existingSiteIndex, error) {
	cursor, err := SiteCol.Find(ctx, bson.M{})
	if err != nil {
		return existingSiteIndex{}, err
	}
	defer cursor.Close(ctx)

	var sites []models.Site
	if err := cursor.All(ctx, &sites); err != nil {
		return existingSiteIndex{}, err
	}

	index := existingSiteIndex{
		ByURL:       make(map[string]models.Site, len(sites)),
		ByChannelID: make(map[int]models.Site, len(sites)),
		ByName:      make(map[string]models.Site, len(sites)),
	}
	for _, site := range sites {
		key := siteURLDedupKey(site.URL)
		if key != "" {
			index.ByURL[key] = mergeExistingSiteSettings(index.ByURL[key], site)
		}
		if site.ChannelID > 0 {
			index.ByChannelID[site.ChannelID] = mergeExistingSiteSettings(index.ByChannelID[site.ChannelID], site)
		}
		if name := siteNameDedupKey(site.Name); name != "" {
			index.ByName[name] = mergeExistingSiteSettings(index.ByName[name], site)
		}
	}
	return index, nil
}

func channelsToSites(channels []upstreamChannel, existingIndex existingSiteIndex) channelImportResult {
	sites := make([]models.Site, 0, len(channels))
	indexByURL := make(map[string]int, len(channels))
	result := channelImportResult{}

	for _, channel := range channels {
		siteURL, err := normalizeSiteURLForRequest(channel.BaseURL)
		if err != nil {
			result.InvalidURLCount++
			continue
		}

		key := siteURLDedupKey(siteURL)
		if key == "" {
			result.InvalidURLCount++
			continue
		}

		site := models.Site{
			ChannelID: channel.ID,
			Status:    channel.Status,
			Name:      strings.TrimSpace(channel.Name),
			URL:       siteURL,
		}
		if site.Name == "" {
			site.Name = siteURL
		}

		applyExistingSiteSettings(&site, channel, key, existingIndex)

		if index, ok := indexByURL[key]; ok {
			if shouldReplaceDuplicateSite(sites[index], site) {
				applySiteSettings(&site, sites[index])
				sites[index] = site
			}
			result.DuplicateURLCount++
			continue
		}
		indexByURL[key] = len(sites)
		sites = append(sites, site)
	}

	result.Sites = sites
	return result
}

func shouldReplaceDuplicateSite(current, candidate models.Site) bool {
	currentEnabled := current.Status == 1
	candidateEnabled := candidate.Status == 1
	if currentEnabled != candidateEnabled {
		return candidateEnabled
	}
	return true
}

func mergeExistingSiteSettings(current, candidate models.Site) models.Site {
	if current.ID.IsZero() && strings.TrimSpace(current.URL) == "" && current.ChannelID == 0 && strings.TrimSpace(current.Name) == "" {
		return candidate
	}
	applySiteSettings(&current, candidate)
	return current
}

func applyExistingSiteSettings(site *models.Site, channel upstreamChannel, urlKey string, existingIndex existingSiteIndex) {
	if existing, ok := existingIndex.ByURL[urlKey]; ok {
		applySiteSettings(site, existing)
		return
	}
	if existing, ok := existingIndex.ByChannelID[channel.ID]; ok {
		applySiteSettings(site, existing)
		return
	}
	if existing, ok := existingIndex.ByName[siteNameDedupKey(channel.Name)]; ok {
		applySiteSettings(site, existing)
	}
}

func applySiteSettings(site *models.Site, existing models.Site) {
	if strings.TrimSpace(site.Token) == "" {
		site.Token = existing.Token
	}
	if strings.TrimSpace(site.UserID) == "" {
		site.UserID = existing.UserID
	}
	if strings.TrimSpace(site.Adapter) == "" {
		site.Adapter = existing.Adapter
	}
	if strings.TrimSpace(site.AdminAccount) == "" {
		site.AdminAccount = existing.AdminAccount
	}
	if strings.TrimSpace(site.AdminPassword) == "" {
		site.AdminPassword = existing.AdminPassword
	}
	if strings.TrimSpace(site.Remark) == "" {
		site.Remark = existing.Remark
	}
	if isEmptyModelDetectionConfig(site.ModelDetection) && !isEmptyModelDetectionConfig(existing.ModelDetection) {
		site.ModelDetection = existing.ModelDetection
	}
}

func replaceSites(ctx context.Context, sites []models.Site) error {
	if _, err := SiteCol.DeleteMany(ctx, bson.M{}); err != nil {
		return err
	}
	if len(sites) == 0 {
		return nil
	}

	docs := make([]interface{}, 0, len(sites))
	for _, site := range sites {
		docs = append(docs, site)
	}
	_, err := SiteCol.InsertMany(ctx, docs)
	return err
}

func siteURLDedupKey(value string) string {
	normalized, err := normalizeSiteURLForRequest(value)
	if err != nil {
		return strings.ToLower(strings.TrimRight(strings.TrimSpace(value), "/"))
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return strings.ToLower(strings.TrimRight(strings.TrimSpace(normalized), "/"))
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	if parsed.Path == "/api/user/self" {
		parsed.Path = ""
	} else if strings.HasSuffix(parsed.Path, "/api/user/self") {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/api/user/self")
	}
	return parsed.String()
}

func siteNameDedupKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func ProxyHandler(c *gin.Context) {
	targetURL := strings.TrimSpace(c.Query("url"))
	if targetURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing URL parameter"})
		return
	}

	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "https://" + targetURL
	}

	target, err := url.Parse(targetURL)
	if err != nil || target.Host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid URL"})
		return
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only http and https URLs are supported"})
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(&url.URL{
		Scheme: target.Scheme,
		Host:   target.Host,
	})

	proxy.Director = func(r *http.Request) {
		r.URL.Scheme = target.Scheme
		r.URL.Host = target.Host
		r.URL.Path = target.Path
		r.URL.RawPath = target.RawPath
		r.URL.RawQuery = target.RawQuery
		r.Host = target.Host

		r.Header = make(http.Header)
		r.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		r.Header.Set("Accept", "application/json")
		if contentType := c.GetHeader("Content-Type"); contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}

		targetAuth := normalizeBearerToken(c.GetHeader("Target-Authorization"))
		if targetAuth != "" {
			r.Header.Set("Authorization", targetAuth)
		}

		targetUser := strings.TrimSpace(c.GetHeader("New-Api-User"))
		if targetUser != "" {
			r.Header.Set("New-Api-User", targetUser)
		}
	}

	proxy.ServeHTTP(c.Writer, c.Request)
}

func normalizeBearerToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return token
	}
	return "Bearer " + token
}
