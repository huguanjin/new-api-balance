package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"time"

	"balanceserver/handlers"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	username := flag.String("username", "admin", "username to reset")
	password := flag.String("password", "", "new password; generated when empty")
	flag.Parse()

	newPassword := *password
	if newPassword == "" {
		generated, err := generatePassword()
		if err != nil {
			log.Fatalf("Failed to generate password: %v", err)
		}
		newPassword = generated
	}

	mongoURI, databaseName, err := handlers.MongoConnectionSettings()
	if err != nil {
		log.Fatalf("Failed to load MongoDB config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("Failed to connect MongoDB: %v", err)
	}
	defer func() {
		disconnectCtx, disconnectCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer disconnectCancel()
		if err := client.Disconnect(disconnectCtx); err != nil {
			log.Printf("Failed to disconnect MongoDB: %v", err)
		}
	}()

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("Failed to ping MongoDB: %v", err)
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("Failed to hash password: %v", err)
	}

	users := client.Database(databaseName).Collection("users")
	result, err := users.UpdateOne(
		ctx,
		bson.M{"username": *username},
		bson.M{
			"$set": bson.M{
				"username": *username,
				"password": string(hashedPassword),
			},
		},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		log.Fatalf("Failed to reset password: %v", err)
	}

	fmt.Println("=====================================================")
	if result.UpsertedCount > 0 {
		fmt.Println("Admin user created successfully")
	} else {
		fmt.Println("Admin password reset successfully")
	}
	fmt.Printf("Username: %s\n", *username)
	fmt.Printf("Password: %s\n", newPassword)
	fmt.Printf("Database: %s\n", databaseName)
	fmt.Println("Please login and change your password as soon as possible.")
	fmt.Println("=====================================================")
}

func generatePassword() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
