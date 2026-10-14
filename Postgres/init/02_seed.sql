-- Seed data — applied once on first container start.
--
-- 1. Bootstrap admin user. added_by = own id (self-referential) because there
--    is no higher authority at system initialisation time.
--    Replace <YOUR_FIREBASE_UID> with the actual Firebase UID before running.
--
-- 2. Ricovr root org (id=1). All customer orgs are descendants of this node.
--    There is no API to create a second root; this row is the single tree root.
--
-- 3. Root org_users row granting the bootstrap admin full access.
--    added_by = own id for the same bootstrap reason as the users row.
--
-- Order matters: users must exist before orgs (orgs.added_by → users.id),
-- and both must exist before org_users.

INSERT INTO users (id, email, added_by)
VALUES ('Uuf5k0JLe0dJBQzofTm0aHHpUzd2', 'lakshay.mohan94@gmail.com', 'Uuf5k0JLe0dJBQzofTm0aHHpUzd2')
ON CONFLICT (id) DO NOTHING;

INSERT INTO orgs (id, name, parent_org_id, added_by)
VALUES (1, 'Ricovr', NULL, 'Uuf5k0JLe0dJBQzofTm0aHHpUzd2')
ON CONFLICT (id) DO NOTHING;

-- Keep the SERIAL sequence in sync so the next auto-generated id starts at 2.
SELECT setval('orgs_id_seq', 1, true);

INSERT INTO org_users (user_id, org_id, role, can_read, can_upload, granted_at, added_by)
VALUES ('Uuf5k0JLe0dJBQzofTm0aHHpUzd2', 1, 'admin', true, true, now(), 'Uuf5k0JLe0dJBQzofTm0aHHpUzd2')
ON CONFLICT DO NOTHING;
