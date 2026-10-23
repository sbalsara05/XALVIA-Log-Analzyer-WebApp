package middleware

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

const deviceReadQuery = `
	WITH RECURSIVE org_tree AS (
		SELECT id, parent_org_id
		FROM orgs
		WHERE id = (SELECT org_id FROM devices WHERE id = $1 AND deleted_at IS NULL)
		  AND deleted_at IS NULL
		UNION ALL
		SELECT o.id, o.parent_org_id
		FROM orgs o
		INNER JOIN org_tree ot ON o.id = ot.parent_org_id
		WHERE o.deleted_at IS NULL
	)
	SELECT EXISTS (
		SELECT 1
		FROM org_tree ot
		JOIN org_users ou ON ot.id = ou.org_id
		JOIN users u ON u.id = ou.user_id
		WHERE ou.user_id = $2
		  AND ou.revoked_at IS NULL
		  AND u.deleted_at IS NULL
		  AND (ou.role = 'admin' OR ou.can_read = true)
	);`

const deviceUploadQuery = `
	WITH RECURSIVE org_tree AS (
		SELECT id, parent_org_id
		FROM orgs
		WHERE id = (SELECT org_id FROM devices WHERE id = $1 AND deleted_at IS NULL)
		  AND deleted_at IS NULL
		UNION ALL
		SELECT o.id, o.parent_org_id
		FROM orgs o
		INNER JOIN org_tree ot ON o.id = ot.parent_org_id
		WHERE o.deleted_at IS NULL
	)
	SELECT EXISTS (
		SELECT 1
		FROM org_tree ot
		JOIN org_users ou ON ot.id = ou.org_id
		JOIN users u ON u.id = ou.user_id
		WHERE ou.user_id = $2
		  AND ou.revoked_at IS NULL
		  AND u.deleted_at IS NULL
		  AND (ou.role = 'admin' OR ou.can_upload = true)
	);`

// RequireDeviceAccess returns a Gin middleware that verifies the calling user has
// the required permission on the device they are requesting.
//
// requiredAction must be "read" or "upload". The check is:
//   - If the user holds role='admin' in any org in the device's ownership chain → allow.
//   - Otherwise, check the specific flag (can_read or can_upload) in org_users.
//
// All device-scoped routes must supply mac_id as a URL query parameter.
// Aborts with 403 Forbidden if the user has no matching permission path.
func RequireDeviceAccess(db *sql.DB, requiredAction string) gin.HandlerFunc {
	var query string
	switch requiredAction {
	case "read":
		query = deviceReadQuery
	case "upload":
		query = deviceUploadQuery
	default:
		panic(fmt.Sprintf("RequireDeviceAccess: requiredAction must be 'read' or 'upload', got %q", requiredAction))
	}

	return func(c *gin.Context) {
		uid := c.GetString(ContextKeyUID)
		macID := c.Query("mac_id")
		source := "query"

		// POST routes (e.g. /signed-url/upload) carry mac_id in the JSON body.
		// Peek at the body and restore it so the handler can read it again.
		if macID == "" && c.Request.Body != nil {
			raw, _ := io.ReadAll(c.Request.Body)
			c.Request.Body = io.NopCloser(bytes.NewBuffer(raw))
			var tmp struct {
				MacID string `json:"mac_id"`
			}
			_ = json.Unmarshal(raw, &tmp)
			macID = tmp.MacID
			source = "body"
		}

		log.Printf("[DEBUG] RequireDeviceAccess action=%s path=%s uid=%s mac_id=%q source=%s",
			requiredAction, c.FullPath(), uid, macID, source)

		if uid == "" || macID == "" {
			log.Printf("[DEBUG] RequireDeviceAccess DENIED: missing uid or mac_id (uid=%q mac_id=%q)", uid, macID)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}

		var hasAccess bool
		err := db.QueryRowContext(c.Request.Context(), query, macID, uid).Scan(&hasAccess)
		if err != nil {
			log.Printf("[DEBUG] RequireDeviceAccess DB error action=%s mac_id=%q uid=%s: %v",
				requiredAction, macID, uid, err)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		if !hasAccess {
			log.Printf("[DEBUG] RequireDeviceAccess DENIED: no %s permission for uid=%s mac_id=%q",
				requiredAction, uid, macID)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}

		log.Printf("[DEBUG] RequireDeviceAccess ALLOWED action=%s uid=%s mac_id=%q", requiredAction, uid, macID)
		c.Next()
	}
}
