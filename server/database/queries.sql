-- name: StubQuery :one
select 1;

-- name: ObtainExclusiveTxAdvisoryLock :exec
SELECT pg_advisory_xact_lock(@key::bigint);

-- name: TryObtainExclusiveTxAdvisoryLock :one
SELECT pg_try_advisory_xact_lock(@key::bigint);