-- name: CurrentWALLSN :one
-- Returns the primary's current WAL insert LSN in text form.
SELECT pg_current_wal_lsn()::text AS lsn;

-- name: WALReplayCaughtUp :one
-- True once this connection's replayed WAL LSN has reached target_lsn. On a
-- primary (pg_last_wal_replay_lsn() IS NULL) this always returns true.
-- target_lsn is cast text -> pg_lsn so sqlc types the parameter as a Go string
-- (pg_lsn has no Go/pgx mapping) while Postgres does the comparison as pg_lsn.
SELECT COALESCE(pg_last_wal_replay_lsn() >= CAST(@target_lsn AS text)::pg_lsn, TRUE)::boolean AS caught_up;
