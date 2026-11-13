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
-- Runs created since a time, for the daily cap. Failed and deleted runs count
-- too: they spent credits, and deleting a run must not refund the cap. A run
-- that failed before any workflow claimed it spent nothing, so it is left out.
SELECT count(*) FROM memory_eval_runs
WHERE created_at >= @since
  AND NOT (status = 'failed' AND started_at IS NULL);

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
-- other's summaries and digests in the shared eval projects. A soft-deleted
-- run counts while it is unresolved: deleting the row does not stop it.
SELECT count(*) FROM memory_eval_runs
WHERE status IN ('queued', 'running');

-- name: FailStaleActiveRuns :execrows
-- Fails queued or running runs that started (or, if never claimed, were
-- created) before a cutoff older than any live workflow. Nothing else resolves a row whose workflow never started or was
-- terminated, and an unresolved row blocks every later start.
UPDATE memory_eval_runs
SET status = 'failed', error = @error, finished_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE status IN ('queued', 'running') AND COALESCE(started_at, created_at) < @created_before;

-- name: GetRunStatus :one
-- A live run's status, so a claim that found nothing can tell an orphaned
-- running row from one that is finished or gone.
SELECT status FROM memory_eval_runs WHERE id = @id AND deleted IS FALSE;

-- name: DeleteRun :execrows
-- Soft-deletes a run. It still counts toward the daily cap.
UPDATE memory_eval_runs
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = @id AND deleted IS FALSE;

-- name: FailQueuedRun :execrows
-- Fails a run whose workflow never started. Only a still-queued row matches,
-- so a run a workflow already claimed is never failed under it.
UPDATE memory_eval_runs
SET status = 'failed', error = @error, finished_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = @id AND status = 'queued';
