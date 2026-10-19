package middleware

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// orgAdminQuery resolves to true when the given user ($2) holds role='admin'
// in the target org ($1) or any ancestor of it.
const orgAdminQuery = `
	WITH RECURSIVE org_tree AS (
		SELECT id, parent_org_id FROM orgs WHERE id = $1 AND deleted_at IS NULL
		UNION ALL
		SELECT o.id, o.parent_org_id FROM orgs o
		INNER JOIN org_tree ot ON o.id = ot.parent_org_id
		WHERE o.deleted_at IS NULL
	)
	SELECT EXISTS (
		SELECT 1 FROM org_tree ot
		JOIN org_users ou ON ot.id = ou.org_id
		JOIN users u ON u.id = ou.user_id
		WHERE ou.user_id = $2
		  AND ou.role = 'admin'
		  AND ou.revoked_at IS NULL
		  AND u.deleted_at IS NULL
	);`

// IsOrgAdmin reports whether uid holds role='admin' in orgID or any ancestor.
// Returns (false, nil) when the answer is no; (false, err) on a database error.
func IsOrgAdmin(db *sql.DB, c *gin.Context, orgID int, uid string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(c.Request.Context(), orgAdminQuery, orgID, uid).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// RequireOrgAdmin verifies the calling user holds role='admin' in the target
// org (identified by the :org_id URL param) OR in any ancestor org up to the
// root. This implements the downward-subtree rule: if you admin an ancestor you
// implicitly admin everything below it.
// Aborts with 403 if the check fails.
func RequireOrgAdmin(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.GetString(ContextKeyUID)
		orgIDStr := c.Param("org_id")

		if uid == "" || orgIDStr == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}

		orgID, err := strconv.Atoi(orgIDStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "org_id must be an integer"})
			return
		}

		var exists bool
		if err = db.QueryRowContext(c.Request.Context(), orgAdminQuery, orgID, uid).Scan(&exists); err != nil || !exists {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}

		c.Next()
	}
}
