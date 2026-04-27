-- Backfills mcp_frontends and mcp_slugs from existing toolsets data.
--
-- One-shot, manual backfill for AGE-1880. Designed to be run exactly once
-- per database after the mcp_frontends/mcp_slugs tables are created and
-- before the AGE-1902 read-path cutover.
--
-- The script is wrapped in a single transaction and aborts loudly on:
--   * mcp_frontends or mcp_slugs already containing rows (re-run guard)
--   * a toolsets.default_environment_slug that does not resolve to a live
--     environments row
--   * the inserted row counts not matching the expected counts derived
--     from toolsets
--   * any constraint violation on the target tables (e.g. duplicate slug)
--
-- Run from a gram-infra checkout with:
--   mise gcp:db:run-script <env> \
--     --script /absolute/path/to/gram/server/database/scripts/backfill-mcp-frontends-and-slugs.sql \
--     --permission ALL

\set ON_ERROR_STOP on

BEGIN;

DO $$
DECLARE
  existing_frontends int;
  existing_slugs int;
  orphan_count int;
  expected_frontends int;
  expected_slugs int;
  inserted_frontends_count int;
  inserted_slugs_count int;
BEGIN
  -- Re-run guard: this script is one-shot. If either target table already
  -- has rows, abort so the operator can investigate before duplicating data
  -- (mcp_frontends has no UNIQUE on toolset_id to catch duplicates).
  SELECT count(*) INTO existing_frontends FROM mcp_frontends;
  IF existing_frontends > 0 THEN
    RAISE EXCEPTION
      'mcp_frontends already contains % row(s); refusing to run one-shot backfill.',
      existing_frontends;
  END IF;

  SELECT count(*) INTO existing_slugs FROM mcp_slugs;
  IF existing_slugs > 0 THEN
    RAISE EXCEPTION
      'mcp_slugs already contains % row(s); refusing to run one-shot backfill.',
      existing_slugs;
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
  SELECT count(*) INTO expected_frontends
  FROM toolsets WHERE deleted IS FALSE;

  SELECT count(*) INTO expected_slugs
  FROM toolsets WHERE deleted IS FALSE AND mcp_slug IS NOT NULL;

  -- Backfill: insert one mcp_frontends row per live toolset, then chain
  -- those returned ids back to toolsets to insert mcp_slugs rows for any
  -- toolset that has mcp_slug set.
  WITH inserted_frontends AS (
    INSERT INTO mcp_frontends (
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
  inserted_slugs AS (
    INSERT INTO mcp_slugs (
      project_id,
      custom_domain_id,
      mcp_frontend_id,
      slug
    )
    SELECT
      t.project_id,
      t.custom_domain_id,
      f.id,
      t.mcp_slug
    FROM inserted_frontends f
    JOIN toolsets t ON t.id = f.toolset_id
    WHERE t.mcp_slug IS NOT NULL
    RETURNING id
  )
  SELECT
    (SELECT count(*) FROM inserted_frontends),
    (SELECT count(*) FROM inserted_slugs)
  INTO inserted_frontends_count, inserted_slugs_count;

  -- Row-count assertions: catch a typo or unexpected filter behavior
  -- before COMMIT.
  IF inserted_frontends_count <> expected_frontends THEN
    RAISE EXCEPTION
      'Inserted % mcp_frontends row(s), expected %; aborting.',
      inserted_frontends_count, expected_frontends;
  END IF;

  IF inserted_slugs_count <> expected_slugs THEN
    RAISE EXCEPTION
      'Inserted % mcp_slugs row(s), expected %; aborting.',
      inserted_slugs_count, expected_slugs;
  END IF;

  RAISE NOTICE
    'Backfill complete: % mcp_frontends row(s), % mcp_slugs row(s).',
    inserted_frontends_count, inserted_slugs_count;
END $$;

COMMIT;
