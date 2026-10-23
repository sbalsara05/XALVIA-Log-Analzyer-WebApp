# Postgres — Schema, Internals & Go Middleware Integration

The `Postgres/` directory contains the Docker Compose file that runs the local development database and serves as the reference for the live schema. The database is only accessed by the **control plane** mode of the Go gateway (`SERVER_MODE=CONTROL_PLANE`). The data plane and the Python client are stateless with respect to Postgres.

---

## Running Locally

```bash
# Start the container (persists data in a named Docker volume)
docker compose up -d

# Connect manually
docker exec -it ricovr_postgres psql -U postgres -d ricovr_db

# Stop (data is preserved)
docker compose down

# Stop and wipe all data
docker compose down -v
```

Container name: `ricovr_postgres`  
DSN for Go gateway: `postgres://postgres:localpassword@localhost:5432/ricovr_db?sslmode=disable`  
Set this as `DATABASE_URL` in the gateway's `.env`.

---

## Schema

### `orgs` — Organizations

```sql
CREATE TABLE orgs (
    id            INTEGER PRIMARY KEY DEFAULT nextval('orgs_id_seq'),
    name          VARCHAR NOT NULL,
    parent_org_id INTEGER REFERENCES orgs(id),  -- NULL = root org
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ                    -- NULL = active
);
```

Orgs form a **tree** via the self-referential `parent_org_id` foreign key. Root orgs have `parent_org_id = NULL`. The tree can be arbitrarily deep. Soft-deletion (`deleted_at IS NOT NULL`) hides an org from all queries without destroying the hierarchy record or audit trail.

Only a super admin can create root orgs or soft-delete any org. Org admins can create sub-orgs under orgs they manage.

---

### `users` — User Accounts

```sql
CREATE TABLE users (
    id             VARCHAR PRIMARY KEY,          -- Firebase UID
    email          VARCHAR NOT NULL,
    is_super_admin BOOLEAN NOT NULL DEFAULT false,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at     TIMESTAMPTZ                   -- NULL = active
);
```

`id` is the Firebase UID string (e.g. `"abc123XYZ"`). Rows are created **exclusively by the control plane** when an admin grants permissions via `POST /api/orgs/:org_id/users`. Sign-in does **not** insert rows here — the Python client never touches this table.

`deleted_at` deactivates an account. A deactivated user's Firebase token still passes JWT verification but `RequireSuperAdmin` will reject them (it filters `deleted_at IS NULL`). Their org memberships remain in `org_users` and must be revoked separately.

To bootstrap the first super admin, insert directly:
```sql
INSERT INTO users (id, email, is_super_admin)
VALUES ('<firebase_uid>', 'admin@example.com', true);
```

---

### `org_users` — Org Memberships & Permissions

```sql
CREATE TABLE org_users (
    user_id    VARCHAR NOT NULL REFERENCES users(id),
    org_id     INTEGER NOT NULL REFERENCES orgs(id),
    role       VARCHAR NOT NULL CHECK (role IN ('admin', 'user')),
    can_read   BOOLEAN NOT NULL DEFAULT false,
    can_upload BOOLEAN NOT NULL DEFAULT false,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,                      -- NULL = active
    PRIMARY KEY (user_id, org_id)
);
```

This is the **central permission table**. Every access control decision for device operations traces back here.

- A user can have different roles and flags in different orgs — permissions are scoped to the `(user_id, org_id)` pair, not the user globally.
- `role = 'admin'` grants full read + upload on the org and all descendant orgs (evaluated via recursive CTE in middleware).
- `can_read` / `can_upload` grant specific actions without admin privileges.
- `revoked_at IS NOT NULL` means the membership was revoked. The row is kept for audit; all active-membership queries filter `revoked_at IS NULL`.
- Re-granting a previously revoked membership resets `revoked_at = NULL` and refreshes `granted_at` via upsert.
- The `CHECK (role IN ('admin', 'user'))` constraint is enforced at the DB level — no invalid role string can land here even from direct DB access.

---

### `devices` — Device Registry

```sql
CREATE TABLE devices (
    id            VARCHAR PRIMARY KEY,           -- mac_id string
    org_id        INTEGER REFERENCES orgs(id),
    registered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ                    -- NULL = active
);
```

`id` is the device's MAC address string as used throughout the system (`mac_id`). Each device belongs to exactly one org. `RequireDeviceAccess` walks from this device's `org_id` up the org tree to determine if the requesting user has permission.

---

## Entity-Relationship Diagram

```
orgs (tree, self-referential)
  id ──────────────────────────────────────────┐
  parent_org_id → orgs.id (NULL = root)        │
                                               │
users                        org_users         │      devices
  id (Firebase UID) ──── user_id               │        id (mac_id)
                         org_id ───────────────┘        org_id → orgs.id
                         role
                         can_read
                         can_upload
                         granted_at
                         revoked_at
```

---

## Soft-Delete Lifecycle

Every table uses soft-deletion instead of `DELETE`. This preserves the full audit trail of who had access to what and when.

| Table | Active filter | Deactivation column | Set by |
|---|---|---|---|
| `orgs` | `deleted_at IS NULL` | `deleted_at` | `DELETE /api/orgs/:org_id` (super admin) |
| `users` | `deleted_at IS NULL` | `deleted_at` | `DELETE /api/users/:user_id` (super admin) |
| `org_users` | `revoked_at IS NULL` | `revoked_at` | `DELETE /api/orgs/:org_id/users/:user_id` (org admin) |
| `devices` | `deleted_at IS NULL` | `deleted_at` | Direct DB / future endpoint |

Cascades are intentionally **not** automatic. Deleting an org does not auto-revoke memberships or delete child orgs — the operator must act on each explicitly. This prevents accidental mass-revocation from a single mistaken delete.

---

## Org Tree & Permission Inheritance

The `orgs` table models a tree. Permissions are **inherited upward**: a user who holds `role = 'admin'` in a parent org automatically has admin access to all its descendants, evaluated at query time via a recursive CTE in the Go middleware.

```
Root Org (id=1)
  ├── Division A (id=2)    ← user is admin here
  │     ├── Team A1 (id=4) ← user auto-inherits admin access
  │     └── Team A2 (id=5) ← user auto-inherits admin access
  └── Division B (id=3)    ← user has no role here
        └── Team B1 (id=6) ← user has no access
```

The recursive CTEs in `RequireOrgAdmin` and `RequireDeviceAccess` walk this tree at request time. There is no denormalized "effective permissions" cache — the DB is the single source of truth.

---

## Migration History

```sql
-- 1. Initial schema (can_read / can_upload added early)
CREATE TABLE orgs (id, name, parent_org_id);
CREATE TABLE users (id, email, is_super_admin);
CREATE TABLE org_users (user_id, org_id, role, can_read, can_upload);
CREATE TABLE devices (id, org_id);

-- 2. Lifecycle columns migration
ALTER TABLE orgs      ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE orgs      ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE users     ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE users     ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE org_users ADD COLUMN granted_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE org_users ADD COLUMN revoked_at TIMESTAMPTZ;
ALTER TABLE org_users ADD CONSTRAINT org_users_role_check CHECK (role IN ('admin', 'user'));
ALTER TABLE devices   ADD COLUMN registered_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE devices   ADD COLUMN deleted_at TIMESTAMPTZ;
```

---

## How the Go Middleware Uses This Schema

The Go gateway (`Server/go_gateway/`) is the **only process that reads or writes to Postgres**. All permission checks happen in middleware before any handler runs. See [Server/README.md](../Server/README.md) for the full middleware reference.

### Quick reference: which middleware queries which table

| Middleware | Tables queried | Key filter |
|---|---|---|
| `RequireSuperAdmin` | `users` | `id = $uid AND deleted_at IS NULL AND is_super_admin = true` |
| `RequireOrgAdmin` | `orgs`, `org_users` | Recursive CTE up to root; `deleted_at IS NULL` on orgs, `revoked_at IS NULL` on org_users |
| `RequireDeviceAccess` | `devices`, `orgs`, `org_users` | Recursive CTE from device's org up to root; all three soft-delete filters applied |

### Key invariant

**No code path outside the Go control plane writes to Postgres.** In particular:
- Firebase sign-in does not insert a `users` row
- The Python desktop client (`GatewayClient`) never connects to Postgres
- The data plane (`SERVER_MODE=DATA_PLANE`) never opens a DB connection
