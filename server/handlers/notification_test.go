package handlers

import (
	"math"
	"testing"
	"time"

	"balanceserver/models"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestNotificationDueUsesActiveScheduleInterval(t *testing.T) {
	now := time.Date(2026, 6, 7, 9, 0, 0, 0, time.Local)
	lastAttempt := now.Add(-29 * time.Minute)
	config := models.NotificationConfig{
		IntervalMinutes: defaultNotificationIntervalMinute,
		LastAttemptAt:   &lastAttempt,
		Schedules: []models.NotificationSchedule{
			{StartTime: "08:00", EndTime: "12:00", IntervalMinutes: 30},
		},
	}

	if notificationDue(config, now) {
		t.Fatal("expected notification not to be due before active schedule interval elapsed")
	}

	lastAttempt = now.Add(-30 * time.Minute)
	config.LastAttemptAt = &lastAttempt
	if !notificationDue(config, now) {
		t.Fatal("expected notification to be due when active schedule interval elapsed")
	}
}

func TestNotificationDueSkipsOutsideSchedules(t *testing.T) {
	now := time.Date(2026, 6, 7, 7, 59, 0, 0, time.Local)
	config := models.NotificationConfig{
		Schedules: []models.NotificationSchedule{
			{StartTime: "08:00", EndTime: "12:00", IntervalMinutes: 30},
		},
	}

	if notificationDue(config, now) {
		t.Fatal("expected notification not to be due outside configured schedules")
	}
}

func TestNotificationDueRunsAtNewScheduleStart(t *testing.T) {
	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.Local)
	lastAttempt := time.Date(2026, 6, 7, 11, 30, 0, 0, time.Local)
	config := models.NotificationConfig{
		LastAttemptAt: &lastAttempt,
		Schedules: []models.NotificationSchedule{
			{StartTime: "08:00", EndTime: "12:00", IntervalMinutes: 30},
			{StartTime: "12:00", EndTime: "17:30", IntervalMinutes: 40},
		},
	}

	if !notificationDue(config, now) {
		t.Fatal("expected notification to be due at the start of a new schedule")
	}
}

func TestValidateNotificationSchedulesRejectsOverlap(t *testing.T) {
	err := validateNotificationSchedules([]models.NotificationSchedule{
		{StartTime: "08:00", EndTime: "12:00", IntervalMinutes: 30},
		{StartTime: "11:00", EndTime: "17:30", IntervalMinutes: 40},
	})
	if err == nil {
		t.Fatal("expected overlapping schedules to be rejected")
	}
}

func TestGrisaBalancePayloadUsesCredits(t *testing.T) {
	quota, _, err := parseBalancePayload([]byte(`{"code":0,"data":{"credits":7144660},"msg":"success"}`), siteAdapterGrisa)
	if err != nil {
		t.Fatalf("expected grisa payload to parse: %v", err)
	}

	balance := quotaToUSD(quota)
	if math.Abs(balance-357.233) > 0.000001 {
		t.Fatalf("expected balance 357.233, got %.3f", balance)
	}
}

func TestGrisaBalanceEndpoint(t *testing.T) {
	endpoint, err := siteBalanceEndpoint(models.Site{
		URL:     "https://grsaiapi.com",
		Adapter: siteAdapterGrisa,
	})
	if err != nil {
		t.Fatalf("expected grisa endpoint to build: %v", err)
	}
	if endpoint != "https://grsaiapi.com/client/openapi/getCredits" {
		t.Fatalf("unexpected grisa endpoint: %s", endpoint)
	}
}

func TestGrisaBalanceEndpointUsesDefaultHost(t *testing.T) {
	endpoint, err := siteBalanceEndpoint(models.Site{
		URL:     "https://grsai.com",
		Adapter: siteAdapterGrisa,
	})
	if err != nil {
		t.Fatalf("expected grisa endpoint to build: %v", err)
	}
	if endpoint != siteAdapterGrisaEndpoint {
		t.Fatalf("unexpected grisa endpoint: %s", endpoint)
	}
}

func TestGrisaNotificationEligibilityDoesNotRequireUserID(t *testing.T) {
	eligible := siteEligibleForBalanceNotification(models.Site{
		Status:  1,
		Token:   "token",
		Adapter: siteAdapterGrisa,
	})
	if !eligible {
		t.Fatal("expected grisa site with token to be eligible without user id")
	}
}

func TestBalanceColorThresholds(t *testing.T) {
	cases := []struct {
		name    string
		balance float64
		want    string
	}{
		{name: "below red threshold", balance: 99.99, want: "red"},
		{name: "at red threshold", balance: 100, want: "red"},
		{name: "above red threshold", balance: 100.01, want: "yellow"},
		{name: "at yellow threshold", balance: 500, want: "yellow"},
		{name: "above yellow threshold", balance: 500.01, want: "green"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := balanceColor(tc.balance, defaultRedBalanceThreshold, defaultYellowBalanceThreshold); got != tc.want {
				t.Fatalf("balanceColor(%v) = %s, want %s", tc.balance, got, tc.want)
			}
		})
	}
}

func TestBalanceColorUsesConfiguredThresholds(t *testing.T) {
	redThreshold := 50.0
	yellowThreshold := 200.0

	cases := []struct {
		balance float64
		want    string
	}{
		{balance: 50, want: "red"},
		{balance: 50.01, want: "yellow"},
		{balance: 200, want: "yellow"},
		{balance: 200.01, want: "green"},
	}

	for _, tc := range cases {
		if got := balanceColor(tc.balance, redThreshold, yellowThreshold); got != tc.want {
			t.Fatalf("balanceColor(%v) = %s, want %s", tc.balance, got, tc.want)
		}
	}
}

func TestNormalizeNotificationConfigDefaultsBalanceWarningThresholds(t *testing.T) {
	config := models.NotificationConfig{}
	normalizeNotificationConfig(&config)

	if config.RedBalanceThreshold != defaultRedBalanceThreshold {
		t.Fatalf("red threshold = %v, want %v", config.RedBalanceThreshold, defaultRedBalanceThreshold)
	}
	if config.YellowBalanceThreshold != defaultYellowBalanceThreshold {
		t.Fatalf("yellow threshold = %v, want %v", config.YellowBalanceThreshold, defaultYellowBalanceThreshold)
	}
}

func TestValidateNotificationConfigRejectsRedThresholdAboveYellowThreshold(t *testing.T) {
	config := defaultNotificationConfig()
	config.RedBalanceThreshold = 501
	config.YellowBalanceThreshold = 500

	if err := validateNotificationConfig(config, false); err == nil {
		t.Fatal("expected red threshold above yellow threshold to be rejected")
	}
}

func TestFormatNotificationTimeDefaultsToShanghai(t *testing.T) {
	utcTime := time.Date(2026, 6, 9, 2, 33, 32, 0, time.UTC)
	location := loadNotificationTimeLocation("")

	if got := formatNotificationTimeInLocation(utcTime, location); got != "2026-06-09 10:33:32" {
		t.Fatalf("formatted notification time = %s, want 2026-06-09 10:33:32", got)
	}
}

func TestModelDetectionReportLinksIncludePortalAndOriginalReport(t *testing.T) {
	jobID := primitive.NewObjectID()
	links := modelDetectionReportLinks(models.ModelDetectionNotificationConfig{
		ReportBaseURL: "https://balance.example.com/app",
	}, models.ModelDetectionJob{
		ID:        jobID,
		ResultURL: "https://veridrop.example.com/results/job-1",
	})

	if len(links) != 2 {
		t.Fatalf("expected 2 report links, got %d", len(links))
	}
	if links[0].Label != "查看报告" || links[0].URL != "https://balance.example.com/app/model-detection?jobId="+jobID.Hex() {
		t.Fatalf("unexpected portal report link: %+v", links[0])
	}
	if links[1].Label != "查看原始报告" || links[1].URL != "https://veridrop.example.com/results/job-1" {
		t.Fatalf("unexpected original report link: %+v", links[1])
	}
}

func TestModelDetectionReportLinksFallBackToOriginalReport(t *testing.T) {
	links := modelDetectionReportLinks(models.ModelDetectionNotificationConfig{}, models.ModelDetectionJob{
		ID:        primitive.NewObjectID(),
		ResultURL: "https://veridrop.example.com/results/job-1",
	})

	if len(links) != 1 {
		t.Fatalf("expected 1 report link, got %d", len(links))
	}
	if links[0].Label != "查看原始报告" {
		t.Fatalf("expected original report fallback link, got %+v", links[0])
	}
}
