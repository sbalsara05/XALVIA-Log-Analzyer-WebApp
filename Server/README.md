# Go Gateway — Architecture, Middleware & Route Reference

The Go gateway is a dual-mode HTTP server built with [Gin](https://github.com/gin-gonic/gin). Its operating mode is selected at startup via the `SERVER_MODE` environment variable.

| Mode | Purpose | DB connection |
|---|---|---|
| `CONTROL_PLANE` | Org and user administration (admin GUI backend) | Yes — Postgres |
| `DATA_PLANE` | Device file upload, download, and log parsing | No — GCS only |

---

## Directory Layout

```
go_gateway/
├── internal/
│   ├── config/config.go       — env var loading and validation
│   └── db/db.go               — Postgres connection pool (lib/pq)
├── middleware/
│   ├── auth.go                — VerifyFirebaseJWT
│   ├── org_admin.go           — RequireOrgAdmin (recursive CTE)
│   ├── super_admin.go         — RequireSuperAdmin
│   ├── device_access.go       — RequireDeviceAccess (recursive CTE)
│   ├── rate_limit.go          — RateLimit (per-IP token bucket)
│   └── acl.go                 — legacy CSV ACL checks (paused / pass-through)
├── handlers/
│   ├── env.go                 — shared Env struct injected into all handlers
│   ├── orgs.go                — org and user CRUD (control plane)
│   ├── files.go               — signed URL generation, file listing
│   ├── parse.go               — device log and test log parsing
│   ├── profile.go             — caller profile
│   ├── health.go              — liveness probe
│   └── admin.go               — UpdatePermissions (stub)
├── routes/
│   ├── control_plane.go       — registers control plane routes
│   └── data_plane.go          — registers data plane routes
├── config/acl.go              — CSV ACL store loader
├── models/user.go             — ACLStore, User, DeviceEntry types
├── main.go                    — entry point and mode switchboard
├── docker-compose.yml         — local gateway + Postgres
└── go.mod
```

---

## Environment Variables

| Variable | Required | Default | Notes |
|---|---|---|---|
| `SERVER_MODE` | Always | — | `DATA_PLANE` or `CONTROL_PLANE` |
| `PORT` | No | `8080` | Listen port |
| `FIREBASE_PROJECT_ID` | Always | — | e.g. `ricovr-device-cloud` |
| `SERVICE_ACCOUNT_EMAIL` | Always | — | Used for IAM URL signing |
| `GCS_BUCKET` | Always | — | GCS bucket name |
| `GOOGLE_APPLICATION_CREDENTIALS` | Local dev only | — | Path to service account key JSON; consumed by ADC, not app code |
| `DATABASE_URL` | `CONTROL_PLANE` only | — | Postgres DSN e.g. `postgres://user:pass@host/db?sslmode=require` |
| `DB_MAX_OPEN_CONNS` | No | `5` | Max open Postgres connections per instance |
| `DB_MAX_IDLE_CONNS` | No | `2` | Max idle Postgres connections per instance |

---

## Middleware Reference

Middleware is applied per-route at registration time in `routes/`. The chain runs left-to-right; any middleware that calls `c.Abort*` short-circuits the rest.

### `VerifyFirebaseJWT` — `middleware/auth.go`

```
Request → [VerifyFirebaseJWT] → handler
```

Validates the `Authorization: Bearer <token>` header against the Firebase Admin SDK. On success, injects the verified Firebase UID into the Gin context under the key `"uid"`. Aborts with **401** on any failure (missing header, expired token, invalid signature).

All authenticated routes use this as the first middleware in the chain.

**Context key injected:** `"uid"` (`string`)

---

### `RequireSuperAdmin` — `middleware/super_admin.go`

```
[VerifyFirebaseJWT] → [RequireSuperAdmin] → handler
```

Reads the `uid` from context and queries:

```sql
SELECT is_super_admin FROM users WHERE id = $uid AND deleted_at IS NULL
```

Aborts with **403** if:
- The user row does not exist in Postgres (has never been granted any permission)
- `is_super_admin = false`
- The user is soft-deleted (`deleted_at IS NOT NULL`)

Used on: `POST /api/orgs`, `DELETE /api/orgs/:org_id`, `DELETE /api/users/:user_id`

---

### `RequireOrgAdmin` — `middleware/org_admin.go`

```
[VerifyFirebaseJWT] → [RequireOrgAdmin] → handler
```

Reads `uid` from context and `:org_id` from the URL. Executes a **recursive upward CTE** that walks from the target org to the root, then checks whether the caller holds `role = 'admin'` anywhere in that ancestor chain:

```sql
WITH RECURSIVE org_tree AS (
    SELECT id, parent_org_id FROM orgs WHERE id = $org_id AND deleted_at IS NULL
    UNION ALL
    SELECT o.id, o.parent_org_id FROM orgs o
    INNER JOIN org_tree ot ON o.id = ot.parent_org_id
    WHERE o.deleted_at IS NULL
)
SELECT EXISTS (
    SELECT 1 FROM org_tree ot
    JOIN org_users ou ON ot.id = ou.org_id
    WHERE ou.user_id = $uid
      AND ou.role = 'admin'
      AND ou.revoked_at IS NULL
);
```

This means an admin of a parent org automatically has admin rights in all descendant orgs — no explicit membership row in the child org is required.

Aborts with **403** if the caller has no admin role anywhere in the org's ancestor chain, or if the org is soft-deleted.

`org_id` is parsed as `int` (matches `orgs.id INTEGER SERIAL`).

Used on: `POST /api/orgs/:org_id/suborgs`, `GET /api/orgs/:org_id/users`, `POST /api/orgs/:org_id/users`, `DELETE /api/orgs/:org_id/users/:user_id`

---

### `RequireDeviceAccess` — `middleware/device_access.go`

```
[VerifyFirebaseJWT] → [ACL] → [RequireDeviceAccess("read"|"upload")] → handler
```

Takes a `requiredAction` parameter (`"read"` or `"upload"`) at construction time. Reads `uid` from context and `mac_id` from the query string. Executes a **recursive upward CTE** that walks from the device's owning org to the root, then checks for a matching permission on the calling user:

```sql
WITH RECURSIVE org_tree AS (
    SELECT id, parent_org_id FROM orgs
    WHERE id = (SELECT org_id FROM devices WHERE id = $mac_id AND deleted_at IS NULL)
      AND deleted_at IS NULL
    UNION ALL
    SELECT o.id, o.parent_org_id FROM orgs o
    INNER JOIN org_tree ot ON o.id = ot.parent_org_id
    WHERE o.deleted_at IS NULL
)
SELECT EXISTS (
    SELECT 1 FROM org_tree ot
    JOIN org_users ou ON ot.id = ou.org_id
    WHERE ou.user_id = $uid
      AND ou.revoked_at IS NULL
      AND (ou.role = 'admin' OR ou.can_read = true)   -- or can_upload for "upload" action
);
```

The `permissionCol` (`ou.can_read` vs `ou.can_upload`) is selected at **construction time** and embedded as a string literal in the SQL — no user input is ever interpolated into the query.

Soft-delete filters on `devices`, `orgs`, and `org_users` are all applied. A soft-deleted device, org, or revoked membership causes access to be denied.

Aborts with **403** if:
- `mac_id` is missing from the query string
- The device does not exist or is soft-deleted
- The user has no matching permission path up the org tree

The SQL query string is built **once at construction** (not per-request), so there is no per-request string allocation for the query.

---

### `RateLimit` — `middleware/rate_limit.go`

```
[VerifyFirebaseJWT] → [RateLimit(rps, burst)] → [RequireOrgAdmin] → handler
```

Per-IP token bucket limiter using `golang.org/x/time/rate`. Each unique client IP gets its own `rate.Limiter`. A cleanup goroutine runs every 5 minutes and removes entries idle for more than 10 minutes to prevent unbounded memory growth.

Current configuration on `POST /api/orgs/:org_id/users`: **10 requests per minute** (`rps = 10.0/60`, `burst = 10`).

Note: this is in-process per-instance. On multi-instance Cloud Run deployments, each instance has an independent counter. Replace with Redis-backed limiting if horizontal scaling is needed.

---

### `ACL` / `RequireRead` / `RequireWrite` / `RequireAdmin` — `middleware/acl.go`

Legacy CSV-based ACL checks. Currently **paused** (pass-through mode). `ACL` still injects a permissive `User` struct into context so downstream handlers that call `c.MustGet("user")` don't panic. The actual permission checks in `RequireRead`, `RequireWrite`, and `RequireAdmin` are no-ops.

These will be replaced with DB-backed checks once the control-plane permission model is fully stable.

---

## Middleware Chain Diagram

```
All routes
  └── VerifyFirebaseJWT (401 on bad token)

Control Plane routes (/api/*)
  ├── POST   /api/orgs                        JWT → SuperAdmin → CreateOrg
  ├── GET    /api/orgs                        JWT → ListOrgs
  ├── DELETE /api/orgs/:org_id                JWT → SuperAdmin → DeleteOrg
  ├── POST   /api/orgs/:org_id/suborgs        JWT → OrgAdmin → CreateSubOrg
  ├── GET    /api/orgs/:org_id/users          JWT → OrgAdmin → ListOrgUsers
  ├── POST   /api/orgs/:org_id/users          JWT → RateLimit → OrgAdmin → UpsertOrgUser
  ├── DELETE /api/orgs/:org_id/users/:user_id JWT → OrgAdmin → RevokeOrgUser
  └── DELETE /api/users/:user_id              JWT → SuperAdmin → DeleteUser

Data Plane routes
  ├── GET  /health                            (no auth)
  ├── GET  /api/profile                       JWT → ACL → Profile
  ├── PUT  /api/users/:id/permissions         JWT → ACL → RequireAdmin → UpdatePermissions
  ├── GET  /api/files                         JWT → ACL → DeviceAccess("read") → ListFiles
  ├── GET  /api/signed-url/read               JWT → ACL → DeviceAccess("read") → SignedReadURL
  ├── POST /api/parse/device-log              JWT → ACL → DeviceAccess("read") → ParseDeviceLog
  ├── POST /api/parse/test-log                JWT → ACL → DeviceAccess("read") → ParseTestLog
  ├── POST /api/signed-url/upload             JWT → ACL → DeviceAccess("upload") → SignedUploadURL
  └── POST /api/commit                        JWT → ACL → DeviceAccess("upload") → CommitSession
```

---

## Handler Reference — Control Plane

### `ListOrgs` — `GET /api/orgs`

Returns all orgs the calling user can admin, including the full descendant subtree below any org where they hold `role = 'admin'`. Uses a recursive **downward** CTE (opposite direction from `RequireOrgAdmin`) seeded from the caller's direct admin memberships.

Only active orgs (`deleted_at IS NULL`) and active memberships (`revoked_at IS NULL`) are included. The admin GUI uses this to populate the org picker — a caller can only see and assign permissions in orgs they actually manage.

### `UpsertOrgUser` — `POST /api/orgs/:org_id/users`

Accepts `{ email, role, can_read, can_upload }`. Flow:

1. Calls `authClient.GetUserByEmail(ctx, email)` via Firebase Admin SDK
2. **404** if no Firebase account exists for that email
3. Upserts the `users` row: `INSERT ... ON CONFLICT (id) DO UPDATE SET email = ..., deleted_at = NULL`
4. Upserts the `org_users` row: sets `granted_at = now()`, clears `revoked_at = NULL` (re-grant after revocation works correctly)

The target user does **not** need to have signed in previously. The control plane owns all `users` row creation.

### `RevokeOrgUser` — `DELETE /api/orgs/:org_id/users/:user_id`

Sets `org_users.revoked_at = now()` where `revoked_at IS NULL`. Returns **404** if no active membership is found. Does not delete the row — the grant/revoke history is preserved.

### `DeleteOrg` — `DELETE /api/orgs/:org_id`

Sets `orgs.deleted_at = now()`. Super admin only. Does not cascade — child orgs and memberships are unaffected and must be acted on separately.

### `DeleteUser` — `DELETE /api/users/:user_id`

Sets `users.deleted_at = now()`. Super admin only. Does not touch `org_users` rows — memberships remain and must be revoked separately via `RevokeOrgUser`.

---

## Handler Reference — Data Plane

### `ListFiles` — `GET /api/files?mac_id=`

Lists deduplicated filenames available for a device. Resolves both legacy flat paths and committed session paths in GCS. Latest session wins on filename collision.

### `SignedReadURL` — `GET /api/signed-url/read?mac_id=&filename=`

Returns a 15-minute GCS signed GET URL for a specific file. Session is resolved server-side.

### `SignedUploadURL` — `POST /api/signed-url/upload`

Generates a new session ID and signed PUT URLs for each requested filename. Enforces a 50 MB file size cap via `X-Goog-Content-Length-Range` header. URLs expire in 15 minutes.

### `CommitSession` — `POST /api/commit`

Writes `{mac_id}/{session_id}/_COMMIT.json` to GCS to seal an upload session. The Python client calls this after all files are uploaded.

---

## Postgres Integration

Postgres is accessed **only by the control plane**. `internal/db/db.go` opens a `lib/pq` connection pool on startup with configurable open/idle connection caps. The data plane never calls `db.Connect()`.

The pool is passed directly into middleware constructors and handler functions — there is no global state. See [Postgres/README.md](../Postgres/README.md) for the full schema reference and soft-delete lifecycle documentation.

### Why lib/pq instead of pgx

`lib/pq` is a pure Go driver (`CGO_ENABLED=0` compatible). This is required for the alpine multi-stage Docker build — `pgx` with its C extensions would require a non-alpine base or a CGO-enabled build stage.

---

## Local Development

```bash
# Start the Postgres container first
cd ../Postgres && docker compose up -d

# Copy and fill in the env file
cp .env.example .env   # set DATABASE_URL, FIREBASE_PROJECT_ID, etc.

# Run in control plane mode
SERVER_MODE=CONTROL_PLANE go run .

# Run in data plane mode
SERVER_MODE=DATA_PLANE go run .

# Build
go build ./...
```

---

## Testing

```
go_gateway/
├── handlers/
│   └── files_test.go           ← unit tests (fake GCS, no DB, no network)
└── tests/
    └── integration/
        └── control_plane_test.go  ← integration tests (real local Postgres)
```

**Unit tests** live co-located with the file they test. They use in-process fakes (fake GCS HTTP server) and have no external dependencies. Run anywhere, any time:

```bash
go test ./handlers/... -v
```

**Integration tests** live in `tests/integration/` and hit the real Docker Postgres container. The `DATABASE_URL` in `.env` uses `host.docker.internal` (for container→container). When running `go test` from your dev machine, point at `localhost` instead:

```bash
# from go_gateway/
DATABASE_URL="postgres://postgres:localpassword@localhost:5432/ricovr_db?sslmode=disable" \
  go test ricovr/gateway/tests/integration -v
```

The Go server in Docker uses the `DATABASE_URL` from `.env` (`host.docker.internal`) unchanged — no extra config needed there.

**Why `sslmode=disable`:** the local Docker Postgres has no TLS certificate configured. Both containers share a Docker network so there is no plaintext network hop worth encrypting. In production, Cloud Run connects to Cloud SQL via the Cloud SQL Auth Proxy, which handles TLS internally.

---

## Cloud Run Deployment

The gateway runs on Cloud Run using Application Default Credentials (ADC). No service account JSON key file is baked into the image. See [GO_CLOUD_RUN.md](GO_CLOUD_RUN.md) for the full ADC migration plan.

Required IAM roles on the Cloud Run service account:
- `roles/iam.serviceAccountTokenCreator` — to call `signBlob` for signed URLs
- `roles/storage.objectAdmin` — GCS read/write
- Firebase Auth is handled server-side via the Admin SDK; no extra IAM role required
