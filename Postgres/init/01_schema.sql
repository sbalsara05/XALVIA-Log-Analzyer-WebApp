-- ricovr_db schema — auto-applied on first container start

-- ── users ────────────────────────────────────────────────────────────────────
-- Identity anchor for referential integrity. Role/admin status lives in org_users.
-- added_by / removed_by are self-referential: the bootstrap admin row sets
-- added_by = own id (seeded before any other row exists).
-- removed_by is NULL iff deleted_at IS NULL (enforced by CHECK constraint).
CREATE TABLE IF NOT EXISTS users (
    id         TEXT PRIMARY KEY,
    email      TEXT NOT NULL,
    added_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    added_by   TEXT        NOT NULL REFERENCES users(id),
    deleted_at TIMESTAMPTZ,
    removed_by TEXT        REFERENCES users(id),
    CHECK ((deleted_at IS NULL) = (removed_by IS NULL))
);

-- Case-insensitive unique email (active users only)
CREATE UNIQUE INDEX IF NOT EXISTS users_email_lower_uidx
    ON users (lower(email))
    WHERE deleted_at IS NULL;

-- ── orgs ─────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS orgs (
    id             SERIAL PRIMARY KEY,
    name           VARCHAR     NOT NULL,
    parent_org_id  INTEGER     REFERENCES orgs(id),
    added_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    added_by       TEXT        NOT NULL REFERENCES users(id),
    deleted_at     TIMESTAMPTZ,
    removed_by     TEXT        REFERENCES users(id),
    CHECK ((deleted_at IS NULL) = (removed_by IS NULL))
);

-- Case-insensitive uniqueness for root org names
CREATE UNIQUE INDEX IF NOT EXISTS orgs_root_name_lower_uidx
    ON orgs (lower(name))
    WHERE parent_org_id IS NULL AND deleted_at IS NULL;

-- Case-insensitive uniqueness for child org names within the same parent
CREATE UNIQUE INDEX IF NOT EXISTS orgs_child_name_lower_uidx
    ON orgs (lower(name), parent_org_id)
    WHERE parent_org_id IS NOT NULL AND deleted_at IS NULL;

-- ── org_users ─────────────────────────────────────────────────────────────────
-- Temporal table: every permission change revokes the current row and inserts a
-- new one, preserving full history. granted_at uses clock_timestamp() (not now())
-- so concurrent grants in the same transaction still produce distinct PKs.
-- removed_by is NULL iff revoked_at IS NULL (enforced by CHECK constraint).
CREATE TABLE IF NOT EXISTS org_users (
    user_id    TEXT        NOT NULL REFERENCES users(id),
    org_id     INTEGER     NOT NULL REFERENCES orgs(id),
    role       TEXT        NOT NULL CHECK (role IN ('admin', 'user')),
    can_read   BOOLEAN     NOT NULL DEFAULT false,
    can_upload BOOLEAN     NOT NULL DEFAULT false,
    granted_at TIMESTAMPTZ NOT NULL,
    added_by   TEXT        NOT NULL REFERENCES users(id),
    revoked_at TIMESTAMPTZ,
    removed_by TEXT        REFERENCES users(id),
    PRIMARY KEY (user_id, org_id, granted_at),
    CHECK ((revoked_at IS NULL) = (removed_by IS NULL))
);

-- Only one active (non-revoked) row per (user_id, org_id) is allowed.
CREATE UNIQUE INDEX IF NOT EXISTS org_users_active_uidx
    ON org_users (user_id, org_id)
    WHERE revoked_at IS NULL;

-- ── devices ───────────────────────────────────────────────────────────────────
-- Temporal table: each registration of a MAC is a separate row.
-- A MAC can be soft-deleted and later re-registered as a new row.
-- The partial unique index enforces that at most one active row exists per MAC
-- globally — a MAC cannot be active in two orgs simultaneously, and any attempt
-- to INSERT while an active row already exists will fail at the DB level.
-- removed_by is NULL iff deleted_at IS NULL (enforced by CHECK constraint).
CREATE TABLE IF NOT EXISTS devices (
    id         TEXT        NOT NULL,
    org_id     INTEGER     NOT NULL REFERENCES orgs(id),
    added_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    added_by   TEXT        NOT NULL REFERENCES users(id),
    deleted_at TIMESTAMPTZ,
    removed_by TEXT        REFERENCES users(id),
    PRIMARY KEY (id, added_at),
    CHECK ((deleted_at IS NULL) = (removed_by IS NULL))
);

-- At most one active (non-deleted) row per MAC address globally.
CREATE UNIQUE INDEX IF NOT EXISTS devices_active_mac_uidx
    ON devices (id)
    WHERE deleted_at IS NULL;
