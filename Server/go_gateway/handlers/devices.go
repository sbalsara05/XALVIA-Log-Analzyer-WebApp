package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ricovr/gateway/middleware"
)

// macRe matches any non-empty alphanumeric device ID.
var macRe = regexp.MustCompile(`^[0-9a-zA-Z]+$`)

// normaliseMACID validates and returns an upper-case alphanumeric device ID,
// or an error if the input contains non-alphanumeric characters.
func normaliseMACID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !macRe.MatchString(raw) {
		return "", fmt.Errorf("not a valid device ID: %q", raw)
	}
	return strings.ToUpper(raw), nil
}

// ── Response / request types ─────────────────────────────────────────────────

type addDeviceRequest struct {
	MacID string `json:"mac_id" binding:"required"`
}

type deviceRow struct {
	ID      string    `json:"id"`
	OrgID   int       `json:"org_id"`
	AddedAt time.Time `json:"added_at"`
	AddedBy string    `json:"added_by"`
}

// ── Handlers ─────────────────────────────────────────────────────────────────

// ListOrgDevices handles GET /api/orgs/:org_id/devices.
// Requires RequireOrgAdmin upstream.
// Returns all active (not soft-deleted) devices that belong directly to the org.
func ListOrgDevices(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, err := strconv.Atoi(c.Param("org_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "org_id must be an integer"})
			return
		}

		rows, err := db.QueryContext(c.Request.Context(), `
			SELECT id, org_id, added_at, added_by
			FROM devices
			WHERE org_id = $1 AND deleted_at IS NULL
			ORDER BY added_at
		`, orgID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
			return
		}
		defer rows.Close()

		devices := make([]deviceRow, 0)
		for rows.Next() {
			var r deviceRow
			if err := rows.Scan(&r.ID, &r.OrgID, &r.AddedAt, &r.AddedBy); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "scan failed"})
				return
			}
			devices = append(devices, r)
		}
		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "rows error"})
			return
		}

		c.JSON(http.StatusOK, devices)
	}
}

// AddDevice handles POST /api/orgs/:org_id/devices.
// Requires RequireOrgAdmin upstream.
// Body: {"mac_id": "AABBCCDDEEFF"} (colons/hyphens accepted, normalised to upper hex).
//
// Each registration is a new row. A soft-deleted MAC can be re-registered, which
// inserts a fresh row alongside the historical one.
// Returns 409 if the MAC is already active (non-deleted) in any org.
func AddDevice(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, err := strconv.Atoi(c.Param("org_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "org_id must be an integer"})
			return
		}

		var req addDeviceRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		macID, err := normaliseMACID(req.MacID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "mac_id must be a non-empty alphanumeric string"})
			return
		}

		callerUID := c.GetString(middleware.ContextKeyUID)
		ctx := c.Request.Context()

		_, err = db.ExecContext(ctx, `
			INSERT INTO devices (id, org_id, added_by) VALUES ($1, $2, $3)
		`, macID, orgID, callerUID)
		if err != nil {
			if isUniqueViolation(err) {
				c.JSON(http.StatusConflict, gin.H{"error": "device already registered"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "device insert failed"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{"ok": true, "id": macID, "org_id": orgID})
	}
}

// RemoveDevice handles DELETE /api/orgs/:org_id/devices/:mac_id.
// Requires RequireOrgAdmin upstream.
// Soft-deletes the device by setting deleted_at and removed_by.
// Returns 200 with already_deleted=true if the device exists but is already
// soft-deleted (idempotent, consistent with DeleteOrg/DeleteUser/RevokeOrgUser).
// Returns 404 if the device has never been registered to this org.
func RemoveDevice(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, err := strconv.Atoi(c.Param("org_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "org_id must be an integer"})
			return
		}

		macID, err := normaliseMACID(c.Param("mac_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "mac_id must be a non-empty alphanumeric string"})
			return
		}

		callerUID := c.GetString(middleware.ContextKeyUID)

		ctx := c.Request.Context()

		// Check existence separately so we can distinguish "never existed" from
		// "already soft-deleted" — only the former is a 404.
		var exists bool
		err = db.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM devices WHERE id = $1 AND org_id = $2)
		`, macID, orgID).Scan(&exists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "lookup failed"})
			return
		}
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
			return
		}

		res, err := db.ExecContext(ctx, `
			UPDATE devices
			SET deleted_at = now(), removed_by = $3
			WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL
		`, macID, orgID, callerUID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			// Device exists but was already soft-deleted — idempotent success.
			c.JSON(http.StatusOK, gin.H{"ok": true, "already_deleted": true})
			return
		}

		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
