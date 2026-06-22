package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"balanceserver/models"

	"github.com/goccy/go-yaml"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

var (
	MongoClient             *mongo.Client
	UserCol                 *mongo.Collection
	SiteCol                 *mongo.Collection
	NotificationConfigCol   *mongo.Collection
	UpstreamSiteCol              *mongo.Collection
	ModelDetectionConfigCol      *mongo.Collection
	ModelDetectionJobCol         *mongo.Collection
	ChannelAvailabilityNotifyCol       *mongo.Collection
	ChannelAvailabilityGlobalNotifyCol *mongo.Collection
	UpstreamChannelCol                 *mongo.Collection
	ChannelTestResultCol         *mongo.Collection
	CodexConfigCol               *mongo.Collection
	SiteDailyStatsCol            *mongo.Collection
	DashboardConfigCol           *mongo.Collection
)

type mongoConfigFile struct {
	MongoDB MongoConfig `yaml:"mongodb"`
}

type MongoConfig struct {
	ConnectionString string `yaml:"connection_string"`
	DatabaseName     string `yaml:"database_name"`
}

func InitDB() error {
	mongoURI, databaseName, err := MongoConnectionSettings()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return err
	}

	err = client.Ping(ctx, nil)
	if err != nil {
		return err
	}

	MongoClient = client
	db := client.Database(databaseName)
	UserCol = db.Collection("users")
	SiteCol = db.Collection("sites")
	NotificationConfigCol = db.Collection("notification_configs")
	UpstreamSiteCol = db.Collection("upstream_sites")
	ModelDetectionConfigCol = db.Collection("model_detection_configs")
	ModelDetectionJobCol = db.Collection("model_detection_jobs")
	ChannelAvailabilityNotifyCol = db.Collection("channel_availability_notify_configs")
	ChannelAvailabilityGlobalNotifyCol = db.Collection("channel_availability_global_notify_config")
	UpstreamChannelCol = db.Collection("upstream_channels")
	ChannelTestResultCol = db.Collection("channel_test_results")
	CodexConfigCol = db.Collection("codex_configs")
	SiteDailyStatsCol = db.Collection("site_daily_stats")
	DashboardConfigCol = db.Collection("dashboard_config")

	if err := ensureDashboardIndexes(ctx); err != nil {
		return fmt.Errorf("dashboard index creation failed: %w", err)
	}

	if err := migrateToUpstreamSites(ctx); err != nil {
		return fmt.Errorf("upstream sites migration failed: %w", err)
	}

	// Ensure admin exists
	err = ensureAdminExists()
	if err != nil {
		return err
	}

	return nil
}

func ensureDashboardIndexes(ctx context.Context) error {
	_, err := SiteDailyStatsCol.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "upstream_site_id", Value: 1}, {Key: "date", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	return err
}

func MongoConnectionSettings() (string, string, error) {
	config, err := LoadMongoConfig()
	if err != nil {
		return "", "", err
	}

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = config.ConnectionString
	}
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	databaseName := os.Getenv("MONGO_DATABASE")
	if databaseName == "" {
		databaseName = config.DatabaseName
	}
	if databaseName == "" {
		databaseName = "balance_db"
	}

	return mongoURI, databaseName, nil
}

func LoadMongoConfig() (MongoConfig, error) {
	if configPath := os.Getenv("MONGO_CONFIG_PATH"); configPath != "" {
		return readMongoConfig(configPath)
	}

	for _, configPath := range []string{"server/mongodb_config.yaml", "mongodb_config.yaml"} {
		config, err := readMongoConfig(configPath)
		if err == nil {
			return config, nil
		}
		if !os.IsNotExist(err) {
			return MongoConfig{}, err
		}
	}

	return MongoConfig{}, nil
}

func readMongoConfig(configPath string) (MongoConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return MongoConfig{}, err
	}

	var configFile mongoConfigFile
	if err := yaml.Unmarshal(data, &configFile); err != nil {
		return MongoConfig{}, fmt.Errorf("failed to parse MongoDB config %s: %w", configPath, err)
	}

	return configFile.MongoDB, nil
}

func ensureAdminExists() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var admin models.User
	err := UserCol.FindOne(ctx, bson.M{"username": "admin"}).Decode(&admin)
	if err == mongo.ErrNoDocuments {
		// Generate random password
		bytes := make([]byte, 8)
		if _, err := rand.Read(bytes); err != nil {
			return err
		}
		randomPassword := hex.EncodeToString(bytes)

		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(randomPassword), bcrypt.DefaultCost)
		if err != nil {
			return err
		}

		admin = models.User{
			Username: "admin",
			Password: string(hashedPassword),
		}

		_, err = UserCol.InsertOne(ctx, admin)
		if err != nil {
			return err
		}

		fmt.Println("=====================================================")
		fmt.Println("Admin user created successfully")
		fmt.Printf("Username: admin\n")
		fmt.Printf("Password: %s\n", randomPassword)
		fmt.Println("Please login and change your password as soon as possible.")
		fmt.Println("=====================================================")
	} else if err != nil {
		return err
	}

	return nil
}
