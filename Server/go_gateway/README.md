# go_gateway — HTTP Gateway Service

## Overview

The Go Gateway is an authenticated HTTP proxy between the Python/web clients and Google Cloud Storage. It enforces Firebase JWT + TOTP MFA on every request, checks device and org access via Postgres, generates GCS signed URLs on behalf of clients, and runs server-side log parsing.

---

## Server Modes

Set via `SERVER_MODE` environment variable. The gateway refuses to start if the value is not one of the three below.

| `SERVER_MODE` | Routes active | `DATABASE_URL` required |
|---|---|---|
| `DATA_PLANE` | Data plane only | No (but DB is still connected if set) |
| `CONTROL_PLANE` | Control plane only | Yes |
| `UNIFIED` | Both | Yes |

---

## Auth Middleware — Applied to all `/api/*` routes

Every `/api/*` route runs `VerifyFirebaseJWT` then `ACL`. Failures abort before the handler runs.

### `VerifyFirebaseJWT`

| Condition | Status | Body |
|-----------|--------|------|
| `Authorization` header absent or not `Bearer ...` | **401** | `{"error": "authorization header missing or malformed", "code": "token_missing"}` |
| Token signature invalid, wrong project, etc. | **401** | `{"error": "unauthorized", "code": "token_invalid"}` |
| Token past its `exp` claim | **401** | `{"error": "unauthorized", "code": "token_expired"}` |
| Token valid but `sign_in_second_factor` claim absent (TOTP MFA not completed) | **403** | `{"error": "MFA required", "code": "mfa_required"}` |
| Token valid and MFA completed | — | UID injected into context, continues |

### `RequireDeviceAccess(action)` — data plane device routes only

Checks that the calling user has `role='admin'` or the required flag (`can_read` / `can_upload`) in any org in the device's ownership chain.

| Condition | Status | Body |
|-----------|--------|------|
| `mac_id` absent from query and body | **403** | `{"error": "forbidden"}` |
| DB query error | **403** | `{"error": "forbidden"}` |
| No matching permission found | **403** | `{"error": "forbidden"}` |
| Access granted | — | Continues |

### `RequireOrgAdmin` — control plane org routes

| Condition | Status | Body |
|-----------|--------|------|
| `:org_id` param missing or not an integer | **400** | `{"error": "org_id must be an integer"}` |
| Caller is not admin of the org (or any ancestor) | **403** | `{"error": "forbidden"}` |
| Access granted | — | Continues |

### `RequireRootOrgAdmin` — `DELETE /api/users/:user_id` only

| Condition | Status | Body |
|-----------|--------|------|
| Caller is not admin of the root org | **403** | `{"error": "forbidden"}` |
| DB error resolving root org | **500** | `{"error": "auth check failed"}` |

---

## Data Plane Endpoints

### `GET /health`

No auth required.

| Status | Body |
|--------|------|
| **200** | `{"status": "ok"}` |

---

### `GET /api/profile`

Requires: `VerifyFirebaseJWT`, `ACL`.

| Status | Body |
|--------|------|
| **200** | `{"uid": "...", "email": "...", "organization": "...", "role": "...", "read": bool, "write": bool}` |

---

### `GET /api/me/permissions`

Requires: `VerifyFirebaseJWT`. No device check - the response only covers the caller.

Lists the caller's active org memberships and every active device they can reach, with the effective flags `RequireDeviceAccess` would enforce:

- `role='admin'` in the device's org or any ancestor → `can_read` and `can_upload` true, `via: "admin"`.
- `role='user'` in the device's own org → that membership's flags, `via: "member"`. User grants do not pass down to child orgs.

Devices where both flags are false are left out. Sorted by org name, then MAC.

| Status | Body |
|--------|------|
| **200** | `{"uid": "...", "email": "...", "memberships": [{"org_id": 3, "org_name": "...", "role": "user", "can_read": true, "can_upload": false, "granted_at": "<RFC3339>"}], "devices": [{"mac_id": "AABBCCDDEEFF", "org_id": 3, "org_name": "...", "can_read": true, "can_upload": false, "via": "member"}]}` - both arrays are `[]` when the user has no access |
| **401 / 403** | See Auth Middleware |
| **404** | `{"error": "user not found"}` - no active `users` row |
| **500** | `{"error": "query failed"}` |

---

### `GET /api/files?mac_id=X`

Requires: `VerifyFirebaseJWT`, `ACL`, `RequireDeviceAccess("read")`.

Returns deduplicated filenames across legacy flat files and committed sessions. Only files in sessions with a `_COMMIT.json` marker are included; the latest session wins on name collision.

| Status | Body |
|--------|------|
| **200** | `["filename1.txt", "filename2.txt"]` — empty array if no committed files |
| **400** | `{"error": "mac_id is missing or not a valid MAC address"}` — invalid or missing mac_id |
| **401 / 403** | See Auth Middleware |
| **500** | `{"error": "storage error"}` — GCS listing failed |

---

### `GET /api/files/metadata?mac_id=X`

Requires: `VerifyFirebaseJWT`, `RequireDeviceAccess("read")`.

Returns the same files as `GET /api/files`, sorted by name, each described by the GCS object `signed-url/read` would serve. `session_id` is `null` for legacy flat files. `uploaded_by` is `null` for files uploaded before the gateway recorded uploaders.

| Status | Body |
|--------|------|
| **200** | `[{"name": "f1.txt", "size": 48213, "updated": "<RFC3339>", "content_type": "application/octet-stream", "session_id": "YYYYMMDD_HHMMSS_microseconds", "uploaded_by": "<firebase uid>"}]` - empty array if no committed files |
| **400** | `{"error": "mac_id is missing or not a valid MAC address"}` |
| **401 / 403** | See Auth Middleware |
| **500** | `{"error": "storage error"}` - GCS listing failed |

---

### `GET /api/signed-url/read?mac_id=X&filename=Y`

Requires: `VerifyFirebaseJWT`, `ACL`, `RequireDeviceAccess("read")`.

Resolves the file across legacy path and committed sessions (latest session wins), then signs a 15-minute GET URL.

| Status | Body |
|--------|------|
| **200** | `{"url": "<GCS signed GET URL>"}` |
| **400** | `{"error": "mac_id and filename query params required"}` |
| **401 / 403** | See Auth Middleware |
| **404** | `{"error": "file not found"}` — not in any committed session |
| **500** | `{"error": "could not generate download URL"}` — IAM signing failed |

---

### `POST /api/signed-url/upload`

Requires: `VerifyFirebaseJWT`, `ACL`, `RequireDeviceAccess("upload")`.

`mac_id` is read from the JSON body (not query string) — the device access middleware peeks at the body and restores it for the handler.

Generates a new `session_id` (format: `YYYYMMDD_HHMMSS_microseconds`) and signs a 15-minute PUT URL per file. `required_headers` must be forwarded verbatim in every PUT to GCS — they are part of the signed string and GCS will return `SignatureDoesNotMatch` (403) if absent. `X-Goog-Meta-Uploaded-By` carries the caller's UID; GCS stores it as object metadata `uploaded-by`, which `GET /api/files/metadata` returns.

**Request body:** `{"mac_id": "<alphanumeric>", "files": ["f1.txt", "f2.txt"]}`

| Status | Body |
|--------|------|
| **200** | `{"session_id": "YYYYMMDD_HHMMSS_microseconds", "urls": {"f1.txt": "<signed PUT URL>", ...}, "required_headers": {"X-Goog-Content-Length-Range": "0,52428800", "X-Goog-Meta-Uploaded-By": "<caller uid>"}}` |
| **400** | `{"error": "body must be {mac_id, files[]}"}` — JSON binding failed |
| **400** | `{"error": "files array must not be empty"}` |
| **400** | `{"error": "mac_id must be a non-empty alphanumeric string"}` — invalid mac_id |
| **401 / 403** | See Auth Middleware |
| **500** | `{"error": "could not generate upload URL"}` — IAM signing failed |

---

### `POST /api/commit?mac_id=X&session_id=Y`

Requires: `VerifyFirebaseJWT`, `ACL`, `RequireDeviceAccess("upload")`.

Writes `{mac_id}/{session_id}/_COMMIT.json` (`{"session_id", "timestamp", "files", "uploaded_by"}`) to GCS, making the session visible to `ListFiles`.

**Request body:** JSON array of uploaded filenames — `["f1.txt", "f2.txt"]`

| Status | Body |
|--------|------|
| **200** | `{"committed": "<mac_id>/<session_id>/_COMMIT.json"}` |
| **400** | `{"error": "mac_id and session_id required"}` — missing or invalid mac_id / missing session_id |
| **400** | `{"error": "session_id format invalid"}` — must match `YYYYMMDD_HHMMSS_microseconds` |
| **400** | `{"error": "body must be a JSON array of filenames"}` — body binding failed |
| **401 / 403** | See Auth Middleware |
| **500** | `{"error": "GCS write failed"}` or `{"error": "GCS finalize failed"}` |

---

### `POST /api/parse/device-log?mac_id=X`

Requires: `VerifyFirebaseJWT`, `ACL`, `RequireDeviceAccess("read")`.

**Request body:** Raw hex log bytes (`Content-Type: application/octet-stream`)

| Status | Body |
|--------|------|
| **200** | `[{"key": "<timestamp ms>", "val": "<description>"}, ...]` — empty array if log is empty |
| **400** | `{"error": "mac_id query param required"}` |
| **401 / 403** | See Auth Middleware |

---

### `POST /api/parse/test-log?mac_id=X`

Requires: `VerifyFirebaseJWT`, `ACL`, `RequireDeviceAccess("read")`.

**Request body:** Raw test log bytes (`Content-Type: application/octet-stream`)

| Status | Body |
|--------|------|
| **200** | Full `PlotSpec` JSON — `{"success": bool, "title": "...", "x_axis_label": "...", "y_axis_label": "...", "x_values": [...], "series": [...]}`. `success: false` means parsing succeeded but no optical values were found. |
| **400** | `{"error": "mac_id query param required"}` |
| **401 / 403** | See Auth Middleware |

---

## Control Plane Endpoints

All control plane routes require `VerifyFirebaseJWT`. Org-scoped routes additionally require `RequireOrgAdmin`.

These endpoints are consumed exclusively by the admin GUI (`admin_gui/`). See [`admin_gui/README.md`](../admin_gui/README.md) for the client-side response handling.

---

### `GET /api/me/access`

Requires: `VerifyFirebaseJWT`. Used by the admin GUI on login to gate entry.

| Status | Body |
|--------|------|
| **200** | `{"admin": true}` |
| **403** | `{"error": "no admin access", "code": "no_admin_access"}` — user has no `role='admin'` row in any active org |
| **401** | See Auth Middleware |
| **500** | `{"error": "access check failed"}` |

---

### `GET /api/orgs`

Requires: `VerifyFirebaseJWT`. Returns all orgs the caller admins (their direct admin orgs plus every descendant).

| Status | Body |
|--------|------|
| **200** | `[{"id": int, "name": "...", "parent_org_id": int|null}, ...]` — empty array if none |
| **401** | See Auth Middleware |
| **500** | `{"error": "query failed"}` or `{"error": "scan failed"}` or `{"error": "rows error"}` |

---

### `POST /api/orgs`

Requires: `VerifyFirebaseJWT`. Caller must be admin of the specified `parent_org_id`.

**Request body:** `{"name": "<org name>", "parent_org_id": <int>}`

| Status | Body |
|--------|------|
| **201** | `{"id": <new_id>, "name": "...", "parent_org_id": <int>}` |
| **400** | `{"error": "..."}` — JSON binding failed or name empty/invalid after sanitisation |
| **401** | See Auth Middleware |
| **403** | `{"error": "forbidden: not an admin of the specified parent org"}` |
| **409** | `{"error": "an org with that name already exists under this parent"}` |
| **500** | `{"error": "insert failed"}` or `{"error": "auth check failed"}` |

---

### `DELETE /api/orgs/:org_id`

Requires: `VerifyFirebaseJWT`, `RequireOrgAdmin`. Soft-deletes the org and its entire subtree in one transaction (revokes memberships, soft-deletes devices, soft-deletes orgs). The root org cannot be deleted.

| Status | Body |
|--------|------|
| **200** | `{"ok": true}` |
| **200** | `{"ok": true, "already_deleted": true}` — idempotent; org was already gone |
| **400** | `{"error": "org_id must be an integer"}` |
| **401 / 403** | See Auth Middleware |
| **403** | `{"error": "forbidden: cannot delete the root org"}` |
| **500** | `{"error": "root org check failed"}` — DB error when resolving which org is root |
| **500** | `{"error": "..."}` — tx begin, membership revoke, device delete, org delete, or commit failed |

---

### `GET /api/orgs/:org_id/users`

Requires: `VerifyFirebaseJWT`, `RequireOrgAdmin`.

| Status | Body |
|--------|------|
| **200** | `[{"user_id": "...", "email": "...", "role": "...", "can_read": bool, "can_upload": bool, "granted_at": "...", "added_by": "..."}, ...]` |
| **400** | `{"error": "org_id must be an integer"}` |
| **401 / 403** | See Auth Middleware |
| **500** | `{"error": "query failed"}` or `{"error": "scan failed"}` or `{"error": "rows error"}` |

---

### `POST /api/orgs/:org_id/users`

Requires: `VerifyFirebaseJWT`, `RequireOrgAdmin`. Rate-limited at **10 requests per minute per IP** (burst 10 — up to 10 instantaneous requests before the bucket is exhausted, then 1 token replenished per 6 seconds).

**Request body:** `{"email": "...", "role": "admin"|"user", "can_read": bool, "can_upload": bool}`

Note: `role="admin"` forces `can_read=true` and `can_upload=true` regardless of input.

If the Firebase account doesn't exist, it is created with a random throwaway password and a password-reset invite email is sent.

| Status | Body |
|--------|------|
| **200** | `{"ok": true, "user_id": "...", "changed": true, "invited": bool}` — `invited=true` when a new Firebase account was created. May include `"email_warning"` if invite email send failed. |
| **200** | `{"ok": true, "user_id": "...", "changed": false}` — permissions unchanged, no-op |
| **400** | `{"error": "..."}` — JSON binding failed or `role` not `"admin"`/`"user"` |
| **401 / 403** | See Auth Middleware |
| **403** | `{"error": "cannot change your own permissions"}` |
| **409** | `{"error": "this email already exists in the system; please contact Ricovr admin before re-inviting"}` — Firebase/DB state mismatch |
| **429** | `{"error": "rate limit exceeded — try again later"}` — per-IP invite rate limit exceeded |
| **500** | `{"error": "..."}` — Firebase lookup, DB upsert, or tx failure |

---

### `DELETE /api/orgs/:org_id/users/:user_id`

Requires: `VerifyFirebaseJWT`, `RequireOrgAdmin`.

Soft-revokes the user's membership in the org (sets `revoked_at`). Idempotent.

| Status | Body |
|--------|------|
| **200** | `{"ok": true}` |
| **200** | `{"ok": true, "already_revoked": true}` — membership was already revoked |
| **400** | `{"error": "org_id must be an integer"}` |
| **401 / 403** | See Auth Middleware |
| **403** | `{"error": "cannot revoke your own membership"}` |
| **500** | `{"error": "revoke failed"}` |

---

### `GET /api/orgs/:org_id/devices`

Requires: `VerifyFirebaseJWT`, `RequireOrgAdmin`.

| Status | Body |
|--------|------|
| **200** | `[{"id": "...", "org_id": int, "added_at": "...", "added_by": "..."}, ...]` |
| **400** | `{"error": "org_id must be an integer"}` |
| **401 / 403** | See Auth Middleware |
| **500** | `{"error": "query failed"}` or `{"error": "scan failed"}` or `{"error": "rows error"}` |

---

### `POST /api/orgs/:org_id/devices`

Requires: `VerifyFirebaseJWT`, `RequireOrgAdmin`.

**Request body:** `{"mac_id": "<alphanumeric device ID>"}`

Device ID is uppercased and stored normalised. Accepts any non-empty alphanumeric string.

| Status | Body |
|--------|------|
| **201** | `{"ok": true, "id": "<normalised mac_id>", "org_id": int}` |
| **400** | `{"error": "..."}` — JSON binding failed |
| **400** | `{"error": "mac_id must be a non-empty alphanumeric string"}` |
| **401 / 403** | See Auth Middleware |
| **409** | `{"error": "device already registered"}` — unique constraint violation |
| **500** | `{"error": "device insert failed"}` |

---

### `DELETE /api/orgs/:org_id/devices/:mac_id`

Requires: `VerifyFirebaseJWT`, `RequireOrgAdmin`.

Soft-deletes the device. Idempotent.

| Status | Body |
|--------|------|
| **200** | `{"ok": true}` |
| **200** | `{"ok": true, "already_deleted": true}` — device exists but already soft-deleted |
| **400** | `{"error": "org_id must be an integer"}` |
| **400** | `{"error": "mac_id must be a non-empty alphanumeric string"}` |
| **401 / 403** | See Auth Middleware |
| **404** | `{"error": "device not found"}` — never registered to this org |
| **500** | `{"error": "lookup failed"}` or `{"error": "delete failed"}` |

---

### `DELETE /api/users/:user_id`

Requires: `VerifyFirebaseJWT`, `RequireRootOrgAdmin`.

Soft-deletes the user and revokes all active org memberships in one transaction.

| Status | Body |
|--------|------|
| **200** | `{"ok": true}` |
| **200** | `{"ok": true, "already_deleted": true}` — user already deleted |
| **401 / 403** | See Auth Middleware |
| **500** | `{"error": "..."}` — tx or DB operation failed |

---

## mac_id Normalisation

All endpoints that accept a `mac_id` pass it through `normaliseMACID` (control plane) or `normaliseMACForPath` (data plane). Both:

1. Trim whitespace
2. Validate against `^[0-9a-zA-Z]+$` — non-empty alphanumeric only
3. Uppercase the result

The normalised ID is what is stored in the DB and used as the GCS path prefix.

---

## Related Files

- [`routes/data_plane.go`](routes/data_plane.go) — data plane route registration
- [`routes/control_plane.go`](routes/control_plane.go) — control plane route registration
- [`middleware/`](middleware/) — auth, ACL, device access, org admin, rate limiting
- [`handlers/`](handlers/) — handler implementations
- [`internal/config/config.go`](internal/config/config.go) — `SERVER_MODE` validation and env loading
- [`admin_gui/README.md`](../admin_gui/README.md) — admin GUI client; documents how each control-plane response is handled
