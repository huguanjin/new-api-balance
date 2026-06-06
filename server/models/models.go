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
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ChannelID int                `bson:"channelId" json:"channelId"`
	Status    int                `bson:"status" json:"status"`
	Name      string             `bson:"name" json:"name"`
	URL       string             `bson:"url" json:"url"`
	Adapter   string             `bson:"adapter" json:"adapter"`
	Token     string             `bson:"token" json:"token"`
	UserID    string             `bson:"userId" json:"userId"`
}

type NotificationConfig struct {
	ID               string     `bson:"_id,omitempty" json:"-"`
	Enabled          bool       `bson:"enabled" json:"enabled"`
	NotificationType string     `bson:"notification_type" json:"notification_type"`
	WebhookURL       string     `bson:"webhook_url" json:"webhook_url"`
	SignKey          string     `bson:"sign_key" json:"sign_key"`
	WeworkWebhookURL string     `bson:"wework_webhook_url" json:"wework_webhook_url"`
	IntervalMinutes  int        `bson:"interval_minutes" json:"interval_minutes"`
	BalanceThreshold float64    `bson:"balance_threshold" json:"balance_threshold"`
	LastAttemptAt    *time.Time `bson:"last_attempt_at,omitempty" json:"last_attempt_at,omitempty"`
	LastSentAt       *time.Time `bson:"last_sent_at,omitempty" json:"last_sent_at,omitempty"`
	LastError        string     `bson:"last_error" json:"last_error"`
	UpdatedAt        time.Time  `bson:"updated_at" json:"updated_at"`
}

type ChannelImportConfig struct {
	ID        string    `bson:"_id,omitempty" json:"-"`
	URL       string    `bson:"url" json:"url"`
	Token     string    `bson:"token" json:"token"`
	UserID    string    `bson:"userId" json:"userId"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}
