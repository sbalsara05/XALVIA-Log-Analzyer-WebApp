package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Health handles GET /health — no auth required.
func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
