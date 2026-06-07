package handlers

import (
	"math"
	"testing"
	"time"

	"balanceserver/models"
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
