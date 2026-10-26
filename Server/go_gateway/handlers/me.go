package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ricovr/gateway/middleware"
)

// ── Response types ───────────────────────────────────────────────────────────

type membershipRow struct {
	OrgID     int       `json:"org_id"`
	OrgName   string    `json:"org_name"`
	Role      string    `json:"role"`
	CanRead   bool      `json:"can_read"`
	CanUpload bool      `json:"can_upload"`
	GrantedAt time.Time `json:"granted_at"`
}

type deviceAccessRow struct {
	MacID     string `json:"mac_id"`
	OrgID     int    `json:"org_id"`
	OrgName   string `json:"org_name"`
	CanRead   bool   `json:"can_read"`
	CanUpload bool   `json:"can_upload"`
	Via       string `json:"via"` // "admin" or "member"
}

type myPermissionsResponse struct {
	UID         string            `json:"uid"`
	Email       string            `json:"email"`
	Memberships []membershipRow   `json:"memberships"`
	Devices     []deviceAccessRow `json:"devices"`
}

// myDeviceAccessQuery returns every active device the user can reach and the
// effective flags. It must grant exactly what RequireDeviceAccess enforces:
//   - admin in the device's org or any ancestor → read + upload ("admin")
//   - non-admin member of the device's own org → that row's flags ("member")
//
// The middleware walks up from one device; this walks down from the user's
// admin orgs. Both stop at soft-deleted orgs, so the reachable sets match.
// TestMyPermissions_ParityWithRequireDeviceAccess guards the equivalence.
const myDeviceAccessQuery = `
	WITH RECURSIVE admin_tree AS (
		SELECT o.id FROM orgs o
		JOIN org_users ou ON ou.org_id = o.id
		WHERE ou.user_id = $1
		  AND ou.role = 'admin'
		  AND ou.revoked_at IS NULL
		  AND o.deleted_at IS NULL
		UNION
		SELECT o.id FROM orgs o
		INNER JOIN admin_tree t ON o.parent_org_id = t.id
		WHERE o.deleted_at IS NULL
	),
	grants AS (
		SELECT d.id AS mac_id, d.org_id, true AS can_read, true AS can_upload, true AS via_admin
		FROM devices d
		WHERE d.deleted_at IS NULL
		  AND d.org_id IN (SELECT id FROM admin_tree)
		UNION ALL
		SELECT d.id, d.org_id, ou.can_read, ou.can_upload, false
		FROM devices d
		JOIN org_users ou ON ou.org_id = d.org_id
		WHERE d.deleted_at IS NULL
		  AND ou.user_id = $1
		  AND ou.role != 'admin'
		  AND ou.revoked_at IS NULL
	)
	SELECT g.mac_id, g.org_id, o.name,
	       bool_or(g.can_read), bool_or(g.can_upload), bool_or(g.via_admin)
	FROM grants g
	JOIN orgs o ON o.id = g.org_id
	GROUP BY g.mac_id, g.org_id, o.name
	HAVING bool_or(g.can_read) OR bool_or(g.can_upload)
	ORDER BY o.name, g.mac_id`

// ── Handlers ─────────────────────────────────────────────────────────────────

// MyPermissions handles GET /api/me/permissions.
// Any authenticated user may call it; it only returns the caller's own data.
// Returns the caller's active org memberships and every device they can reach
// with effective can_read / can_upload flags. A user with no memberships gets
// 200 with empty arrays. A missing or soft-deleted user gets 404.
func MyPermissions(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.GetString(middleware.ContextKeyUID)
		ctx := c.Request.Context()

		var email string
		err := db.QueryRowContext(ctx,
			`SELECT email FROM users WHERE id = $1 AND deleted_at IS NULL`, uid).Scan(&email)
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		if err != nil {
			log.Printf("[ERROR] MyPermissions: user lookup failed uid=%s: %v", uid, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
			return
		}

		memberships, err := queryMemberships(ctx, db, uid)
		if err != nil {
			log.Printf("[ERROR] MyPermissions: memberships query failed uid=%s: %v", uid, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
			return
		}

		devices, err := queryDeviceAccess(ctx, db, uid)
		if err != nil {
			log.Printf("[ERROR] MyPermissions: devices query failed uid=%s: %v", uid, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
			return
		}

		c.JSON(http.StatusOK, myPermissionsResponse{
			UID:         uid,
			Email:       email,
			Memberships: memberships,
			Devices:     devices,
		})
	}
}

func queryMemberships(ctx context.Context, db *sql.DB, uid string) ([]membershipRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT ou.org_id, o.name, ou.role, ou.can_read, ou.can_upload, ou.granted_at
		FROM org_users ou
		JOIN orgs o ON o.id = ou.org_id
		WHERE ou.user_id = $1
		  AND ou.revoked_at IS NULL
		  AND o.deleted_at IS NULL
		ORDER BY o.name, ou.org_id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	memberships := make([]membershipRow, 0)
	for rows.Next() {
		var r membershipRow
		if err := rows.Scan(&r.OrgID, &r.OrgName, &r.Role, &r.CanRead, &r.CanUpload, &r.GrantedAt); err != nil {
			return nil, err
		}
		memberships = append(memberships, r)
	}
	return memberships, rows.Err()
}

func queryDeviceAccess(ctx context.Context, db *sql.DB, uid string) ([]deviceAccessRow, error) {
	rows, err := db.QueryContext(ctx, myDeviceAccessQuery, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	devices := make([]deviceAccessRow, 0)
	for rows.Next() {
		var r deviceAccessRow
		var viaAdmin bool
		if err := rows.Scan(&r.MacID, &r.OrgID, &r.OrgName, &r.CanRead, &r.CanUpload, &viaAdmin); err != nil {
			return nil, err
		}
		r.Via = "member"
		if viaAdmin {
			r.Via = "admin"
		}
		devices = append(devices, r)
	}
	return devices, rows.Err()
}
