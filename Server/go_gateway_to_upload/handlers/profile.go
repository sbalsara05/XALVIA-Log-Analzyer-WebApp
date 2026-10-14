package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ricovr/gateway/middleware"
	"ricovr/gateway/models"
)

// Profile handles GET /api/profile — requires valid Firebase Bearer token + ACL entry.
// Returns the caller's uid, org, role, and permissions.
func Profile(c *gin.Context) {
	u := c.MustGet(middleware.ContextKeyUser).(models.User)
	c.JSON(http.StatusOK, gin.H{
		"uid":          u.FirebaseUID,
		"email":        u.Email,
		"organization": u.OrganizationID,
		"role":         u.Role,
		"read":         u.Read,
		"write":        u.Write,
	})
}
