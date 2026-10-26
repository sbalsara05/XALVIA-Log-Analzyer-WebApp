package middleware

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

// rootOrgQuery finds the single org with no parent (the tree root).
// There is exactly one such org; the schema has no API to create a second.
// ORDER BY id keeps the answer deterministic if extra parentless orgs exist
// (e.g. inserted directly in tests): without it Postgres scans the root-name
// index and returns whichever root sorts first alphabetically.
const rootOrgQuery = `SELECT id FROM orgs WHERE parent_org_id IS NULL AND deleted_at IS NULL ORDER BY id LIMIT 1`

// RootOrgID resolves the root org ID from the database. Returns 0 and an error
// if the query fails or no root org exists.
func RootOrgID(db *sql.DB, c *gin.Context) (int, error) {
	var id int
	err := db.QueryRowContext(c.Request.Context(), rootOrgQuery).Scan(&id)
	return id, err
}

// RequireRootOrgAdmin aborts with 403 unless the caller holds role='admin' in
// the root org (the single org with no parent) or any ancestor — in practice
// this means only Ricovr employees. Use this for system-wide operations
// (e.g. deleting a user account) where no URL :org_id param is available.
func RequireRootOrgAdmin(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.GetString(ContextKeyUID)
		if uid == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		orgID, err := RootOrgID(db, c)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "auth check failed"})
			return
		}
		ok, err := IsOrgAdmin(db, c, orgID, uid)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "auth check failed"})
			return
		}
		if !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		c.Next()
	}
}
