-- Backfill organization_id on remote_session_clients from each client's owning
-- project, covering rows created before the AIS-222 dual-write deployed.
-- Idempotent: only rows still NULL are touched, so re-running is a no-op. Every
-- client is project-scoped today (organization-level clients arrive in AIS-221),
-- so the owning project's organization is unambiguous. No deleted filter: the
-- eventual NOT NULL contract must hold for soft-deleted rows too.
UPDATE remote_session_clients AS c
SET organization_id = p.organization_id
FROM projects AS p
WHERE c.project_id = p.id
  AND c.organization_id IS NULL;
