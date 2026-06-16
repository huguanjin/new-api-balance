package main

import (
	"log"
	"net/http"
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
	handlers.StartModelDetectionScheduler()
	handlers.StartChannelAvailabilityScheduler()

	r := gin.Default()
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// API routes
	api := r.Group("/api")
	{
		api.POST("/login", handlers.LoginHandler)

		// Protected routes
		protected := api.Group("/")
		protected.Use(handlers.AuthMiddleware())
		{
			protected.GET("/sites", handlers.GetSitesHandler)
			protected.PUT("/user/password", handlers.ChangePasswordHandler)
			protected.POST("/sites", handlers.SaveSitesHandler)
			protected.PUT("/sites/:id/model-detection", handlers.SaveSiteModelDetectionHandler)
			protected.POST("/balance/query", handlers.QueryBalanceHandler)
			protected.POST("/channels/import", handlers.ImportChannelsHandler)
			protected.GET("/upstream-sites", handlers.ListUpstreamSitesHandler)
			protected.POST("/upstream-sites", handlers.CreateUpstreamSiteHandler)
			protected.PUT("/upstream-sites/:id", handlers.UpdateUpstreamSiteHandler)
			protected.DELETE("/upstream-sites/:id", handlers.DeleteUpstreamSiteHandler)
			protected.GET("/notification", handlers.GetNotificationConfigHandler)
			protected.PUT("/notification", handlers.SaveNotificationConfigHandler)
			protected.POST("/notification/test", handlers.TestNotificationHandler)
			protected.POST("/notification/send-now", handlers.SendBalanceNotificationNowHandler)
			protected.GET("/model-detection/config", handlers.GetModelDetectionNotificationConfigHandler)
			protected.PUT("/model-detection/config", handlers.SaveModelDetectionNotificationConfigHandler)
			protected.POST("/model-detection/test-notification", handlers.TestModelDetectionNotificationHandler)
			protected.POST("/model-detection/run", handlers.RunModelDetectionHandler)
			protected.GET("/model-detection/jobs", handlers.GetModelDetectionJobsHandler)
			protected.GET("/model-detection/jobs/:id/report", handlers.GetModelDetectionJobReportHandler)
			protected.POST("/model-detection/jobs/:id/push", handlers.PushModelDetectionJobReportHandler)
			protected.POST("/channel-availability/fetch", handlers.FetchUpstreamChannelsHandler)
			protected.GET("/channel-availability/channels", handlers.GetUpstreamChannelsHandler)
			protected.GET("/channel-availability/groups", handlers.FetchUpstreamGroupsHandler)
			protected.POST("/channel-availability/test", handlers.TestChannelAvailabilityHandler)
			protected.POST("/channel-availability/test/:channelId", handlers.TestSingleChannelAvailabilityHandler)
			protected.POST("/channel-availability/test-models/:channelId", handlers.TestChannelModelsHandler)
			protected.PUT("/channel-availability/channels/:channelId/test-models", handlers.SaveChannelCustomTestModelsHandler)
			protected.POST("/channel-availability/batch-status", handlers.BatchUpdateChannelStatusHandler)
			protected.POST("/channel-availability/delete", handlers.DeleteUpstreamChannelsHandler)
			protected.GET("/channel-availability/notify-config", handlers.GetChannelAvailabilityNotifyConfigHandler)
			protected.PUT("/channel-availability/notify-config", handlers.SaveChannelAvailabilityNotifyConfigHandler)
			protected.GET("/channel-availability/global-notify-config", handlers.GetChannelAvailabilityGlobalNotifyConfigHandler)
			protected.PUT("/channel-availability/global-notify-config", handlers.SaveChannelAvailabilityGlobalNotifyConfigHandler)
			protected.POST("/channel-availability/notify-test", handlers.TestChannelAvailabilityNotifyHandler)
			protected.POST("/channel-availability/notify-run", handlers.RunChannelAvailabilityNotifyHandler)
			protected.GET("/codex/config", handlers.GetCodexConfigHandler)
			protected.PUT("/codex/config", handlers.SaveCodexConfigHandler)
			protected.POST("/codex/query", handlers.QueryCodexBalanceHandler)
			protected.Any("/proxy", handlers.ProxyHandler)
		}
	}

	r.Static("/assets", "./views/dist/assets")
	r.StaticFile("/", "./views/dist/index.html")
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api") {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
			return
		}
		c.File("./views/dist/index.html")
	})

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
