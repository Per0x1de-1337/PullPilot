package event

import (
	"io/ioutil"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/keploy/PullPilot/internal/config"
)

type WebhookHandler struct {
	cfg       *config.Config
	processor *Processor
}

func NewWebhookHandler(cfg *config.Config) *WebhookHandler {
	return &WebhookHandler{
		cfg:       cfg,
		processor: NewProcessor(cfg),
	}
}

func (h *WebhookHandler) HandleGitHub(c *gin.Context) {

	signature := c.GetHeader("X-Hub-Signature-256")
	if signature == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing signature"})
		return
	}

	body, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read request body"})
		return
	}

	eventType := c.GetHeader("X-GitHub-Event")

	if eventType == "pull_request" {
		go func() {
			log.Printf("webhook file mein hoon ")
			if err := h.processor.ProcessGitHubEvent(eventType, body); err != nil {
				log.Printf("Failed to process GitHub event: %v", err)
			}
		}()
	}

	c.JSON(http.StatusOK, gin.H{"status": "processing"})
}

func (h *WebhookHandler) HandleGitLab(c *gin.Context) {

	body, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read request body"})
		return
	}

	eventType := c.GetHeader("X-Gitlab-Event")

	if eventType == "Merge Request Hook" {
		go func() {
			if err := h.processor.ProcessGitLabEvent(eventType, body); err != nil {
				log.Printf("Failed to process GitLab event: %v", err)
			}
		}()
	}

	c.JSON(http.StatusOK, gin.H{"status": "processing"})
}

func (h *WebhookHandler) HandleManualAnalysis(c *gin.Context) {

	c.JSON(http.StatusNotImplemented, gin.H{"status": "not implemented"})
}
