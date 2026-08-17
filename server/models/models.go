package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type User struct {
	ID       primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Username string             `bson:"username" json:"username"`
	Password string             `bson:"password" json:"-"`
}

type Site struct {
	ID             primitive.ObjectID   `bson:"_id,omitempty" json:"id"`
	ChannelID      int                  `bson:"channelId" json:"channelId"`
	Status         int                  `bson:"status" json:"status"`
	Name           string               `bson:"name" json:"name"`
	URL            string               `bson:"url" json:"url"`
	Adapter        string               `bson:"adapter" json:"adapter"`
	Token          string               `bson:"token" json:"token"`
	UserID         string               `bson:"userId" json:"userId"`
	AdminAccount   string               `bson:"admin_account,omitempty" json:"adminAccount,omitempty"`
	AdminPassword  string               `bson:"admin_password,omitempty" json:"adminPassword,omitempty"`
	Remark         string               `bson:"remark,omitempty" json:"remark,omitempty"`
	ModelDetection ModelDetectionConfig `bson:"model_detection,omitempty" json:"modelDetection,omitempty"`
}

type ModelDetectionConfig struct {
	Enabled bool                   `bson:"enabled" json:"enabled"`
	APIKey  string                 `bson:"api_key" json:"apiKey"`
	Targets []ModelDetectionTarget `bson:"targets" json:"targets"`
}

type ModelDetectionTarget struct {
	ID                        string `bson:"id" json:"id"`
	Enabled                   bool   `bson:"enabled" json:"enabled"`
	Protocol                  string `bson:"protocol" json:"protocol"`
	Model                     string `bson:"model" json:"model"`
	BaseURL                   string `bson:"base_url" json:"baseUrl"`
	Mode                      string `bson:"mode" json:"mode"`
	IncludeLongContext        bool   `bson:"include_long_context" json:"includeLongContext"`
	IncludeLongContextExtreme bool   `bson:"include_long_context_extreme" json:"includeLongContextExtreme"`
	Force                     bool   `bson:"force" json:"force"`
}

type NotificationConfig struct {
	ID                     string                 `bson:"_id,omitempty" json:"-"`
	Enabled                bool                   `bson:"enabled" json:"enabled"`
	NotificationType       string                 `bson:"notification_type" json:"notification_type"`
	WebhookURL             string                 `bson:"webhook_url" json:"webhook_url"`
	SignKey                string                 `bson:"sign_key" json:"sign_key"`
	WeworkWebhookURL       string                 `bson:"wework_webhook_url" json:"wework_webhook_url"`
	IntervalMinutes        int                    `bson:"interval_minutes" json:"interval_minutes"`
	Schedules              []NotificationSchedule `bson:"schedules" json:"schedules"`
	BalanceThreshold       float64                `bson:"balance_threshold" json:"balance_threshold"`
	RedBalanceThreshold    float64                `bson:"red_balance_threshold" json:"red_balance_threshold"`
	YellowBalanceThreshold float64                `bson:"yellow_balance_threshold" json:"yellow_balance_threshold"`
	LastAttemptAt          *time.Time             `bson:"last_attempt_at,omitempty" json:"last_attempt_at,omitempty"`
	LastSentAt             *time.Time             `bson:"last_sent_at,omitempty" json:"last_sent_at,omitempty"`
	LastError              string                 `bson:"last_error" json:"last_error"`
	UpdatedAt              time.Time              `bson:"updated_at" json:"updated_at"`
}

type NotificationSchedule struct {
	StartTime       string `bson:"start_time" json:"start_time"`
	EndTime         string `bson:"end_time" json:"end_time"`
	IntervalMinutes int    `bson:"interval_minutes" json:"interval_minutes"`
}

type UpstreamSite struct {
	ID              primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name            string             `bson:"name" json:"name"`
	URL             string             `bson:"url" json:"url"`
	Token           string             `bson:"token" json:"token"`
	UserID          string             `bson:"userId" json:"userId"`
	SkipStatusCodes []int              `bson:"skip_status_codes,omitempty" json:"skipStatusCodes,omitempty"`
	SqlDsn          string             `bson:"sql_dsn,omitempty" json:"sqlDsn,omitempty"`
	CreatedAt       time.Time          `bson:"created_at" json:"createdAt"`
	UpdatedAt       time.Time          `bson:"updated_at" json:"updatedAt"`
}

type BillExportJob struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UpstreamSiteID primitive.ObjectID `bson:"upstream_site_id" json:"upstreamSiteId"`
	Username       string             `bson:"username,omitempty" json:"username,omitempty"`
	UserID         int64              `bson:"user_id,omitempty" json:"userId,omitempty"`
	StartTS        int64              `bson:"start_ts" json:"startTs"`
	EndTS          int64              `bson:"end_ts" json:"endTs"`
	Status         string             `bson:"status" json:"status"`
	Error          string             `bson:"error,omitempty" json:"error,omitempty"`
	FilePath       string             `bson:"file_path,omitempty" json:"-"`
	FileName       string             `bson:"file_name,omitempty" json:"fileName,omitempty"`
	RowCount       int                `bson:"row_count" json:"rowCount"`
	CreatedAt      time.Time          `bson:"created_at" json:"createdAt"`
	UpdatedAt      time.Time          `bson:"updated_at" json:"updatedAt"`
}

type CustomSqlExportJob struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UpstreamSiteID primitive.ObjectID `bson:"upstream_site_id" json:"upstreamSiteId"`
	SQL            string             `bson:"sql" json:"sql"`
	Status         string             `bson:"status" json:"status"`
	Error          string             `bson:"error,omitempty" json:"error,omitempty"`
	FilePath       string             `bson:"file_path,omitempty" json:"-"`
	FileName       string             `bson:"file_name,omitempty" json:"fileName,omitempty"`
	RowCount       int                `bson:"row_count" json:"rowCount"`
	Truncated      bool               `bson:"truncated,omitempty" json:"truncated,omitempty"`
	CreatedAt      time.Time          `bson:"created_at" json:"createdAt"`
	UpdatedAt      time.Time          `bson:"updated_at" json:"updatedAt"`
}

type KeyCustomerConfig struct {
	ID               primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UpstreamSiteID   primitive.ObjectID `bson:"upstream_site_id" json:"upstreamSiteId"`
	UserIDs          []int              `bson:"user_ids" json:"userIds"`
	WarningThreshold float64            `bson:"warning_threshold" json:"warningThreshold"`
	UpdatedAt        time.Time          `bson:"updated_at" json:"updatedAt"`
}

type ModelDetectionNotificationConfig struct {
	ID                string                 `bson:"_id,omitempty" json:"-"`
	Enabled           bool                   `bson:"enabled" json:"enabled"`
	AutoDetectEnabled bool                   `bson:"auto_detect_enabled" json:"autoDetectEnabled"`
	VeridropURL       string                 `bson:"veridrop_url" json:"veridropUrl"`
	VeridropAPIToken  string                 `bson:"veridrop_api_token" json:"veridropApiToken"`
	ReportBaseURL     string                 `bson:"report_base_url" json:"reportBaseUrl"`
	NotificationType  string                 `bson:"notification_type" json:"notification_type"`
	WebhookURL        string                 `bson:"webhook_url" json:"webhook_url"`
	SignKey           string                 `bson:"sign_key" json:"sign_key"`
	WeworkWebhookURL  string                 `bson:"wework_webhook_url" json:"wework_webhook_url"`
	IntervalMinutes   int                    `bson:"interval_minutes" json:"interval_minutes"`
	Schedules         []NotificationSchedule `bson:"schedules" json:"schedules"`
	PushPolicy        string                 `bson:"push_policy" json:"pushPolicy"`
	LastAutoRunAt     *time.Time             `bson:"last_auto_run_at,omitempty" json:"last_auto_run_at,omitempty"`
	LastAttemptAt     *time.Time             `bson:"last_attempt_at,omitempty" json:"last_attempt_at,omitempty"`
	LastSentAt        *time.Time             `bson:"last_sent_at,omitempty" json:"last_sent_at,omitempty"`
	LastError         string                 `bson:"last_error" json:"last_error"`
	UpdatedAt         time.Time              `bson:"updated_at" json:"updated_at"`
}

type MonitoringGroup struct {
	Name            string                 `bson:"name" json:"name"`
	ChannelIDs      []int                  `bson:"channel_ids" json:"channelIds"`
	StatusFilter    int                    `bson:"status_filter" json:"statusFilter"`
	AutoToggle      bool                   `bson:"auto_toggle" json:"autoToggle"`
	AnyModelSuccess bool                   `bson:"any_model_success" json:"anyModelSuccess"`
	SlowThresholdMs int                    `bson:"slow_threshold_ms" json:"slowThresholdMs"`
	SkipStatusCodes []int                  `bson:"skip_status_codes,omitempty" json:"skipStatusCodes,omitempty"`
	Schedules       []NotificationSchedule `bson:"schedules" json:"schedules"`
	LastAttemptAt   *time.Time             `bson:"last_attempt_at,omitempty" json:"lastAttemptAt,omitempty"`
}

type ChannelAvailabilityNotifyConfig struct {
	ID               primitive.ObjectID     `bson:"_id,omitempty" json:"id"`
	UpstreamSiteID   primitive.ObjectID     `bson:"upstream_site_id" json:"upstreamSiteId"`
	Enabled          bool                   `bson:"enabled" json:"enabled"`
	NotificationType string                 `bson:"notification_type" json:"notificationType"`
	WebhookURL       string                 `bson:"webhook_url" json:"webhookUrl"`
	SignKey          string                 `bson:"sign_key" json:"signKey"`
	WeworkWebhookURL string                 `bson:"wework_webhook_url" json:"weworkWebhookUrl"`
	RefreshChannels  *bool                  `bson:"refresh_channels" json:"refreshChannels"`
	MonitoringGroups []MonitoringGroup      `bson:"monitoring_groups" json:"monitoringGroups"`
	ChannelIDs       []int                  `bson:"channel_ids,omitempty" json:"channelIds,omitempty"`
	StatusFilter     int                    `bson:"status_filter,omitempty" json:"statusFilter,omitempty"`
	AutoToggle       bool                   `bson:"auto_toggle,omitempty" json:"autoToggle,omitempty"`
	SlowThresholdMs  int                    `bson:"slow_threshold_ms,omitempty" json:"slowThresholdMs,omitempty"`
	Schedules        []NotificationSchedule `bson:"schedules,omitempty" json:"schedules,omitempty"`
	LastAttemptAt    *time.Time             `bson:"last_attempt_at,omitempty" json:"lastAttemptAt,omitempty"`
	UpdatedAt        time.Time              `bson:"updated_at" json:"updatedAt"`
}

type ChannelAvailabilityGlobalNotifyConfig struct {
	ID               string                 `bson:"_id,omitempty" json:"-"`
	NotificationType string                 `bson:"notification_type" json:"notificationType"`
	WebhookURL       string                 `bson:"webhook_url" json:"webhookUrl"`
	SignKey          string                 `bson:"sign_key" json:"signKey"`
	WeworkWebhookURL string                 `bson:"wework_webhook_url" json:"weworkWebhookUrl"`
	AlwaysNotify     bool                   `bson:"always_notify" json:"alwaysNotify"`
	Schedules        []NotificationSchedule `bson:"schedules" json:"schedules"`
	UpdatedAt        time.Time              `bson:"updated_at" json:"updatedAt"`
}

type UpstreamChannel struct {
	ID               primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UpstreamSiteID   primitive.ObjectID `bson:"upstream_site_id" json:"upstreamSiteId"`
	ChannelID        int                `bson:"channelId" json:"channelId"`
	Type           int                `bson:"type" json:"type"`
	Status         int                `bson:"status" json:"status"`
	Name           string             `bson:"name" json:"name"`
	Weight         int                `bson:"weight" json:"weight"`
	CreatedTime    int64              `bson:"created_time" json:"createdTime"`
	TestTime       int64              `bson:"test_time" json:"testTime"`
	ResponseTime   int                `bson:"response_time" json:"responseTime"`
	BaseURL        string             `bson:"base_url" json:"baseUrl"`
	Balance        float64            `bson:"balance" json:"balance"`
	Models         string             `bson:"models" json:"models"`
	Group          string             `bson:"group" json:"group"`
	UsedQuota      int64              `bson:"used_quota" json:"usedQuota"`
	ModelMapping   string             `bson:"model_mapping" json:"modelMapping"`
	Priority       int                `bson:"priority" json:"priority"`
	AutoBan        int                `bson:"auto_ban" json:"autoBan"`
	Tag            string             `bson:"tag" json:"tag"`
	TestModel        string             `bson:"test_model" json:"testModel"`
	CustomTestModels []string           `bson:"custom_test_models,omitempty" json:"customTestModels,omitempty"`
	TestResult       string             `bson:"test_result" json:"testResult"`
	TestError      string             `bson:"test_error" json:"testError"`
	TestedAt       *time.Time         `bson:"tested_at,omitempty" json:"testedAt,omitempty"`
	FetchedAt      time.Time          `bson:"fetched_at" json:"fetchedAt"`
}

type ChannelTestResultDetail struct {
	Model        string `bson:"model" json:"model"`
	Success      bool   `bson:"success" json:"success"`
	ResponseTime int    `bson:"response_time" json:"responseTime"`
	Error        string `bson:"error,omitempty" json:"error,omitempty"`
}

type ChannelTestResult struct {
	ID           primitive.ObjectID       `bson:"_id,omitempty" json:"id"`
	RunID        string                   `bson:"run_id" json:"runId"`
	ChannelID    int                      `bson:"channel_id" json:"channelId"`
	Name         string                   `bson:"name" json:"name"`
	TestModel    string                   `bson:"test_model" json:"testModel"`
	Success      bool                     `bson:"success" json:"success"`
	ResponseTime int                      `bson:"response_time" json:"responseTime"`
	Error        string                   `bson:"error,omitempty" json:"error,omitempty"`
	ModelResults []ChannelTestResultDetail `bson:"model_results,omitempty" json:"modelResults,omitempty"`
	Status       int                      `bson:"status" json:"status"`
	TestedAt     time.Time                `bson:"tested_at" json:"testedAt"`
}

type CodexConfig struct {
	ID         string    `bson:"_id,omitempty" json:"-"`
	BaseURL    string    `bson:"base_url" json:"baseUrl"`
	AdminToken string    `bson:"admin_token" json:"adminToken"`
	Timeout    int       `bson:"timeout" json:"timeout"`
	UpdatedAt  time.Time `bson:"updated_at" json:"updatedAt"`
}

type DashboardRankItem struct {
	Name  string `bson:"name" json:"name"`
	Quota int64  `bson:"quota" json:"quota"`
	Count int    `bson:"count" json:"count"`
}

type SiteDailyStats struct {
	ID                primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UpstreamSiteID    primitive.ObjectID `bson:"upstream_site_id" json:"upstreamSiteId"`
	SiteName          string             `bson:"site_name" json:"siteName"`
	Date              string             `bson:"date" json:"date"`
	TotalQuota        int64              `bson:"total_quota" json:"totalQuota"`
	SuccessCount      int                `bson:"success_count" json:"successCount"`
	ErrorCount        int                `bson:"error_count" json:"errorCount"`
	TotalCount        int                `bson:"total_count" json:"totalCount"`
	ModelRanking      []DashboardRankItem `bson:"model_ranking" json:"modelRanking"`
	ChannelRanking    []DashboardRankItem `bson:"channel_ranking" json:"channelRanking"`
	UserRanking       []DashboardRankItem `bson:"user_ranking" json:"userRanking"`
	ErrorModelRanking []DashboardRankItem `bson:"error_model_ranking" json:"errorModelRanking"`
	ComputedAt        time.Time           `bson:"computed_at" json:"computedAt"`
}

type DashboardConfig struct {
	ID          string    `bson:"_id,omitempty" json:"-"`
	StartDate   string    `bson:"start_date" json:"startDate"`
	Concurrency int       `bson:"concurrency" json:"concurrency"`
	UpdatedAt   time.Time `bson:"updated_at" json:"updatedAt"`
}

type DashboardNotificationConfig struct {
	ID               string     `bson:"_id,omitempty" json:"-"`
	Enabled          bool       `bson:"enabled" json:"enabled"`
	NotificationType string     `bson:"notification_type" json:"notification_type"`
	WebhookURL       string     `bson:"webhook_url" json:"webhook_url"`
	SignKey          string     `bson:"sign_key" json:"sign_key"`
	WeworkWebhookURL string     `bson:"wework_webhook_url" json:"wework_webhook_url"`
	PushTime         string     `bson:"push_time" json:"push_time"`
	TopN             int        `bson:"top_n" json:"top_n"`
	AutoCompute      bool       `bson:"auto_compute" json:"auto_compute"`
	LastAttemptAt    *time.Time `bson:"last_attempt_at,omitempty" json:"last_attempt_at,omitempty"`
	LastSentAt       *time.Time `bson:"last_sent_at,omitempty" json:"last_sent_at,omitempty"`
	LastSentDate     string     `bson:"last_sent_date" json:"last_sent_date"`
	LastError        string     `bson:"last_error" json:"last_error"`
	UpdatedAt        time.Time  `bson:"updated_at" json:"updated_at"`
}

type DashboardComputeTask struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UpstreamSiteID primitive.ObjectID `bson:"upstream_site_id" json:"upstreamSiteId"`
	SiteName       string             `bson:"site_name" json:"siteName"`
	Date           string             `bson:"date" json:"date"`

	PaginationStatus        string `bson:"pagination_status" json:"paginationStatus"`
	PaginationError         string `bson:"pagination_error,omitempty" json:"paginationError,omitempty"`
	ModelRankingStatus      string `bson:"model_ranking_status" json:"modelRankingStatus"`
	ModelRankingError       string `bson:"model_ranking_error,omitempty" json:"modelRankingError,omitempty"`
	ChannelRankingStatus    string `bson:"channel_ranking_status" json:"channelRankingStatus"`
	ChannelRankingError     string `bson:"channel_ranking_error,omitempty" json:"channelRankingError,omitempty"`
	UserRankingStatus       string `bson:"user_ranking_status" json:"userRankingStatus"`
	UserRankingError        string `bson:"user_ranking_error,omitempty" json:"userRankingError,omitempty"`
	ErrorModelRankingStatus string `bson:"error_model_ranking_status" json:"errorModelRankingStatus"`
	ErrorModelRankingError  string `bson:"error_model_ranking_error,omitempty" json:"errorModelRankingError,omitempty"`

	StatQuota             int64            `bson:"stat_quota" json:"statQuota"`
	PaginatedQuota        int64            `bson:"paginated_quota" json:"paginatedQuota"`
	SuccessCount          int              `bson:"success_count" json:"successCount"`
	PaginatedModelQuota   map[string]int64 `bson:"paginated_model_quota,omitempty" json:"-"`
	PaginatedChannelQuota map[string]int64 `bson:"paginated_channel_quota,omitempty" json:"-"`
	ChannelIDToName       map[string]string `bson:"channel_id_to_name,omitempty" json:"-"`
	PaginatedUserQuota    map[string]int64 `bson:"paginated_user_quota,omitempty" json:"-"`

	ModelRanking      []DashboardRankItem `bson:"model_ranking,omitempty" json:"modelRanking,omitempty"`
	ChannelRanking    []DashboardRankItem `bson:"channel_ranking,omitempty" json:"channelRanking,omitempty"`
	UserRanking       []DashboardRankItem `bson:"user_ranking,omitempty" json:"userRanking,omitempty"`
	ErrorCount        int                 `bson:"error_count" json:"errorCount"`
	ErrorModelRanking []DashboardRankItem `bson:"error_model_ranking,omitempty" json:"errorModelRanking,omitempty"`

	CreatedAt time.Time `bson:"created_at" json:"createdAt"`
	UpdatedAt time.Time `bson:"updated_at" json:"updatedAt"`
}

type ModelDetectionJob struct {
	ID             primitive.ObjectID     `bson:"_id,omitempty" json:"id"`
	SiteID         primitive.ObjectID     `bson:"site_id" json:"siteId"`
	SiteName       string                 `bson:"site_name" json:"siteName"`
	ChannelID      int                    `bson:"channel_id" json:"channelId"`
	TargetID       string                 `bson:"target_id" json:"targetId"`
	Protocol       string                 `bson:"protocol" json:"protocol"`
	Model          string                 `bson:"model" json:"model"`
	Mode           string                 `bson:"mode" json:"mode"`
	BaseURL        string                 `bson:"base_url" json:"baseUrl"`
	VeridropJobID  string                 `bson:"veridrop_job_id" json:"veridropJobId"`
	Status         string                 `bson:"status" json:"status"`
	ResultURL      string                 `bson:"result_url" json:"resultUrl"`
	ImageURL       string                 `bson:"image_url" json:"imageUrl"`
	JSONURL        string                 `bson:"json_url" json:"jsonUrl"`
	Verdict        string                 `bson:"verdict" json:"verdict"`
	Tier           string                 `bson:"tier" json:"tier"`
	TierTitle      string                 `bson:"tier_title" json:"tierTitle"`
	TotalScore     float64                `bson:"total_score" json:"totalScore"`
	Summary        string                 `bson:"summary" json:"summary"`
	Error          string                 `bson:"error" json:"error"`
	Report         map[string]interface{} `bson:"report,omitempty" json:"report,omitempty"`
	CreatedAt      time.Time              `bson:"created_at" json:"createdAt"`
	SubmittedAt    *time.Time             `bson:"submitted_at,omitempty" json:"submittedAt,omitempty"`
	StartedAt      *time.Time             `bson:"started_at,omitempty" json:"startedAt,omitempty"`
	FinishedAt     *time.Time             `bson:"finished_at,omitempty" json:"finishedAt,omitempty"`
	UpdatedAt      time.Time              `bson:"updated_at" json:"updatedAt"`
	NotifiedAt     *time.Time             `bson:"notified_at,omitempty" json:"notifiedAt,omitempty"`
	LastPolledAt   *time.Time             `bson:"last_polled_at,omitempty" json:"lastPolledAt,omitempty"`
	NotificationAt *time.Time             `bson:"notification_at,omitempty" json:"notificationAt,omitempty"`
}
