-- Backfill legacy remote_session_clients join bindings (AIS-134)
-- https://linear.app/speakeasy/issue/AIS-134/backfill-all-legacy-join-bindings-one-off-sql-script
--
-- Deterministically backfills `remote_session_client_user_session_issuers`
-- join rows for every active `remote_session_clients` row that still relies
-- only on the legacy `user_session_issuer_id` column. This is the parity gate
-- for removing the legacy read fallback in AIS-135.
--
-- The runtime opportunistically backfills on read (see
-- server/internal/remotesessions/client_bindings.go); this script guarantees
-- completeness up front so the fallback can be deleted safely. The script is
-- safe to run multiple times, although this will likely be manually ran once
-- per environment in dev and prod.
--
-- The final SELECT must report remaining_legacy_only_clients = 0. After a
-- successful run, the "using legacy remote session client user_session_issuer
-- binding" warn log should drop to zero in that environment.

\set ON_ERROR_STOP on

BEGIN;

-- Pre-flight drift guard. Basis: CountLegacyRemoteSessionClientsForUserSessionIssuer.
-- user_session_issuer_id is project-scoped, so grouping by it alone matches the
-- global `one_per_issuer` unique index. Aborts the transaction (before any
-- insert) if any active issuer maps to more than one active client.
DO $$
DECLARE
  drift_issuers text;
  drift_count int;
BEGIN
  SELECT string_agg(issuer.user_session_issuer_id::text || ' (' || issuer.legacy_client_count::text || ')', ', '),
         count(*)
  INTO drift_issuers, drift_count
  FROM (
    SELECT c.user_session_issuer_id, count(*) AS legacy_client_count
    FROM remote_session_clients AS c
    JOIN user_session_issuers AS usi ON usi.id = c.user_session_issuer_id
    WHERE c.deleted IS FALSE
      AND usi.deleted IS FALSE
      AND c.project_id = usi.project_id
    GROUP BY c.user_session_issuer_id
    HAVING count(*) > 1
  ) AS issuer;

  IF drift_count > 0 THEN
    RAISE EXCEPTION
      'remote session client binding drift: % user_session_issuer(s) map to multiple active clients: %',
      drift_count, drift_issuers;
  END IF;
END $$;

-- Backfill join rows from the legacy column. Mirrors the runtime legacy
-- fallback population (ListRemoteSessionClientsForUserSessionIssuerLegacy):
-- client, remote session issuer, and user session issuer all active, scoped to
-- the same project. ON CONFLICT covers re-runs and rows already backfilled on
-- read.
INSERT INTO remote_session_client_user_session_issuers (
    remote_session_client_id,
    user_session_issuer_id
)
SELECT c.id, c.user_session_issuer_id
FROM remote_session_clients AS c
JOIN remote_session_issuers AS i ON i.id = c.remote_session_issuer_id
JOIN user_session_issuers AS usi ON usi.id = c.user_session_issuer_id
WHERE c.deleted IS FALSE
  AND i.deleted IS FALSE
  AND usi.deleted IS FALSE
  AND c.project_id = usi.project_id
ON CONFLICT (remote_session_client_id, user_session_issuer_id) DO NOTHING;

-- Verify completeness. Counts active clients (legacy fallback population) that
-- still lack a join-table binding. Expected: 0.
SELECT count(*) AS remaining_legacy_only_clients
FROM remote_session_clients AS c
JOIN remote_session_issuers AS i ON i.id = c.remote_session_issuer_id
JOIN user_session_issuers AS usi ON usi.id = c.user_session_issuer_id
WHERE c.deleted IS FALSE
  AND i.deleted IS FALSE
  AND usi.deleted IS FALSE
  AND c.project_id = usi.project_id
  AND NOT EXISTS (
    SELECT 1
    FROM remote_session_client_user_session_issuers AS link
    WHERE link.remote_session_client_id = c.id
      AND link.user_session_issuer_id = c.user_session_issuer_id
  );

COMMIT;
