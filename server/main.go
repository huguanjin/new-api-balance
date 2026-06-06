package main

import (
	"log"
	"os"
	"strings"

	"balanceserver/handlers"

	"github.com/gin-gonic/gin"
)

func main() {
	// Initialize MongoDB
	err := handlers.InitDB()
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	handlers.StartNotificationScheduler()

	r := gin.Default()

	// API routes
	api := r.Group("/api")
	{
		api.POST("/login", handlers.LoginHandler)

		// Protected routes
		protected := api.Group("/")
		protected.Use(handlers.AuthMiddleware())
		{
			protected.GET("/sites", handlers.GetSitesHandler)
			protected.POST("/sites", handlers.SaveSitesHandler)
			protected.POST("/balance/query", handlers.QueryBalanceHandler)
			protected.GET("/channels/import-config", handlers.GetChannelImportConfigHandler)
			protected.PUT("/channels/import-config", handlers.SaveChannelImportConfigHandler)
			protected.POST("/channels/import", handlers.ImportChannelsHandler)
			protected.GET("/notification", handlers.GetNotificationConfigHandler)
			protected.PUT("/notification", handlers.SaveNotificationConfigHandler)
			protected.POST("/notification/test", handlers.TestNotificationHandler)
			protected.POST("/notification/send-now", handlers.SendBalanceNotificationNowHandler)
			protected.Any("/proxy", handlers.ProxyHandler)
		}
	}

	// Serve static files for frontend embed (if embedding)
	// r.Static("/assets", "./views/dist/assets")
	// r.StaticFile("/", "./views/dist/index.html")

	addr := serverAddress()
	log.Printf("Server starting on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

func serverAddress() string {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	if strings.HasPrefix(port, ":") {
		return port
	}
	return ":" + port
}
