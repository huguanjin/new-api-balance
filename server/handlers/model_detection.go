package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
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
	modelDetectionConfigID              = "model_detection_notification"
	defaultModelDetectionIntervalMinute = 1440
	modelDetectionStatusSubmitting      = "submitting"
	modelDetectionStatusQueued          = "queued"
	modelDetectionStatusRunning         = "running"
	modelDetectionStatusDone            = "done"
	modelDetectionStatusError           = "error"
	modelDetectionPushPolicyAll         = "all"
	modelDetectionPushPolicyFailures    = "failures"
)

var (
	modelDetectionHTTPClient = &http.Client{Timeout: 30 * time.Second}
	modelDetectionRunMu      sync.Mutex
)

type modelDetectionConfigRequest struct {
	Enabled           bool                          `json:"enabled"`
	AutoDetectEnabled bool                          `json:"autoDetectEnabled"`
	VeridropURL       string                        `json:"veridropUrl"`
	VeridropAPIToken  string                        `json:"veridropApiToken"`
	ReportBaseURL     string                        `json:"reportBaseUrl"`
	NotificationType  string                        `json:"notification_type"`
	WebhookURL        string                        `json:"webhook_url"`
	SignKey           string                        `json:"sign_key"`
	WeworkWebhookURL  string                        `json:"wework_webhook_url"`
	IntervalMinutes   int                           `json:"interval_minutes"`
	Schedules         []models.NotificationSchedule `json:"schedules"`
	PushPolicy        string                        `json:"pushPolicy"`
}

type modelDetectionRunRequest struct {
	SiteIDs   []string `json:"siteIds"`
	TargetIDs []string `json:"targetIds"`
}

type modelDetectionRunResponse struct {
	CreatedCount int                        `json:"created_count"`
	ErrorCount   int                        `json:"error_count"`
	Jobs         []models.ModelDetectionJob `json:"jobs"`
	Errors       []string                   `json:"errors"`
}

type veridropSubmitResponse struct {
	JobID     string `json:"job_id"`
	StatusURL string `json:"status_url"`
}

type veridropStatusResponse struct {
	JobID       string   `json:"job_id"`
	Protocol    string   `json:"protocol"`
	Status      string   `json:"status"`
	BaseURL     string   `json:"base_url"`
	TargetModel string   `json:"target_model"`
	Mode        string   `json:"mode"`
	CreatedAt   float64  `json:"created_at"`
	StartedAt   *float64 `json:"started_at"`
	FinishedAt  *float64 `json:"finished_at"`
	ResultURL   string   `json:"result_url"`
	ImageURL    string   `json:"image_url"`
	JSONURL     string   `json:"json_url"`
	Error       string   `json:"error"`
}

type modelDetectionNotificationLink struct {
	Label string
	URL   string
}

func SaveSiteModelDetectionHandler(c *gin.Context) {
	siteID, err := primitive.ObjectIDFromHex(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid site id"})
		return
	}

	var config models.ModelDetectionConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var site models.Site
	if err := SiteCol.FindOne(ctx, bson.M{"_id": siteID}).Decode(&site); err != nil {
		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{"error": "Site not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load site"})
		return
	}

	normalized, err := normalizeSiteModelDetectionConfig(site, config, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if _, err := SiteCol.UpdateOne(ctx, bson.M{"_id": siteID}, bson.M{"$set": bson.M{"model_detection": normalized}}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save model detection config"})
		return
	}

	c.JSON(http.StatusOK, normalized)
}

func GetModelDetectionNotificationConfigHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config, _, err := loadModelDetectionConfig(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load model detection config"})
		return
	}

	c.JSON(http.StatusOK, config)
}

func SaveModelDetectionNotificationConfigHandler(c *gin.Context) {
	var req modelDetectionConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	config := models.ModelDetectionNotificationConfig{
		Enabled:           req.Enabled,
		AutoDetectEnabled: req.AutoDetectEnabled,
		VeridropURL:       req.VeridropURL,
		VeridropAPIToken:  req.VeridropAPIToken,
		ReportBaseURL:     req.ReportBaseURL,
		NotificationType:  req.NotificationType,
		WebhookURL:        req.WebhookURL,
		SignKey:           req.SignKey,
		WeworkWebhookURL:  req.WeworkWebhookURL,
		IntervalMinutes:   req.IntervalMinutes,
		Schedules:         req.Schedules,
		PushPolicy:        req.PushPolicy,
	}
	normalizeModelDetectionConfig(&config)
	if err := validateModelDetectionConfig(config, config.Enabled, config.AutoDetectEnabled); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	existing, _, err := loadModelDetectionConfig(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load model detection config"})
		return
	}

	now := time.Now()
	update := bson.M{
		"enabled":             config.Enabled,
		"auto_detect_enabled": config.AutoDetectEnabled,
		"veridrop_url":        config.VeridropURL,
		"veridrop_api_token":  config.VeridropAPIToken,
		"report_base_url":     config.ReportBaseURL,
		"notification_type":   config.NotificationType,
		"webhook_url":         config.WebhookURL,
		"sign_key":            config.SignKey,
		"wework_webhook_url":  config.WeworkWebhookURL,
		"interval_minutes":    config.IntervalMinutes,
		"schedules":           config.Schedules,
		"push_policy":         config.PushPolicy,
		"last_auto_run_at":    existing.LastAutoRunAt,
		"last_attempt_at":     existing.LastAttemptAt,
		"last_sent_at":        existing.LastSentAt,
		"last_error":          existing.LastError,
		"updated_at":          now,
	}

	opts := options.Update().SetUpsert(true)
	if _, err := ModelDetectionConfigCol.UpdateOne(ctx, bson.M{"_id": modelDetectionConfigID}, bson.M{"$set": update}, opts); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save model detection config"})
		return
	}

	config.ID = modelDetectionConfigID
	config.LastAutoRunAt = existing.LastAutoRunAt
	config.LastAttemptAt = existing.LastAttemptAt
	config.LastSentAt = existing.LastSentAt
	config.LastError = existing.LastError
	config.UpdatedAt = now
	c.JSON(http.StatusOK, config)
}

func TestModelDetectionNotificationHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	config, _, err := loadModelDetectionConfig(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Failed to load model detection config"})
		return
	}
	if err := validateModelDetectionConfig(config, true, false); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	if err := sendModelDetectionTestNotification(ctx, config); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "模型检测通知测试成功"})
}

func RunModelDetectionHandler(c *gin.Context) {
	var req modelDetectionRunRequest
	if c.Request.Body != nil {
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
			return
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	config, _, err := loadModelDetectionConfig(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load model detection config"})
		return
	}
	if err := validateModelDetectionConfig(config, false, true); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response := submitModelDetectionJobs(ctx, config, req)
	status := http.StatusOK
	if response.CreatedCount == 0 && response.ErrorCount > 0 {
		status = http.StatusBadGateway
	}
	c.JSON(status, response)
}

func GetModelDetectionJobsHandler(c *gin.Context) {
	limit := positiveIntQuery(c.Request.URL.Query(), "limit", 100)
	if limit > 500 {
		limit = 500
	}

	filter := bson.M{}
	if siteIDText := strings.TrimSpace(c.Query("siteId")); siteIDText != "" {
		siteID, err := primitive.ObjectIDFromHex(siteIDText)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid site id"})
			return
		}
		filter["site_id"] = siteID
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cursor, err := ModelDetectionJobCol.Find(
		ctx,
		filter,
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(int64(limit)),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch model detection jobs"})
		return
	}
	defer cursor.Close(ctx)

	var jobs []models.ModelDetectionJob
	if err := cursor.All(ctx, &jobs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to decode model detection jobs"})
		return
	}
	if jobs == nil {
		jobs = []models.ModelDetectionJob{}
	}
	c.JSON(http.StatusOK, jobs)
}

func GetModelDetectionJobReportHandler(c *gin.Context) {
	jobID, err := primitive.ObjectIDFromHex(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid job id"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	job, err := loadModelDetectionJob(ctx, jobID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{"error": "Model detection job not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load model detection job"})
		return
	}

	if len(job.Report) == 0 && strings.TrimSpace(job.VeridropJobID) != "" {
		config, _, err := loadModelDetectionConfig(ctx)
		if err == nil {
			if report, reportErr := fetchVeridropReport(ctx, config, job.VeridropJobID, job.JSONURL); reportErr == nil {
				update := bson.M{
					"report":     report,
					"updated_at": time.Now(),
				}
				applyReportToJobUpdate(update, report)
				_, _ = ModelDetectionJobCol.UpdateOne(ctx, bson.M{"_id": job.ID}, bson.M{"$set": update})
				job, _ = loadModelDetectionJob(ctx, job.ID)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"job":    job,
		"report": job.Report,
	})
}

func PushModelDetectionJobReportHandler(c *gin.Context) {
	jobID, err := primitive.ObjectIDFromHex(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid job id"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	config, _, err := loadModelDetectionConfig(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Failed to load model detection config"})
		return
	}
	if err := validateModelDetectionConfig(config, true, false); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	job, err := loadModelDetectionJob(ctx, jobID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Model detection job not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Failed to load model detection job"})
		return
	}

	if err := sendModelDetectionJobNotification(ctx, config, job); err != nil {
		recordModelDetectionNotificationAttempt(err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	now := time.Now()
	_, _ = ModelDetectionJobCol.UpdateOne(ctx, bson.M{"_id": job.ID}, bson.M{"$set": bson.M{
		"notified_at":     now,
		"notification_at": now,
		"updated_at":      now,
	}})
	recordModelDetectionNotificationAttempt(nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "模型检测报告已推送"})
}

func StartModelDetectionScheduler() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			runModelDetectionMaintenance()
		}
	}()
}

func runModelDetectionMaintenance() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	config, _, err := loadModelDetectionConfig(ctx)
	if err != nil {
		log.Printf("Failed to load model detection config: %v", err)
		return
	}

	if err := pollActiveModelDetectionJobs(ctx, config); err != nil {
		log.Printf("Failed to poll model detection jobs: %v", err)
	}

	if !config.AutoDetectEnabled || !modelDetectionAutoDue(config, time.Now()) {
		return
	}
	if err := validateModelDetectionConfig(config, false, true); err != nil {
		recordModelDetectionLastError(err)
		log.Printf("Model detection config is invalid: %v", err)
		return
	}

	response := submitModelDetectionJobs(ctx, config, modelDetectionRunRequest{})
	recordModelDetectionAutoRun(response)
	if response.ErrorCount > 0 {
		log.Printf("Scheduled model detection submitted with %d errors", response.ErrorCount)
	}
}

func submitModelDetectionJobs(ctx context.Context, config models.ModelDetectionNotificationConfig, req modelDetectionRunRequest) modelDetectionRunResponse {
	modelDetectionRunMu.Lock()
	defer modelDetectionRunMu.Unlock()

	response := modelDetectionRunResponse{
		Jobs:   []models.ModelDetectionJob{},
		Errors: []string{},
	}

	siteFilter, siteFilterErr := objectIDFilter(req.SiteIDs)
	if siteFilterErr != nil {
		response.Errors = append(response.Errors, siteFilterErr.Error())
		response.ErrorCount = len(response.Errors)
		return response
	}
	targetFilter := stringSet(req.TargetIDs)

	cursor, err := SiteCol.Find(ctx, bson.M{})
	if err != nil {
		response.Errors = append(response.Errors, "读取站点列表失败: "+err.Error())
		response.ErrorCount = len(response.Errors)
		return response
	}
	defer cursor.Close(ctx)

	var sites []models.Site
	if err := cursor.All(ctx, &sites); err != nil {
		response.Errors = append(response.Errors, "解析站点列表失败: "+err.Error())
		response.ErrorCount = len(response.Errors)
		return response
	}

	for _, site := range sites {
		if len(siteFilter) > 0 && !siteFilter[site.ID.Hex()] {
			continue
		}
		if !site.ModelDetection.Enabled {
			continue
		}

		normalized, err := normalizeSiteModelDetectionConfig(site, site.ModelDetection, true)
		if err != nil {
			response.Errors = append(response.Errors, fmt.Sprintf("%s: %s", displaySiteName(site), err.Error()))
			continue
		}

		for _, target := range normalized.Targets {
			if !target.Enabled {
				continue
			}
			if len(targetFilter) > 0 && !targetFilter[target.ID] {
				continue
			}
			job, err := createAndSubmitModelDetectionJob(ctx, config, site, normalized.APIKey, target)
			if err != nil {
				response.Errors = append(response.Errors, fmt.Sprintf("%s / %s: %s", displaySiteName(site), target.Model, err.Error()))
				continue
			}
			response.Jobs = append(response.Jobs, job)
		}
	}

	response.CreatedCount = len(response.Jobs)
	response.ErrorCount = len(response.Errors)
	return response
}

func createAndSubmitModelDetectionJob(ctx context.Context, config models.ModelDetectionNotificationConfig, site models.Site, apiKey string, target models.ModelDetectionTarget) (models.ModelDetectionJob, error) {
	now := time.Now()
	job := models.ModelDetectionJob{
		SiteID:    site.ID,
		SiteName:  displaySiteName(site),
		ChannelID: site.ChannelID,
		TargetID:  target.ID,
		Protocol:  target.Protocol,
		Model:     target.Model,
		Mode:      target.Mode,
		BaseURL:   target.BaseURL,
		Status:    modelDetectionStatusSubmitting,
		CreatedAt: now,
		UpdatedAt: now,
	}

	insertResult, err := ModelDetectionJobCol.InsertOne(ctx, job)
	if err != nil {
		return job, err
	}
	if insertedID, ok := insertResult.InsertedID.(primitive.ObjectID); ok {
		job.ID = insertedID
	}

	submit, err := submitVeridropDetection(ctx, config, target, apiKey)
	submittedAt := time.Now()
	update := bson.M{
		"updated_at":      submittedAt,
		"submitted_at":    submittedAt,
		"veridrop_job_id": submit.JobID,
	}
	if err != nil {
		job.Status = modelDetectionStatusError
		job.Error = err.Error()
		job.UpdatedAt = submittedAt
		job.SubmittedAt = &submittedAt
		update["status"] = modelDetectionStatusError
		update["error"] = err.Error()
		_, _ = ModelDetectionJobCol.UpdateOne(ctx, bson.M{"_id": job.ID}, bson.M{"$set": update})
		return job, err
	}

	job.Status = modelDetectionStatusQueued
	job.VeridropJobID = submit.JobID
	job.SubmittedAt = &submittedAt
	job.UpdatedAt = submittedAt
	update["status"] = modelDetectionStatusQueued
	update["error"] = ""
	if _, err := ModelDetectionJobCol.UpdateOne(ctx, bson.M{"_id": job.ID}, bson.M{"$set": update}); err != nil {
		return job, err
	}
	return job, nil
}

func submitVeridropDetection(ctx context.Context, config models.ModelDetectionNotificationConfig, target models.ModelDetectionTarget, apiKey string) (veridropSubmitResponse, error) {
	endpoint, err := veridropEndpoint(config.VeridropURL, "/api/detect/"+target.Protocol)
	if err != nil {
		return veridropSubmitResponse{}, err
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fields := map[string]string{
		"base_url": target.BaseURL,
		"api_key":  apiKey,
		"model":    target.Model,
		"mode":     target.Mode,
	}
	if target.Protocol != "gemini" {
		fields["include_long_context"] = strconv.FormatBool(target.IncludeLongContext)
		fields["include_long_context_extreme"] = strconv.FormatBool(target.IncludeLongContextExtreme)
	}
	if target.Force {
		fields["force"] = "true"
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return veridropSubmitResponse{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return veridropSubmitResponse{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return veridropSubmitResponse{}, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Accept", "application/json")
	setVeridropAuthHeader(request, config.VeridropAPIToken)

	response, err := modelDetectionHTTPClient.Do(request)
	if err != nil {
		return veridropSubmitResponse{}, err
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return veridropSubmitResponse{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return veridropSubmitResponse{}, fmt.Errorf("Veridrop 返回 HTTP %d: %s", response.StatusCode, compactBody(responseBody))
	}

	var payload veridropSubmitResponse
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return veridropSubmitResponse{}, fmt.Errorf("解析 Veridrop 提交响应失败: %w", err)
	}
	if strings.TrimSpace(payload.JobID) == "" {
		return veridropSubmitResponse{}, errors.New("Veridrop 提交响应缺少 job_id")
	}
	payload.JobID = strings.TrimSpace(payload.JobID)
	return payload, nil
}

func pollActiveModelDetectionJobs(ctx context.Context, config models.ModelDetectionNotificationConfig) error {
	cursor, err := ModelDetectionJobCol.Find(
		ctx,
		bson.M{"status": bson.M{"$in": []string{modelDetectionStatusSubmitting, modelDetectionStatusQueued, modelDetectionStatusRunning}}},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}}).SetLimit(100),
	)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	var jobs []models.ModelDetectionJob
	if err := cursor.All(ctx, &jobs); err != nil {
		return err
	}

	for _, job := range jobs {
		if strings.TrimSpace(job.VeridropJobID) == "" {
			markModelDetectionJobError(ctx, job, "检测任务缺少 Veridrop job_id")
			continue
		}
		if err := pollOneModelDetectionJob(ctx, config, job); err != nil {
			log.Printf("Failed to poll model detection job %s: %v", job.ID.Hex(), err)
		}
	}
	return nil
}

func pollOneModelDetectionJob(ctx context.Context, config models.ModelDetectionNotificationConfig, job models.ModelDetectionJob) error {
	status, err := fetchVeridropStatus(ctx, config, job.VeridropJobID)
	now := time.Now()
	if err != nil {
		_, _ = ModelDetectionJobCol.UpdateOne(ctx, bson.M{"_id": job.ID}, bson.M{"$set": bson.M{
			"last_polled_at": now,
			"updated_at":     now,
			"error":          err.Error(),
		}})
		return err
	}

	update := bson.M{
		"status":         normalizeVeridropJobStatus(status.Status),
		"result_url":     resolveVeridropLink(config.VeridropURL, status.ResultURL),
		"image_url":      resolveVeridropLink(config.VeridropURL, status.ImageURL),
		"json_url":       resolveVeridropLink(config.VeridropURL, status.JSONURL),
		"last_polled_at": now,
		"updated_at":     now,
		"error":          status.Error,
	}
	if status.StartedAt != nil {
		update["started_at"] = unixFloatTime(*status.StartedAt)
	}
	if status.FinishedAt != nil {
		update["finished_at"] = unixFloatTime(*status.FinishedAt)
	}

	if status.Status == modelDetectionStatusDone {
		report, reportErr := fetchVeridropReport(ctx, config, job.VeridropJobID, status.JSONURL)
		if reportErr != nil {
			update["status"] = modelDetectionStatusError
			update["error"] = "获取检测报告失败: " + reportErr.Error()
		} else {
			applyReportToJobUpdate(update, report)
			update["report"] = report
		}
	}

	if _, err := ModelDetectionJobCol.UpdateOne(ctx, bson.M{"_id": job.ID}, bson.M{"$set": update}); err != nil {
		return err
	}

	if update["status"] == modelDetectionStatusDone || update["status"] == modelDetectionStatusError {
		finishedJob, err := loadModelDetectionJob(ctx, job.ID)
		if err != nil {
			return err
		}
		if err := sendModelDetectionNotificationIfNeeded(ctx, config, finishedJob); err != nil {
			recordModelDetectionNotificationAttempt(err)
			return err
		}
	}

	return nil
}

func fetchVeridropStatus(ctx context.Context, config models.ModelDetectionNotificationConfig, jobID string) (veridropStatusResponse, error) {
	endpoint, err := veridropEndpoint(config.VeridropURL, "/api/status/"+url.PathEscape(jobID))
	if err != nil {
		return veridropStatusResponse{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return veridropStatusResponse{}, err
	}
	request.Header.Set("Accept", "application/json")
	setVeridropAuthHeader(request, config.VeridropAPIToken)

	response, err := modelDetectionHTTPClient.Do(request)
	if err != nil {
		return veridropStatusResponse{}, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return veridropStatusResponse{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return veridropStatusResponse{}, fmt.Errorf("Veridrop 状态查询返回 HTTP %d: %s", response.StatusCode, compactBody(body))
	}

	var status veridropStatusResponse
	if err := json.Unmarshal(body, &status); err != nil {
		return veridropStatusResponse{}, fmt.Errorf("解析 Veridrop 状态失败: %w", err)
	}
	return status, nil
}

func fetchVeridropReport(ctx context.Context, config models.ModelDetectionNotificationConfig, jobID, jsonURL string) (map[string]interface{}, error) {
	path := jsonURL
	if strings.TrimSpace(path) == "" {
		path = "/api/result/" + url.PathEscape(jobID) + ".json"
	}
	endpoint, err := veridropEndpoint(config.VeridropURL, path)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	setVeridropAuthHeader(request, config.VeridropAPIToken)

	response, err := modelDetectionHTTPClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Veridrop 报告返回 HTTP %d: %s", response.StatusCode, compactBody(body))
	}

	var report map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&report); err != nil {
		return nil, fmt.Errorf("解析 Veridrop 报告失败: %w", err)
	}
	return report, nil
}

func markModelDetectionJobError(ctx context.Context, job models.ModelDetectionJob, message string) {
	now := time.Now()
	_, _ = ModelDetectionJobCol.UpdateOne(ctx, bson.M{"_id": job.ID}, bson.M{"$set": bson.M{
		"status":      modelDetectionStatusError,
		"error":       message,
		"updated_at":  now,
		"finished_at": now,
	}})
}

func loadModelDetectionJob(ctx context.Context, id primitive.ObjectID) (models.ModelDetectionJob, error) {
	var job models.ModelDetectionJob
	err := ModelDetectionJobCol.FindOne(ctx, bson.M{"_id": id}).Decode(&job)
	return job, err
}

func sendModelDetectionNotificationIfNeeded(ctx context.Context, config models.ModelDetectionNotificationConfig, job models.ModelDetectionJob) error {
	if !config.Enabled || !modelDetectionShouldNotify(config, job) || job.NotifiedAt != nil {
		return nil
	}
	if err := sendModelDetectionJobNotification(ctx, config, job); err != nil {
		return err
	}

	now := time.Now()
	if _, err := ModelDetectionJobCol.UpdateOne(ctx, bson.M{"_id": job.ID}, bson.M{"$set": bson.M{"notified_at": now, "updated_at": now}}); err != nil {
		return err
	}
	recordModelDetectionNotificationAttempt(nil)
	return nil
}

func sendModelDetectionJobNotification(ctx context.Context, config models.ModelDetectionNotificationConfig, job models.ModelDetectionJob) error {
	if config.NotificationType == "wework" {
		return sendWeworkMarkdownNotification(ctx, config.WeworkWebhookURL, buildWeworkModelDetectionMessage(config, job))
	}
	return sendFeishuCardNotification(ctx, config.WebhookURL, buildFeishuModelDetectionCard(config, job), config.SignKey)
}

func sendModelDetectionTestNotification(ctx context.Context, config models.ModelDetectionNotificationConfig) error {
	now := formatNotificationTime(notificationNow())
	if config.NotificationType == "wework" {
		message := fmt.Sprintf("#### New API 模型检测通知测试\n> 发送时间：%s\n> 状态：<font color=\"info\">Webhook 配置正常</font>", now)
		return sendWeworkMarkdownNotification(ctx, config.WeworkWebhookURL, message)
	}

	card := map[string]interface{}{
		"config": map[string]interface{}{"wide_screen_mode": true},
		"header": map[string]interface{}{
			"template": "green",
			"title": map[string]string{
				"tag":     "plain_text",
				"content": "New API 模型检测通知测试",
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

func buildFeishuModelDetectionCard(config models.ModelDetectionNotificationConfig, job models.ModelDetectionJob) map[string]interface{} {
	template := "green"
	if job.Status == modelDetectionStatusError || job.Verdict == "failed" {
		template = "red"
	} else if job.Verdict == "marginal" {
		template = "orange"
	}

	fields := []interface{}{
		feishuField("检测时间", formatOptionalTime(job.FinishedAt)),
		feishuField("渠道", formatModelDetectionSite(job)),
		feishuField("协议 / 模型", fmt.Sprintf("%s / %s", strings.ToUpper(job.Protocol), job.Model)),
		feishuField("结论", modelDetectionVerdictText(job)),
		feishuField("分数", formatModelDetectionScore(job.TotalScore)),
		feishuField("等级", valueOrDash(job.TierTitle)),
	}

	elements := []interface{}{
		map[string]interface{}{
			"tag":    "div",
			"fields": fields,
		},
		map[string]interface{}{"tag": "hr"},
		map[string]interface{}{
			"tag":  "div",
			"text": feishuMarkdown(modelDetectionSummaryText(job)),
		},
	}
	if links := modelDetectionReportLinks(config, job); len(links) > 0 {
		actions := make([]interface{}, 0, len(links))
		for _, link := range links {
			actions = append(actions, map[string]interface{}{
				"tag": "button",
				"text": map[string]string{
					"tag":     "plain_text",
					"content": link.Label,
				},
				"url":  link.URL,
				"type": "primary",
			})
		}
		elements = append(elements, map[string]interface{}{
			"tag":     "action",
			"actions": actions,
		})
	}

	return map[string]interface{}{
		"config": map[string]interface{}{
			"wide_screen_mode": true,
		},
		"header": map[string]interface{}{
			"template": template,
			"title": map[string]string{
				"tag":     "plain_text",
				"content": modelDetectionNotificationTitle(job),
			},
		},
		"elements": elements,
	}
}

func buildWeworkModelDetectionMessage(config models.ModelDetectionNotificationConfig, job models.ModelDetectionJob) string {
	color := "info"
	if job.Status == modelDetectionStatusError || job.Verdict == "failed" {
		color = "warning"
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("#### %s\n", modelDetectionNotificationTitle(job)))
	builder.WriteString(fmt.Sprintf("> 检测时间：%s\n", formatOptionalTime(job.FinishedAt)))
	builder.WriteString(fmt.Sprintf("> 渠道：%s\n", formatModelDetectionSite(job)))
	builder.WriteString(fmt.Sprintf("> 协议 / 模型：%s / %s\n", strings.ToUpper(job.Protocol), job.Model))
	builder.WriteString(fmt.Sprintf("> 结论：<font color=\"%s\">%s</font>\n", color, modelDetectionVerdictText(job)))
	builder.WriteString(fmt.Sprintf("> 分数：%s\n", formatModelDetectionScore(job.TotalScore)))
	builder.WriteString(fmt.Sprintf("> 等级：%s\n", valueOrDash(job.TierTitle)))
	builder.WriteString("\n")
	builder.WriteString(modelDetectionSummaryText(job))
	if links := modelDetectionReportLinks(config, job); len(links) > 0 {
		for _, link := range links {
			builder.WriteString(fmt.Sprintf("\n\n[%s](%s)", link.Label, link.URL))
		}
	}
	return builder.String()
}

func loadModelDetectionConfig(ctx context.Context) (models.ModelDetectionNotificationConfig, bool, error) {
	config := defaultModelDetectionConfig()
	err := ModelDetectionConfigCol.FindOne(ctx, bson.M{"_id": modelDetectionConfigID}).Decode(&config)
	if err == mongo.ErrNoDocuments {
		return config, false, nil
	}
	if err != nil {
		return config, false, err
	}
	normalizeModelDetectionConfig(&config)
	return config, true, nil
}

func defaultModelDetectionConfig() models.ModelDetectionNotificationConfig {
	return models.ModelDetectionNotificationConfig{
		ID:               modelDetectionConfigID,
		VeridropURL:      defaultModelDetectionVeridropURL(),
		ReportBaseURL:    defaultModelDetectionReportBaseURL(),
		NotificationType: "feishu",
		IntervalMinutes:  defaultModelDetectionIntervalMinute,
		PushPolicy:       modelDetectionPushPolicyAll,
	}
}

func defaultModelDetectionVeridropURL() string {
	if value := strings.TrimRight(strings.TrimSpace(os.Getenv("VERIDROP_URL")), "/"); value != "" {
		return value
	}
	return "http://127.0.0.1:8080"
}

func defaultModelDetectionReportBaseURL() string {
	for _, key := range []string{"APP_PUBLIC_URL", "PUBLIC_BASE_URL"} {
		if value := strings.TrimRight(strings.TrimSpace(os.Getenv(key)), "/"); value != "" {
			return value
		}
	}
	return ""
}

func normalizeModelDetectionConfig(config *models.ModelDetectionNotificationConfig) {
	config.VeridropURL = strings.TrimRight(strings.TrimSpace(config.VeridropURL), "/")
	config.VeridropAPIToken = strings.TrimSpace(config.VeridropAPIToken)
	config.ReportBaseURL = strings.TrimRight(strings.TrimSpace(config.ReportBaseURL), "/")
	config.NotificationType = strings.ToLower(strings.TrimSpace(config.NotificationType))
	if config.NotificationType != "wework" {
		config.NotificationType = "feishu"
	}
	config.WebhookURL = strings.TrimSpace(config.WebhookURL)
	config.SignKey = strings.TrimSpace(config.SignKey)
	config.WeworkWebhookURL = strings.TrimSpace(config.WeworkWebhookURL)
	if config.IntervalMinutes <= 0 {
		config.IntervalMinutes = defaultModelDetectionIntervalMinute
	}
	if config.IntervalMinutes > maxNotificationIntervalMinute {
		config.IntervalMinutes = maxNotificationIntervalMinute
	}
	config.Schedules = normalizeNotificationSchedules(config.Schedules)
	config.PushPolicy = strings.ToLower(strings.TrimSpace(config.PushPolicy))
	if config.PushPolicy != modelDetectionPushPolicyFailures {
		config.PushPolicy = modelDetectionPushPolicyAll
	}
}

func validateModelDetectionConfig(config models.ModelDetectionNotificationConfig, requireWebhook, requireVeridrop bool) error {
	if requireVeridrop {
		if err := validateWebhookURL(config.VeridropURL, "Veridrop 服务地址"); err != nil {
			return err
		}
	}
	if config.ReportBaseURL != "" {
		if err := validateWebhookURL(config.ReportBaseURL, "报告访问地址"); err != nil {
			return err
		}
	}
	if err := validateNotificationSchedules(config.Schedules); err != nil {
		return err
	}
	if !requireWebhook {
		return nil
	}
	if config.NotificationType == "wework" {
		return validateWebhookURL(config.WeworkWebhookURL, "模型检测企业微信 Webhook URL")
	}
	return validateWebhookURL(config.WebhookURL, "模型检测飞书 Webhook URL")
}

func normalizeSiteModelDetectionConfig(site models.Site, config models.ModelDetectionConfig, requireRunnable bool) (models.ModelDetectionConfig, error) {
	config.APIKey = strings.TrimSpace(config.APIKey)
	normalized := models.ModelDetectionConfig{
		Enabled: config.Enabled,
		APIKey:  config.APIKey,
		Targets: []models.ModelDetectionTarget{},
	}

	for index, target := range config.Targets {
		normalizedTarget, err := normalizeModelDetectionTarget(site, target)
		if err != nil {
			return normalized, fmt.Errorf("第 %d 个检测模型配置错误: %w", index+1, err)
		}
		normalized.Targets = append(normalized.Targets, normalizedTarget)
	}

	if !normalized.Enabled {
		return normalized, nil
	}

	enabledTargetCount := 0
	for _, target := range normalized.Targets {
		if target.Enabled {
			enabledTargetCount++
		}
	}
	if enabledTargetCount == 0 {
		if requireRunnable {
			return normalized, errors.New("请至少启用一个检测模型")
		}
		return normalized, nil
	}
	if normalized.APIKey == "" {
		return normalized, errors.New("请填写模型检测 API Key")
	}
	return normalized, nil
}

func normalizeModelDetectionTarget(site models.Site, target models.ModelDetectionTarget) (models.ModelDetectionTarget, error) {
	target.ID = strings.TrimSpace(target.ID)
	if target.ID == "" {
		target.ID = primitive.NewObjectID().Hex()
	}
	target.Protocol = normalizeModelDetectionProtocol(target.Protocol)
	if target.Protocol == "" {
		return target, errors.New("协议必须是 claude、openai 或 gemini")
	}
	target.Model = strings.TrimSpace(target.Model)
	target.Mode = normalizeModelDetectionMode(target.Protocol, target.Mode)
	target.BaseURL = strings.TrimSpace(target.BaseURL)
	if target.BaseURL == "" {
		target.BaseURL = strings.TrimSpace(site.URL)
	}
	if target.Enabled {
		if target.Model == "" {
			return target, errors.New("启用的检测模型必须填写模型名称")
		}
		baseURL, err := normalizeSiteURLForRequest(target.BaseURL)
		if err != nil {
			return target, errors.New("检测 Base URL 必须是完整的 http:// 或 https:// 地址")
		}
		target.BaseURL = baseURL
	} else if target.BaseURL != "" {
		baseURL, err := normalizeSiteURLForRequest(target.BaseURL)
		if err == nil {
			target.BaseURL = baseURL
		}
	}
	return target, nil
}

func normalizeModelDetectionProtocol(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "anthropic", "claude":
		return "claude"
	case "openai":
		return "openai"
	case "gemini":
		return "gemini"
	default:
		return ""
	}
}

func normalizeModelDetectionMode(protocol, value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value != "quick" && value != "standard" && value != "full" {
		if protocol == "claude" {
			return "full"
		}
		return "standard"
	}
	return value
}

func preserveModelDetectionForSites(ctx context.Context, sites []models.Site) error {
	existingIndex, err := existingSitesIndex(ctx)
	if err != nil {
		return err
	}
	for index := range sites {
		if !isEmptyModelDetectionConfig(sites[index].ModelDetection) {
			continue
		}
		key := siteURLDedupKey(sites[index].URL)
		if existing, ok := existingIndex.ByURL[key]; ok && !isEmptyModelDetectionConfig(existing.ModelDetection) {
			sites[index].ModelDetection = existing.ModelDetection
			continue
		}
		if sites[index].ChannelID > 0 {
			if existing, ok := existingIndex.ByChannelID[sites[index].ChannelID]; ok && !isEmptyModelDetectionConfig(existing.ModelDetection) {
				sites[index].ModelDetection = existing.ModelDetection
				continue
			}
		}
		if name := siteNameDedupKey(sites[index].Name); name != "" {
			if existing, ok := existingIndex.ByName[name]; ok && !isEmptyModelDetectionConfig(existing.ModelDetection) {
				sites[index].ModelDetection = existing.ModelDetection
			}
		}
	}
	return nil
}

func isEmptyModelDetectionConfig(config models.ModelDetectionConfig) bool {
	return !config.Enabled && strings.TrimSpace(config.APIKey) == "" && len(config.Targets) == 0
}

func modelDetectionAutoDue(config models.ModelDetectionNotificationConfig, now time.Time) bool {
	schedule, scheduleStart, ok := activeNotificationSchedule(config.Schedules, now)
	if len(config.Schedules) > 0 && !ok {
		return false
	}

	intervalMinutes := config.IntervalMinutes
	if ok {
		intervalMinutes = schedule.IntervalMinutes
	}
	if config.LastAutoRunAt == nil {
		return true
	}
	if ok && config.LastAutoRunAt.Before(scheduleStart) {
		return true
	}
	return now.Sub(*config.LastAutoRunAt) >= time.Duration(intervalMinutes)*time.Minute
}

func recordModelDetectionAutoRun(response modelDetectionRunResponse) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	now := time.Now()
	update := bson.M{
		"last_auto_run_at": now,
		"updated_at":       now,
	}
	if response.ErrorCount > 0 {
		update["last_error"] = strings.Join(response.Errors, "\n")
	} else {
		update["last_error"] = ""
	}
	if _, err := ModelDetectionConfigCol.UpdateOne(ctx, bson.M{"_id": modelDetectionConfigID}, bson.M{"$set": update}); err != nil {
		log.Printf("Failed to update model detection auto run status: %v", err)
	}
}

func recordModelDetectionNotificationAttempt(sendErr error) {
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
		update["last_error"] = ""
	}
	if _, err := ModelDetectionConfigCol.UpdateOne(ctx, bson.M{"_id": modelDetectionConfigID}, bson.M{"$set": update}); err != nil {
		log.Printf("Failed to update model detection notification status: %v", err)
	}
}

func recordModelDetectionLastError(sendErr error) {
	if sendErr == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	now := time.Now()
	if _, err := ModelDetectionConfigCol.UpdateOne(ctx, bson.M{"_id": modelDetectionConfigID}, bson.M{"$set": bson.M{
		"last_error": sendErr.Error(),
		"updated_at": now,
	}}); err != nil {
		log.Printf("Failed to update model detection error status: %v", err)
	}
}

func applyReportToJobUpdate(update bson.M, report map[string]interface{}) {
	update["verdict"] = stringFromReport(report, "verdict")
	update["tier"] = stringFromReport(report, "tier")
	update["tier_title"] = stringFromReport(report, "tier_title")
	update["summary"] = stringFromReport(report, "summary")
	if score, ok := numberFromReport(report, "total_score"); ok {
		update["total_score"] = score
	}
	if runError := stringFromReport(report, "run_error"); runError != "" {
		update["error"] = runError
	}
}

func normalizeVeridropJobStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case modelDetectionStatusQueued:
		return modelDetectionStatusQueued
	case modelDetectionStatusRunning:
		return modelDetectionStatusRunning
	case modelDetectionStatusDone:
		return modelDetectionStatusDone
	case modelDetectionStatusError:
		return modelDetectionStatusError
	default:
		return modelDetectionStatusRunning
	}
}

func veridropEndpoint(baseURL, pathValue string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", errors.New("请先配置 Veridrop 服务地址")
	}
	if strings.HasPrefix(pathValue, "http://") || strings.HasPrefix(pathValue, "https://") {
		return pathValue, nil
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("Veridrop 服务地址必须是完整的 http:// 或 https:// 地址")
	}
	if !strings.HasPrefix(pathValue, "/") {
		pathValue = "/" + pathValue
	}
	return baseURL + pathValue, nil
}

func resolveVeridropLink(baseURL, pathValue string) string {
	if strings.TrimSpace(pathValue) == "" {
		return ""
	}
	endpoint, err := veridropEndpoint(baseURL, strings.TrimSpace(pathValue))
	if err != nil {
		return strings.TrimSpace(pathValue)
	}
	return endpoint
}

func setVeridropAuthHeader(request *http.Request, apiToken string) {
	apiToken = strings.TrimSpace(apiToken)
	if apiToken == "" {
		return
	}
	if strings.HasPrefix(strings.ToLower(apiToken), "bearer ") {
		request.Header.Set("Authorization", apiToken)
		return
	}
	request.Header.Set("Authorization", "Bearer "+apiToken)
}

func objectIDFilter(values []string) (map[string]bool, error) {
	filter := make(map[string]bool)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		id, err := primitive.ObjectIDFromHex(value)
		if err != nil {
			return nil, fmt.Errorf("无效站点 ID: %s", value)
		}
		filter[id.Hex()] = true
	}
	return filter, nil
}

func stringSet(values []string) map[string]bool {
	filter := make(map[string]bool)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			filter[value] = true
		}
	}
	return filter
}

func unixFloatTime(value float64) time.Time {
	seconds := int64(value)
	nanos := int64((value - float64(seconds)) * 1_000_000_000)
	return time.Unix(seconds, nanos)
}

func stringFromReport(report map[string]interface{}, key string) string {
	value, ok := report[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func numberFromReport(report map[string]interface{}, key string) (float64, bool) {
	value, ok := report[key]
	if !ok || value == nil {
		return 0, false
	}
	switch typed := value.(type) {
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func modelDetectionShouldNotify(config models.ModelDetectionNotificationConfig, job models.ModelDetectionJob) bool {
	if config.PushPolicy != modelDetectionPushPolicyFailures {
		return true
	}
	if job.Status == modelDetectionStatusError {
		return true
	}
	return strings.ToLower(strings.TrimSpace(job.Verdict)) != "passed"
}

func modelDetectionNotificationTitle(job models.ModelDetectionJob) string {
	if job.Status == modelDetectionStatusError {
		return "模型真实性检测异常"
	}
	return "模型真实性检测完成"
}

func modelDetectionVerdictText(job models.ModelDetectionJob) string {
	if job.Status == modelDetectionStatusError {
		return "检测异常"
	}
	verdict := strings.ToLower(strings.TrimSpace(job.Verdict))
	switch verdict {
	case "passed":
		return "通过"
	case "marginal":
		return "存疑"
	case "failed":
		return "未通过"
	case "":
		return "-"
	default:
		return verdict
	}
}

func modelDetectionSummaryText(job models.ModelDetectionJob) string {
	if job.Error != "" && job.Status == modelDetectionStatusError {
		return "错误：" + strings.ReplaceAll(job.Error, "\n", " ")
	}
	if strings.TrimSpace(job.Summary) != "" {
		return strings.TrimSpace(job.Summary)
	}
	return "报告已生成。"
}

func modelDetectionReportLinks(config models.ModelDetectionNotificationConfig, job models.ModelDetectionJob) []modelDetectionNotificationLink {
	links := make([]modelDetectionNotificationLink, 0, 2)
	seen := make(map[string]struct{}, 2)

	addLink := func(label, rawURL string) {
		rawURL = strings.TrimSpace(rawURL)
		if label == "" || rawURL == "" {
			return
		}
		if !isAbsoluteHTTPURL(rawURL) {
			return
		}
		if _, ok := seen[rawURL]; ok {
			return
		}
		seen[rawURL] = struct{}{}
		links = append(links, modelDetectionNotificationLink{Label: label, URL: rawURL})
	}

	addLink("查看报告", modelDetectionReportLink(config, job))
	addLink("查看原始报告", job.ResultURL)

	return links
}

func modelDetectionReportLink(config models.ModelDetectionNotificationConfig, job models.ModelDetectionJob) string {
	baseURL := strings.TrimRight(strings.TrimSpace(config.ReportBaseURL), "/")
	if baseURL == "" || job.ID.IsZero() {
		return ""
	}

	parsed, err := parseAbsoluteHTTPURL(baseURL)
	if err != nil {
		return ""
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/model-detection"
	query := parsed.Query()
	query.Set("jobId", job.ID.Hex())
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func isAbsoluteHTTPURL(rawURL string) bool {
	_, err := parseAbsoluteHTTPURL(rawURL)
	return err == nil
}

func parseAbsoluteHTTPURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("invalid absolute url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("url must use http or https")
	}
	return parsed, nil
}

func formatModelDetectionSite(job models.ModelDetectionJob) string {
	name := strings.TrimSpace(job.SiteName)
	if name == "" {
		name = "-"
	}
	if job.ChannelID > 0 {
		return fmt.Sprintf("%s（ID %d）", name, job.ChannelID)
	}
	return name
}

func formatModelDetectionScore(value float64) string {
	if value <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", value)
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return "-"
	}
	return formatNotificationTime(*value)
}

func displaySiteName(site models.Site) string {
	if strings.TrimSpace(site.Name) != "" {
		return strings.TrimSpace(site.Name)
	}
	if strings.TrimSpace(site.URL) != "" {
		return strings.TrimSpace(site.URL)
	}
	if site.ChannelID > 0 {
		return fmt.Sprintf("渠道 %d", site.ChannelID)
	}
	return site.ID.Hex()
}

func valueOrDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}
