package handlers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"
	"github.com/lib/pq"

	"ricovr/gateway/middleware"
)

// CheckAdminAccess handles GET /api/me/access.
// Returns 200 if the caller holds role='admin' in at least one active org,
// 403 with code "no_admin_access" otherwise.
// Used by the GUI on login to gate entry for non-admin Firebase accounts.
func CheckAdminAccess(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.GetString(middleware.ContextKeyUID)

		var exists bool
		err := db.QueryRowContext(c.Request.Context(), `
			SELECT EXISTS (
				SELECT 1 FROM org_users ou
				JOIN users u ON u.id = ou.user_id
				WHERE ou.user_id = $1
				  AND ou.role = 'admin'
				  AND ou.revoked_at IS NULL
				  AND u.deleted_at IS NULL
			)
		`, uid).Scan(&exists)
		if err != nil {
			log.Printf("[ERROR] CheckAdminAccess: db query failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "access check failed"})
			return
		}
		if !exists {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "no admin access",
				"code":  "no_admin_access",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{"admin": true})
	}
}

// ErrFirebaseUserNotFound is a sentinel error that fakes can return from
// GetUserByEmail to trigger the same 404 path as auth.IsUserNotFound.
var ErrFirebaseUserNotFound = errors.New("firebase: user not found")

// isUniqueViolation reports whether err is a Postgres unique-constraint violation.
func isUniqueViolation(err error) bool {
	if pqErr, ok := err.(*pq.Error); ok {
		return pqErr.Code == "23505"
	}
	return false
}

// FirebaseUserAdmin is the subset of auth.Client used by UpsertOrgUser.
// Extracted as an interface so tests can inject a fake without a real Firebase project.
type FirebaseUserAdmin interface {
	GetUserByEmail(ctx context.Context, email string) (*auth.UserRecord, error)
	CreateUser(ctx context.Context, user *auth.UserToCreate) (*auth.UserRecord, error)
}

// generateRandomPassword returns a crypto-random 32-char hex string used as a
// throwaway password for newly-invited Firebase accounts. The user replaces it
// immediately via the password-reset link sent in the invite email.
func generateRandomPassword() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// orgNameRe strips any character that is not a printable ASCII letter, digit,
// space, hyphen, underscore, or dot — prevents XSS via org names.
var orgNameRe = regexp.MustCompile(`[^\p{L}\p{N} _\-.]`)

func sanitizeOrgName(s string) string {
	s = strings.TrimSpace(s)
	return orgNameRe.ReplaceAllString(s, "")
}

// ── Response / request types ────────────────────────────────────────────────

type orgRow struct {
	ID          int  `json:"id"`
	Name        string `json:"name"`
	ParentOrgID *int   `json:"parent_org_id"`
}

type orgUserRow struct {
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	CanRead   bool   `json:"can_read"`
	CanUpload bool   `json:"can_upload"`
	GrantedAt string `json:"granted_at"`
	AddedBy   string `json:"added_by"`
}

type createOrgRequest struct {
	Name        string `json:"name"          binding:"required"`
	ParentOrgID int    `json:"parent_org_id" binding:"required"`
}

type upsertOrgUserRequest struct {
	Email     string `json:"email"      binding:"required"`
	Role      string `json:"role"       binding:"required"`
	CanRead   bool   `json:"can_read"`
	CanUpload bool   `json:"can_upload"`
}

// ── Handlers ────────────────────────────────────────────────────────────────

// ListOrgs handles GET /api/orgs.
// Returns all orgs the calling user can admin — their direct admin memberships
// plus every descendant org — via a recursive downward CTE.
// Only active orgs (deleted_at IS NULL) and active memberships (revoked_at IS NULL) are included.
func ListOrgs(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.GetString("uid")

		rows, err := db.QueryContext(c.Request.Context(), `
			WITH RECURSIVE admin_orgs AS (
				SELECT o.id, o.name, o.parent_org_id
				FROM orgs o
				JOIN org_users ou ON o.id = ou.org_id
				WHERE ou.user_id = $1
				  AND ou.role = 'admin'
				  AND ou.revoked_at IS NULL
				  AND o.deleted_at IS NULL
				UNION
				SELECT o.id, o.name, o.parent_org_id
				FROM orgs o
				INNER JOIN admin_orgs ao ON o.parent_org_id = ao.id
				WHERE o.deleted_at IS NULL
			)
			SELECT id, name, parent_org_id FROM admin_orgs
		`, uid)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
			return
		}
		defer rows.Close()

		orgs := make([]orgRow, 0)
		for rows.Next() {
			var r orgRow
			if err := rows.Scan(&r.ID, &r.Name, &r.ParentOrgID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "scan failed"})
				return
			}
			orgs = append(orgs, r)
		}
		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "rows error"})
			return
		}

		c.JSON(http.StatusOK, orgs)
	}
}

// ListOrgUsers handles GET /api/orgs/:org_id/users.
// Requires RequireOrgAdmin upstream. Returns all active members (revoked_at IS NULL)
// with their role, permission flags, email, and grant timestamp.
func ListOrgUsers(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, err := strconv.Atoi(c.Param("org_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "org_id must be an integer"})
			return
		}

		rows, err := db.QueryContext(c.Request.Context(), `
			SELECT ou.user_id, u.email, ou.role, ou.can_read, ou.can_upload, ou.granted_at, ou.added_by
			FROM org_users ou
			JOIN users u ON u.id = ou.user_id
			WHERE ou.org_id = $1
			  AND ou.revoked_at IS NULL
			  AND u.deleted_at IS NULL
		`, orgID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
			return
		}
		defer rows.Close()

		users := make([]orgUserRow, 0)
		for rows.Next() {
			var r orgUserRow
			if err := rows.Scan(&r.UserID, &r.Email, &r.Role, &r.CanRead, &r.CanUpload, &r.GrantedAt, &r.AddedBy); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "scan failed"})
				return
			}
			users = append(users, r)
		}
		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "rows error"})
			return
		}

		c.JSON(http.StatusOK, users)
	}
}

// UpsertOrgUser handles POST /api/orgs/:org_id/users.
// Caller must be admin of the target org or any ancestor (enforced by RequireOrgAdmin).
//
// Accepts {email, role, can_read, can_upload}.
//   - role='admin': can_read and can_upload are forced true regardless of input.
//   - role='user':  caller specifies can_read and can_upload explicitly.
//
// If no Firebase account exists for the email, one is created and a password-reset
// invite email is sent via oob. The response includes invited=true in that case.
// If the account exists but has been soft-deleted in our DB, 404 is returned.
//
// The users row is inserted if not yet present. Permission changes use a
// temporal pattern: the active org_users row is revoked and a new one inserted,
// preserving full history. Identical permissions → no-op (200, changed=false).
func UpsertOrgUser(db *sql.DB, authClient FirebaseUserAdmin, oob OOBSender) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, err := strconv.Atoi(c.Param("org_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "org_id must be an integer"})
			return
		}

		var req upsertOrgUserRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if req.Role != "admin" && req.Role != "user" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "role must be 'admin' or 'user'"})
			return
		}

		// Admins always get full read+upload permissions — caller-supplied flags are ignored.
		if req.Role == "admin" {
			req.CanRead = true
			req.CanUpload = true
		}

		ctx := c.Request.Context()
		callerUID := c.GetString(middleware.ContextKeyUID)

		// ── Step 1: read Firebase state ──────────────────────────────────────────
		fbUser, fbLookupErr := authClient.GetUserByEmail(ctx, req.Email)
		firebaseExists := fbLookupErr == nil
		if fbLookupErr != nil && !auth.IsUserNotFound(fbLookupErr) && !errors.Is(fbLookupErr, ErrFirebaseUserNotFound) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Firebase user lookup failed"})
			return
		}

		// ── Step 2: read DB state (active record by email) ───────────────────────
		var dbUID string
		dbLookupErr := db.QueryRowContext(ctx,
			`SELECT id FROM users WHERE lower(email) = lower($1) AND deleted_at IS NULL`,
			req.Email,
		).Scan(&dbUID)
		dbExists := dbLookupErr == nil
		if dbLookupErr != nil && dbLookupErr != sql.ErrNoRows {
			log.Printf("UpsertOrgUser: DB email lookup failed email=%s: %v", req.Email, dbLookupErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "user lookup failed"})
			return
		}

		// ── Step 3: four-case state machine ──────────────────────────────────────
		//
		//  Firebase \ DB  │  absent             │  active record
		//  ───────────────┼─────────────────────┼──────────────────────────────
		//  absent         │ (1) create + invite  │ (2) stale record → 409
		//  exists         │ (3) DB-only insert   │ (4) UID match → ok
		//                 │     no email         │     UID mismatch → 409
		//
		const contactAdmin = "this email already exists in the system; please contact Ricovr admin before re-inviting"
		var invited bool

		switch {
		case !firebaseExists && !dbExists:
			// Case 1: brand-new user — create Firebase account.
			// Invite email is sent after DB commit to avoid notifying the user for a rolled-back transaction.
			var createErr error
			fbUser, createErr = authClient.CreateUser(ctx, (&auth.UserToCreate{}).
				Email(req.Email).
				EmailVerified(false).
				Password(generateRandomPassword()),
			)
			if createErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create Firebase account"})
				return
			}
			invited = true

		case !firebaseExists && dbExists:
			// Case 2: no Firebase account but DB record exists — stale state.
			c.JSON(http.StatusConflict, gin.H{"error": contactAdmin})
			return

		case firebaseExists && !dbExists:
			// Case 3: Firebase account exists but never added to our DB.
			// Add the DB record; no invite email (user already has Firebase credentials).

		case dbUID == fbUser.UID:
			// Case 4 (match): user fully in sync — proceed to org_users update only.

		default:
			// Case 4 (mismatch): active DB record belongs to a different Firebase UID.
			c.JSON(http.StatusConflict, gin.H{"error": contactAdmin})
			return
		}

		if fbUser.UID == callerUID {
			c.JSON(http.StatusForbidden, gin.H{"error": "cannot change your own permissions"})
			return
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "tx begin failed"})
			return
		}
		defer tx.Rollback()

		// Insert the users row for cases 1 and 3 (dbExists is false).
		if !dbExists {
			if _, err = tx.ExecContext(ctx,
				`INSERT INTO users (id, email, added_by) VALUES ($1, $2, $3)`,
				fbUser.UID, fbUser.Email, callerUID,
			); err != nil {
				log.Printf("UpsertOrgUser: INSERT users failed uid=%s: %v", fbUser.UID, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "user upsert failed"})
				return
			}
		}

		// Check current active row. If permissions are identical, skip all writes.
		var cur orgUserRow
		err = tx.QueryRowContext(ctx, `
			SELECT role, can_read, can_upload
			FROM org_users
			WHERE user_id = $1 AND org_id = $2 AND revoked_at IS NULL
		`, fbUser.UID, orgID).Scan(&cur.Role, &cur.CanRead, &cur.CanUpload)

		switch {
		case err == sql.ErrNoRows:
			// No active row — first grant, fall through to insert.

		case err != nil:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "permission lookup failed"})
			return

		default:
			// Active row exists — check if anything actually changed.
			if cur.Role == req.Role && cur.CanRead == req.CanRead && cur.CanUpload == req.CanUpload {
				c.JSON(http.StatusOK, gin.H{"ok": true, "user_id": fbUser.UID, "changed": false})
				return
			}
			// Revoke the current active row before inserting the new one.
			_, err = tx.ExecContext(ctx, `
				UPDATE org_users SET revoked_at = clock_timestamp(), removed_by = $3
				WHERE user_id = $1 AND org_id = $2 AND revoked_at IS NULL
			`, fbUser.UID, orgID, callerUID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "revoke failed"})
				return
			}
		}

		// Insert new active row.
		_, err = tx.ExecContext(ctx, `
			INSERT INTO org_users (user_id, org_id, role, can_read, can_upload, granted_at, added_by)
			VALUES ($1, $2, $3, $4, $5, clock_timestamp(), $6)
		`, fbUser.UID, orgID, req.Role, req.CanRead, req.CanUpload, callerUID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "permission grant failed"})
			return
		}

		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "tx commit failed"})
			return
		}

		// Send invite email only after the transaction is committed.
		// If the send fails the DB is already consistent, so log and surface a warning
		// rather than returning 500 (the user and org_users rows exist and are correct).
		resp := gin.H{"ok": true, "user_id": fbUser.UID, "changed": true, "invited": invited}
		if invited {
			if emailErr := oob.SendPasswordReset(ctx, req.Email); emailErr != nil {
				log.Printf("UpsertOrgUser: invite email failed for %s: %v", req.Email, emailErr)
				resp["email_warning"] = "invite email failed; please resend manually"
			}
		}
		c.JSON(http.StatusOK, resp)
	}
}

// RevokeOrgUser handles DELETE /api/orgs/:org_id/users/:user_id.
// Requires RequireOrgAdmin upstream.
// Soft-revokes the membership by setting revoked_at = now().
func RevokeOrgUser(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, err := strconv.Atoi(c.Param("org_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "org_id must be an integer"})
			return
		}
		userID := c.Param("user_id")
		callerUID := c.GetString(middleware.ContextKeyUID)

		if userID == callerUID {
			c.JSON(http.StatusForbidden, gin.H{"error": "cannot revoke your own membership"})
			return
		}

		res, err := db.ExecContext(c.Request.Context(), `
			UPDATE org_users
			SET revoked_at = now(), removed_by = $3
			WHERE user_id = $1 AND org_id = $2 AND revoked_at IS NULL
		`, userID, orgID, callerUID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "revoke failed"})
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			// Already revoked — idempotent success.
			c.JSON(http.StatusOK, gin.H{"ok": true, "already_revoked": true})
			return
		}

		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// CreateOrg handles POST /api/orgs.
// Any org admin may call this. The body must include parent_org_id; the caller
// must be admin of that parent org (or any ancestor of it). The Ricovr root org
// is always the ultimate root — there is no endpoint to create a second root.
//
// No org_users row is written for the creator: their access to the new org
// flows implicitly from their existing admin role on the parent (upward CTE).
func CreateOrg(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.GetString(middleware.ContextKeyUID)

		var req createOrgRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		name := sanitizeOrgName(req.Name)
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "org name is empty or invalid"})
			return
		}

		// Verify caller is admin of the requested parent org (or any ancestor).
		ok, err := middleware.IsOrgAdmin(db, c, req.ParentOrgID, uid)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "auth check failed"})
			return
		}
		if !ok {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: not an admin of the specified parent org"})
			return
		}

		var newID int
		err = db.QueryRowContext(c.Request.Context(), `
			INSERT INTO orgs (name, parent_org_id, added_by) VALUES ($1, $2, $3) RETURNING id
		`, name, req.ParentOrgID, uid).Scan(&newID)
		if err != nil {
			// Unique index violation: an active org with this name already exists under this parent.
			if isUniqueViolation(err) {
				c.JSON(http.StatusConflict, gin.H{"error": "an org with that name already exists under this parent"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "insert failed"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{"id": newID, "name": name, "parent_org_id": req.ParentOrgID})
	}
}

// DeleteOrg handles DELETE /api/orgs/:org_id.
// Soft-deletes the target org and its entire downward subtree in one transaction:
//  1. Revoke all active org_users memberships in the subtree.
//  2. Soft-delete all devices in the subtree.
//  3. Soft-delete all orgs in the subtree (target last to satisfy FK order).
//
// Returns 200 with already_deleted=true if the org does not exist or is already deleted (idempotent).
// The root org (the single org with no parent) cannot be deleted.
func DeleteOrg(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, err := strconv.Atoi(c.Param("org_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "org_id must be an integer"})
			return
		}

		// Prevent deletion of the root org (the single org with no parent).
		rootID, err := middleware.RootOrgID(db, c)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "root org check failed"})
			return
		}
		if orgID == rootID {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: cannot delete the root org"})
			return
		}

		ctx := c.Request.Context()
		callerUID := c.GetString(middleware.ContextKeyUID)

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "tx begin failed"})
			return
		}
		defer tx.Rollback()

		subtreeCTE := `
			WITH RECURSIVE subtree AS (
				SELECT id FROM orgs WHERE id = $1 AND deleted_at IS NULL
				UNION ALL
				SELECT o.id FROM orgs o
				INNER JOIN subtree s ON o.parent_org_id = s.id
				WHERE o.deleted_at IS NULL
			)`

		// Step 1: revoke active memberships in the subtree.
		_, err = tx.ExecContext(ctx, subtreeCTE+`
			UPDATE org_users SET revoked_at = now(), removed_by = $2
			WHERE org_id IN (SELECT id FROM subtree) AND revoked_at IS NULL
		`, orgID, callerUID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "membership revoke failed"})
			return
		}

		// Step 2: soft-delete devices in the subtree.
		_, err = tx.ExecContext(ctx, subtreeCTE+`
			UPDATE devices SET deleted_at = now(), removed_by = $2
			WHERE org_id IN (SELECT id FROM subtree) AND deleted_at IS NULL
		`, orgID, callerUID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "device delete failed"})
			return
		}

		// Step 3: soft-delete all orgs in the subtree.
		// Check rows affected to detect "org already deleted or never existed".
		res, err := tx.ExecContext(ctx, subtreeCTE+`
			UPDATE orgs SET deleted_at = now(), removed_by = $2
			WHERE id IN (SELECT id FROM subtree) AND deleted_at IS NULL
		`, orgID, callerUID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "org delete failed"})
			return
		}
		n, _ := res.RowsAffected()
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "tx commit failed"})
			return
		}
		if n == 0 {
			// Org was already deleted (or never existed); treat as success for idempotency.
			c.JSON(http.StatusOK, gin.H{"ok": true, "already_deleted": true})
			return
		}

		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// DeleteUser handles DELETE /api/users/:user_id.
// Super admin only — soft-deletes a user account and revokes all active org memberships
// in the same transaction so no access path remains.
func DeleteUser(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		userID := c.Param("user_id")
		callerUID := c.GetString(middleware.ContextKeyUID)

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "tx begin failed"})
			return
		}
		defer tx.Rollback()

		// Tombstone the user row. Returns 404 if already deleted.
		res, err := tx.ExecContext(ctx, `
			UPDATE users SET deleted_at = now(), removed_by = $2
			WHERE id = $1 AND deleted_at IS NULL
		`, userID, callerUID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			// Already deleted — idempotent success. defer tx.Rollback() cleans up.
			c.JSON(http.StatusOK, gin.H{"ok": true, "already_deleted": true})
			return
		}

		// Revoke all active org memberships so no access path remains.
		_, err = tx.ExecContext(ctx, `
			UPDATE org_users SET revoked_at = now(), removed_by = $2
			WHERE user_id = $1 AND revoked_at IS NULL
		`, userID, callerUID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "membership revocation failed"})
			return
		}

		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "commit failed"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
