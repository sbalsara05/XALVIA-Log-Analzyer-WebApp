package integration

// Tests for GET /api/me/permissions (handlers.MyPermissions).
// Shares seed helpers with control_plane_test.go and needs the same Docker Postgres.

import (
	"database/sql"
	"fmt"
	"net/http"
	"testing"

	"ricovr/gateway/handlers"
	"ricovr/gateway/middleware"
)

type meMembership struct {
	OrgID     int    `json:"org_id"`
	OrgName   string `json:"org_name"`
	Role      string `json:"role"`
	CanRead   bool   `json:"can_read"`
	CanUpload bool   `json:"can_upload"`
	GrantedAt string `json:"granted_at"`
}

type meDevice struct {
	MacID     string `json:"mac_id"`
	OrgID     int    `json:"org_id"`
	OrgName   string `json:"org_name"`
	CanRead   bool   `json:"can_read"`
	CanUpload bool   `json:"can_upload"`
	Via       string `json:"via"`
}

type mePermissions struct {
	UID         string         `json:"uid"`
	Email       string         `json:"email"`
	Memberships []meMembership `json:"memberships"`
	Devices     []meDevice     `json:"devices"`
}

// getMyPermissions calls MyPermissions as uid and decodes a 200 response.
func getMyPermissions(t *testing.T, db *sql.DB, uid string) mePermissions {
	t.Helper()
	w := callHandler(http.MethodGet, "/api/me/permissions", uid, nil, nil, handlers.MyPermissions(db))
	if w.Code != http.StatusOK {
		t.Fatalf("MyPermissions(%s): want 200, got %d: %s", uid, w.Code, w.Body.String())
	}
	var got mePermissions
	decodeJSON(t, w, &got)
	return got
}

func deviceByMAC(p mePermissions, macID string) (meDevice, bool) {
	for _, d := range p.Devices {
		if d.MacID == macID {
			return d, true
		}
	}
	return meDevice{}, false
}

func TestMyPermissions_NoMemberships_ReturnsEmptyArrays(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	uid := "me-none-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")

	w := callHandler(http.MethodGet, "/api/me/permissions", uid, nil, nil, handlers.MyPermissions(db))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var raw map[string]any
	decodeJSON(t, w, &raw)
	for _, key := range []string{"memberships", "devices"} {
		arr, ok := raw[key].([]any)
		if !ok {
			t.Errorf("%s must be a JSON array, got %#v", key, raw[key])
		} else if len(arr) != 0 {
			t.Errorf("%s: want empty, got %v", key, arr)
		}
	}
	if raw["uid"] != uid || raw["email"] != uid+"@example.com" {
		t.Errorf("identity: got uid=%v email=%v", raw["uid"], raw["email"])
	}
}

func TestMyPermissions_UnknownUser_Returns404(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	w := callHandler(http.MethodGet, "/api/me/permissions", "me-ghost-"+t.Name(), nil, nil, handlers.MyPermissions(db))
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMyPermissions_DeletedUser_Returns404(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "MeDelUser_"+t.Name(), nil)
	uid := "me-del-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "admin", true, true)
	db.Exec(`UPDATE users SET deleted_at = now(), removed_by = $2 WHERE id = $1`, uid, systemUID)

	w := callHandler(http.MethodGet, "/api/me/permissions", uid, nil, nil, handlers.MyPermissions(db))
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMyPermissions_PlainUser_ReadOnly(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgName := "MeRead_" + t.Name()
	orgID := insertOrg(t, db, orgName, nil)
	macID := "MERD" + fmt.Sprintf("%08d", orgID)
	uid := "me-reader-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "user", true, false)
	insertDevice(t, db, macID, orgID)

	got := getMyPermissions(t, db, uid)
	if len(got.Memberships) != 1 {
		t.Fatalf("want 1 membership, got %+v", got.Memberships)
	}
	m := got.Memberships[0]
	if m.OrgID != orgID || m.OrgName != orgName || m.Role != "user" || !m.CanRead || m.CanUpload || m.GrantedAt == "" {
		t.Errorf("membership: got %+v", m)
	}
	d, ok := deviceByMAC(got, macID)
	if !ok {
		t.Fatalf("device %s missing: %+v", macID, got.Devices)
	}
	if !d.CanRead || d.CanUpload || d.Via != "member" || d.OrgID != orgID || d.OrgName != orgName {
		t.Errorf("device: got %+v", d)
	}
}

func TestMyPermissions_PlainUser_NoFlags_DeviceOmitted(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "MeNoFlags_"+t.Name(), nil)
	macID := "MENF" + fmt.Sprintf("%08d", orgID)
	uid := "me-noflags-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "user", false, false)
	insertDevice(t, db, macID, orgID)

	got := getMyPermissions(t, db, uid)
	if len(got.Memberships) != 1 {
		t.Errorf("membership must still be listed, got %+v", got.Memberships)
	}
	if _, ok := deviceByMAC(got, macID); ok {
		t.Errorf("device with no flags must be omitted: %+v", got.Devices)
	}
}

func TestMyPermissions_PlainUserInParent_DoesNotReachChildDevices(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	parentID := insertOrg(t, db, "MeParent_"+t.Name(), nil)
	childID := insertOrg(t, db, "MeChild", intPtr(parentID))
	childMAC := "MEPC" + fmt.Sprintf("%08d", childID)
	uid := "me-parentuser-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, parentID, "user", true, true)
	insertDevice(t, db, childMAC, childID)

	got := getMyPermissions(t, db, uid)
	if _, ok := deviceByMAC(got, childMAC); ok {
		t.Errorf("plain-user grant must not pass down to child org devices: %+v", got.Devices)
	}
}

func TestMyPermissions_AncestorAdmin_ReachesGrandchildDevices(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	rootID := insertOrg(t, db, "MeGrand_"+t.Name(), nil)
	childID := insertOrg(t, db, "MeGrandChild", intPtr(rootID))
	grandID := insertOrg(t, db, "MeGrandGrandchild", intPtr(childID))
	macID := "MEGG" + fmt.Sprintf("%08d", grandID)
	uid := "me-grandadmin-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, rootID, "admin", true, true)
	insertDevice(t, db, macID, grandID)

	got := getMyPermissions(t, db, uid)
	d, ok := deviceByMAC(got, macID)
	if !ok {
		t.Fatalf("grandchild device missing: %+v", got.Devices)
	}
	if !d.CanRead || !d.CanUpload || d.Via != "admin" || d.OrgID != grandID || d.OrgName != "MeGrandGrandchild" {
		t.Errorf("device: got %+v", d)
	}
}

func TestMyPermissions_AdminAndMemberPaths_AdminWins(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	parentID := insertOrg(t, db, "MeBoth_"+t.Name(), nil)
	childID := insertOrg(t, db, "MeBothChild", intPtr(parentID))
	macID := "MEBO" + fmt.Sprintf("%08d", childID)
	uid := "me-both-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, parentID, "admin", true, true)
	grantMembership(t, db, uid, childID, "user", true, false)
	insertDevice(t, db, macID, childID)

	got := getMyPermissions(t, db, uid)
	if len(got.Memberships) != 2 {
		t.Errorf("want 2 memberships, got %+v", got.Memberships)
	}
	count := 0
	for _, d := range got.Devices {
		if d.MacID == macID {
			count++
			if !d.CanRead || !d.CanUpload || d.Via != "admin" {
				t.Errorf("device: got %+v", d)
			}
		}
	}
	if count != 1 {
		t.Errorf("device must appear exactly once, got %d: %+v", count, got.Devices)
	}
}

func TestMyPermissions_RevokedMembership_Excluded(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	orgID := insertOrg(t, db, "MeRevoked_"+t.Name(), nil)
	macID := "MERV" + fmt.Sprintf("%08d", orgID)
	uid := "me-revoked-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, orgID, "admin", true, true)
	insertDevice(t, db, macID, orgID)
	revokeInDB(t, db, uid, orgID)

	got := getMyPermissions(t, db, uid)
	if len(got.Memberships) != 0 || len(got.Devices) != 0 {
		t.Errorf("revoked membership must grant nothing, got %+v", got)
	}
}

func TestMyPermissions_DeletedOrgAndDevice_Excluded(t *testing.T) {
	recordResult(t)
	db := testDB(t)
	rootID := insertOrg(t, db, "MeDeleted_"+t.Name(), nil)
	deadOrgID := insertOrg(t, db, "MeDeadOrg", intPtr(rootID))
	deadOrgMAC := "MEDO" + fmt.Sprintf("%08d", deadOrgID)
	deadMAC := "MEDD" + fmt.Sprintf("%08d", rootID)
	uid := "me-deleted-" + t.Name()
	insertUser(t, db, uid, uid+"@example.com")
	grantMembership(t, db, uid, rootID, "admin", true, true)
	insertDevice(t, db, deadOrgMAC, deadOrgID)
	insertDevice(t, db, deadMAC, rootID)
	db.Exec(`UPDATE orgs SET deleted_at = now(), removed_by = $2 WHERE id = $1`, deadOrgID, systemUID)
	db.Exec(`UPDATE devices SET deleted_at = now(), removed_by = $2 WHERE id = $1`, deadMAC, systemUID)

	got := getMyPermissions(t, db, uid)
	for _, mac := range []string{deadOrgMAC, deadMAC} {
		if _, ok := deviceByMAC(got, mac); ok {
			t.Errorf("device %s must be excluded: %+v", mac, got.Devices)
		}
	}
}

// TestMyPermissions_ParityWithRequireDeviceAccess checks that, for every
// (user, device) pair in a mixed org tree, the flags MyPermissions reports
// equal what RequireDeviceAccess actually enforces for read and upload.
func TestMyPermissions_ParityWithRequireDeviceAccess(t *testing.T) {
	recordResult(t)
	db := testDB(t)

	// Tree: root → child → grandchild, root → sibling, child → dead (soft-deleted).
	rootID := insertOrg(t, db, "MeParity_"+t.Name(), nil)
	orgs := map[string]int{"root": rootID}
	orgs["child"] = insertOrg(t, db, "MeParityChild", intPtr(rootID))
	orgs["grand"] = insertOrg(t, db, "MeParityGrand", intPtr(orgs["child"]))
	orgs["sibling"] = insertOrg(t, db, "MeParitySibling", intPtr(rootID))
	orgs["dead"] = insertOrg(t, db, "MeParityDead", intPtr(orgs["child"]))

	var macs []string
	for _, id := range orgs {
		mac := fmt.Sprintf("MEPAR%08d", id)
		insertDevice(t, db, mac, id)
		macs = append(macs, mac)
	}

	type grant struct {
		org                string
		role               string
		canRead, canUpload bool
	}
	users := map[string][]grant{
		"root-admin":    {{"root", "admin", true, true}},
		"child-admin":   {{"child", "admin", true, true}},
		"child-reader":  {{"child", "user", true, false}},
		"child-up":      {{"child", "user", false, true}},
		"child-both":    {{"child", "user", true, true}},
		"child-none":    {{"child", "user", false, false}},
		"root-reader":   {{"root", "user", true, false}},
		"mixed":         {{"child", "user", true, false}, {"grand", "admin", true, true}},
		"dead-member":   {{"dead", "user", true, true}},
		"dead-admin":    {{"dead", "admin", true, true}},
		"sibling-admin": {{"sibling", "admin", true, true}},
		"revoked-admin": {{"root", "admin", true, true}},
		"no-grants":     nil,
	}

	uids := map[string]string{}
	for name, grants := range users {
		uid := "me-par-" + name + "-" + t.Name()
		uids[name] = uid
		insertUser(t, db, uid, uid+"@example.com")
		for _, g := range grants {
			grantMembership(t, db, uid, orgs[g.org], g.role, g.canRead, g.canUpload)
		}
	}
	revokeInDB(t, db, uids["revoked-admin"], rootID)

	// Soft-delete only the dead org row, leaving its device and memberships
	// active, so both code paths see the same partially-deleted state.
	db.Exec(`UPDATE orgs SET deleted_at = now(), removed_by = $2 WHERE id = $1`, orgs["dead"], systemUID)

	for name, uid := range uids {
		got := getMyPermissions(t, db, uid)
		for _, mac := range macs {
			d, _ := deviceByMAC(got, mac)
			reported := map[string]bool{"read": d.CanRead, "upload": d.CanUpload}
			for _, action := range []string{"read", "upload"} {
				_, enforced := runMiddleware(middleware.RequireDeviceAccess(db, action), uid, "", mac)
				if reported[action] != enforced {
					t.Errorf("user=%s device=%s action=%s: /me/permissions says %v, middleware says %v",
						name, mac, action, reported[action], enforced)
				}
			}
		}
	}
}
