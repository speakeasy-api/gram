# Backfill: mcp_servers.remote_session_issuer_id

One-time, hand-run fill of the authorization server each MCP server's upstream
authenticates against, for rows that predate the sync (gram#5767). A human runs
this per environment; no tooling executes it. Record the counts from every run
on AIM-135.

The statement is the deployed resync (`ResyncMCPServerRemoteSessionIssuers`,
`server/internal/mcpservers/queries.sql`) widened from the caller's project to
all projects: same derivation, same liveness filters, same
one-distinct-issuer-stamps / zero-or-several-NULL rule, same `IS DISTINCT FROM`
guard. If that query has changed since this document was written, re-derive the
statement from the current text rather than trusting this copy.

Semantics verified locally against a live database before check-in: a unique
binding stamps, a second distinct issuer clears, tombstoning it re-stamps, and
an identical re-run updates zero rows.

## Ordering

Run only after #5767 is deployed to production. Backfilling first leaves rows
that drift the moment a binding changes. The sync is best-effort, so values
that go stale after this run degrade to fail-closed routing and heal on the
next binding change or a re-run of this statement — re-running is always safe.

## Safety properties

- Idempotent: a re-run writes zero rows.
- Hosted servers untouched: `user_session_issuer_id IS NULL` never matches, so
  an operator-set value (direct upstream authorization, AIM-28) cannot be
  clobbered.
- First run only ever sets: every matchable row is NULL beforehand (preflight
  confirms).
- No locks needed: concurrent client mutations at worst leave a stale value on
  the same recoverable terms as the sync itself.

## Preflight

```sql
-- Expect 0: nothing has written the column on the rows the statement can
-- match. Operator-set hosted rows (AIM-28, user_session_issuer_id IS NULL)
-- are excluded — the UPDATE can never touch them. Non-zero means something
-- already wrote the sync-managed slice; stop and investigate.
SELECT count(*) FROM mcp_servers
WHERE remote_session_issuer_id IS NOT NULL
  AND user_session_issuer_id IS NOT NULL
  AND deleted IS FALSE;

-- Coverage buckets over live servers carrying a user session issuer:
-- how many will stamp (unique), stay NULL with no derivable issuer (none),
-- or stay NULL from a multi-issuer binding (ambiguous).
WITH derived AS (
    SELECT usi.id AS user_session_issuer_id,
           count(DISTINCT i.id) AS issuer_count
    FROM user_session_issuers AS usi
    JOIN projects AS p ON p.id = usi.project_id
    LEFT JOIN remote_session_client_user_session_issuers AS link
           ON link.user_session_issuer_id = usi.id
    LEFT JOIN remote_session_clients AS c
           ON c.id = link.remote_session_client_id
          AND c.deleted IS FALSE
          AND (c.project_id = usi.project_id
               OR (c.project_id IS NULL AND c.organization_id = p.organization_id))
    LEFT JOIN remote_session_issuers AS i
           ON i.id = c.remote_session_issuer_id
          AND i.deleted IS FALSE
    WHERE usi.project_id IS NOT NULL
    GROUP BY usi.id
)
SELECT CASE WHEN d.issuer_count = 1 THEN 'unique'
            WHEN d.issuer_count = 0 THEN 'none'
            ELSE 'ambiguous' END AS bucket,
       count(*) AS servers
FROM mcp_servers AS s
JOIN derived AS d ON d.user_session_issuer_id = s.user_session_issuer_id
WHERE s.deleted IS FALSE
GROUP BY 1 ORDER BY 1;
```

Sanity-check the buckets against expectations before writing (production
baseline on the 2026-08-25 read replica: 104 unique, 33 none, 0 ambiguous).
The `none` bucket is servers whose upstream needs no OAuth — spot-check a few
before accepting the number.

## The statement

Dry run first: execute inside `BEGIN`, read the row count, `ROLLBACK`. The
count is exact because it is the real statement. Then repeat with `COMMIT`.

```sql
BEGIN;

WITH resolved AS (
    SELECT usi.id AS user_session_issuer_id,
           usi.project_id,
           CASE WHEN count(DISTINCT i.id) = 1
                THEN (array_agg(DISTINCT i.id))[1]
           END AS remote_session_issuer_id
    FROM user_session_issuers AS usi
    JOIN projects AS p
      ON p.id = usi.project_id
    LEFT JOIN remote_session_client_user_session_issuers AS link
           ON link.user_session_issuer_id = usi.id
    LEFT JOIN remote_session_clients AS c
           ON c.id = link.remote_session_client_id
          AND c.deleted IS FALSE
          AND (c.project_id = usi.project_id
               OR (c.project_id IS NULL AND c.organization_id = p.organization_id))
    LEFT JOIN remote_session_issuers AS i
           ON i.id = c.remote_session_issuer_id
          AND i.deleted IS FALSE
    WHERE usi.project_id IS NOT NULL
    GROUP BY usi.id, usi.project_id
)
UPDATE mcp_servers AS s
SET remote_session_issuer_id = resolved.remote_session_issuer_id,
    updated_at = clock_timestamp()
FROM resolved
WHERE s.user_session_issuer_id = resolved.user_session_issuer_id
  AND s.project_id = resolved.project_id
  AND s.deleted IS FALSE
  AND s.remote_session_issuer_id IS DISTINCT FROM resolved.remote_session_issuer_id;

-- Row count must equal the preflight's `unique` bucket. If it does not,
-- ROLLBACK and investigate before committing.

COMMIT; -- or ROLLBACK for the dry run
```

Divergences from the sync query, both deliberate: iteration over every
project-tier user session issuer replaces the caller's id array, and
`s.project_id = resolved.project_id` replaces the caller's project parameter —
the per-issuer derivation itself is identical. Organization-tier issuers
(`project_id IS NULL`) are skipped exactly as the sync skips them; none exist
until the organization-tier management API ships.

## Verify

```sql
-- Stamped total over issuer-carrying servers equals the `unique` bucket;
-- operator-set hosted rows (AIM-28) are excluded, as in the preflight.
SELECT count(*) FROM mcp_servers
WHERE remote_session_issuer_id IS NOT NULL
  AND user_session_issuer_id IS NOT NULL
  AND deleted IS FALSE;

-- Idempotence: run the statement again; it must report UPDATE 0.
```

## Runbook

1. Local: dry run, apply, re-run (expect 0). `mise run seed` data derives
   nothing — that is expected until the seed grows remote session clients.
2. Production: preflight on the read replica, dry run and apply on the primary,
   re-run to confirm zero writes.
3. Record every count on AIM-135.
