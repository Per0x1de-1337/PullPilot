package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/keploy/PullPilot/internal/config"
	"github.com/keploy/PullPilot/internal/event"
)

func NewRouter(cfg *config.Config) *gin.Engine {
	r := gin.Default()

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	webhookHandler := event.NewWebhookHandler(cfg)

	r.POST("/webhook/github", webhookHandler.HandleGitHub)

	r.POST("/webhook/gitlab", webhookHandler.HandleGitLab)

	api := r.Group("/api")
	{

		api.POST("/analyze", webhookHandler.HandleManualAnalysis)

		api.GET("/results/:id", func(c *gin.Context) {

			c.JSON(http.StatusNotImplemented, gin.H{
				"status": "not implemented",
			})
		})
	}

	return r
}
