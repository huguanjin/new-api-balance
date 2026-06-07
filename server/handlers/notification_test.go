package handlers

import (
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
