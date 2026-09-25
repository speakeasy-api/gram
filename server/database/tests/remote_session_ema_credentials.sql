-- Run with psql -X -v ON_ERROR_STOP=1 against an isolated local database with
-- server/migrations applied. All fixtures and assertions are rolled back.
-- This is a storage contract test; AIM-62 owns runtime usability and erasure.
BEGIN;

CREATE FUNCTION pg_temp.fixture_id(n integer) RETURNS uuid LANGUAGE sql AS $$
  SELECT ('00000000-0000-0000-0000-' || lpad(n::text, 12, '0'))::uuid;
$$;

CREATE FUNCTION pg_temp.assert_true(actual boolean, message text)
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
  IF actual IS DISTINCT FROM TRUE THEN RAISE EXCEPTION '%', message; END IF;
END;
$$;

CREATE FUNCTION pg_temp.expect_error(statement text, expected_state text, expected_constraint text)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE actual_constraint text;
BEGIN
  BEGIN
    EXECUTE statement;
  EXCEPTION WHEN integrity_constraint_violation THEN
    GET STACKED DIAGNOSTICS actual_constraint = CONSTRAINT_NAME;
    -- PostgreSQL truncates identifiers to 63 bytes (these names are ASCII).
    IF SQLSTATE = expected_state AND actual_constraint = left(expected_constraint, 63) THEN RETURN; END IF;
    RAISE;
  END;
  RAISE EXCEPTION 'Expected % (%) to reject %', expected_constraint, expected_state, statement;
END;
$$;

INSERT INTO organization_metadata (id, name, slug) VALUES
  ('fixture-ema-org', 'Fixture EMA', 'fixture-ema'),
  ('fixture-ema-other', 'Fixture EMA Other', 'fixture-ema-other');
INSERT INTO projects (id, name, slug, organization_id)
SELECT pg_temp.fixture_id(n), 'Fixture ' || n, 'fixture-' || n, 'fixture-ema-org'
FROM generate_series(1, 2) AS n;
INSERT INTO remote_session_issuers (id, slug, issuer)
SELECT pg_temp.fixture_id(n), 'fixture-ema-' || n, 'https://issuer.example.invalid/' || n
FROM generate_series(1, 2) AS n;
INSERT INTO user_session_issuers (id, project_id, organization_id, slug, authn_challenge_mode, session_duration)
SELECT pg_temp.fixture_id(n), pg_temp.fixture_id(1), 'fixture-ema-org',
  'fixture-ema-' || n, 'interactive', interval '1 hour'
FROM generate_series(1, 2) AS n;
INSERT INTO remote_session_clients (id, remote_session_issuer_id, client_id)
SELECT pg_temp.fixture_id(n), pg_temp.fixture_id(1), 'fixture-client-' || n
FROM generate_series(1, 2) AS n;
INSERT INTO remote_session_ema_bindings
  (id, project_id, organization_id, user_session_issuer_id, remote_session_issuer_id, resource, remote_session_client_id)
VALUES (pg_temp.fixture_id(1), pg_temp.fixture_id(1), 'fixture-ema-org', pg_temp.fixture_id(1),
  pg_temp.fixture_id(1), 'https://resource.example.invalid/', pg_temp.fixture_id(1));
INSERT INTO trusted_issuer_sessions (id, organization_id, remote_session_client_id, subject_urn)
VALUES (pg_temp.fixture_id(1), 'fixture-ema-org', pg_temp.fixture_id(1), 'user:fixture-human');

CREATE FUNCTION pg_temp.credential(
  n integer, project integer DEFAULT 1, user_issuer integer DEFAULT 1,
  client integer DEFAULT 1, target text DEFAULT 'https://resource.example.invalid/',
  subject text DEFAULT 'user:fixture-human', selection text DEFAULT 'binding'
) RETURNS uuid LANGUAGE sql AS $$
  INSERT INTO remote_session_ema_credentials
    (id, organization_id, project_id, user_session_issuer_id, remote_session_issuer_id,
     remote_session_client_id, resource, subject_urn, client_selection,
     remote_session_ema_binding_id, ema_binding_generation, trusted_issuer_session_id,
     access_token_encrypted, access_expires_at)
  VALUES (pg_temp.fixture_id(n), 'fixture-ema-org', pg_temp.fixture_id(project),
    pg_temp.fixture_id(user_issuer), pg_temp.fixture_id(1), pg_temp.fixture_id(client),
    target, subject, selection,
    CASE WHEN selection = 'binding' THEN pg_temp.fixture_id(1) END,
    CASE WHEN selection = 'binding' THEN 1 END, pg_temp.fixture_id(1),
    'fixture-ciphertext', clock_timestamp() + interval '1 hour')
  RETURNING id;
$$;

SELECT pg_temp.credential(1);
SELECT pg_temp.assert_true(
  (SELECT requested_scopes = ARRAY[]::text[] AND granted_scopes = ARRAY[]::text[]
    AND downstream_refresh_token_observed IS FALSE AND deleted IS FALSE
    AND deleted_at IS NULL AND last_used_at IS NULL
    AND created_at IS NOT NULL AND updated_at IS NOT NULL
   FROM remote_session_ema_credentials WHERE id = pg_temp.fixture_id(1)),
  'credential defaults');
SELECT pg_temp.expect_error('SELECT pg_temp.credential(2)', '23505',
  'remote_session_ema_credentials_subject_key');
SELECT pg_temp.expect_error(
  $$SELECT pg_temp.credential(2, selection => 'implicit')$$, '23505',
  'remote_session_ema_credentials_subject_key');

-- Each component independently partitions live uniqueness, including the slash.
SELECT pg_temp.credential(2, project => 2);
SELECT pg_temp.credential(3, user_issuer => 2);
SELECT pg_temp.credential(4, client => 2);
SELECT pg_temp.credential(5, target => 'https://resource.example.invalid');
SELECT pg_temp.credential(6, subject => 'user:fixture-other');
SELECT pg_temp.credential(7, subject => 'user:fixture-implicit', selection => 'implicit');
SELECT pg_temp.assert_true(
  (SELECT client_selection = 'implicit' AND remote_session_ema_binding_id IS NULL
    AND ema_binding_generation IS NULL FROM remote_session_ema_credentials
   WHERE id = pg_temp.fixture_id(7)), 'implicit selection requires no binding');

-- The writer clears ciphertext explicitly; soft deletion alone is not erasure.
UPDATE remote_session_ema_credentials
SET deleted_at = clock_timestamp(), access_token_encrypted = NULL
WHERE id = pg_temp.fixture_id(1);
SELECT pg_temp.assert_true(
  (SELECT deleted AND access_token_encrypted IS NULL FROM remote_session_ema_credentials
   WHERE id = pg_temp.fixture_id(1)), 'soft deletion permits ciphertext erasure');
SELECT pg_temp.credential(8, selection => 'implicit');
SELECT pg_temp.expect_error('SELECT pg_temp.credential(9)', '23505',
  'remote_session_ema_credentials_subject_key');

SAVEPOINT generated_id;
INSERT INTO remote_session_ema_credentials
  (resource, subject_urn, client_selection, access_expires_at)
VALUES ('https://resource.example.invalid/', 'user:fixture-default-id', 'implicit', clock_timestamp());
SELECT pg_temp.assert_true(
  (SELECT id IS NOT NULL AND access_token_encrypted IS NULL
   FROM remote_session_ema_credentials WHERE subject_urn = 'user:fixture-default-id'),
  'UUID default and nullable ciphertext support metadata-only orphan storage');
ROLLBACK TO generated_id;

SELECT pg_temp.expect_error(
  $$UPDATE remote_session_ema_credentials SET access_expires_at = NULL WHERE id = pg_temp.fixture_id(8)$$,
  '23502', '');
SELECT pg_temp.expect_error(
  $$UPDATE remote_session_ema_credentials SET resource = NULL WHERE id = pg_temp.fixture_id(8)$$,
  '23502', '');
SELECT pg_temp.expect_error(
  $$UPDATE remote_session_ema_credentials SET subject_urn = NULL WHERE id = pg_temp.fixture_id(8)$$,
  '23502', '');
SELECT pg_temp.expect_error(
  $$UPDATE remote_session_ema_credentials SET client_selection = NULL WHERE id = pg_temp.fixture_id(8)$$,
  '23502', '');

-- Assert the tenant pair and client/issuer pair, not just independent existence.
SELECT pg_temp.expect_error(
  $$UPDATE remote_session_ema_credentials SET organization_id = 'fixture-ema-other' WHERE id = pg_temp.fixture_id(8)$$,
  '23503', 'remote_session_ema_credentials_project_id_fkey');
SELECT pg_temp.expect_error(
  $$UPDATE remote_session_ema_credentials SET remote_session_issuer_id = pg_temp.fixture_id(2) WHERE id = pg_temp.fixture_id(8)$$,
  '23503', 'remote_session_ema_credentials_remote_session_client_id_fkey');
SELECT pg_temp.expect_error(
  $$UPDATE remote_session_ema_credentials SET user_session_issuer_id = pg_temp.fixture_id(99) WHERE id = pg_temp.fixture_id(8)$$,
  '23503', 'remote_session_ema_credentials_user_session_issuer_id_fkey');
SELECT pg_temp.expect_error(
  $$UPDATE remote_session_ema_credentials SET remote_session_ema_binding_id = pg_temp.fixture_id(99) WHERE id = pg_temp.fixture_id(8)$$,
  '23503', 'remote_session_ema_credentials_remote_session_ema_binding_id_fkey');
SELECT pg_temp.expect_error(
  $$UPDATE remote_session_ema_credentials SET trusted_issuer_session_id = pg_temp.fixture_id(99) WHERE id = pg_temp.fixture_id(8)$$,
  '23503', 'remote_session_ema_credentials_trusted_issuer_session_id_fkey');

-- Test FK actions on both live and soft-deleted rows without losing fixtures.
SAVEPOINT binding_deletion;
DELETE FROM remote_session_ema_bindings WHERE id = pg_temp.fixture_id(1);
SELECT pg_temp.assert_true(
  (SELECT count(*) = 8 AND bool_and(remote_session_ema_binding_id IS NULL)
   FROM remote_session_ema_credentials), 'binding deletion preserves rows and nulls references');
SELECT pg_temp.assert_true(
  (SELECT client_selection = 'binding' AND ema_binding_generation = 1
   FROM remote_session_ema_credentials WHERE id = pg_temp.fixture_id(1)),
  'binding nullness does not change recorded selection or generation');
ROLLBACK TO binding_deletion;

SAVEPOINT delegation_deletion;
DELETE FROM trusted_issuer_sessions WHERE id = pg_temp.fixture_id(1);
SELECT pg_temp.assert_true(
  (SELECT count(*) = 8 AND bool_and(trusted_issuer_session_id IS NULL)
   FROM remote_session_ema_credentials), 'delegation deletion preserves rows and nulls references');
ROLLBACK TO delegation_deletion;

-- Existing binding NOT NULL parent columns require lifecycle cleanup first.
SAVEPOINT client_deletion;
DELETE FROM remote_session_ema_bindings WHERE id = pg_temp.fixture_id(1);
DELETE FROM remote_session_clients WHERE id = pg_temp.fixture_id(1);
SELECT pg_temp.assert_true(
  (SELECT count(*) = 7 AND bool_and(remote_session_client_id IS NULL AND remote_session_issuer_id IS NULL)
   FROM remote_session_ema_credentials WHERE id <> pg_temp.fixture_id(4)),
  'client deletion nulls both composite reference columns on live and deleted rows');
SELECT pg_temp.assert_true(
  (SELECT remote_session_client_id = pg_temp.fixture_id(2) AND remote_session_issuer_id = pg_temp.fixture_id(1)
   FROM remote_session_ema_credentials WHERE id = pg_temp.fixture_id(4)), 'other client is unaffected');
ROLLBACK TO client_deletion;

SAVEPOINT remote_issuer_deletion;
DELETE FROM remote_session_ema_bindings WHERE id = pg_temp.fixture_id(1);
DELETE FROM remote_session_issuers WHERE id = pg_temp.fixture_id(1);
SELECT pg_temp.assert_true(
  (SELECT count(*) = 8 AND bool_and(remote_session_client_id IS NULL AND remote_session_issuer_id IS NULL)
   FROM remote_session_ema_credentials), 'remote issuer cascade nulls both credential references');
ROLLBACK TO remote_issuer_deletion;

SAVEPOINT user_issuer_deletion;
DELETE FROM remote_session_ema_bindings WHERE id = pg_temp.fixture_id(1);
DELETE FROM user_session_issuers WHERE id = pg_temp.fixture_id(1);
SELECT pg_temp.assert_true(
  (SELECT count(*) = 7 AND bool_and(user_session_issuer_id IS NULL)
   FROM remote_session_ema_credentials WHERE id <> pg_temp.fixture_id(3)),
  'user issuer deletion nulls live and deleted references');
ROLLBACK TO user_issuer_deletion;

SAVEPOINT project_deletion;
DELETE FROM remote_session_ema_bindings WHERE id = pg_temp.fixture_id(1);
DELETE FROM projects WHERE id = pg_temp.fixture_id(1);
SELECT pg_temp.assert_true(
  (SELECT count(*) = 7 AND bool_and(project_id IS NULL AND organization_id IS NULL)
   FROM remote_session_ema_credentials WHERE id <> pg_temp.fixture_id(2)),
  'project deletion nulls both tenant columns');
ROLLBACK TO project_deletion;

SAVEPOINT project_tenant_update;
UPDATE projects SET organization_id = 'fixture-ema-other' WHERE id = pg_temp.fixture_id(2);
SELECT pg_temp.assert_true(
  (SELECT organization_id = 'fixture-ema-other' AND project_id = pg_temp.fixture_id(2)
   FROM remote_session_ema_credentials WHERE id = pg_temp.fixture_id(2)),
  'project tenant updates cascade to the credential');
ROLLBACK TO project_tenant_update;

-- Verify definitions, including column order and predicates, not only names.
SELECT pg_temp.assert_true(
  (SELECT count(*) = 7 AND bool_and(
    i.indisvalid AND i.indisready AND i.indisunique = expected.is_unique
    AND i.indnkeyatts = cardinality(expected.columns) AND i.indnatts = i.indnkeyatts
    AND ARRAY(SELECT a.attname::text FROM unnest(i.indkey) WITH ORDINALITY AS k(attnum, position)
      JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
      ORDER BY k.position) = expected.columns
    AND pg_get_expr(i.indpred, i.indrelid) IS NOT DISTINCT FROM expected.predicate)
   FROM (VALUES
     ('subject_key', ARRAY['project_id', 'user_session_issuer_id', 'remote_session_client_id', 'resource', 'subject_urn'], TRUE, '(deleted IS FALSE)'),
     ('project_id_idx', ARRAY['project_id'], FALSE, NULL),
     ('user_session_issuer_id_idx', ARRAY['user_session_issuer_id'], FALSE, NULL),
     ('remote_session_client_id_idx', ARRAY['remote_session_client_id'], FALSE, NULL),
     ('remote_session_ema_binding_id_idx', ARRAY['remote_session_ema_binding_id'], FALSE, NULL),
     ('trusted_issuer_session_id_idx', ARRAY['trusted_issuer_session_id'], FALSE, NULL),
     ('access_expires_at_idx', ARRAY['access_expires_at', 'id'], FALSE, NULL)
   ) AS expected(suffix, columns, is_unique, predicate)
   JOIN pg_class c ON c.relname = left('remote_session_ema_credentials_' || expected.suffix, 63)
   JOIN pg_index i ON i.indexrelid = c.oid
     AND i.indrelid = 'remote_session_ema_credentials'::regclass),
  'all seven supporting indexes have the expected keys, uniqueness and predicates');

ROLLBACK;
