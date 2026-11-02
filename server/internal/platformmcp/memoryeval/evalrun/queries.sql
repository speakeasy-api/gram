-- name: CreateRun :one
INSERT INTO memory_eval_runs (organization_id, config, requested_by)
VALUES (@organization_id, @config, @requested_by)
RETURNING *;

-- name: GetRun :one
SELECT * FROM memory_eval_runs WHERE id = @id AND deleted IS FALSE;

-- name: ListRuns :many
-- Newest first, keyset-paged on created_at and id.
SELECT * FROM memory_eval_runs
WHERE deleted IS FALSE
  AND (sqlc.narg(before_created_at)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(before_created_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;

-- name: CountRunsSince :one
-- Runs started since a time, for the daily cap. Failed runs count too: they
-- spent credits before failing.
SELECT count(*) FROM memory_eval_runs
WHERE deleted IS FALSE AND created_at >= @since;

-- name: MarkRunRunning :one
-- Claims a queued run. Returns no row when the run is gone or was already
-- claimed, so a duplicate activity attempt does not spend twice.
UPDATE memory_eval_runs
SET status = 'running', started_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = @id AND status = 'queued' AND deleted IS FALSE
RETURNING *;

-- name: CompleteRun :execrows
UPDATE memory_eval_runs
SET status = 'succeeded', report = @report, cost_usd = @cost_usd,
    finished_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = @id AND status = 'running';

-- name: FailRun :execrows
-- Fails a run that has not finished. A no-op for runs that already resolved.
UPDATE memory_eval_runs
SET status = 'failed', error = @error, cost_usd = sqlc.narg(cost_usd),
    finished_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = @id AND status IN ('queued', 'running');

-- name: LockRunStarts :exec
-- Serializes run starts for the transaction, so the daily cap and the
-- one-active-run rule hold under concurrent starts.
SELECT pg_advisory_xact_lock(hashtext('memory_eval_runs.start'));

-- name: CountActiveRuns :one
-- Runs that may still write team memory. Two at once would overwrite each
-- other's summaries and digests in the shared eval projects.
SELECT count(*) FROM memory_eval_runs
WHERE deleted IS FALSE AND status IN ('queued', 'running');
