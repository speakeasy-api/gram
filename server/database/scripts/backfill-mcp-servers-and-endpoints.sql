-- Backfills mcp_servers and mcp_endpoints from existing toolsets data.
--
-- One-shot, manual backfill for AGE-1880. Designed to be run exactly once
-- per database after the mcp_servers/mcp_endpoints tables are created and
-- before the AGE-1902 read-path cutover.
--
-- The script is wrapped in a single transaction and aborts loudly on:
--   * mcp_servers or mcp_endpoints already containing rows (re-run guard)
--   * a toolsets.default_environment_slug that does not resolve to a live
--     environments row
--   * the inserted row counts not matching the expected counts derived
--     from toolsets
--   * any constraint violation on the target tables (e.g. duplicate slug)
--
-- Run from a gram-infra checkout with:
--   mise gcp:db:run-script <env> \
--     --script /absolute/path/to/gram/server/database/scripts/backfill-mcp-servers-and-endpoints.sql \
--     --permission ALL

\set ON_ERROR_STOP on

BEGIN;

DO $$
DECLARE
  existing_servers int;
  existing_endpoints int;
  orphan_count int;
  expected_servers int;
  expected_endpoints int;
  inserted_servers_count int;
  inserted_endpoints_count int;
BEGIN
  -- Re-run guard: this script is one-shot. If either target table already
  -- has rows, abort so the operator can investigate before duplicating data
  -- (mcp_servers has no UNIQUE on toolset_id to catch duplicates).
  SELECT count(*) INTO existing_servers FROM mcp_servers;
  IF existing_servers > 0 THEN
    RAISE EXCEPTION
      'mcp_servers already contains % row(s); refusing to run one-shot backfill.',
      existing_servers;
  END IF;

  SELECT count(*) INTO existing_endpoints FROM mcp_endpoints;
  IF existing_endpoints > 0 THEN
    RAISE EXCEPTION
      'mcp_endpoints already contains % row(s); refusing to run one-shot backfill.',
      existing_endpoints;
  END IF;

  -- Pre-check: every live toolset with a non-NULL default_environment_slug
  -- must resolve to a live environments row. Otherwise the LEFT JOIN below
  -- would silently insert a NULL environment_id.
  SELECT count(*) INTO orphan_count
  FROM toolsets t
  WHERE t.deleted IS FALSE
    AND t.default_environment_slug IS NOT NULL
    AND NOT EXISTS (
      SELECT 1 FROM environments e
      WHERE e.project_id = t.project_id
        AND e.slug = t.default_environment_slug
        AND e.deleted IS FALSE
    );
  IF orphan_count > 0 THEN
    RAISE EXCEPTION
      'Found % live toolset(s) whose default_environment_slug does not resolve to a live environment; resolve before re-running.',
      orphan_count;
  END IF;

  -- Expected counts, used for post-insert assertions to catch a bad WHERE
  -- clause or an unexpected NULL filter.
  SELECT count(*) INTO expected_servers
  FROM toolsets WHERE deleted IS FALSE;

  SELECT count(*) INTO expected_endpoints
  FROM toolsets WHERE deleted IS FALSE AND mcp_slug IS NOT NULL;

  -- Backfill: insert one mcp_servers row per live toolset, then chain
  -- those returned ids back to toolsets to insert mcp_endpoints rows for any
  -- toolset that has mcp_slug set.
  WITH inserted_servers AS (
    INSERT INTO mcp_servers (
      project_id,
      environment_id,
      external_oauth_server_id,
      oauth_proxy_server_id,
      toolset_id,
      visibility
    )
    SELECT
      t.project_id,
      e.id,
      t.external_oauth_server_id,
      t.oauth_proxy_server_id,
      t.id,
      CASE
        WHEN NOT t.mcp_enabled   THEN 'disabled'
        WHEN NOT t.mcp_is_public THEN 'private'
        ELSE                          'public'
      END
    FROM toolsets t
    LEFT JOIN environments e
      ON  e.project_id = t.project_id
      AND e.slug       = t.default_environment_slug
      AND e.deleted IS FALSE
    WHERE t.deleted IS FALSE
    RETURNING id, toolset_id
  ),
  inserted_endpoints AS (
    INSERT INTO mcp_endpoints (
      project_id,
      custom_domain_id,
      mcp_server_id,
      slug
    )
    SELECT
      t.project_id,
      t.custom_domain_id,
      s.id,
      t.mcp_slug
    FROM inserted_servers s
    JOIN toolsets t ON t.id = s.toolset_id
    WHERE t.mcp_slug IS NOT NULL
    RETURNING id
  )
  SELECT
    (SELECT count(*) FROM inserted_servers),
    (SELECT count(*) FROM inserted_endpoints)
  INTO inserted_servers_count, inserted_endpoints_count;

  -- Row-count assertions: catch a typo or unexpected filter behavior
  -- before COMMIT.
  IF inserted_servers_count <> expected_servers THEN
    RAISE EXCEPTION
      'Inserted % mcp_servers row(s), expected %; aborting.',
      inserted_servers_count, expected_servers;
  END IF;

  IF inserted_endpoints_count <> expected_endpoints THEN
    RAISE EXCEPTION
      'Inserted % mcp_endpoints row(s), expected %; aborting.',
      inserted_endpoints_count, expected_endpoints;
  END IF;

  RAISE NOTICE
    'Backfill complete: % mcp_servers row(s), % mcp_endpoints row(s).',
    inserted_servers_count, inserted_endpoints_count;
END $$;

COMMIT;
