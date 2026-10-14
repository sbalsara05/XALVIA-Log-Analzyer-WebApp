package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"

	"ricovr/gateway/middleware"
)

// Profile handles GET /api/profile.
// Returns the caller's uid and email from the users table.
// Requires a valid Firebase Bearer token (verified by VerifyFirebaseJWT middleware).
func Profile(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.GetString(middleware.ContextKeyUID)

		var email string
		err := db.QueryRowContext(c.Request.Context(),
			`SELECT email FROM users WHERE id = $1 AND deleted_at IS NULL`, uid).Scan(&email)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"uid":   uid,
			"email": email,
		})
	}
}
