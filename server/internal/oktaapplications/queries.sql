-- Verified connections whose snapshot is stale or was requested after the
-- last run started. Due-ness is evaluated on the database clock; excluded
-- ids are the ones this coordinator pass already attempted.
-- name: ListSyncCandidates :many
SELECT
    c.id AS connection_id
  , c.organization_id
  , om.slug AS organization_slug
FROM identity_provider_connections AS c
JOIN okta_identity_provider_connections AS o
  ON o.identity_provider_connection_id = c.id
 AND o.organization_id = c.organization_id
 AND o.deleted IS FALSE
JOIN organization_metadata AS om
  ON om.id = c.organization_id
WHERE c.provider = 'okta'
  AND c.deleted IS FALSE
  AND c.status = 'verified'
  AND (
    o.applications_synced_at IS NULL
    OR o.applications_sync_requested_at > o.applications_synced_at
    OR o.applications_synced_at + make_interval(secs => o.applications_sync_interval_seconds) <= clock_timestamp()
  )
  AND NOT (c.id = ANY (@exclude_connection_ids::uuid[]))
ORDER BY o.applications_synced_at ASC NULLS FIRST, c.id ASC
LIMIT @limit_count;

-- The credential configuration a run needs; the managed client is the
-- org-level remote session client provisioned for the connection.
-- name: GetSyncTarget :one
SELECT
    c.id AS connection_id
  , c.organization_id
  , c.status
  , o.org_url
  , rc.id AS remote_session_client_id
  , rc.client_id
  , rc.json_web_key_set_id
FROM identity_provider_connections AS c
JOIN okta_identity_provider_connections AS o
  ON o.identity_provider_connection_id = c.id
 AND o.organization_id = c.organization_id
 AND o.deleted IS FALSE
JOIN remote_session_clients AS rc
  ON rc.organization_id = c.organization_id
 AND rc.project_id IS NULL
 AND rc.identity_provider_connection_id = c.id
 AND rc.deleted IS FALSE
WHERE c.id = @connection_id
  AND c.provider = 'okta'
  AND c.deleted IS FALSE;

-- Serializes applies with each other and with revoke, which locks the same
-- row; a revoked or unverified connection yields no row. Read the watermark
-- in a separate statement after this lock: a joined, unlocked subtype row
-- could retain the statement's pre-wait snapshot after another apply commits.
-- name: LockSyncConnection :one
SELECT c.id
FROM identity_provider_connections AS c
WHERE c.id = @connection_id
  AND c.organization_id = @organization_id
  AND c.provider = 'okta'
  AND c.deleted IS FALSE
  AND c.status = 'verified'
FOR UPDATE OF c;

-- Called with the connection lock held, so READ COMMITTED sees the watermark
-- committed by any apply we waited for, without changing revoke's lock order.
-- name: GetApplicationsSyncedAt :one
SELECT applications_synced_at
FROM okta_identity_provider_connections
WHERE identity_provider_connection_id = @identity_provider_connection_id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- Monotonic: a late-finishing older run never rewinds a newer watermark.
-- name: MarkApplicationsSynced :execrows
UPDATE okta_identity_provider_connections
SET applications_synced_at = GREATEST(applications_synced_at, @synced_at::timestamptz),
    updated_at = clock_timestamp()
WHERE identity_provider_connection_id = @identity_provider_connection_id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- A run that never finished (worker died) is closed before a new one opens.
-- name: FailInterruptedReconcileRuns :execrows
UPDATE okta_application_reconcile_runs
SET status = 'failed',
    finished_at = clock_timestamp(),
    error = 'interrupted',
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id
  AND status = 'running';

-- name: PruneReconcileRuns :execrows
DELETE FROM okta_application_reconcile_runs
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id
  AND started_at < @before::timestamptz;

-- name: CreateReconcileRun :one
INSERT INTO okta_application_reconcile_runs (
  organization_id,
  identity_provider_connection_id
) VALUES (
  @organization_id,
  @identity_provider_connection_id
)
RETURNING *;

-- name: FinishReconcileRun :one
UPDATE okta_application_reconcile_runs
SET status = @status,
    finished_at = clock_timestamp(),
    applications_seen = @applications_seen,
    applications_added = @applications_added,
    applications_removed = @applications_removed,
    assignments_added = @assignments_added,
    assignments_removed = @assignments_removed,
    skipped_app_ids = @skipped_app_ids::text[],
    truncated = @truncated,
    error = sqlc.narg(error),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND status = 'running'
RETURNING *;

-- name: GetLatestReconcileRun :one
SELECT *
FROM okta_application_reconcile_runs
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id
ORDER BY started_at DESC, id DESC
LIMIT 1;

-- Revocation deletes the snapshot outright: it is tenant-wide directory
-- data. Assignments cascade from the application rows.
-- name: DeleteApplicationsForConnection :execrows
DELETE FROM okta_applications
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id;

-- name: DeleteReconcileRunsForConnection :execrows
DELETE FROM okta_application_reconcile_runs
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id;

-- name: ListLiveApplicationIDs :many
SELECT okta_app_id
FROM okta_applications
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id
  AND removed_at IS NULL;

-- name: ListLiveAssignmentKeys :many
SELECT okta_app_id, principal_kind, okta_principal_id
FROM okta_application_assignments
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id
  AND removed_at IS NULL;

-- Upsert one chunk of applications. last_seen_at always advances; updated_at
-- moves only when the recorded attributes changed. features travels as a
-- comma-joined string per row because sqlc cannot type a text[][] parameter.
-- name: UpsertApplications :exec
INSERT INTO okta_applications (
  organization_id,
  identity_provider_connection_id,
  okta_app_id,
  label,
  name,
  sign_on_mode,
  status,
  features,
  okta_created_at,
  okta_last_updated_at,
  first_seen_at,
  last_seen_at
)
SELECT
  @organization_id,
  @identity_provider_connection_id,
  s.okta_app_id,
  s.label,
  s.name,
  s.sign_on_mode,
  s.status,
  s.features,
  s.okta_created_at,
  s.okta_last_updated_at,
  @seen_at::timestamptz,
  @seen_at::timestamptz
FROM (
  SELECT unnest(@okta_app_ids::text[]) AS okta_app_id,
         unnest(@labels::text[]) AS label,
         unnest(@names::text[]) AS name,
         unnest(@sign_on_modes::text[]) AS sign_on_mode,
         unnest(@statuses::text[]) AS status,
         string_to_array(unnest(@features_csv::text[]), ',') AS features,
         unnest(@okta_created_ats::timestamptz[]) AS okta_created_at,
         unnest(@okta_last_updated_ats::timestamptz[]) AS okta_last_updated_at
) AS s
ON CONFLICT (organization_id, identity_provider_connection_id, okta_app_id) DO UPDATE
SET label = EXCLUDED.label,
    name = EXCLUDED.name,
    sign_on_mode = EXCLUDED.sign_on_mode,
    status = EXCLUDED.status,
    features = EXCLUDED.features,
    okta_created_at = EXCLUDED.okta_created_at,
    okta_last_updated_at = EXCLUDED.okta_last_updated_at,
    last_seen_at = EXCLUDED.last_seen_at,
    removed_at = NULL,
    updated_at = CASE
      WHEN okta_applications.removed_at IS NOT NULL
        OR (okta_applications.label, okta_applications.name, okta_applications.sign_on_mode, okta_applications.status, okta_applications.features, okta_applications.okta_created_at, okta_applications.okta_last_updated_at)
           IS DISTINCT FROM (EXCLUDED.label, EXCLUDED.name, EXCLUDED.sign_on_mode, EXCLUDED.status, EXCLUDED.features, EXCLUDED.okta_created_at, EXCLUDED.okta_last_updated_at)
      THEN clock_timestamp()
      ELSE okta_applications.updated_at
    END;

-- name: RemoveApplications :exec
UPDATE okta_applications
SET removed_at = @removed_at::timestamptz,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id
  AND removed_at IS NULL
  AND okta_app_id = ANY (@okta_app_ids::text[]);

-- name: UpsertAssignments :exec
INSERT INTO okta_application_assignments (
  organization_id,
  identity_provider_connection_id,
  okta_app_id,
  principal_kind,
  okta_principal_id,
  assignment_scope,
  first_seen_at,
  last_seen_at
)
SELECT
  @organization_id,
  @identity_provider_connection_id,
  s.okta_app_id,
  s.principal_kind,
  s.okta_principal_id,
  s.assignment_scope,
  @seen_at::timestamptz,
  @seen_at::timestamptz
FROM (
  SELECT unnest(@okta_app_ids::text[]) AS okta_app_id,
         unnest(@principal_kinds::text[]) AS principal_kind,
         unnest(@okta_principal_ids::text[]) AS okta_principal_id,
         unnest(@assignment_scopes::text[]) AS assignment_scope
) AS s
ON CONFLICT (organization_id, identity_provider_connection_id, okta_app_id, principal_kind, okta_principal_id) DO UPDATE
SET assignment_scope = EXCLUDED.assignment_scope,
    last_seen_at = EXCLUDED.last_seen_at,
    removed_at = NULL,
    updated_at = CASE
      WHEN okta_application_assignments.removed_at IS NOT NULL
        OR okta_application_assignments.assignment_scope IS DISTINCT FROM EXCLUDED.assignment_scope
      THEN clock_timestamp()
      ELSE okta_application_assignments.updated_at
    END;

-- name: RemoveAssignments :exec
UPDATE okta_application_assignments AS a
SET removed_at = @removed_at::timestamptz,
    updated_at = clock_timestamp()
FROM (
  SELECT unnest(@okta_app_ids::text[]) AS okta_app_id,
         unnest(@principal_kinds::text[]) AS principal_kind,
         unnest(@okta_principal_ids::text[]) AS okta_principal_id
) AS s
WHERE a.organization_id = @organization_id
  AND a.identity_provider_connection_id = @identity_provider_connection_id
  AND a.removed_at IS NULL
  AND a.okta_app_id = s.okta_app_id
  AND a.principal_kind = s.principal_kind
  AND a.okta_principal_id = s.okta_principal_id;

-- Live rows sort first so the cap never hides them behind removed ones.
-- name: ListApplications :many
SELECT
    a.*
  , (SELECT COUNT(*) FROM okta_application_assignments AS u
      WHERE u.organization_id = a.organization_id
        AND u.identity_provider_connection_id = a.identity_provider_connection_id
        AND u.okta_app_id = a.okta_app_id
        AND u.principal_kind = 'user'
        AND u.removed_at IS NULL)::integer AS user_assignments
  , (SELECT COUNT(*) FROM okta_application_assignments AS g
      WHERE g.organization_id = a.organization_id
        AND g.identity_provider_connection_id = a.identity_provider_connection_id
        AND g.okta_app_id = a.okta_app_id
        AND g.principal_kind = 'group'
        AND g.removed_at IS NULL)::integer AS group_assignments
FROM okta_applications AS a
WHERE a.organization_id = @organization_id
  AND a.identity_provider_connection_id = @identity_provider_connection_id
  AND (a.removed_at IS NULL OR @include_removed::boolean)
ORDER BY (a.removed_at IS NULL) DESC, a.label ASC, a.okta_app_id ASC
LIMIT @limit_count;

-- name: CountApplicationsForConnection :one
SELECT COUNT(*)
FROM okta_applications
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id;

-- name: CountAssignmentsForConnection :one
SELECT COUNT(*)
FROM okta_application_assignments
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id;

-- name: CountReconcileRunsForConnection :one
SELECT COUNT(*)
FROM okta_application_reconcile_runs
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id;

-- name: GetReconcileRun :one
SELECT *
FROM okta_application_reconcile_runs
WHERE id = @id
  AND organization_id = @organization_id;

-- Test fixture: ages or closes a run so interruption and pruning can be observed.
-- name: BackdateReconcileRun :exec
UPDATE okta_application_reconcile_runs
SET status = @status,
    started_at = @started_at::timestamptz
WHERE id = @id
  AND organization_id = @organization_id;

-- Close only attempts covered by this terminal activity failure. Retrying this
-- statement cannot rewrite a finished run or touch a subsequently started run.
-- name: FinalizeInterruptedReconcileRuns :execrows
UPDATE okta_application_reconcile_runs
SET status = 'failed', finished_at = clock_timestamp(),
    error = 'interrupted', updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id
  AND started_at <= @cutoff::timestamptz
  AND status = 'running';
