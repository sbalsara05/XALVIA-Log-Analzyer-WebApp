// Package integration contains end-to-end tests for the control-plane handlers
// and auth middleware. Tests run against the real local Docker Postgres container.
//
// Requires:
//
//	Docker container ricovr_postgres running (cd Postgres && docker compose up -d)
//	DATABASE_URL env var set, or defaults to localhost:5432/ricovr_db
//
// Run from repo root:
//
//	go test ricovr/gateway/tests/integration -v -timeout 60s
package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync/atomic"
	"testing"

	"firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"

	"ricovr/gateway/handlers"
	"ricovr/gateway/middleware"
)

// ── Suite pass/fail summary ──────────────────────────────────────────────────

var (
	suiteTotal  atomic.Int64
	suitePassed atomic.Int64
)

func TestMain(m *testing.M) {
	code := m.Run()
	total := suiteTotal.Load()
	passed := suitePassed.Load()
	fmt.Printf("\n=== SUMMARY: %d/%d passed", passed, total)
	if passed == total {
		fmt.Println(" PASS")
	} else {
		fmt.Printf(", %d failed FAIL\n", total-passed)
	}
	os.Exit(code)
}

func recordResult(t *testing.T) {
	t.Helper()
	suiteTotal.Add(1)
	t.Cleanup(func() {
		if !t.Failed() {
			suitePassed.Add(1)
		}
	})
}

// ── DB connection ────────────────────────────────────────────────────────────

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:localpassword@localhost:5432/ricovr_db?sslmode=disable"
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("db ping: %v\n\nIs the Docker Postgres container running?\n  cd Postgres && docker compose up -d", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ── Seed helpers ─────────────────────────────────────────────────────────────
//
// Helpers use soft-deletes (UPDATE … SET deleted_at/revoked_at) matching what
// production code does, followed by a hard DELETE to keep the DB clean between
// runs. This means the soft-delete logic itself is exercised in every cleanup.
// Cleanups run LIFO (t.Cleanup), so FK order is respected automatically:
//   org_users → devices → orgs → users

const systemUID = "system-bootstrap"
const systemEmail = "system@ricovr.internal"

// ensureSystemUser inserts the system bootstrap user (self-referential added_by)
// if it does not already exist. Every test that needs to insert other rows calls
// this first so that added_by constraints are satisfied.
func ensureSystemUser(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO users (id, email, added_by)
		VALUES ($1, $2, $1)
		ON CONFLICT (id) DO NOTHING`,
		systemUID, systemEmail,
	)
	if err != nil {
		t.Fatalf("ensureSystemUser: %v", err)
	}
}

// insertUser inserts a users row with added_by = systemUID.
func insertUser(t *testing.T, db *sql.DB, id, email string) {
	t.Helper()
	ensureSystemUser(t, db)
	_, err := db.Exec(`
		INSERT INTO users (id, email, added_by)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET email = EXCLUDED.email`,
		id, email, systemUID,
	)
	if err != nil {
		t.Fatalf("insertUser(%q): %v", id, err)
	}
	t.Cleanup(func() {
		// Soft-delete first (exercises the production delete path), then hard-delete
		// to keep the DB clean for subsequent runs.
		db.Exec(`UPDATE org_users SET revoked_at = now(), removed_by = $2 WHERE user_id = $1 AND revoked_at IS NULL`, id, systemUID)
		db.Exec(`UPDATE users SET deleted_at = now(), removed_by = $2 WHERE id = $1 AND deleted_at IS NULL`, id, systemUID)
		db.Exec(`DELETE FROM org_users WHERE user_id = $1`, id)
		db.Exec(`DELETE FROM users      WHERE id       = $1`, id)
	})
}

// insertOrg inserts an org row (added_by = systemUID) and cleans up after the test.
func insertOrg(t *testing.T, db *sql.DB, name string, parentID *int) int {
	t.Helper()
	ensureSystemUser(t, db)

	// Remove any stale row left by a prior crashed run.
	if parentID == nil {
		db.Exec(`
			WITH RECURSIVE sub AS (
				SELECT id FROM orgs WHERE lower(name)=lower($1) AND parent_org_id IS NULL
				UNION ALL SELECT o.id FROM orgs o JOIN sub s ON o.parent_org_id=s.id
			)
			DELETE FROM org_users WHERE org_id IN (SELECT id FROM sub)`, name)
		db.Exec(`
			WITH RECURSIVE sub AS (
				SELECT id FROM orgs WHERE lower(name)=lower($1) AND parent_org_id IS NULL
				UNION ALL SELECT o.id FROM orgs o JOIN sub s ON o.parent_org_id=s.id
			)
			DELETE FROM orgs WHERE id IN (SELECT id FROM sub)`, name)
	} else {
		db.Exec(`
			WITH RECURSIVE sub AS (
				SELECT id FROM orgs WHERE lower(name)=lower($1) AND parent_org_id=$2
				UNION ALL SELECT o.id FROM orgs o JOIN sub s ON o.parent_org_id=s.id
			)
			DELETE FROM org_users WHERE org_id IN (SELECT id FROM sub)`, name, *parentID)
		db.Exec(`
			WITH RECURSIVE sub AS (
				SELECT id FROM orgs WHERE lower(name)=lower($1) AND parent_org_id=$2
				UNION ALL SELECT o.id FROM orgs o JOIN sub s ON o.parent_org_id=s.id
			)
			DELETE FROM orgs WHERE id IN (SELECT id FROM sub)`, name, *parentID)
	}

	var id int
	var err error
	if parentID == nil {
		err = db.QueryRow(
			`INSERT INTO orgs (name, added_by) VALUES ($1, $2) RETURNING id`,
			name, systemUID,
		).Scan(&id)
	} else {
		err = db.QueryRow(
			`INSERT INTO orgs (name, parent_org_id, added_by) VALUES ($1, $2, $3) RETURNING id`,
			name, *parentID, systemUID,
		).Scan(&id)
	}
	if err != nil {
		t.Fatalf("insertOrg(%q): %v", name, err)
	}
	t.Cleanup(func() {
		// Soft-delete first, then hard-delete for clean state.
		db.Exec(`UPDATE org_users SET revoked_at = now(), removed_by = $2 WHERE org_id = $1 AND revoked_at IS NULL`, id, systemUID)
		db.Exec(`UPDATE devices SET deleted_at = now(), removed_by = $2 WHERE org_id = $1 AND deleted_at IS NULL`, id, systemUID)
		db.Exec(`UPDATE orgs SET deleted_at = now(), removed_by = $2 WHERE id = $1 AND deleted_at IS NULL`, id, systemUID)
		db.Exec(`DELETE FROM org_users WHERE org_id = $1`, id)
		db.Exec(`DELETE FROM devices   WHERE org_id = $1`, id)
		db.Exec(`DELETE FROM orgs      WHERE id     = $1`, id)
	})
	return id
}

// insertDevice inserts a devices row (added_by = systemUID).
func insertDevice(t *testing.T, db *sql.DB, macID string, orgID int) {
	t.Helper()
	ensureSystemUser(t, db)
	_, err := db.Exec(`
		INSERT INTO devices (id, org_id, added_by) VALUES ($1, $2, $3)`,
		macID, orgID, systemUID,
	)
	if err != nil {
		t.Fatalf("insertDevice(%q): %v", macID, err)
	}
	t.Cleanup(func() {
		// Soft-delete first, then hard-delete for clean state.
		db.Exec(`UPDATE devices SET deleted_at = now(), removed_by = $2 WHERE id = $1 AND deleted_at IS NULL`, macID, systemUID)
		db.Exec(`DELETE FROM devices WHERE id = $1`, macID)
	})
}

// grantMembership revokes any active row, then inserts a fresh active org_users
// row. added_by = systemUID for test seed rows.
func grantMembership(t *testing.T, db *sql.DB, userID string, orgID int, role string, canRead, canUpload bool) {
	t.Helper()
	ensureSystemUser(t, db)
	db.Exec(`
		UPDATE org_users SET revoked_at = clock_timestamp(), removed_by = $3
		WHERE user_id = $1 AND org_id = $2 AND revoked_at IS NULL`,
		userID, orgID, systemUID)
	_, err := db.Exec(`
		INSERT INTO org_users (user_id, org_id, role, can_read, can_upload, granted_at, added_by)
		VALUES ($1, $2, $3, $4, $5, clock_timestamp(), $6)`,
		userID, orgID, role, canRead, canUpload, systemUID)
	if err != nil {
		t.Fatalf("grantMembership(user=%s org=%d): %v", userID, orgID, err)
	}
}

// revokeInDB hard-sets revoked_at and removed_by on the active row.
func revokeInDB(t *testing.T, db *sql.DB, userID string, orgID int) {
	t.Helper()
	ensureSystemUser(t, db)
	_, err := db.Exec(`
		UPDATE org_users SET revoked_at = now(), removed_by = $3
		WHERE user_id = $1 AND org_id = $2 AND revoked_at IS NULL`,
		userID, orgID, systemUID)
	if err != nil {
		t.Fatalf("revokeInDB: %v", err)
	}
}

// ── Fake Firebase ─────────────────────────────────────────────────────────────

type fakeFirebase struct {
	record       *auth.UserRecord
	err          error
	createRecord *auth.UserRecord
	createErr    error
	createCalled bool
}

func (f *fakeFirebase) GetUserByEmail(_ context.Context, _ string) (*auth.UserRecord, error) {
	return f.record, f.err
}

func (f *fakeFirebase) CreateUser(_ context.Context, _ *auth.UserToCreate) (*auth.UserRecord, error) {
	f.createCalled = true
	return f.createRecord, f.createErr
}

func fakeUserRecord(uid, email string) *auth.UserRecord {
	return &auth.UserRecord{UserInfo: &auth.UserInfo{UID: uid, Email: email}}
}

var errFakeLookup = fmt.Errorf("firebase: simulated lookup failure")

// ── Fake OOB sender ───────────────────────────────────────────────────────────

type fakeOOBSender struct {
	called bool
	err    error
}

func (f *fakeOOBSender) SendPasswordReset(_ context.Context, _ string) error {
	f.called = true
	return f.err
}

// noopOOB is used by tests that exercise existing-user paths where no invite
// email should be sent; it records whether it was unexpectedly called.
var noopOOB = &fakeOOBSender{}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

func callHandler(method, path, uid string, params gin.Params, body any, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	var buf *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	} else {
		buf = bytes.NewReader(nil)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set(middleware.ContextKeyUID, uid)
	c.Params = params
	handler(c)
	return w
}

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(w.Body).Decode(v); err != nil {
		t.Fatalf("JSON decode failed: %v\nbody: %s", err, w.Body.String())
	}
}

// chainHandlers wires a middleware in front of a handler so callHandler can
// exercise the full middleware+handler chain with a single call.
func chainHandlers(mw, h gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		mw(c)
		if !c.IsAborted() {
			h(c)
		}
	}
}

// runMiddleware fires a middleware against a minimal context (uid + optional
// org_id param + optional mac_id query). Returns the HTTP status and whether
// the next handler was reached.
func runMiddleware(mw gin.HandlerFunc, uid, orgIDParam, macIDParam string) (int, bool) {
	gin.SetMode(gin.TestMode)
	url := "/"
	if macIDParam != "" {
		url = "/?mac_id=" + macIDParam
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	if uid != "" {
		c.Set(middleware.ContextKeyUID, uid)
	}
	if orgIDParam != "" {
		c.Params = gin.Params{{Key: "org_id", Value: orgIDParam}}
	}
	reached := false
	mw(c)
	if !c.IsAborted() {
		reached = true
	}
	return w.Code, reached
}

// ── DB assertion helpers ──────────────────────────────────────────────────────

func countOrgUserRows(t *testing.T, db *sql.DB, userID string, orgID int) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM org_users WHERE user_id=$1 AND org_id=$2`,
		userID, orgID,
	).Scan(&n); err != nil {
		t.Fatalf("countOrgUserRows: %v", err)
	}
	return n
}

// activeOrgUser returns the single non-revoked org_users row for (userID, orgID).
// Fails the test if zero or more than one active row exists.
func activeOrgUser(t *testing.T, db *sql.DB, userID string, orgID int) (role string, canRead, canUpload bool, addedBy string) {
	t.Helper()
	rows, err := db.Query(`
		SELECT role, can_read, can_upload, added_by
		FROM org_users
		WHERE user_id=$1 AND org_id=$2 AND revoked_at IS NULL`,
		userID, orgID)
	if err != nil {
		t.Fatalf("activeOrgUser query: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		if err := rows.Scan(&role, &canRead, &canUpload, &addedBy); err != nil {
			t.Fatalf("activeOrgUser scan: %v", err)
		}
		count++
	}
	if count == 0 {
		t.Fatal("activeOrgUser: no active org_users row found")
	}
	if count > 1 {
		t.Fatalf("activeOrgUser: partial unique index violated — %d active rows found", count)
	}
	return
}

func intPtr(v int) *int { return &v }

// ── CreateOrg ────────────────────────────────────────────────────────────────

func TestCreateOrg_AdminOfParent_Returns200(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	parentID := insertOrg(t, db, "COParent_"+t.Name(), nil)
	callerUID := "co-caller-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	grantMembership(t, db, callerUID, parentID, "admin", true, true)

	name := "CONewOrg_" + t.Name()
	w := callHandler(http.MethodPost, "/api/orgs", callerUID, nil,
		map[string]any{"name": name, "parent_org_id": parentID},
		handlers.CreateOrg(db),
	)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		ParentOrgID int    `json:"parent_org_id"`
	}
	decodeJSON(t, w, &resp)
	if resp.ID == 0 {
		t.Error("expected non-zero id")
	}
	if resp.Name != name {
		t.Errorf("name: got %q want %q", resp.Name, name)
	}
	if resp.ParentOrgID != parentID {
		t.Errorf("parent_org_id: got %d want %d", resp.ParentOrgID, parentID)
	}
	// verify in DB
	var dbParent int
	if err := db.QueryRow(`SELECT parent_org_id FROM orgs WHERE id=$1 AND deleted_at IS NULL`, resp.ID).Scan(&dbParent); err != nil {
		t.Fatalf("org row missing: %v", err)
	}
	if dbParent != parentID {
		t.Errorf("db parent: got %d want %d", dbParent, parentID)
	}
	// added_by must be the caller
	var addedBy string
	db.QueryRow(`SELECT added_by FROM orgs WHERE id=$1`, resp.ID).Scan(&addedBy)
	if addedBy != callerUID {
		t.Errorf("added_by: got %q want %q", addedBy, callerUID)
	}
	// no explicit org_users row for creator — access flows via ancestor CTE
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM org_users WHERE user_id=$1 AND org_id=$2`, callerUID, resp.ID).Scan(&count)
	if count != 0 {
		t.Errorf("creator must not receive an explicit org_users row; got %d", count)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM org_users WHERE org_id=$1`, resp.ID)
		db.Exec(`DELETE FROM orgs      WHERE id=$1`, resp.ID)
	})
}

func TestCreateOrg_AncestorAdmin_Returns200(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	rootID := insertOrg(t, db, "COAncRoot_"+t.Name(), nil)
	childID := insertOrg(t, db, "COAncChild_"+t.Name(), &rootID)
	callerUID := "co-anc-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	// admin of root — should be able to create under child
	grantMembership(t, db, callerUID, rootID, "admin", true, true)

	w := callHandler(http.MethodPost, "/api/orgs", callerUID, nil,
		map[string]any{"name": "COAncGrand_" + t.Name(), "parent_org_id": childID},
		handlers.CreateOrg(db),
	)
	if w.Code != http.StatusCreated {
		t.Fatalf("ancestor admin creating grandchild: want 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct{ ID int `json:"id"` }
	decodeJSON(t, w, &resp)
	t.Cleanup(func() { db.Exec(`DELETE FROM orgs WHERE id=$1`, resp.ID) })
}

func TestCreateOrg_NotAdmin_Returns403(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	parentID := insertOrg(t, db, "COUnauth_"+t.Name(), nil)
	callerUID := "co-unauth-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	// no membership at all

	w := callHandler(http.MethodPost, "/api/orgs", callerUID, nil,
		map[string]any{"name": "ShouldFail_" + t.Name(), "parent_org_id": parentID},
		handlers.CreateOrg(db),
	)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateOrg_PlainRoleMember_Returns403(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	parentID := insertOrg(t, db, "COPlain_"+t.Name(), nil)
	callerUID := "co-plain-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	grantMembership(t, db, callerUID, parentID, "user", true, true)

	w := callHandler(http.MethodPost, "/api/orgs", callerUID, nil,
		map[string]any{"name": "ShouldFail_" + t.Name(), "parent_org_id": parentID},
		handlers.CreateOrg(db),
	)
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member creating org: want 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateOrg_DuplicateName_Returns409(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	parentID := insertOrg(t, db, "CODupParent_"+t.Name(), nil)
	callerUID := "co-dup-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	grantMembership(t, db, callerUID, parentID, "admin", true, true)

	name := "CODupChild_" + t.Name()
	create := func() int {
		w := callHandler(http.MethodPost, "/api/orgs", callerUID, nil,
			map[string]any{"name": name, "parent_org_id": parentID},
			handlers.CreateOrg(db),
		)
		return w.Code
	}
	if code := create(); code != http.StatusCreated {
		t.Fatalf("first create: want 201, got %d", code)
	}
	if code := create(); code != http.StatusConflict {
		t.Fatalf("duplicate create: want 409, got %d", code)
	}
}

func TestCreateOrg_InvalidName_Returns400(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	parentID := insertOrg(t, db, "COEmpty_"+t.Name(), nil)
	callerUID := "co-empty-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	grantMembership(t, db, callerUID, parentID, "admin", true, true)

	w := callHandler(http.MethodPost, "/api/orgs", callerUID, nil,
		map[string]any{"name": "   @@@###$$$", "parent_org_id": parentID},
		handlers.CreateOrg(db),
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ── UpsertOrgUser — admin grant ───────────────────────────────────────────────

func TestUpsertOrgUser_AdminGrant_ForcesReadUploadTrue(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsAdm_"+t.Name(), nil)
	callerUID := "ups-adm-caller-" + t.Name()
	targetUID := "ups-adm-target-" + t.Name()
	email := targetUID + "@example.com"
	insertUser(t, db, callerUID, callerUID+"@example.com")
	insertUser(t, db, targetUID, email)
	grantMembership(t, db, callerUID, orgID, "admin", true, true)

	// Pass can_read=false, can_upload=false — handler must override both to true for admins.
	fb := &fakeFirebase{record: fakeUserRecord(targetUID, email)}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", callerUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "admin", "can_read": false, "can_upload": false},
		handlers.UpsertOrgUser(db, fb, noopOOB),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	role, canRead, canUpload, addedBy := activeOrgUser(t, db, targetUID, orgID)
	if role != "admin" {
		t.Errorf("role: got %q want admin", role)
	}
	if !canRead {
		t.Error("admin must always have can_read=true")
	}
	if !canUpload {
		t.Error("admin must always have can_upload=true")
	}
	if addedBy != callerUID {
		t.Errorf("added_by: got %q want %q", addedBy, callerUID)
	}
}

func TestUpsertOrgUser_AdminGrant_AddsUserRowIfAbsent(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsNewUser_"+t.Name(), nil)
	targetUID := "ups-new-user-" + t.Name()
	email := targetUID + "@example.com"
	// target has no users row yet

	fb := &fakeFirebase{record: fakeUserRecord(targetUID, email)}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "admin"},
		handlers.UpsertOrgUser(db, fb, noopOOB),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var storedEmail string
	if err := db.QueryRow(`SELECT email FROM users WHERE id=$1 AND deleted_at IS NULL`, targetUID).Scan(&storedEmail); err != nil {
		t.Fatalf("users row not created: %v", err)
	}
	if storedEmail != email {
		t.Errorf("email: got %q want %q", storedEmail, email)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM org_users WHERE user_id=$1`, targetUID)
		db.Exec(`DELETE FROM users      WHERE id=$1`, targetUID)
	})
}

// ── UpsertOrgUser — user grant ────────────────────────────────────────────────

func TestUpsertOrgUser_UserGrant_RespectsExplicitFlags(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsUsr_"+t.Name(), nil)
	targetUID := "ups-usr-" + t.Name()
	email := targetUID + "@example.com"
	insertUser(t, db, targetUID, email)

	for _, tc := range []struct {
		canRead, canUpload bool
	}{
		{true, false},
		{false, true},
		{true, true},
		{false, false},
	} {
		// clean slate each iteration
		db.Exec(`DELETE FROM org_users WHERE user_id=$1 AND org_id=$2`, targetUID, orgID)

		fb := &fakeFirebase{record: fakeUserRecord(targetUID, email)}
		w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
			gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
			map[string]any{"email": email, "role": "user", "can_read": tc.canRead, "can_upload": tc.canUpload},
			handlers.UpsertOrgUser(db, fb, noopOOB),
		)
		if w.Code != http.StatusOK {
			t.Fatalf("can_read=%v can_upload=%v: want 200, got %d: %s", tc.canRead, tc.canUpload, w.Code, w.Body.String())
		}
		_, gotRead, gotUpload, _ := activeOrgUser(t, db, targetUID, orgID)
		if gotRead != tc.canRead || gotUpload != tc.canUpload {
			t.Errorf("can_read=%v can_upload=%v: got read=%v upload=%v", tc.canRead, tc.canUpload, gotRead, gotUpload)
		}
	}
}

// ── UpsertOrgUser — no-op and temporal ───────────────────────────────────────

func TestUpsertOrgUser_IdenticalPermissions_IsNoop(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsNoop_"+t.Name(), nil)
	uid := "ups-noop-" + t.Name()
	email := uid + "@example.com"
	insertUser(t, db, uid, email)
	grantMembership(t, db, uid, orgID, "user", true, false)

	before := countOrgUserRows(t, db, uid, orgID)

	fb := &fakeFirebase{record: fakeUserRecord(uid, email)}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "user", "can_read": true, "can_upload": false},
		handlers.UpsertOrgUser(db, fb, noopOOB),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct{ Changed bool `json:"changed"` }
	decodeJSON(t, w, &resp)
	if resp.Changed {
		t.Error("identical permissions must return changed=false")
	}
	if after := countOrgUserRows(t, db, uid, orgID); after != before {
		t.Errorf("row count changed on no-op: before=%d after=%d", before, after)
	}
}

func TestUpsertOrgUser_PermissionChange_RevokesOldInsertsNew(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsTempChange_"+t.Name(), nil)
	uid := "ups-temp-" + t.Name()
	email := uid + "@example.com"
	insertUser(t, db, uid, email)
	grantMembership(t, db, uid, orgID, "user", true, false)

	fb := &fakeFirebase{record: fakeUserRecord(uid, email)}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "admin"},
		handlers.UpsertOrgUser(db, fb, noopOOB),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct{ Changed bool `json:"changed"` }
	decodeJSON(t, w, &resp)
	if !resp.Changed {
		t.Error("permission change must return changed=true")
	}

	total := countOrgUserRows(t, db, uid, orgID)
	if total != 2 {
		t.Errorf("want 2 total rows after one change, got %d", total)
	}

	role, canRead, canUpload, _ := activeOrgUser(t, db, uid, orgID)
	if role != "admin" || !canRead || !canUpload {
		t.Errorf("active row: got role=%q canRead=%v canUpload=%v; want admin/true/true", role, canRead, canUpload)
	}

	var revokedCount int
	db.QueryRow(`SELECT COUNT(*) FROM org_users WHERE user_id=$1 AND org_id=$2 AND revoked_at IS NOT NULL`, uid, orgID).Scan(&revokedCount)
	if revokedCount != 1 {
		t.Errorf("want 1 revoked historical row, got %d", revokedCount)
	}
	// revoked row must also have removed_by set
	var removedBy sql.NullString
	db.QueryRow(`SELECT removed_by FROM org_users WHERE user_id=$1 AND org_id=$2 AND revoked_at IS NOT NULL`, uid, orgID).Scan(&removedBy)
	if !removedBy.Valid || removedBy.String == "" {
		t.Error("revoked historical row must have removed_by set")
	}
}

func TestUpsertOrgUser_MultipleChanges_AccumulatesHistory(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsMulti_"+t.Name(), nil)
	uid := "ups-multi-" + t.Name()
	email := uid + "@example.com"
	insertUser(t, db, uid, email)
	grantMembership(t, db, uid, orgID, "user", false, false)

	fb := &fakeFirebase{record: fakeUserRecord(uid, email)}
	changes := []map[string]any{
		{"email": email, "role": "user", "can_read": true, "can_upload": false},
		{"email": email, "role": "user", "can_read": true, "can_upload": true},
		{"email": email, "role": "admin"},
	}
	for i, body := range changes {
		w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
			gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
			body, handlers.UpsertOrgUser(db, fb, noopOOB),
		)
		if w.Code != http.StatusOK {
			t.Fatalf("change %d: want 200, got %d: %s", i, w.Code, w.Body.String())
		}
	}

	// 1 original + 3 changes = 4 total rows
	if total := countOrgUserRows(t, db, uid, orgID); total != 4 {
		t.Errorf("want 4 total rows, got %d", total)
	}
	activeOrgUser(t, db, uid, orgID) // fails if count != 1

	var revokedCount int
	db.QueryRow(`SELECT COUNT(*) FROM org_users WHERE user_id=$1 AND org_id=$2 AND revoked_at IS NOT NULL`, uid, orgID).Scan(&revokedCount)
	if revokedCount != 3 {
		t.Errorf("want 3 revoked rows, got %d", revokedCount)
	}
}

func TestUpsertOrgUser_RevokedThenRegranted_HistoryPreserved(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsRegrant_"+t.Name(), nil)
	uid := "ups-regrant-" + t.Name()
	email := uid + "@example.com"
	insertUser(t, db, uid, email)
	grantMembership(t, db, uid, orgID, "user", true, false)
	revokeInDB(t, db, uid, orgID)

	fb := &fakeFirebase{record: fakeUserRecord(uid, email)}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "user", "can_read": true, "can_upload": true},
		handlers.UpsertOrgUser(db, fb, noopOOB),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if total := countOrgUserRows(t, db, uid, orgID); total != 2 {
		t.Errorf("want 2 total rows after revoke+regrant, got %d", total)
	}
	_, _, canUpload, _ := activeOrgUser(t, db, uid, orgID)
	if !canUpload {
		t.Error("re-granted row should have can_upload=true")
	}
}

// ── UpsertOrgUser — error paths ───────────────────────────────────────────────

func TestUpsertOrgUser_CreateUserFailure_Returns500(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsFbCreate_"+t.Name(), nil)

	// GetUserByEmail returns not-found → invite path → CreateUser fails → 500.
	fb := &fakeFirebase{
		err:       handlers.ErrFirebaseUserNotFound,
		createErr: fmt.Errorf("firebase: create user failed"),
	}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": "new@example.com", "role": "user"},
		handlers.UpsertOrgUser(db, fb, noopOOB),
	)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 when CreateUser fails, got %d: %s", w.Code, w.Body.String())
	}
	if !fb.createCalled {
		t.Error("CreateUser must be called when GetUserByEmail returns not-found")
	}
}

func TestUpsertOrgUser_FirebaseLookupError_Returns500(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsFbErr_"+t.Name(), nil)

	fb := &fakeFirebase{err: errFakeLookup}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": "x@example.com", "role": "user"},
		handlers.UpsertOrgUser(db, fb, noopOOB),
	)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 for Firebase error, got %d: %s", w.Code, w.Body.String())
	}
}

// ── UpsertOrgUser — invite flow (I-1) ────────────────────────────────────────

func TestUpsertOrgUser_NewEmail_CreatesAccountAndSendsInvite(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsInvite_"+t.Name(), nil)
	newUID := "ups-invite-new-" + t.Name()
	email := newUID + "@example.com"

	oob := &fakeOOBSender{}
	fb := &fakeFirebase{
		err:          handlers.ErrFirebaseUserNotFound,
		createRecord: fakeUserRecord(newUID, email),
	}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "user", "can_read": true, "can_upload": false},
		handlers.UpsertOrgUser(db, fb, oob),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 on new-user invite, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		OK      bool   `json:"ok"`
		UserID  string `json:"user_id"`
		Changed bool   `json:"changed"`
		Invited bool   `json:"invited"`
	}
	decodeJSON(t, w, &resp)
	if !resp.OK {
		t.Error("ok must be true")
	}
	if resp.UserID != newUID {
		t.Errorf("user_id: got %q want %q", resp.UserID, newUID)
	}
	if !resp.Changed {
		t.Error("changed must be true for new grant")
	}
	if !resp.Invited {
		t.Error("invited must be true when a new Firebase account was created")
	}
	if !fb.createCalled {
		t.Error("CreateUser must have been called")
	}
	if !oob.called {
		t.Error("SendPasswordReset must have been called")
	}
	// Verify users and org_users rows were written.
	var storedEmail string
	if err := db.QueryRow(`SELECT email FROM users WHERE id=$1 AND deleted_at IS NULL`, newUID).Scan(&storedEmail); err != nil {
		t.Fatalf("users row not created: %v", err)
	}
	activeOrgUser(t, db, newUID, orgID)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM org_users WHERE user_id=$1`, newUID)
		db.Exec(`DELETE FROM users      WHERE id=$1`, newUID)
	})
}

func TestUpsertOrgUser_OOBSendFailure_Returns500(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsOOBFail_"+t.Name(), nil)
	newUID := "ups-oob-fail-" + t.Name()
	email := newUID + "@example.com"

	oob := &fakeOOBSender{err: fmt.Errorf("smtp: connection refused")}
	fb := &fakeFirebase{
		err:          handlers.ErrFirebaseUserNotFound,
		createRecord: fakeUserRecord(newUID, email),
	}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "user"},
		handlers.UpsertOrgUser(db, fb, oob),
	)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 when OOB send fails, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpsertOrgUser_ExistingFirebaseUser_NeverCallsCreateUser(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsNoCreate_"+t.Name(), nil)
	uid := "ups-no-create-" + t.Name()
	email := uid + "@example.com"
	insertUser(t, db, uid, email)

	fb := &fakeFirebase{record: fakeUserRecord(uid, email)}
	oob := &fakeOOBSender{}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "user", "can_read": true, "can_upload": false},
		handlers.UpsertOrgUser(db, fb, oob),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if fb.createCalled {
		t.Error("CreateUser must NOT be called when Firebase account already exists")
	}
	if oob.called {
		t.Error("SendPasswordReset must NOT be called for existing users")
	}
	var resp struct{ Invited bool `json:"invited"` }
	decodeJSON(t, w, &resp)
	if resp.Invited {
		t.Error("invited must be false for existing Firebase users")
	}
}

// ── UpsertOrgUser — invite flow: DB correctness ──────────────────────────────

func TestUpsertOrgUser_Invite_UsersRowStoredCorrectly(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	newUID := "ups-inv-row-" + t.Name()
	email := newUID + "@example.com"
	insertOrg(t, db, "UpsInvRow_"+t.Name(), nil) // only needed for FK

	orgID := insertOrg(t, db, "UpsInvRowOrg_"+t.Name(), nil)
	callerUID := "ups-inv-row-caller-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")

	oob := &fakeOOBSender{}
	fb := &fakeFirebase{
		err:          handlers.ErrFirebaseUserNotFound,
		createRecord: fakeUserRecord(newUID, email),
	}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", callerUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "user", "can_read": true, "can_upload": false},
		handlers.UpsertOrgUser(db, fb, oob),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	// users row: email correct, not soft-deleted, added_by = caller
	var storedEmail, addedBy string
	var deletedAt sql.NullTime
	if err := db.QueryRow(`SELECT email, added_by, deleted_at FROM users WHERE id=$1`, newUID).
		Scan(&storedEmail, &addedBy, &deletedAt); err != nil {
		t.Fatalf("users row missing: %v", err)
	}
	if storedEmail != email {
		t.Errorf("users.email: got %q want %q", storedEmail, email)
	}
	if addedBy != callerUID {
		t.Errorf("users.added_by: got %q want %q (caller)", addedBy, callerUID)
	}
	if deletedAt.Valid {
		t.Error("users.deleted_at must be NULL for a freshly invited user")
	}

	// org_users row: correct role, permissions, added_by = caller
	role, canRead, canUpload, ouAddedBy := activeOrgUser(t, db, newUID, orgID)
	if role != "user" {
		t.Errorf("role: got %q want user", role)
	}
	if !canRead {
		t.Error("can_read should be true")
	}
	if canUpload {
		t.Error("can_upload should be false")
	}
	if ouAddedBy != callerUID {
		t.Errorf("org_users.added_by: got %q want %q (caller)", ouAddedBy, callerUID)
	}

	t.Cleanup(func() {
		db.Exec(`DELETE FROM org_users WHERE user_id=$1`, newUID)
		db.Exec(`DELETE FROM users      WHERE id=$1`, newUID)
	})
}

func TestUpsertOrgUser_Invite_AdminRole_ForcesReadUploadTrue(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsInvAdmin_"+t.Name(), nil)
	newUID := "ups-inv-admin-" + t.Name()
	email := newUID + "@example.com"

	oob := &fakeOOBSender{}
	fb := &fakeFirebase{
		err:          handlers.ErrFirebaseUserNotFound,
		createRecord: fakeUserRecord(newUID, email),
	}
	// Pass can_read=false, can_upload=false — admin role must override both.
	w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "admin", "can_read": false, "can_upload": false},
		handlers.UpsertOrgUser(db, fb, oob),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct{ Invited bool `json:"invited"` }
	decodeJSON(t, w, &resp)
	if !resp.Invited {
		t.Error("invited must be true")
	}

	role, canRead, canUpload, _ := activeOrgUser(t, db, newUID, orgID)
	if role != "admin" {
		t.Errorf("role: got %q want admin", role)
	}
	if !canRead || !canUpload {
		t.Errorf("admin invite must force can_read=true, can_upload=true; got %v/%v", canRead, canUpload)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM org_users WHERE user_id=$1`, newUID)
		db.Exec(`DELETE FROM users      WHERE id=$1`, newUID)
	})
}

func TestUpsertOrgUser_Invite_UserRole_RespectsExplicitFlags(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsInvFlags_"+t.Name(), nil)

	for _, tc := range []struct{ canRead, canUpload bool }{
		{true, false},
		{false, true},
		{true, true},
		{false, false},
	} {
		newUID := fmt.Sprintf("ups-inv-flags-%v-%v-%s", tc.canRead, tc.canUpload, t.Name())
		email := newUID + "@example.com"
		db.Exec(`DELETE FROM org_users WHERE user_id=$1`, newUID)
		db.Exec(`DELETE FROM users      WHERE id=$1`, newUID)

		oob := &fakeOOBSender{}
		fb := &fakeFirebase{
			err:          handlers.ErrFirebaseUserNotFound,
			createRecord: fakeUserRecord(newUID, email),
		}
		w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
			gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
			map[string]any{"email": email, "role": "user", "can_read": tc.canRead, "can_upload": tc.canUpload},
			handlers.UpsertOrgUser(db, fb, oob),
		)
		if w.Code != http.StatusOK {
			t.Fatalf("can_read=%v can_upload=%v: want 200, got %d: %s", tc.canRead, tc.canUpload, w.Code, w.Body.String())
		}
		_, gotRead, gotUpload, _ := activeOrgUser(t, db, newUID, orgID)
		if gotRead != tc.canRead || gotUpload != tc.canUpload {
			t.Errorf("can_read=%v can_upload=%v: DB got read=%v upload=%v", tc.canRead, tc.canUpload, gotRead, gotUpload)
		}
		db.Exec(`DELETE FROM org_users WHERE user_id=$1`, newUID)
		db.Exec(`DELETE FROM users      WHERE id=$1`, newUID)
	}
}

func TestUpsertOrgUser_Invite_ThenReAdd_SecondCallIsNormalFlow(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsInvReAdd_"+t.Name(), nil)
	newUID := "ups-inv-readd-" + t.Name()
	email := newUID + "@example.com"

	// First call: new email → invite flow.
	oob1 := &fakeOOBSender{}
	fb1 := &fakeFirebase{err: handlers.ErrFirebaseUserNotFound, createRecord: fakeUserRecord(newUID, email)}
	w1 := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "user", "can_read": true, "can_upload": false},
		handlers.UpsertOrgUser(db, fb1, oob1),
	)
	if w1.Code != http.StatusOK {
		t.Fatalf("first call: want 200, got %d: %s", w1.Code, w1.Body.String())
	}
	var r1 struct{ Invited bool `json:"invited"` }
	decodeJSON(t, w1, &r1)
	if !r1.Invited {
		t.Error("first call: invited must be true")
	}
	if !oob1.called {
		t.Error("first call: OOB must have been called")
	}

	// Second call: same email, Firebase account now exists → normal upsert (no invite).
	oob2 := &fakeOOBSender{}
	fb2 := &fakeFirebase{record: fakeUserRecord(newUID, email)} // GetUserByEmail succeeds
	w2 := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "admin"},
		handlers.UpsertOrgUser(db, fb2, oob2),
	)
	if w2.Code != http.StatusOK {
		t.Fatalf("second call: want 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var r2 struct {
		Changed bool `json:"changed"`
		Invited bool `json:"invited"`
	}
	decodeJSON(t, w2, &r2)
	if r2.Invited {
		t.Error("second call: invited must be false — account already existed")
	}
	if !r2.Changed {
		t.Error("second call: changed must be true — role changed from user to admin")
	}
	if fb2.createCalled {
		t.Error("second call: CreateUser must NOT be called when account exists")
	}
	if oob2.called {
		t.Error("second call: OOB must NOT be called when account exists")
	}

	// DB should now show admin row active, original user row historical.
	role, canRead, canUpload, _ := activeOrgUser(t, db, newUID, orgID)
	if role != "admin" || !canRead || !canUpload {
		t.Errorf("after re-add: want admin/true/true; got role=%q can_read=%v can_upload=%v", role, canRead, canUpload)
	}
	if total := countOrgUserRows(t, db, newUID, orgID); total != 2 {
		t.Errorf("want 2 total org_users rows (1 historical + 1 active), got %d", total)
	}

	t.Cleanup(func() {
		db.Exec(`DELETE FROM org_users WHERE user_id=$1`, newUID)
		db.Exec(`DELETE FROM users      WHERE id=$1`, newUID)
	})
}

// ── UpsertOrgUser — invite flow: negative paths ───────────────────────────────

func TestUpsertOrgUser_Invite_InvalidRole_Returns400_BeforeFirebase(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsInvBadRole_"+t.Name(), nil)

	// Even though GetUserByEmail would return not-found (invite path), role validation
	// fires first and the handler must return 400 without touching Firebase.
	fb := &fakeFirebase{err: handlers.ErrFirebaseUserNotFound}
	oob := &fakeOOBSender{}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": "new@example.com", "role": "superuser"},
		handlers.UpsertOrgUser(db, fb, oob),
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid role, got %d: %s", w.Code, w.Body.String())
	}
	if fb.createCalled {
		t.Error("CreateUser must NOT be called when role validation fails")
	}
	if oob.called {
		t.Error("OOB must NOT be called when role validation fails")
	}
}

func TestUpsertOrgUser_Invite_CallerSelf_ExistingAccount_Returns403(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsInvSelf_"+t.Name(), nil)
	callerUID := "ups-inv-self-" + t.Name()
	email := callerUID + "@example.com"
	insertUser(t, db, callerUID, email)

	// Firebase returns the caller's own record — handler must reject self-grant.
	fb := &fakeFirebase{record: fakeUserRecord(callerUID, email)}
	oob := &fakeOOBSender{}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", callerUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "user"},
		handlers.UpsertOrgUser(db, fb, oob),
	)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 for self-grant, got %d: %s", w.Code, w.Body.String())
	}
	if fb.createCalled {
		t.Error("CreateUser must NOT be called — account exists and self-grant check fires first")
	}
	if oob.called {
		t.Error("OOB must NOT be called on a rejected self-grant")
	}
}

func TestUpsertOrgUser_Invite_MissingEmail_Returns400_BeforeFirebase(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsInvNoEmail_"+t.Name(), nil)

	fb := &fakeFirebase{err: handlers.ErrFirebaseUserNotFound}
	oob := &fakeOOBSender{}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"role": "user"}, // email field omitted
		handlers.UpsertOrgUser(db, fb, oob),
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for missing email, got %d: %s", w.Code, w.Body.String())
	}
	if fb.createCalled {
		t.Error("CreateUser must NOT be called when request binding fails")
	}
	if oob.called {
		t.Error("OOB must NOT be called when request binding fails")
	}
}

func TestUpsertOrgUser_Invite_NoDBRowWrittenOnCreateUserFailure(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsInvNoRow_"+t.Name(), nil)
	newUID := "ups-inv-norow-" + t.Name()
	email := newUID + "@example.com"

	fb := &fakeFirebase{
		err:       handlers.ErrFirebaseUserNotFound,
		createErr: fmt.Errorf("firebase: quota exceeded"),
	}
	oob := &fakeOOBSender{}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "user"},
		handlers.UpsertOrgUser(db, fb, oob),
	)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d: %s", w.Code, w.Body.String())
	}
	// No users or org_users rows must have been written.
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM users WHERE id=$1`, newUID).Scan(&count)
	if count != 0 {
		t.Errorf("users row must not exist when CreateUser failed; found %d", count)
	}
	db.QueryRow(`SELECT COUNT(*) FROM org_users WHERE user_id=$1`, newUID).Scan(&count)
	if count != 0 {
		t.Errorf("org_users row must not exist when CreateUser failed; found %d", count)
	}
}

func TestUpsertOrgUser_Invite_OOBFailure_NoOrgUsersRow(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsInvOOBRow_"+t.Name(), nil)
	newUID := "ups-inv-oob-row-" + t.Name()
	email := newUID + "@example.com"

	oob := &fakeOOBSender{err: fmt.Errorf("firebase: sendOobCode: status 500")}
	fb := &fakeFirebase{
		err:          handlers.ErrFirebaseUserNotFound,
		createRecord: fakeUserRecord(newUID, email),
	}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "user"},
		handlers.UpsertOrgUser(db, fb, oob),
	)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 when OOB fails, got %d: %s", w.Code, w.Body.String())
	}
	// The DB transaction is never committed — no org_users row must exist.
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM org_users WHERE user_id=$1 AND org_id=$2`, newUID, orgID).Scan(&count)
	if count != 0 {
		t.Errorf("org_users row must not exist when OOB send failed; found %d", count)
	}
}

func TestUpsertOrgUser_Invite_FirebaseLookupTransientError_Returns500_NeverInvites(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsInvTransient_"+t.Name(), nil)

	// errFakeLookup is a non-not-found error — treat as transient/internal failure.
	fb := &fakeFirebase{err: errFakeLookup}
	oob := &fakeOOBSender{}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": "x@example.com", "role": "user"},
		handlers.UpsertOrgUser(db, fb, oob),
	)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 for transient Firebase error, got %d: %s", w.Code, w.Body.String())
	}
	// A transient error must NOT trigger the invite path.
	if fb.createCalled {
		t.Error("CreateUser must NOT be called for a transient (non-not-found) Firebase error")
	}
	if oob.called {
		t.Error("OOB must NOT be called for a transient Firebase error")
	}
}

func TestUpsertOrgUser_SoftDeletedUser_Returns404(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsDel_"+t.Name(), nil)
	targetUID := "ups-del-" + t.Name()
	email := targetUID + "@example.com"
	insertUser(t, db, targetUID, email)
	// soft-delete the user
	db.Exec(`UPDATE users SET deleted_at = now(), removed_by = $2 WHERE id = $1`, targetUID, systemUID)

	fb := &fakeFirebase{record: fakeUserRecord(targetUID, email)}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "admin"},
		handlers.UpsertOrgUser(db, fb, noopOOB),
	)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for soft-deleted user, got %d: %s", w.Code, w.Body.String())
	}
	// deleted user row must remain closed — deleted_at must still be set
	var deletedAt sql.NullTime
	db.QueryRow(`SELECT deleted_at FROM users WHERE id=$1`, targetUID).Scan(&deletedAt)
	if !deletedAt.Valid {
		t.Error("soft-deleted user row must not be reopened")
	}
}

func TestUpsertOrgUser_SoftDeletedUser_DoesNotReopenRow(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsNoReopen_"+t.Name(), nil)
	targetUID := "ups-noreopen-" + t.Name()
	email := targetUID + "@example.com"
	insertUser(t, db, targetUID, email)
	db.Exec(`UPDATE users SET deleted_at = now(), removed_by = $2 WHERE id = $1`, targetUID, systemUID)

	fb := &fakeFirebase{record: fakeUserRecord(targetUID, email)}
	callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "user", "can_read": true},
		handlers.UpsertOrgUser(db, fb, noopOOB),
	)

	// Confirm no org_users row was written for the deleted user
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM org_users WHERE user_id=$1 AND org_id=$2 AND revoked_at IS NULL`, targetUID, orgID).Scan(&count)
	if count != 0 {
		t.Errorf("no active org_users row must exist for a soft-deleted user, found %d", count)
	}
}

func TestUpsertOrgUser_InvalidRole_Returns400(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsBadRole_"+t.Name(), nil)
	uid := "ups-bad-role-" + t.Name()
	email := uid + "@example.com"
	insertUser(t, db, uid, email)

	fb := &fakeFirebase{record: fakeUserRecord(uid, email)}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "superuser"},
		handlers.UpsertOrgUser(db, fb, noopOOB),
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid role, got %d: %s", w.Code, w.Body.String())
	}
	if n := countOrgUserRows(t, db, uid, orgID); n != 0 {
		t.Errorf("invalid role request must not write any rows, got %d", n)
	}
}

func TestUpsertOrgUser_PartialUniqueIndex_EnforcedOnActiveRows(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsUniqIdx_"+t.Name(), nil)
	uid := "ups-uniq-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "user", true, false)

	// Direct INSERT of a second active row must violate the partial unique index.
	_, err := db.Exec(`
		INSERT INTO org_users (user_id, org_id, role, can_read, can_upload, granted_at, added_by)
		VALUES ($1, $2, 'admin', true, true, clock_timestamp(), $3)`,
		uid, orgID, systemUID,
	)
	if err == nil {
		t.Error("expected unique index violation inserting second active row, got nil")
	}
}

// ── UpsertOrgUser — authorization (RequireOrgAdmin chain) ────────────────────

func upsertOrgUserViaMiddleware(db *sql.DB, callerUID string, orgID int, target, email, role string) int {
	fb := &fakeFirebase{record: fakeUserRecord(target, email)}
	w := callHandler(
		http.MethodPost, fmt.Sprintf("/api/orgs/%d/users", orgID), callerUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": role, "can_read": true, "can_upload": false},
		chainHandlers(middleware.RequireOrgAdmin(db), handlers.UpsertOrgUser(db, fb, noopOOB)),
	)
	return w.Code
}

func TestUpsertOrgUser_PlainRoleCaller_Returns403(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsAuthPlain_"+t.Name(), nil)
	callerUID := "ups-auth-plain-" + t.Name()
	targetUID := "ups-auth-plain-tgt-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	insertUser(t, db, targetUID, targetUID+"@example.com")
	grantMembership(t, db, callerUID, orgID, "user", true, true)

	code := upsertOrgUserViaMiddleware(db, callerUID, orgID, targetUID, targetUID+"@example.com", "user")
	if code != http.StatusForbidden {
		t.Errorf("plain-role caller: want 403, got %d", code)
	}
}

func TestUpsertOrgUser_NoMembershipCaller_Returns403(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsAuthNone_"+t.Name(), nil)
	callerUID := "ups-auth-none-" + t.Name()
	targetUID := "ups-auth-none-tgt-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	insertUser(t, db, targetUID, targetUID+"@example.com")
	// caller has no membership at all

	code := upsertOrgUserViaMiddleware(db, callerUID, orgID, targetUID, targetUID+"@example.com", "user")
	if code != http.StatusForbidden {
		t.Errorf("no-membership caller: want 403, got %d", code)
	}
}

func TestUpsertOrgUser_SiblingOrgAdmin_Returns403(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	myOrgID := insertOrg(t, db, "UpsAuthSibMy_"+t.Name(), nil)
	targetOrgID := insertOrg(t, db, "UpsAuthSibOther_"+t.Name(), nil)
	callerUID := "ups-auth-sib-" + t.Name()
	targetUID := "ups-auth-sib-tgt-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	insertUser(t, db, targetUID, targetUID+"@example.com")
	grantMembership(t, db, callerUID, myOrgID, "admin", true, true)

	code := upsertOrgUserViaMiddleware(db, callerUID, targetOrgID, targetUID, targetUID+"@example.com", "user")
	if code != http.StatusForbidden {
		t.Errorf("sibling-org admin: want 403, got %d", code)
	}
}

func TestUpsertOrgUser_AncestorAdmin_Returns200(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	rootID := insertOrg(t, db, "UpsAuthAnc_"+t.Name(), nil)
	childID := insertOrg(t, db, "UpsAuthAncChild_"+t.Name(), &rootID)
	callerUID := "ups-auth-anc-" + t.Name()
	targetUID := "ups-auth-anc-tgt-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	insertUser(t, db, targetUID, targetUID+"@example.com")
	grantMembership(t, db, callerUID, rootID, "admin", true, true)

	code := upsertOrgUserViaMiddleware(db, callerUID, childID, targetUID, targetUID+"@example.com", "user")
	if code != http.StatusOK {
		t.Errorf("ancestor admin granting on child: want 200, got %d", code)
	}
}

// ── UpsertOrgUser — user promoted to admin ────────────────────────────────────

func TestUpsertOrgUser_UserPromotedToAdmin_CanThenGrantPermissions(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsPromo_"+t.Name(), nil)
	promoterUID := "ups-promo-promoter-" + t.Name()
	newAdminUID := "ups-promo-admin-" + t.Name()
	granteeUID := "ups-promo-grantee-" + t.Name()
	insertUser(t, db, promoterUID, promoterUID+"@example.com")
	insertUser(t, db, newAdminUID, newAdminUID+"@example.com")
	insertUser(t, db, granteeUID, granteeUID+"@example.com")
	grantMembership(t, db, promoterUID, orgID, "admin", true, true)

	// Start as plain user.
	grantMembership(t, db, newAdminUID, orgID, "user", true, false)

	// Promote to admin via UpsertOrgUser (called by promoter who is org admin).
	fb := &fakeFirebase{record: fakeUserRecord(newAdminUID, newAdminUID+"@example.com")}
	w := callHandler(http.MethodPost, fmt.Sprintf("/api/orgs/%d/users", orgID), promoterUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": newAdminUID + "@example.com", "role": "admin"},
		chainHandlers(middleware.RequireOrgAdmin(db), handlers.UpsertOrgUser(db, fb, noopOOB)),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("promote to admin: want 200, got %d: %s", w.Code, w.Body.String())
	}
	role, canRead, canUpload, _ := activeOrgUser(t, db, newAdminUID, orgID)
	if role != "admin" || !canRead || !canUpload {
		t.Fatalf("after promotion: got role=%q canRead=%v canUpload=%v; want admin/true/true", role, canRead, canUpload)
	}

	// Now newAdmin must be able to grant permissions to grantee via the full middleware chain.
	fb2 := &fakeFirebase{record: fakeUserRecord(granteeUID, granteeUID+"@example.com")}
	w2 := callHandler(http.MethodPost, fmt.Sprintf("/api/orgs/%d/users", orgID), newAdminUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": granteeUID + "@example.com", "role": "user", "can_read": true, "can_upload": false},
		chainHandlers(middleware.RequireOrgAdmin(db), handlers.UpsertOrgUser(db, fb2, noopOOB)),
	)
	if w2.Code != http.StatusOK {
		t.Fatalf("newly-promoted admin granting: want 200, got %d: %s", w2.Code, w2.Body.String())
	}
	activeOrgUser(t, db, granteeUID, orgID) // fails if no active row
}

func TestUpsertOrgUser_AdminDemotedToUser_CanNoLongerGrant(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "UpsDemote_"+t.Name(), nil)
	demoterUID := "ups-demote-demoter-" + t.Name()
	demotedUID := "ups-demote-" + t.Name()
	targetUID := "ups-demote-tgt-" + t.Name()
	insertUser(t, db, demoterUID, demoterUID+"@example.com")
	insertUser(t, db, demotedUID, demotedUID+"@example.com")
	insertUser(t, db, targetUID, targetUID+"@example.com")
	grantMembership(t, db, demoterUID, orgID, "admin", true, true)

	// Start as admin.
	grantMembership(t, db, demotedUID, orgID, "admin", true, true)

	// Demote to plain user (called by demoter who is also org admin).
	fb := &fakeFirebase{record: fakeUserRecord(demotedUID, demotedUID+"@example.com")}
	w := callHandler(http.MethodPost, fmt.Sprintf("/api/orgs/%d/users", orgID), demoterUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": demotedUID + "@example.com", "role": "user", "can_read": true, "can_upload": false},
		chainHandlers(middleware.RequireOrgAdmin(db), handlers.UpsertOrgUser(db, fb, noopOOB)),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("demote to user: want 200, got %d: %s", w.Code, w.Body.String())
	}
	role, _, _, _ := activeOrgUser(t, db, demotedUID, orgID)
	if role != "user" {
		t.Fatalf("after demotion: got role=%q want user", role)
	}

	// Demoted user must now be blocked from granting permissions.
	code := upsertOrgUserViaMiddleware(db, demotedUID, orgID, targetUID, targetUID+"@example.com", "user")
	if code != http.StatusForbidden {
		t.Errorf("demoted user calling UpsertOrgUser: want 403, got %d", code)
	}
}

// ── RevokeOrgUser — authorization ─────────────────────────────────────────────

func revokeOrgUserViaMiddleware(db *sql.DB, callerUID string, orgID int, targetUID string) int {
	w := callHandler(
		http.MethodDelete, fmt.Sprintf("/api/orgs/%d/users/%s", orgID, targetUID), callerUID,
		gin.Params{
			{Key: "org_id", Value: fmt.Sprintf("%d", orgID)},
			{Key: "user_id", Value: targetUID},
		},
		nil,
		chainHandlers(middleware.RequireOrgAdmin(db), handlers.RevokeOrgUser(db)),
	)
	return w.Code
}

func TestRevokeOrgUser_PlainRoleCaller_Returns403(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "RevAuthPlain_"+t.Name(), nil)
	callerUID := "rev-auth-plain-" + t.Name()
	targetUID := "rev-auth-plain-tgt-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	insertUser(t, db, targetUID, targetUID+"@example.com")
	grantMembership(t, db, callerUID, orgID, "user", true, true)
	grantMembership(t, db, targetUID, orgID, "user", true, false)

	if code := revokeOrgUserViaMiddleware(db, callerUID, orgID, targetUID); code != http.StatusForbidden {
		t.Errorf("plain-role caller: want 403, got %d", code)
	}
}

func TestRevokeOrgUser_SiblingOrgAdmin_Returns403(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	myOrgID := insertOrg(t, db, "RevAuthSibMy_"+t.Name(), nil)
	targetOrgID := insertOrg(t, db, "RevAuthSibOther_"+t.Name(), nil)
	callerUID := "rev-auth-sib-" + t.Name()
	targetUID := "rev-auth-sib-tgt-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	insertUser(t, db, targetUID, targetUID+"@example.com")
	grantMembership(t, db, callerUID, myOrgID, "admin", true, true)
	grantMembership(t, db, targetUID, targetOrgID, "user", true, false)

	if code := revokeOrgUserViaMiddleware(db, callerUID, targetOrgID, targetUID); code != http.StatusForbidden {
		t.Errorf("sibling-org admin: want 403, got %d", code)
	}
}

func TestRevokeOrgUser_OrgAdmin_Returns200(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "RevAuthOK_"+t.Name(), nil)
	callerUID := "rev-auth-ok-" + t.Name()
	targetUID := "rev-auth-ok-tgt-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	insertUser(t, db, targetUID, targetUID+"@example.com")
	grantMembership(t, db, callerUID, orgID, "admin", true, true)
	grantMembership(t, db, targetUID, orgID, "user", true, false)

	if code := revokeOrgUserViaMiddleware(db, callerUID, orgID, targetUID); code != http.StatusOK {
		t.Errorf("org admin revoking member: want 200, got %d", code)
	}
}

// ── RevokeOrgUser ─────────────────────────────────────────────────────────────

func TestRevokeOrgUser_SetsRevokedAtAndRemovedBy(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "RevSet_"+t.Name(), nil)
	targetUID := "rev-set-" + t.Name()
	callerUID := "rev-caller-" + t.Name()
	insertUser(t, db, targetUID, targetUID+"@example.com")
	insertUser(t, db, callerUID, callerUID+"@example.com")
	grantMembership(t, db, targetUID, orgID, "user", true, false)

	w := callHandler(http.MethodDelete, "/api/orgs/1/users/x", callerUID,
		gin.Params{
			{Key: "org_id", Value: fmt.Sprintf("%d", orgID)},
			{Key: "user_id", Value: targetUID},
		},
		nil, handlers.RevokeOrgUser(db),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var revokedAt sql.NullTime
	var removedBy sql.NullString
	db.QueryRow(`SELECT revoked_at, removed_by FROM org_users WHERE user_id=$1 AND org_id=$2`, targetUID, orgID).Scan(&revokedAt, &removedBy)
	if !revokedAt.Valid {
		t.Error("revoked_at must be set after RevokeOrgUser")
	}
	if !removedBy.Valid || removedBy.String != callerUID {
		t.Errorf("removed_by: got %q want %q", removedBy.String, callerUID)
	}
}

func TestRevokeOrgUser_LeavesHistoricalRowIntact(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "RevHist_"+t.Name(), nil)
	uid := "rev-hist-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "user", true, false)

	callHandler(http.MethodDelete, "/api/orgs/1/users/x", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}, {Key: "user_id", Value: uid}},
		nil, handlers.RevokeOrgUser(db),
	)

	total := countOrgUserRows(t, db, uid, orgID)
	if total != 1 {
		t.Errorf("revoke must not delete the row; want 1 total row, got %d", total)
	}
}

func TestRevokeOrgUser_AlreadyRevoked_Returns200(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "RevIdem_"+t.Name(), nil)
	uid := "rev-idem-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "user", true, false)
	revokeInDB(t, db, uid, orgID)

	w := callHandler(http.MethodDelete, "/api/orgs/1/users/x", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}, {Key: "user_id", Value: uid}},
		nil, handlers.RevokeOrgUser(db),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("second revoke: want 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ── ListOrgs ──────────────────────────────────────────────────────────────────

func TestListOrgs_AdminSeesOwnSubtreeOnly(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	rootID := insertOrg(t, db, "LORoot_"+t.Name(), nil)
	childID := insertOrg(t, db, "LOChild_"+t.Name(), &rootID)
	grandID := insertOrg(t, db, "LOGrand_"+t.Name(), &childID)
	siblingID := insertOrg(t, db, "LOSibling_"+t.Name(), nil)

	uid := "lo-admin-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, rootID, "admin", false, false)

	w := callHandler(http.MethodGet, "/api/orgs", uid, nil, nil, handlers.ListOrgs(db))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var rows []struct{ ID int `json:"id"` }
	decodeJSON(t, w, &rows)

	seen := map[int]bool{}
	for _, r := range rows {
		seen[r.ID] = true
	}
	for _, id := range []int{rootID, childID, grandID} {
		if !seen[id] {
			t.Errorf("org %d should appear for admin, missing", id)
		}
	}
	if seen[siblingID] {
		t.Errorf("sibling org %d must not appear — no membership", siblingID)
	}
}

func TestListOrgs_PlainRoleMember_SeesNothing(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "LOPlain_"+t.Name(), nil)
	uid := "lo-plain-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "user", true, false)

	w := callHandler(http.MethodGet, "/api/orgs", uid, nil, nil, handlers.ListOrgs(db))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var rows []struct{ ID int `json:"id"` }
	decodeJSON(t, w, &rows)
	for _, r := range rows {
		if r.ID == orgID {
			t.Errorf("plain-role member must not see org %d via ListOrgs", orgID)
		}
	}
}

func TestListOrgs_RevokedAdmin_SeesNothing(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "LORevokedAdmin_"+t.Name(), nil)
	uid := "lo-rev-admin-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "admin", false, false)
	revokeInDB(t, db, uid, orgID)

	w := callHandler(http.MethodGet, "/api/orgs", uid, nil, nil, handlers.ListOrgs(db))
	var rows []struct{ ID int `json:"id"` }
	decodeJSON(t, w, &rows)
	for _, r := range rows {
		if r.ID == orgID {
			t.Errorf("revoked admin must not see org %d via ListOrgs", orgID)
		}
	}
}

// ── ListOrgUsers ──────────────────────────────────────────────────────────────

func TestListOrgUsers_ActiveIncluded_RevokedExcluded(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "LOU_"+t.Name(), nil)
	activeUID := "lou-active-" + t.Name()
	revokedUID := "lou-revoked-" + t.Name()
	insertUser(t, db, activeUID, activeUID+"@example.com")
	insertUser(t, db, revokedUID, revokedUID+"@example.com")
	grantMembership(t, db, activeUID, orgID, "user", true, false)
	grantMembership(t, db, revokedUID, orgID, "user", true, false)
	revokeInDB(t, db, revokedUID, orgID)

	w := callHandler(http.MethodGet, fmt.Sprintf("/api/orgs/%d/users", orgID), systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		nil, handlers.ListOrgUsers(db),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var users []struct {
		UserID  string `json:"user_id"`
		Email   string `json:"email"`
		AddedBy string `json:"added_by"`
	}
	decodeJSON(t, w, &users)

	seenActive, seenRevoked := false, false
	for _, u := range users {
		switch u.UserID {
		case activeUID:
			seenActive = true
			if u.Email == "" {
				t.Error("email must not be empty for active member")
			}
			if u.AddedBy == "" {
				t.Error("added_by must not be empty for active member")
			}
		case revokedUID:
			seenRevoked = true
		}
	}
	if !seenActive {
		t.Errorf("active user %q missing from ListOrgUsers", activeUID)
	}
	if seenRevoked {
		t.Errorf("revoked user %q must not appear in ListOrgUsers", revokedUID)
	}
}

// ── ListOrgUsers — authorization ─────────────────────────────────────────────

func TestListOrgUsers_PlainRoleCaller_Returns403(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "LOUAuthPlain_"+t.Name(), nil)
	callerUID := "lou-auth-plain-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	grantMembership(t, db, callerUID, orgID, "user", true, false)

	w := callHandler(http.MethodGet, fmt.Sprintf("/api/orgs/%d/users", orgID), callerUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		nil,
		chainHandlers(middleware.RequireOrgAdmin(db), handlers.ListOrgUsers(db)),
	)
	if w.Code != http.StatusForbidden {
		t.Errorf("plain-role caller: want 403, got %d", w.Code)
	}
}

func TestListOrgUsers_SiblingOrgAdmin_Returns403(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	myOrgID := insertOrg(t, db, "LOUAuthSibMy_"+t.Name(), nil)
	targetOrgID := insertOrg(t, db, "LOUAuthSibOther_"+t.Name(), nil)
	callerUID := "lou-auth-sib-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	grantMembership(t, db, callerUID, myOrgID, "admin", true, true)

	w := callHandler(http.MethodGet, fmt.Sprintf("/api/orgs/%d/users", targetOrgID), callerUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", targetOrgID)}},
		nil,
		chainHandlers(middleware.RequireOrgAdmin(db), handlers.ListOrgUsers(db)),
	)
	if w.Code != http.StatusForbidden {
		t.Errorf("sibling-org admin: want 403, got %d", w.Code)
	}
}

func TestListOrgUsers_OrgAdmin_Returns200(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "LOUAuthOK_"+t.Name(), nil)
	callerUID := "lou-auth-ok-" + t.Name()
	memberUID := "lou-auth-ok-mem-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	insertUser(t, db, memberUID, memberUID+"@example.com")
	grantMembership(t, db, callerUID, orgID, "admin", true, true)
	grantMembership(t, db, memberUID, orgID, "user", true, false)

	w := callHandler(http.MethodGet, fmt.Sprintf("/api/orgs/%d/users", orgID), callerUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		nil,
		chainHandlers(middleware.RequireOrgAdmin(db), handlers.ListOrgUsers(db)),
	)
	if w.Code != http.StatusOK {
		t.Errorf("org admin listing members: want 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ── DeleteUser — authorization ────────────────────────────────────────────────

func TestDeleteUser_OrgAdminOnly_Returns403(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "DUAuthOrgAdm_"+t.Name(), nil)
	callerUID := "du-auth-org-adm-" + t.Name()
	targetUID := "du-auth-org-adm-tgt-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	insertUser(t, db, targetUID, targetUID+"@example.com")
	// caller is admin of their own org but NOT root org admin
	grantMembership(t, db, callerUID, orgID, "admin", true, true)

	w := callHandler(http.MethodDelete, "/api/users/x", callerUID,
		gin.Params{{Key: "user_id", Value: targetUID}},
		nil,
		chainHandlers(middleware.RequireRootOrgAdmin(db), handlers.DeleteUser(db)),
	)
	if w.Code != http.StatusForbidden {
		t.Errorf("non-root org admin calling DeleteUser: want 403, got %d", w.Code)
	}
	// user row must be untouched
	var deletedAt sql.NullTime
	db.QueryRow(`SELECT deleted_at FROM users WHERE id=$1`, targetUID).Scan(&deletedAt)
	if deletedAt.Valid {
		t.Error("user must not be deleted when caller is not root org admin")
	}
}

func TestDeleteUser_PlainRoleCaller_Returns403(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "DUAuthPlain_"+t.Name(), nil)
	callerUID := "du-auth-plain-" + t.Name()
	targetUID := "du-auth-plain-tgt-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	insertUser(t, db, targetUID, targetUID+"@example.com")
	grantMembership(t, db, callerUID, orgID, "user", true, false)

	w := callHandler(http.MethodDelete, "/api/users/x", callerUID,
		gin.Params{{Key: "user_id", Value: targetUID}},
		nil,
		chainHandlers(middleware.RequireRootOrgAdmin(db), handlers.DeleteUser(db)),
	)
	if w.Code != http.StatusForbidden {
		t.Errorf("plain-role caller: want 403, got %d", w.Code)
	}
}

func TestDeleteUser_RootOrgAdmin_Returns200(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	callerUID := "du-auth-root-" + t.Name()
	targetUID := "du-auth-root-tgt-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	insertUser(t, db, targetUID, targetUID+"@example.com")
	grantMembership(t, db, callerUID, 1, "admin", true, true)

	w := callHandler(http.MethodDelete, "/api/users/x", callerUID,
		gin.Params{{Key: "user_id", Value: targetUID}},
		nil,
		chainHandlers(middleware.RequireRootOrgAdmin(db), handlers.DeleteUser(db)),
	)
	if w.Code != http.StatusOK {
		t.Errorf("root org admin: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var deletedAt sql.NullTime
	db.QueryRow(`SELECT deleted_at FROM users WHERE id=$1`, targetUID).Scan(&deletedAt)
	if !deletedAt.Valid {
		t.Error("user must be soft-deleted by root org admin")
	}
}

// ── DeleteOrg — soft-delete and cascade ──────────────────────────────────────

func TestDeleteOrg_SetsDeletedAtAndRemovedBy(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "DOSet_"+t.Name(), nil)
	callerUID := "do-caller-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	grantMembership(t, db, callerUID, orgID, "admin", false, false)

	w := callHandler(http.MethodDelete, fmt.Sprintf("/api/orgs/%d", orgID), callerUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		nil, handlers.DeleteOrg(db),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var deletedAt sql.NullTime
	var removedBy sql.NullString
	db.QueryRow(`SELECT deleted_at, removed_by FROM orgs WHERE id=$1`, orgID).Scan(&deletedAt, &removedBy)
	if !deletedAt.Valid {
		t.Error("org deleted_at must be set")
	}
	if !removedBy.Valid || removedBy.String != callerUID {
		t.Errorf("org removed_by: got %q want %q", removedBy.String, callerUID)
	}
}

func TestDeleteOrg_CascadesOrgUsersMemberships(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "DOCascMem_"+t.Name(), nil)
	uid := "do-casc-mem-" + t.Name()
	callerUID := "do-casc-caller-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	insertUser(t, db, callerUID, callerUID+"@example.com")
	grantMembership(t, db, uid, orgID, "admin", true, true)

	callHandler(http.MethodDelete, fmt.Sprintf("/api/orgs/%d", orgID), callerUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		nil, handlers.DeleteOrg(db),
	)

	var revokedAt sql.NullTime
	var removedBy sql.NullString
	db.QueryRow(`SELECT revoked_at, removed_by FROM org_users WHERE user_id=$1 AND org_id=$2`, uid, orgID).Scan(&revokedAt, &removedBy)
	if !revokedAt.Valid {
		t.Error("org_users revoked_at must be set on cascade delete")
	}
	if !removedBy.Valid || removedBy.String != callerUID {
		t.Errorf("org_users removed_by: got %q want %q", removedBy.String, callerUID)
	}
}

func TestDeleteOrg_CascadesDevices(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "DOCascDev_"+t.Name(), nil)
	macID := "DDEV" + fmt.Sprintf("%08d", orgID)
	callerUID := "do-dev-caller-" + t.Name()
	insertUser(t, db, callerUID, callerUID+"@example.com")
	insertDevice(t, db, macID, orgID)

	callHandler(http.MethodDelete, fmt.Sprintf("/api/orgs/%d", orgID), callerUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		nil, handlers.DeleteOrg(db),
	)

	var deletedAt sql.NullTime
	var removedBy sql.NullString
	db.QueryRow(`SELECT deleted_at, removed_by FROM devices WHERE id=$1`, macID).Scan(&deletedAt, &removedBy)
	if !deletedAt.Valid {
		t.Error("device deleted_at must be set on cascade delete")
	}
	if !removedBy.Valid || removedBy.String != callerUID {
		t.Errorf("device removed_by: got %q want %q", removedBy.String, callerUID)
	}
}

func TestDeleteOrg_WithChildren_CascadesEntireSubtree(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	rootID := insertOrg(t, db, "DOSubRoot_"+t.Name(), nil)
	childID := insertOrg(t, db, "DOSubChild_"+t.Name(), &rootID)
	grandID := insertOrg(t, db, "DOSubGrand_"+t.Name(), &childID)
	uid := "do-sub-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, rootID, "admin", true, true)
	macRoot := "DSUBR" + fmt.Sprintf("%08d", rootID)
	macChild := "DSUBC" + fmt.Sprintf("%08d", childID)
	insertDevice(t, db, macRoot, rootID)
	insertDevice(t, db, macChild, childID)

	w := callHandler(http.MethodDelete, fmt.Sprintf("/api/orgs/%d", rootID), uid,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", rootID)}},
		nil, handlers.DeleteOrg(db),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	for _, id := range []int{rootID, childID, grandID} {
		var deletedAt sql.NullTime
		db.QueryRow(`SELECT deleted_at FROM orgs WHERE id=$1`, id).Scan(&deletedAt)
		if !deletedAt.Valid {
			t.Errorf("org %d deleted_at must be set after cascade", id)
		}
	}
	for _, mac := range []string{macRoot, macChild} {
		var deletedAt sql.NullTime
		db.QueryRow(`SELECT deleted_at FROM devices WHERE id=$1`, mac).Scan(&deletedAt)
		if !deletedAt.Valid {
			t.Errorf("device %s deleted_at must be set after cascade", mac)
		}
	}

	// None of the cascade-deleted orgs must appear in ListOrgs
	w2 := callHandler(http.MethodGet, "/api/orgs", uid, nil, nil, handlers.ListOrgs(db))
	var rows []struct{ ID int `json:"id"` }
	decodeJSON(t, w2, &rows)
	for _, r := range rows {
		for _, id := range []int{rootID, childID, grandID} {
			if r.ID == id {
				t.Errorf("cascade-deleted org %d still appears in ListOrgs", id)
			}
		}
	}
}

func TestDeleteOrg_AlreadyDeleted_Returns200(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "DOIdem_"+t.Name(), nil)
	db.Exec(`UPDATE orgs SET deleted_at=now(), removed_by=$2 WHERE id=$1`, orgID, systemUID)

	w := callHandler(http.MethodDelete, fmt.Sprintf("/api/orgs/%d", orgID), systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		nil, handlers.DeleteOrg(db),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 for already-deleted org, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteOrg_SecondDelete_Returns200(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "DOIdem2_"+t.Name(), nil)

	del := func() int {
		w := callHandler(http.MethodDelete, fmt.Sprintf("/api/orgs/%d", orgID), systemUID,
			gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
			nil, handlers.DeleteOrg(db),
		)
		return w.Code
	}
	if code := del(); code != http.StatusOK {
		t.Fatalf("first delete: want 200, got %d", code)
	}
	if code := del(); code != http.StatusOK {
		t.Fatalf("second delete: want 200, got %d", code)
	}
}

func TestDeleteOrg_RootOrgID_Returns403(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	uid := "do-root-guard-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, 1, "admin", false, false)

	w := callHandler(http.MethodDelete, "/api/orgs/1", uid,
		gin.Params{{Key: "org_id", Value: "1"}},
		nil, handlers.DeleteOrg(db),
	)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 for root org, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteOrg_SoftDeletedOrg_DoesNotReopenChildren(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	rootID := insertOrg(t, db, "DONoReopen_"+t.Name(), nil)
	childID := insertOrg(t, db, "DONoReopenChild_"+t.Name(), &rootID)
	// pre-delete the child
	db.Exec(`UPDATE orgs SET deleted_at=now(), removed_by=$2 WHERE id=$1`, childID, systemUID)

	uid := "do-noreopen-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, rootID, "admin", false, false)

	w := callHandler(http.MethodDelete, fmt.Sprintf("/api/orgs/%d", rootID), uid,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", rootID)}},
		nil, handlers.DeleteOrg(db),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 when child already deleted, got %d: %s", w.Code, w.Body.String())
	}

	// child deleted_at must still be set (original delete preserved)
	var childDeletedAt sql.NullTime
	db.QueryRow(`SELECT deleted_at FROM orgs WHERE id=$1`, childID).Scan(&childDeletedAt)
	if !childDeletedAt.Valid {
		t.Error("child org must remain soft-deleted")
	}
}

// ── DeleteOrg — authorization (RequireOrgAdmin chain) ────────────────────────

func deleteOrgViaMiddleware(db *sql.DB, callerUID string, orgID int) int {
	w := callHandler(
		http.MethodDelete, fmt.Sprintf("/api/orgs/%d", orgID), callerUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		nil,
		chainHandlers(middleware.RequireOrgAdmin(db), handlers.DeleteOrg(db)),
	)
	return w.Code
}

func TestDeleteOrg_RootAdmin_CanDeleteOwnSubtree(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	rootID := insertOrg(t, db, "DOMWRoot_"+t.Name(), nil)
	childID := insertOrg(t, db, "DOMWChild_"+t.Name(), &rootID)
	grandID := insertOrg(t, db, "DOMWGrand_"+t.Name(), &childID)
	uid := "domw-root-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, rootID, "admin", false, false)

	for _, tc := range []struct{ name string; id int }{
		{"grandchild", grandID}, {"child", childID}, {"root", rootID},
	} {
		db.Exec(`UPDATE orgs SET deleted_at=NULL, removed_by=NULL WHERE id=$1`, tc.id)
		if code := deleteOrgViaMiddleware(db, uid, tc.id); code != http.StatusOK {
			t.Errorf("root admin deleting %s: want 200, got %d", tc.name, code)
		}
	}
}

func TestDeleteOrg_RootAdmin_CannotDeleteSiblingOrg(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	myOrgID := insertOrg(t, db, "DOMWSibMy_"+t.Name(), nil)
	siblingID := insertOrg(t, db, "DOMWSibOther_"+t.Name(), nil)
	uid := "domw-sib-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, myOrgID, "admin", false, false)

	if code := deleteOrgViaMiddleware(db, uid, siblingID); code != http.StatusForbidden {
		t.Errorf("want 403 deleting sibling, got %d", code)
	}
}

func TestDeleteOrg_ChildAdmin_CanDeleteOwnSubtree(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	rootID := insertOrg(t, db, "DOMWCARoot_"+t.Name(), nil)
	childID := insertOrg(t, db, "DOMWCAChild_"+t.Name(), &rootID)
	grandID := insertOrg(t, db, "DOMWCAGrand_"+t.Name(), &childID)
	uid := "domw-child-admin-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, childID, "admin", false, false)

	for _, tc := range []struct{ name string; id int }{
		{"grandchild", grandID}, {"child", childID},
	} {
		db.Exec(`UPDATE orgs SET deleted_at=NULL, removed_by=NULL WHERE id=$1`, tc.id)
		if code := deleteOrgViaMiddleware(db, uid, tc.id); code != http.StatusOK {
			t.Errorf("child admin deleting %s: want 200, got %d", tc.name, code)
		}
	}
}

func TestDeleteOrg_ChildAdmin_CannotDeleteParentOrg(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	rootID := insertOrg(t, db, "DOMWCAParRoot_"+t.Name(), nil)
	childID := insertOrg(t, db, "DOMWCAParChild_"+t.Name(), &rootID)
	uid := "domw-child-par-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, childID, "admin", false, false)

	if code := deleteOrgViaMiddleware(db, uid, rootID); code != http.StatusForbidden {
		t.Errorf("child admin deleting parent: want 403, got %d", code)
	}
}

func TestDeleteOrg_ChildAdmin_CannotDeleteSiblingOrg(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	rootID := insertOrg(t, db, "DOMWCASibRoot_"+t.Name(), nil)
	childID := insertOrg(t, db, "DOMWCASibChild_"+t.Name(), &rootID)
	siblingID := insertOrg(t, db, "DOMWCASibSib_"+t.Name(), &rootID)
	uid := "domw-child-sib-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, childID, "admin", false, false)

	if code := deleteOrgViaMiddleware(db, uid, siblingID); code != http.StatusForbidden {
		t.Errorf("child admin deleting sibling: want 403, got %d", code)
	}
}

func TestDeleteOrg_PlainRoleMember_CannotDelete(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "DOMWPlain_"+t.Name(), nil)
	uid := "domw-plain-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "user", true, true)

	if code := deleteOrgViaMiddleware(db, uid, orgID); code != http.StatusForbidden {
		t.Errorf("plain member deleting own org: want 403, got %d", code)
	}
}

func TestDeleteOrg_RevokedAdmin_CannotDelete(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "DOMWRevAdm_"+t.Name(), nil)
	uid := "domw-rev-adm-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "admin", false, false)
	revokeInDB(t, db, uid, orgID)

	if code := deleteOrgViaMiddleware(db, uid, orgID); code != http.StatusForbidden {
		t.Errorf("revoked admin deleting org: want 403, got %d", code)
	}
}

func TestDeleteOrg_DeviceInDeletedOrg_BlocksDeviceAccess(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "DODevBlk_"+t.Name(), nil)
	uid := "do-dev-blk-" + t.Name()
	macID := "DBLK" + fmt.Sprintf("%08d", orgID)
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "user", true, true)
	insertDevice(t, db, macID, orgID)

	callHandler(http.MethodDelete, fmt.Sprintf("/api/orgs/%d", orgID), uid,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		nil, handlers.DeleteOrg(db),
	)

	for _, action := range []string{"read", "upload"} {
		code, reached := runMiddleware(middleware.RequireDeviceAccess(db, action), uid, "", macID)
		if reached {
			t.Errorf("device in deleted org must block access for action=%q (status=%d)", action, code)
		}
	}
}

// ── DeleteUser ────────────────────────────────────────────────────────────────

func TestDeleteUser_SetsDeletedAtAndRemovedBy(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	targetUID := "du-target-" + t.Name()
	callerUID := "du-caller-" + t.Name()
	insertUser(t, db, targetUID, targetUID+"@example.com")
	insertUser(t, db, callerUID, callerUID+"@example.com")

	w := callHandler(http.MethodDelete, "/api/users/x", callerUID,
		gin.Params{{Key: "user_id", Value: targetUID}},
		nil, handlers.DeleteUser(db),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var deletedAt sql.NullTime
	var removedBy sql.NullString
	db.QueryRow(`SELECT deleted_at, removed_by FROM users WHERE id=$1`, targetUID).Scan(&deletedAt, &removedBy)
	if !deletedAt.Valid {
		t.Error("deleted_at must be set after DeleteUser")
	}
	if !removedBy.Valid || removedBy.String != callerUID {
		t.Errorf("removed_by: got %q want %q", removedBy.String, callerUID)
	}
}

func TestDeleteUser_CascadeRevokesAllMemberships(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	org1ID := insertOrg(t, db, "DUCasc1_"+t.Name(), nil)
	org2ID := insertOrg(t, db, "DUCasc2_"+t.Name(), nil)
	targetUID := "du-casc-" + t.Name()
	insertUser(t, db, targetUID, targetUID+"@example.com")
	grantMembership(t, db, targetUID, org1ID, "admin", true, true)
	grantMembership(t, db, targetUID, org2ID, "user", true, false)

	w := callHandler(http.MethodDelete, "/api/users/x", systemUID,
		gin.Params{{Key: "user_id", Value: targetUID}},
		nil, handlers.DeleteUser(db),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	// All active org_users rows must be revoked.
	var activeCount int
	db.QueryRow(`SELECT COUNT(*) FROM org_users WHERE user_id=$1 AND revoked_at IS NULL`, targetUID).Scan(&activeCount)
	if activeCount != 0 {
		t.Errorf("DeleteUser must revoke all active memberships; found %d active", activeCount)
	}

	// revoked rows must have removed_by set.
	var missingRemovedBy int
	db.QueryRow(`SELECT COUNT(*) FROM org_users WHERE user_id=$1 AND revoked_at IS NOT NULL AND removed_by IS NULL`, targetUID).Scan(&missingRemovedBy)
	if missingRemovedBy != 0 {
		t.Errorf("all revoked rows must have removed_by set; %d missing", missingRemovedBy)
	}
}

func TestDeleteUser_AlreadyDeleted_Returns200(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	uid := "du-idem-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")

	del := func() int {
		w := callHandler(http.MethodDelete, "/api/users/x", systemUID,
			gin.Params{{Key: "user_id", Value: uid}},
			nil, handlers.DeleteUser(db),
		)
		return w.Code
	}
	if code := del(); code != http.StatusOK {
		t.Fatalf("first delete: want 200, got %d", code)
	}
	if code := del(); code != http.StatusOK {
		t.Fatalf("second delete: want 200, got %d", code)
	}
}

// ── RequireOrgAdmin middleware ────────────────────────────────────────────────

func TestRequireOrgAdmin_DirectAdmin_Allowed(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "ROADirect_"+t.Name(), nil)
	uid := "roa-direct-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "admin", false, false)

	_, reached := runMiddleware(middleware.RequireOrgAdmin(db), uid, fmt.Sprintf("%d", orgID), "")
	if !reached {
		t.Error("direct admin must pass RequireOrgAdmin")
	}
}

func TestRequireOrgAdmin_AncestorAdmin_Allowed(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	rootID := insertOrg(t, db, "ROAAncRoot_"+t.Name(), nil)
	childID := insertOrg(t, db, "ROAAncChild_"+t.Name(), &rootID)
	uid := "roa-anc-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, rootID, "admin", false, false)

	_, reached := runMiddleware(middleware.RequireOrgAdmin(db), uid, fmt.Sprintf("%d", childID), "")
	if !reached {
		t.Error("ancestor admin must pass RequireOrgAdmin for child org")
	}
}

func TestRequireOrgAdmin_PlainRoleMember_Blocked(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "ROAPlain_"+t.Name(), nil)
	uid := "roa-plain-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "user", true, false)

	code, reached := runMiddleware(middleware.RequireOrgAdmin(db), uid, fmt.Sprintf("%d", orgID), "")
	if code != http.StatusForbidden || reached {
		t.Errorf("plain member: want 403 not-reached, got code=%d reached=%v", code, reached)
	}
}

func TestRequireOrgAdmin_RevokedAdmin_Blocked(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "ROARevoked_"+t.Name(), nil)
	uid := "roa-revoked-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "admin", false, false)
	revokeInDB(t, db, uid, orgID)

	code, reached := runMiddleware(middleware.RequireOrgAdmin(db), uid, fmt.Sprintf("%d", orgID), "")
	if code != http.StatusForbidden || reached {
		t.Errorf("revoked admin: want 403 not-reached, got code=%d reached=%v", code, reached)
	}
}

func TestRequireOrgAdmin_SoftDeletedOrg_Blocked(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "ROADelOrg_"+t.Name(), nil)
	uid := "roa-del-org-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "admin", false, false)
	db.Exec(`UPDATE orgs SET deleted_at=now(), removed_by=$2 WHERE id=$1`, orgID, systemUID)

	code, reached := runMiddleware(middleware.RequireOrgAdmin(db), uid, fmt.Sprintf("%d", orgID), "")
	if code != http.StatusForbidden || reached {
		t.Errorf("deleted org: want 403 not-reached, got code=%d reached=%v", code, reached)
	}
}

func TestRequireOrgAdmin_CannotActOnOrgAbove(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	rootID := insertOrg(t, db, "ROAAboveRoot_"+t.Name(), nil)
	childID := insertOrg(t, db, "ROAAboveChild_"+t.Name(), &rootID)
	uid := "roa-above-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, childID, "admin", false, false)

	code, reached := runMiddleware(middleware.RequireOrgAdmin(db), uid, fmt.Sprintf("%d", rootID), "")
	if code != http.StatusForbidden || reached {
		t.Errorf("child admin on parent org: want 403 not-reached, got code=%d reached=%v", code, reached)
	}
}

func TestRequireOrgAdmin_CannotActOnUnrelatedOrg(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	myOrgID := insertOrg(t, db, "ROAUnrelMy_"+t.Name(), nil)
	otherOrgID := insertOrg(t, db, "ROAUnrelOther_"+t.Name(), nil)
	uid := "roa-unrel-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, myOrgID, "admin", false, false)

	code, reached := runMiddleware(middleware.RequireOrgAdmin(db), uid, fmt.Sprintf("%d", otherOrgID), "")
	if code != http.StatusForbidden || reached {
		t.Errorf("unrelated org: want 403 not-reached, got code=%d reached=%v", code, reached)
	}
}

func TestRequireOrgAdmin_DeletedUser_Blocked(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "ROADelUser_"+t.Name(), nil)
	uid := "roa-del-user-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "admin", false, false)
	db.Exec(`UPDATE users SET deleted_at=now(), removed_by=$2 WHERE id=$1`, uid, systemUID)

	code, reached := runMiddleware(middleware.RequireOrgAdmin(db), uid, fmt.Sprintf("%d", orgID), "")
	if code != http.StatusForbidden || reached {
		t.Errorf("deleted user: want 403 not-reached, got code=%d reached=%v", code, reached)
	}
}

// ── RequireRootOrgAdmin middleware ────────────────────────────────────────────

func TestRequireRootOrgAdmin_RootAdmin_Allowed(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	uid := "rra-allowed-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, 1, "admin", true, true)

	_, reached := runMiddleware(middleware.RequireRootOrgAdmin(db), uid, "", "")
	if !reached {
		t.Error("root org admin must pass RequireRootOrgAdmin")
	}
}

func TestRequireRootOrgAdmin_NoMembership_Blocked(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	uid := "rra-none-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")

	code, reached := runMiddleware(middleware.RequireRootOrgAdmin(db), uid, "", "")
	if code != http.StatusForbidden || reached {
		t.Errorf("no-membership user: want 403 not-reached, got code=%d reached=%v", code, reached)
	}
}

func TestRequireRootOrgAdmin_ChildOrgAdminOnly_Blocked(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	childID := insertOrg(t, db, "RRAChild_"+t.Name(), intPtr(1))
	uid := "rra-child-only-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, childID, "admin", false, false)

	code, reached := runMiddleware(middleware.RequireRootOrgAdmin(db), uid, "", "")
	if code != http.StatusForbidden || reached {
		t.Errorf("child-only admin: want 403 not-reached, got code=%d reached=%v", code, reached)
	}
}

func TestRequireRootOrgAdmin_RevokedRootAdmin_Blocked(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	uid := "rra-revoked-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, 1, "admin", true, true)
	revokeInDB(t, db, uid, 1)

	code, reached := runMiddleware(middleware.RequireRootOrgAdmin(db), uid, "", "")
	if code != http.StatusForbidden || reached {
		t.Errorf("revoked root admin: want 403 not-reached, got code=%d reached=%v", code, reached)
	}
}

func TestRequireRootOrgAdmin_DeletedUser_Blocked(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	uid := "rra-del-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, 1, "admin", true, true)
	db.Exec(`UPDATE users SET deleted_at=now(), removed_by=$2 WHERE id=$1`, uid, systemUID)

	code, reached := runMiddleware(middleware.RequireRootOrgAdmin(db), uid, "", "")
	if code != http.StatusForbidden || reached {
		t.Errorf("deleted user: want 403 not-reached, got code=%d reached=%v", code, reached)
	}
}

// ── RequireDeviceAccess middleware ────────────────────────────────────────────

func TestRequireDeviceAccess_Admin_AllowsAll(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "RDAAdmin_"+t.Name(), nil)
	macID := "RDAA" + fmt.Sprintf("%08d", orgID)
	uid := "rda-admin-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "admin", false, false)
	insertDevice(t, db, macID, orgID)

	for _, action := range []string{"read", "upload"} {
		_, reached := runMiddleware(middleware.RequireDeviceAccess(db, action), uid, "", macID)
		if !reached {
			t.Errorf("admin must be allowed for action=%q", action)
		}
	}
}

func TestRequireDeviceAccess_CanReadTrue_AllowsReadBlocksUpload(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "RDACRead_"+t.Name(), nil)
	macID := "RDACR" + fmt.Sprintf("%08d", orgID)
	uid := "rda-reader-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "user", true, false)
	insertDevice(t, db, macID, orgID)

	_, reachedRead := runMiddleware(middleware.RequireDeviceAccess(db, "read"), uid, "", macID)
	if !reachedRead {
		t.Error("can_read=true must allow read")
	}
	_, reachedUpload := runMiddleware(middleware.RequireDeviceAccess(db, "upload"), uid, "", macID)
	if reachedUpload {
		t.Error("can_upload=false must block upload")
	}
}

func TestRequireDeviceAccess_CanUploadTrue_AllowsUploadBlocksRead(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "RDACUpload_"+t.Name(), nil)
	macID := "RDACU" + fmt.Sprintf("%08d", orgID)
	uid := "rda-uploader-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "user", false, true)
	insertDevice(t, db, macID, orgID)

	_, reachedRead := runMiddleware(middleware.RequireDeviceAccess(db, "read"), uid, "", macID)
	if reachedRead {
		t.Error("can_read=false must block read")
	}
	_, reachedUpload := runMiddleware(middleware.RequireDeviceAccess(db, "upload"), uid, "", macID)
	if !reachedUpload {
		t.Error("can_upload=true must allow upload")
	}
}

func TestRequireDeviceAccess_NeitherFlag_BlocksBoth(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "RDANone_"+t.Name(), nil)
	macID := "RDAN" + fmt.Sprintf("%08d", orgID)
	uid := "rda-none-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "user", false, false)
	insertDevice(t, db, macID, orgID)

	for _, action := range []string{"read", "upload"} {
		_, reached := runMiddleware(middleware.RequireDeviceAccess(db, action), uid, "", macID)
		if reached {
			t.Errorf("no-flag user must be blocked for action=%q", action)
		}
	}
}

func TestRequireDeviceAccess_AncestorAdmin_Allowed(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	rootID := insertOrg(t, db, "RDAAncRoot_"+t.Name(), nil)
	childID := insertOrg(t, db, "RDAAncChild_"+t.Name(), &rootID)
	macID := "RDAANC" + fmt.Sprintf("%08d", rootID)
	uid := "rda-anc-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, rootID, "admin", false, false)
	insertDevice(t, db, macID, childID) // device in child, admin of root

	for _, action := range []string{"read", "upload"} {
		_, reached := runMiddleware(middleware.RequireDeviceAccess(db, action), uid, "", macID)
		if !reached {
			t.Errorf("ancestor admin must be allowed for device in child org, action=%q", action)
		}
	}
}

func TestRequireDeviceAccess_RevokedMembership_Blocked(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "RDARevoked_"+t.Name(), nil)
	macID := "RDAREV" + fmt.Sprintf("%08d", orgID)
	uid := "rda-revoked-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "user", true, true)
	revokeInDB(t, db, uid, orgID)
	insertDevice(t, db, macID, orgID)

	for _, action := range []string{"read", "upload"} {
		_, reached := runMiddleware(middleware.RequireDeviceAccess(db, action), uid, "", macID)
		if reached {
			t.Errorf("revoked member must be blocked for action=%q", action)
		}
	}
}

func TestRequireDeviceAccess_SoftDeletedOrg_Blocked(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "RDADelOrg_"+t.Name(), nil)
	macID := "RDADO" + fmt.Sprintf("%08d", orgID)
	uid := "rda-del-org-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "admin", true, true)
	insertDevice(t, db, macID, orgID)
	db.Exec(`UPDATE orgs SET deleted_at=now(), removed_by=$2 WHERE id=$1`, orgID, systemUID)

	for _, action := range []string{"read", "upload"} {
		_, reached := runMiddleware(middleware.RequireDeviceAccess(db, action), uid, "", macID)
		if reached {
			t.Errorf("deleted org must block access for action=%q", action)
		}
	}
}

func TestRequireDeviceAccess_SoftDeletedDevice_Blocked(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "RDADelDev_"+t.Name(), nil)
	macID := "RDADD" + fmt.Sprintf("%08d", orgID)
	uid := "rda-del-dev-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "admin", true, true)
	insertDevice(t, db, macID, orgID)
	db.Exec(`UPDATE devices SET deleted_at=now(), removed_by=$2 WHERE id=$1`, macID, systemUID)

	for _, action := range []string{"read", "upload"} {
		_, reached := runMiddleware(middleware.RequireDeviceAccess(db, action), uid, "", macID)
		if reached {
			t.Errorf("soft-deleted device must block access for action=%q", action)
		}
	}
}

func TestRequireDeviceAccess_DeletedUser_Blocked(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "RDADelUser_"+t.Name(), nil)
	macID := "RDADU" + fmt.Sprintf("%08d", orgID)
	uid := "rda-del-user-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "admin", true, true)
	insertDevice(t, db, macID, orgID)
	db.Exec(`UPDATE users SET deleted_at=now(), removed_by=$2 WHERE id=$1`, uid, systemUID)

	for _, action := range []string{"read", "upload"} {
		_, reached := runMiddleware(middleware.RequireDeviceAccess(db, action), uid, "", macID)
		if reached {
			t.Errorf("deleted user must be blocked for action=%q", action)
		}
	}
}

// ── Device management tests ───────────────────────────────────────────────────

// callDeviceAdd is a thin wrapper around callHandler for POST /api/orgs/:org_id/devices.
func callDeviceAdd(db *sql.DB, uid string, orgID int, macID string) *httptest.ResponseRecorder {
	return callHandler(
		http.MethodPost,
		"/api/orgs/"+strconv.Itoa(orgID)+"/devices",
		uid,
		gin.Params{{Key: "org_id", Value: strconv.Itoa(orgID)}},
		map[string]string{"mac_id": macID},
		chainHandlers(middleware.RequireOrgAdmin(db), handlers.AddDevice(db)),
	)
}

// callDeviceRemove is a thin wrapper for DELETE /api/orgs/:org_id/devices/:mac_id.
func callDeviceRemove(db *sql.DB, uid string, orgID int, macID string) *httptest.ResponseRecorder {
	return callHandler(
		http.MethodDelete,
		"/api/orgs/"+strconv.Itoa(orgID)+"/devices/"+macID,
		uid,
		gin.Params{
			{Key: "org_id", Value: strconv.Itoa(orgID)},
			{Key: "mac_id", Value: macID},
		},
		nil,
		chainHandlers(middleware.RequireOrgAdmin(db), handlers.RemoveDevice(db)),
	)
}

// callDeviceList is a thin wrapper for GET /api/orgs/:org_id/devices.
func callDeviceList(db *sql.DB, uid string, orgID int) *httptest.ResponseRecorder {
	return callHandler(
		http.MethodGet,
		"/api/orgs/"+strconv.Itoa(orgID)+"/devices",
		uid,
		gin.Params{{Key: "org_id", Value: strconv.Itoa(orgID)}},
		nil,
		chainHandlers(middleware.RequireOrgAdmin(db), handlers.ListOrgDevices(db)),
	)
}

// TestAddDevice_AdminCanAdd verifies that an org admin can register a device.
func TestAddDevice_AdminCanAdd(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "AddDeviceOrg", nil)
	uid := "add-device-admin"
	insertUser(t, db, uid, uid+"@test.com")
	grantMembership(t, db, uid, orgID, "admin", true, true)
	t.Cleanup(func() { db.Exec(`DELETE FROM devices WHERE id = $1`, "AABBCCDDEEFF") })

	w := callDeviceAdd(db, uid, orgID, "aabbccddeeff")
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	decodeJSON(t, w, &resp)
	if resp["id"] != "AABBCCDDEEFF" {
		t.Errorf("want upper-cased device ID AABBCCDDEEFF, got %v", resp["id"])
	}
}

// TestAddDevice_UserForbidden verifies that a regular user cannot register a device.
func TestAddDevice_UserForbidden(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "AddDeviceUserForbiddenOrg", nil)
	uid := "add-device-user"
	insertUser(t, db, uid, uid+"@test.com")
	grantMembership(t, db, uid, orgID, "user", true, true)

	w := callDeviceAdd(db, uid, orgID, "AABBCCDDEEFF")
	if w.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAddDevice_AncestorAdminCanAdd verifies that an admin of a parent org can
// register a device to a child org.
func TestAddDevice_AncestorAdminCanAdd(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	parentID := insertOrg(t, db, "AncAddParent", nil)
	childID := insertOrg(t, db, "AncAddChild", &parentID)
	uid := "anc-add-admin"
	insertUser(t, db, uid, uid+"@test.com")
	grantMembership(t, db, uid, parentID, "admin", true, true)
	t.Cleanup(func() { db.Exec(`DELETE FROM devices WHERE id = $1`, "BBCCDDEEFF00") })

	w := callDeviceAdd(db, uid, childID, "BBCCDDEEFF00")
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAddDevice_Conflict_ActiveSameOrg verifies 409 when the same MAC is added twice to the same org.
func TestAddDevice_Conflict_ActiveSameOrg(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "AddDeviceConflictOrg", nil)
	uid := "add-conflict-admin"
	insertUser(t, db, uid, uid+"@test.com")
	grantMembership(t, db, uid, orgID, "admin", true, true)
	insertDevice(t, db, "CCDDEEFF0011", orgID)

	w := callDeviceAdd(db, uid, orgID, "CCDDEEFF0011")
	if w.Code != http.StatusConflict {
		t.Errorf("want 409, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAddDevice_Conflict_ActiveOtherOrg verifies 409 when the MAC is already active in a different org.
func TestAddDevice_Conflict_ActiveOtherOrg(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	org1ID := insertOrg(t, db, "AddConflictOrg1", nil)
	org2ID := insertOrg(t, db, "AddConflictOrg2", nil)
	uid := "add-conflict-admin2"
	insertUser(t, db, uid, uid+"@test.com")
	grantMembership(t, db, uid, org1ID, "admin", true, true)
	grantMembership(t, db, uid, org2ID, "admin", true, true)
	insertDevice(t, db, "DDEEFF001122", org1ID)

	w := callDeviceAdd(db, uid, org2ID, "DDEEFF001122")
	if w.Code != http.StatusConflict {
		t.Errorf("want 409, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAddDevice_SoftDeletedMACCanBeReregistered verifies that a soft-deleted MAC
// can be re-registered, inserting a new row alongside the historical one.
func TestAddDevice_SoftDeletedMACCanBeReregistered(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "AddDeviceReregOrg", nil)
	uid := "add-rereg-admin"
	insertUser(t, db, uid, uid+"@test.com")
	grantMembership(t, db, uid, orgID, "admin", true, true)

	mac := "EEFF00112233"
	insertDevice(t, db, mac, orgID)
	db.Exec(`UPDATE devices SET deleted_at=now(), removed_by=$2 WHERE id=$1`, mac, systemUID)

	// Re-registration must succeed.
	w := callDeviceAdd(db, uid, orgID, mac)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201 for re-registration of soft-deleted MAC, got %d: %s", w.Code, w.Body.String())
	}

	// Two rows must exist for the same MAC now (one historical, one active).
	var total, active int
	db.QueryRow(`SELECT COUNT(*) FROM devices WHERE id = $1`, mac).Scan(&total)
	db.QueryRow(`SELECT COUNT(*) FROM devices WHERE id = $1 AND deleted_at IS NULL`, mac).Scan(&active)
	if total != 2 {
		t.Errorf("want 2 total rows for MAC, got %d", total)
	}
	if active != 1 {
		t.Errorf("want exactly 1 active row for MAC, got %d", active)
	}
}

// TestAddDevice_InvalidMAC verifies 400 for a malformed MAC address.
func TestAddDevice_InvalidMAC(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "InvalidMACOrg", nil)
	uid := "invalid-mac-admin"
	insertUser(t, db, uid, uid+"@test.com")
	grantMembership(t, db, uid, orgID, "admin", true, true)

	w := callDeviceAdd(db, uid, orgID, "not-a-mac")
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRemoveDevice_AdminCanRemove verifies that an org admin can soft-delete a device.
func TestRemoveDevice_AdminCanRemove(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "RemoveDeviceOrg", nil)
	uid := "remove-device-admin"
	insertUser(t, db, uid, uid+"@test.com")
	grantMembership(t, db, uid, orgID, "admin", true, true)
	mac := "FF00112233AA"
	insertDevice(t, db, mac, orgID)

	w := callDeviceRemove(db, uid, orgID, mac)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify soft-delete in DB.
	var deletedAt sql.NullTime
	var removedBy sql.NullString
	db.QueryRow(`SELECT deleted_at, removed_by FROM devices WHERE id=$1`, mac).Scan(&deletedAt, &removedBy)
	if !deletedAt.Valid {
		t.Error("expected deleted_at to be set")
	}
	if removedBy.String != uid {
		t.Errorf("expected removed_by=%q, got %q", uid, removedBy.String)
	}
}

// TestRemoveDevice_UserForbidden verifies that a regular user cannot remove a device.
func TestRemoveDevice_UserForbidden(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "RemoveDeviceUserForbiddenOrg", nil)
	uid := "remove-device-user"
	insertUser(t, db, uid, uid+"@test.com")
	grantMembership(t, db, uid, orgID, "user", true, true)
	mac := "001122334455"
	insertDevice(t, db, mac, orgID)

	w := callDeviceRemove(db, uid, orgID, mac)
	if w.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRemoveDevice_AncestorAdminCanRemove verifies that an admin of a parent org
// can soft-delete a device in a child org.
func TestRemoveDevice_AncestorAdminCanRemove(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	parentID := insertOrg(t, db, "RemoveAncParent", nil)
	childID := insertOrg(t, db, "RemoveAncChild", &parentID)
	uid := "remove-anc-admin"
	insertUser(t, db, uid, uid+"@test.com")
	grantMembership(t, db, uid, parentID, "admin", true, true)
	mac := "112233445566"
	insertDevice(t, db, mac, childID)

	w := callDeviceRemove(db, uid, childID, mac)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRemoveDevice_WrongOrg verifies 404 when the device exists but belongs to a
// different org — prevents cross-org deletion even for root admins.
func TestRemoveDevice_WrongOrg(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	org1ID := insertOrg(t, db, "RemoveWrongOrg1", nil)
	org2ID := insertOrg(t, db, "RemoveWrongOrg2", nil)
	uid := "remove-wrong-admin"
	insertUser(t, db, uid, uid+"@test.com")
	grantMembership(t, db, uid, org1ID, "admin", true, true)
	grantMembership(t, db, uid, org2ID, "admin", true, true)
	mac := "223344556677"
	insertDevice(t, db, mac, org1ID)

	// Try to delete from org2 — device belongs to org1.
	w := callDeviceRemove(db, uid, org2ID, mac)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRemoveDevice_NotFound verifies 404 for a non-existent device.
func TestRemoveDevice_NotFound(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "RemoveNotFoundOrg", nil)
	uid := "remove-notfound-admin"
	insertUser(t, db, uid, uid+"@test.com")
	grantMembership(t, db, uid, orgID, "admin", true, true)

	w := callDeviceRemove(db, uid, orgID, "FFFFFFFFFFFF")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRemoveDevice_AlreadyDeleted verifies idempotent 200 for an already-soft-deleted device.
// Consistent with DeleteOrg, DeleteUser, and RevokeOrgUser — all soft-delete operations
// return 200 when the resource is already in the desired (deleted) state.
func TestRemoveDevice_AlreadyDeleted(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "RemoveAlreadyDeletedOrg", nil)
	uid := "remove-already-admin"
	insertUser(t, db, uid, uid+"@test.com")
	grantMembership(t, db, uid, orgID, "admin", true, true)
	mac := "334455667788"
	insertDevice(t, db, mac, orgID)
	db.Exec(`UPDATE devices SET deleted_at=now(), removed_by=$2 WHERE id=$1`, mac, systemUID)

	w := callDeviceRemove(db, uid, orgID, mac)
	if w.Code != http.StatusOK {
		t.Errorf("want 200 (idempotent), got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	decodeJSON(t, w, &resp)
	if resp["already_deleted"] != true {
		t.Errorf("want already_deleted=true in response, got %v", resp)
	}
}

// TestListOrgDevices_AdminSeesDevices verifies an org admin gets all active devices.
func TestListOrgDevices_AdminSeesDevices(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "ListDevicesOrg", nil)
	uid := "list-devices-admin"
	insertUser(t, db, uid, uid+"@test.com")
	grantMembership(t, db, uid, orgID, "admin", true, true)
	insertDevice(t, db, "445566778899", orgID)
	insertDevice(t, db, "5566778899AA", orgID)

	w := callDeviceList(db, uid, orgID)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp []map[string]any
	decodeJSON(t, w, &resp)
	if len(resp) < 2 {
		t.Errorf("want at least 2 devices, got %d", len(resp))
	}
}

// TestListOrgDevices_UserForbidden verifies that a regular user cannot list devices.
func TestListOrgDevices_UserForbidden(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "ListDevicesUserForbiddenOrg", nil)
	uid := "list-devices-user"
	insertUser(t, db, uid, uid+"@test.com")
	grantMembership(t, db, uid, orgID, "user", true, true)

	w := callDeviceList(db, uid, orgID)
	if w.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListOrgDevices_ExcludesDeleted verifies soft-deleted devices are not returned.
func TestListOrgDevices_ExcludesDeleted(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "ListDevicesDeletedOrg", nil)
	uid := "list-deleted-admin"
	insertUser(t, db, uid, uid+"@test.com")
	grantMembership(t, db, uid, orgID, "admin", true, true)
	mac := "6677889900BB"
	insertDevice(t, db, mac, orgID)
	db.Exec(`UPDATE devices SET deleted_at=now(), removed_by=$2 WHERE id=$1`, mac, systemUID)

	w := callDeviceList(db, uid, orgID)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp []map[string]any
	decodeJSON(t, w, &resp)
	for _, d := range resp {
		if d["id"] == mac {
			t.Errorf("soft-deleted device %q must not appear in listing", mac)
		}
	}
}

// ── Soft-delete invariant tests ───────────────────────────────────────────────
// These tests verify that the deleted_at IS NULL filter works correctly for
// each entity type — i.e. soft-deleted rows are invisible to the live queries.

// TestSoftDelete_UserInvisibleAfterDelete verifies that a soft-deleted user
// cannot be granted new org permissions (handler returns 404).
func TestSoftDelete_UserInvisibleAfterDelete(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "SDUser_"+t.Name(), nil)
	uid := "sd-user-" + t.Name()
	email := uid + "@example.com"
	insertUser(t, db, uid, email)

	// Soft-delete the user.
	_, err := db.Exec(`UPDATE users SET deleted_at = now(), removed_by = $2 WHERE id = $1`, uid, systemUID)
	if err != nil {
		t.Fatalf("soft-delete user: %v", err)
	}

	// Confirm deleted_at is set and removed_by is non-empty (CHECK constraint satisfied).
	var deletedAt sql.NullTime
	var removedBy sql.NullString
	db.QueryRow(`SELECT deleted_at, removed_by FROM users WHERE id=$1`, uid).Scan(&deletedAt, &removedBy)
	if !deletedAt.Valid {
		t.Error("deleted_at must be set after soft-delete")
	}
	if !removedBy.Valid || removedBy.String == "" {
		t.Error("removed_by must be set alongside deleted_at (CHECK constraint)")
	}

	// Attempting to grant permissions to the soft-deleted user must return 404.
	fb := &fakeFirebase{record: fakeUserRecord(uid, email)}
	w := callHandler(http.MethodPost, "/api/orgs/1/users", systemUID,
		gin.Params{{Key: "org_id", Value: fmt.Sprintf("%d", orgID)}},
		map[string]any{"email": email, "role": "user", "can_read": true},
		handlers.UpsertOrgUser(db, fb, noopOOB),
	)
	if w.Code != http.StatusNotFound {
		t.Errorf("granting permissions to soft-deleted user: want 404, got %d: %s", w.Code, w.Body.String())
	}

	// The unique email index (on active users only) must allow a new active user
	// with the same email to be inserted once the old row is deleted.
	newUID := uid + "-new"
	_, err = db.Exec(`INSERT INTO users (id, email, added_by) VALUES ($1, $2, $3)`, newUID, email, systemUID)
	if err != nil {
		t.Errorf("inserting new active user with same email as soft-deleted user: %v", err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM users WHERE id = $1`, newUID)
	})
}

// TestSoftDelete_OrgInvisibleAfterDelete verifies that a soft-deleted org does
// not appear in ListOrgs, even for a user who was admin before deletion.
func TestSoftDelete_OrgInvisibleAfterDelete(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "SDOrg_"+t.Name(), nil)
	uid := "sd-org-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "admin", true, true)

	// Soft-delete the org.
	_, err := db.Exec(`UPDATE orgs SET deleted_at = now(), removed_by = $2 WHERE id = $1`, orgID, systemUID)
	if err != nil {
		t.Fatalf("soft-delete org: %v", err)
	}

	// Confirm deleted_at and removed_by both set.
	var deletedAt sql.NullTime
	var removedBy sql.NullString
	db.QueryRow(`SELECT deleted_at, removed_by FROM orgs WHERE id=$1`, orgID).Scan(&deletedAt, &removedBy)
	if !deletedAt.Valid {
		t.Error("org deleted_at must be set")
	}
	if !removedBy.Valid || removedBy.String == "" {
		t.Error("org removed_by must be set alongside deleted_at (CHECK constraint)")
	}

	// ListOrgs must not return the deleted org.
	w := callHandler(http.MethodGet, "/api/orgs", uid, nil, nil, handlers.ListOrgs(db))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var rows []struct{ ID int `json:"id"` }
	decodeJSON(t, w, &rows)
	for _, r := range rows {
		if r.ID == orgID {
			t.Errorf("soft-deleted org %d must not appear in ListOrgs", orgID)
		}
	}

	// RequireOrgAdmin must block the previously-admin user on the deleted org.
	code, reached := runMiddleware(middleware.RequireOrgAdmin(db), uid, fmt.Sprintf("%d", orgID), "")
	if reached || code != http.StatusForbidden {
		t.Errorf("RequireOrgAdmin on deleted org: want 403 not-reached, got code=%d reached=%v", code, reached)
	}
}

// ── IDOR: cross-org device access ────────────────────────────────────────────
//
// Scenario: a user holds full access (can_read=true, can_upload=true) on Org A
// and tries to access a device that belongs to the completely unrelated Org B.
// The RequireDeviceAccess CTE must find no path and return 403.

func TestRequireDeviceAccess_CrossOrgIDOR_Returns403(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgA := insertOrg(t, db, "IDOROrgA_"+t.Name(), nil)
	orgB := insertOrg(t, db, "IDOROrgB_"+t.Name(), nil)
	uid := "idor-user-" + t.Name()
	deviceY := "IDORY" + fmt.Sprintf("%07d", orgB)
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgA, "user", true, true) // full flags on Org A
	insertDevice(t, db, deviceY, orgB)                    // Device Y belongs to Org B

	for _, action := range []string{"read", "upload"} {
		code, reached := runMiddleware(middleware.RequireDeviceAccess(db, action), uid, "", deviceY)
		if reached || code != http.StatusForbidden {
			t.Errorf("Org A user on Org B device (action=%q): want 403 not-reached, got code=%d reached=%v", action, code, reached)
		}
	}
}

// TestRequireDeviceAccess_CrossOrgIDOR_AdminStillBlocked tests that even an Org A
// admin cannot reach a device in the completely unrelated Org B.
func TestRequireDeviceAccess_CrossOrgAdminIDOR_Returns403(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgA := insertOrg(t, db, "IDORAdmA_"+t.Name(), nil)
	orgB := insertOrg(t, db, "IDORAdmB_"+t.Name(), nil)
	uid := "idor-admin-" + t.Name()
	deviceB := "IDORADM" + fmt.Sprintf("%05d", orgB)
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgA, "admin", true, true) // admin on Org A only
	insertDevice(t, db, deviceB, orgB)

	for _, action := range []string{"read", "upload"} {
		code, reached := runMiddleware(middleware.RequireDeviceAccess(db, action), uid, "", deviceB)
		if reached || code != http.StatusForbidden {
			t.Errorf("Org A admin on Org B device (action=%q): want 403 not-reached, got code=%d reached=%v", action, code, reached)
		}
	}
}

// ── Rate limit ────────────────────────────────────────────────────────────────
//
// The RateLimit middleware uses a per-IP token bucket. All requests from the same
// IP share one bucket. We use a high rps so the bucket does not refill during the
// test, and a burst of 3 so we can reliably exhaust it in a small number of calls.

func TestRateLimit_ExceedsBurst_Returns429(t *testing.T) {
	recordResult(t)
	const burst = 3
	mw := middleware.RateLimit(1000.0, burst) // refill fast; only burst matters here

	fire := func() int {
		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = "10.0.0.1:9999" // fixed IP keeps all calls in the same bucket
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		mw(c)
		return w.Code
	}

	allowed, rejected := 0, 0
	for i := 0; i < burst*2; i++ {
		if fire() == http.StatusOK {
			allowed++
		} else {
			rejected++
		}
	}
	if allowed != burst {
		t.Errorf("want %d allowed (burst), got %d", burst, allowed)
	}
	if rejected != burst {
		t.Errorf("want %d rejected with 429, got %d", burst, rejected)
	}
}

// TestRateLimit_DifferentIPs_IndependentBuckets verifies that two callers from
// different IPs each get their own full burst quota.
func TestRateLimit_DifferentIPs_IndependentBuckets(t *testing.T) {
	recordResult(t)
	const burst = 2
	mw := middleware.RateLimit(1000.0, burst)

	fire := func(ip string) int {
		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip + ":9999"
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		mw(c)
		return w.Code
	}

	// Exhaust IP-A's bucket.
	for i := 0; i < burst; i++ {
		if fire("10.0.0.1") != http.StatusOK {
			t.Fatalf("IP-A request %d: want 200 within burst", i+1)
		}
	}
	if fire("10.0.0.1") != http.StatusTooManyRequests {
		t.Error("IP-A: want 429 after burst exhausted")
	}

	// IP-B must still have its own full budget.
	for i := 0; i < burst; i++ {
		if fire("10.0.0.2") != http.StatusOK {
			t.Errorf("IP-B request %d: want 200 (independent bucket), got 429", i+1)
		}
	}
}

// ── TestSoftDelete_DeviceInvisibleAfterDelete ─────────────────────────────────

// TestSoftDelete_DeviceInvisibleAfterDelete verifies that a soft-deleted device
// does not appear in ListOrgDevices and that RequireDeviceAccess blocks access.
// ── VerifyFirebaseJWT — MFA enforcement (A-2/A-3) ────────────────────────────

// fakeVerifier satisfies middleware.TokenVerifier for unit tests.
type fakeVerifier struct {
	token *auth.Token
	err   error
}

func (f *fakeVerifier) VerifyIDToken(_ context.Context, _ string) (*auth.Token, error) {
	return f.token, f.err
}

// makeJWTToken constructs a minimal *auth.Token with the given UID and optional
// second factor. Pass secondFactor="" to simulate a token issued without TOTP.
func makeJWTToken(uid, secondFactor string) *auth.Token {
	firebase := map[string]interface{}{
		"sign_in_provider": "password",
	}
	if secondFactor != "" {
		firebase["sign_in_second_factor"] = secondFactor
	}
	return &auth.Token{
		UID: uid,
		Claims: map[string]interface{}{
			"firebase": firebase,
		},
	}
}

// runJWTMiddleware fires VerifyFirebaseJWT against a test context.
// authHeader is the raw value of the Authorization header; pass "" to omit it.
// Returns the response status code and whether the next handler was reached.
func runJWTMiddleware(t *testing.T, verifier middleware.TokenVerifier, authHeader string) (statusCode int, nextReached bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	middleware.VerifyFirebaseJWT(verifier)(c)
	return w.Code, !c.IsAborted()
}

func TestVerifyFirebaseJWT_WithTOTPSecondFactor_Allows(t *testing.T) {
	recordResult(t)
	verifier := &fakeVerifier{token: makeJWTToken("uid-mfa", "totp")}
	status, reached := runJWTMiddleware(t, verifier, "Bearer valid-token")
	if status != http.StatusOK {
		t.Errorf("TOTP token: want 200, got %d", status)
	}
	if !reached {
		t.Error("TOTP token: next handler must be called")
	}
}

func TestVerifyFirebaseJWT_NoSecondFactor_ReturnsMFARequired(t *testing.T) {
	recordResult(t)
	verifier := &fakeVerifier{token: makeJWTToken("uid-no-mfa", "")}
	status, reached := runJWTMiddleware(t, verifier, "Bearer valid-token")
	if status != http.StatusForbidden {
		t.Errorf("no TOTP second factor: want 403, got %d", status)
	}
	if reached {
		t.Error("no TOTP second factor: next handler must NOT be called")
	}
	// Response body must carry the machine-readable code.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	middleware.VerifyFirebaseJWT(verifier)(c)
	var body struct {
		Code string `json:"code"`
	}
	json.NewDecoder(w.Body).Decode(&body)
	if body.Code != middleware.ErrCodeMFARequired {
		t.Errorf("want code=%q, got %q", middleware.ErrCodeMFARequired, body.Code)
	}
}

func TestVerifyFirebaseJWT_MissingFirebaseClaim_ReturnsMFARequired(t *testing.T) {
	recordResult(t)
	// Token with no "firebase" claim at all — fail-closed.
	verifier := &fakeVerifier{token: &auth.Token{
		UID:    "uid-no-claim",
		Claims: map[string]interface{}{},
	}}
	status, reached := runJWTMiddleware(t, verifier, "Bearer valid-token")
	if status != http.StatusForbidden {
		t.Errorf("missing firebase claim: want 403, got %d", status)
	}
	if reached {
		t.Error("missing firebase claim: next handler must NOT be called")
	}
}

func TestVerifyFirebaseJWT_MissingHeader_Returns401(t *testing.T) {
	recordResult(t)
	verifier := &fakeVerifier{} // VerifyIDToken never called
	status, reached := runJWTMiddleware(t, verifier, "")
	if status != http.StatusUnauthorized {
		t.Errorf("missing header: want 401, got %d", status)
	}
	if reached {
		t.Error("missing header: next handler must NOT be called")
	}
}

func TestVerifyFirebaseJWT_MalformedHeader_Returns401(t *testing.T) {
	recordResult(t)
	verifier := &fakeVerifier{}
	status, reached := runJWTMiddleware(t, verifier, "Token not-a-bearer")
	if status != http.StatusUnauthorized {
		t.Errorf("malformed header: want 401, got %d", status)
	}
	if reached {
		t.Error("malformed header: next handler must NOT be called")
	}
}

func TestVerifyFirebaseJWT_InvalidToken_Returns401(t *testing.T) {
	recordResult(t)
	verifier := &fakeVerifier{err: fmt.Errorf("firebase: invalid token")}
	status, reached := runJWTMiddleware(t, verifier, "Bearer bad-token")
	if status != http.StatusUnauthorized {
		t.Errorf("invalid token: want 401, got %d", status)
	}
	if reached {
		t.Error("invalid token: next handler must NOT be called")
	}
}

func TestVerifyFirebaseJWT_InjectsUID(t *testing.T) {
	recordResult(t)
	verifier := &fakeVerifier{token: makeJWTToken("expected-uid", "totp")}
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	middleware.VerifyFirebaseJWT(verifier)(c)
	uid, exists := c.Get(middleware.ContextKeyUID)
	if !exists {
		t.Fatal("uid key must be set in context after successful auth")
	}
	if uid != "expected-uid" {
		t.Errorf("uid: got %q want %q", uid, "expected-uid")
	}
}

// ── Soft-delete lifecycle (pre-existing) ──────────────────────────────────────

func TestSoftDelete_DeviceInvisibleAfterDelete(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "SDDevice_"+t.Name(), nil)
	uid := "sd-device-" + t.Name()
	mac := "SDDEV" + fmt.Sprintf("%07d", orgID)
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "admin", true, true)
	insertDevice(t, db, mac, orgID)

	// Soft-delete the device.
	_, err := db.Exec(`UPDATE devices SET deleted_at = now(), removed_by = $2 WHERE id = $1`, mac, systemUID)
	if err != nil {
		t.Fatalf("soft-delete device: %v", err)
	}

	// Confirm deleted_at and removed_by both set.
	var deletedAt sql.NullTime
	var removedBy sql.NullString
	db.QueryRow(`SELECT deleted_at, removed_by FROM devices WHERE id=$1`, mac).Scan(&deletedAt, &removedBy)
	if !deletedAt.Valid {
		t.Error("device deleted_at must be set")
	}
	if !removedBy.Valid || removedBy.String == "" {
		t.Error("device removed_by must be set alongside deleted_at (CHECK constraint)")
	}

	// ListOrgDevices must not return the deleted device.
	w := callDeviceList(db, uid, orgID)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp []map[string]any
	decodeJSON(t, w, &resp)
	for _, d := range resp {
		if d["id"] == mac {
			t.Errorf("soft-deleted device %q must not appear in ListOrgDevices", mac)
		}
	}

	// RequireDeviceAccess must block both read and upload on the deleted device.
	for _, action := range []string{"read", "upload"} {
		code, reached := runMiddleware(middleware.RequireDeviceAccess(db, action), uid, "", mac)
		if reached || code != http.StatusForbidden {
			t.Errorf("RequireDeviceAccess(%q) on deleted device: want 403 not-reached, got code=%d reached=%v", action, code, reached)
		}
	}

	// The device can be re-registered (new active row alongside the historical one).
	w2 := callDeviceAdd(db, uid, orgID, mac)
	if w2.Code != http.StatusCreated {
		t.Errorf("re-registering soft-deleted MAC: want 201, got %d: %s", w2.Code, w2.Body.String())
	}
	var total, active int
	db.QueryRow(`SELECT COUNT(*) FROM devices WHERE id = $1`, mac).Scan(&total)
	db.QueryRow(`SELECT COUNT(*) FROM devices WHERE id = $1 AND deleted_at IS NULL`, mac).Scan(&active)
	if total != 2 {
		t.Errorf("want 2 total rows after re-registration, got %d", total)
	}
	if active != 1 {
		t.Errorf("want exactly 1 active row after re-registration, got %d", active)
	}
}
