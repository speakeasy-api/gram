-- Run with psql -v ON_ERROR_STOP=1 against an isolated database loaded from
-- server/database/schema.sql. Fixtures and assertions are rolled back.
BEGIN;

CREATE FUNCTION pg_temp.fixture_id(n integer) RETURNS uuid LANGUAGE sql AS $$
  SELECT ('00000000-0000-0000-0000-' || lpad(n::text, 12, '0'))::uuid;
$$;

CREATE FUNCTION pg_temp.expect_integrity_error(statement text, expected_constraint text)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE actual_constraint text;
BEGIN
  BEGIN
    EXECUTE statement;
  EXCEPTION WHEN integrity_constraint_violation THEN
    GET STACKED DIAGNOSTICS actual_constraint = CONSTRAINT_NAME;
    IF actual_constraint = expected_constraint THEN RETURN; END IF;
    RAISE EXCEPTION 'Expected constraint %, got %', expected_constraint, actual_constraint;
  END;
  RAISE EXCEPTION 'Expected constraint % to reject %', expected_constraint, statement;
END;
$$;

INSERT INTO organization_metadata (id, name, slug) VALUES
  ('fixture-org-a', 'Fixture A', 'fixture-a'), ('fixture-org-b', 'Fixture B', 'fixture-b');
INSERT INTO projects (id, name, slug, organization_id)
SELECT pg_temp.fixture_id(n), 'Fixture ' || n, 'fixture-' || n,
  CASE WHEN n = 3 THEN 'fixture-org-b' ELSE 'fixture-org-a' END
FROM generate_series(1, 3) AS n;
INSERT INTO users (id, email, display_name) VALUES ('fixture-owner', 'fixture@example.invalid', 'Fixture');
INSERT INTO organization_user_relationships (organization_id, user_id)
VALUES ('fixture-org-a', 'fixture-owner'), ('fixture-org-b', 'fixture-owner');
INSERT INTO agents (id, organization_id, owner_user_id, name)
VALUES (pg_temp.fixture_id(1), 'fixture-org-a', 'fixture-owner', 'Fixture A'),
       (pg_temp.fixture_id(2), 'fixture-org-b', 'fixture-owner', 'Fixture B');
INSERT INTO remote_session_issuers (id, slug, issuer)
VALUES (pg_temp.fixture_id(1), 'fixture-upstream', 'https://issuer.example.invalid');
INSERT INTO user_session_issuers (id, project_id, organization_id, slug, authn_challenge_mode, session_duration)
SELECT pg_temp.fixture_id(n), CASE WHEN n <= 3 THEN pg_temp.fixture_id(n) END,
  CASE WHEN n IN (1, 2, 4) THEN 'fixture-org-a' WHEN n IN (3, 5) THEN 'fixture-org-b' END,
  'fixture-' || n, 'interactive', interval '1 hour'
FROM generate_series(1, 6) AS n;
INSERT INTO remote_session_clients (id, project_id, organization_id, remote_session_issuer_id, client_id)
SELECT pg_temp.fixture_id(n), CASE WHEN n <= 3 THEN pg_temp.fixture_id(n) END,
  CASE WHEN n IN (1, 2, 4) THEN 'fixture-org-a' WHEN n IN (3, 5) THEN 'fixture-org-b' END,
  pg_temp.fixture_id(1), 'fixture-' || n
FROM generate_series(1, 6) AS n;
-- Deliberately include cross-scope links: attachment safety must not trust
-- that existing client/issuer links were created with correct tenancy.
INSERT INTO remote_session_client_user_session_issuers (user_session_issuer_id, remote_session_client_id)
SELECT pg_temp.fixture_id(i), pg_temp.fixture_id(c)
FROM generate_series(1, 6) AS i CROSS JOIN generate_series(1, 6) AS c;
INSERT INTO remote_sessions (id, subject_urn, user_session_issuer_id, remote_session_client_id, access_token_encrypted)
SELECT pg_temp.fixture_id(i * 10 + c), 'fixture-subject-' || i,
  pg_temp.fixture_id(i), pg_temp.fixture_id(c), 'fixture-ciphertext'
FROM generate_series(1, 6) AS i CROSS JOIN generate_series(1, 6) AS c;

CREATE FUNCTION pg_temp.attach(p integer, o text, i integer, c integer, s integer DEFAULT NULL, a integer DEFAULT 1)
RETURNS uuid LANGUAGE plpgsql AS $$
DECLARE binding_id uuid;
BEGIN
  INSERT INTO principal_remote_session_bindings
    (project_id, organization_id, principal_id, user_session_issuer_id,
     remote_session_client_id, remote_session_id, grant_generation, attached_by_subject_id)
  VALUES (pg_temp.fixture_id(p), o, pg_temp.fixture_id(a), pg_temp.fixture_id(i),
    pg_temp.fixture_id(c), pg_temp.fixture_id(COALESCE(s, i * 10 + c)), 1, 'fixture-owner')
  RETURNING id INTO binding_id;
  RETURN binding_id;
END;
$$;

-- Project and organization issuers each work with project, organization, and
-- global clients. Scope snapshots are populated without changing insert callers.
DO $$
DECLARE i integer; c integer; b uuid;
BEGIN
  FOREACH i IN ARRAY ARRAY[1, 4] LOOP
    FOREACH c IN ARRAY ARRAY[1, 4, 6] LOOP
      b := pg_temp.attach(1, 'fixture-org-a', i, c);
      DELETE FROM principal_remote_session_bindings WHERE id = b;
    END LOOP;
  END LOOP;
END;
$$;

SELECT pg_temp.expect_integrity_error(
  $$SELECT pg_temp.attach(3, 'fixture-org-a', 4, 6)$$,
  'principal_remote_session_bindings_project_tenant_fkey');
SELECT pg_temp.expect_integrity_error(
  $$SELECT pg_temp.attach(1, 'fixture-org-a', 1, 1, 11, 2)$$,
  'principal_remote_session_bindings_principal_tenant_fkey');
SELECT pg_temp.expect_integrity_error(
  format('SELECT pg_temp.attach(1, %L, %s, 6)', 'fixture-org-a', i),
  'principal_remote_session_bindings_issuer_scope_check')
FROM unnest(ARRAY[2, 3, 5, 6]) AS i;
SELECT pg_temp.expect_integrity_error(
  format('SELECT pg_temp.attach(1, %L, 1, %s)', 'fixture-org-a', c),
  'principal_remote_session_bindings_client_scope_check')
FROM unnest(ARRAY[2, 3, 5]) AS c;
SELECT pg_temp.expect_integrity_error(
  $$SELECT pg_temp.attach(1, 'fixture-org-a', 1, 1, 16)$$,
  'principal_remote_session_bindings_session_fkey');
SELECT pg_temp.expect_integrity_error(
  $$SELECT pg_temp.attach(1, 'fixture-org-a', 1, 1, 41)$$,
  'principal_remote_session_bindings_session_fkey');
DELETE FROM remote_session_client_user_session_issuers
WHERE user_session_issuer_id = pg_temp.fixture_id(1) AND remote_session_client_id = pg_temp.fixture_id(4);
SELECT pg_temp.expect_integrity_error(
  $$SELECT pg_temp.attach(1, 'fixture-org-a', 1, 4)$$,
  'principal_remote_session_bindings_client_issuer_fkey');

-- A native FK pins the copied scopes, including parent UPDATEs. The trigger
-- refreshes snapshots on binding UPDATEs, preventing stale or forged scope keys.
SELECT pg_temp.attach(1, 'fixture-org-a', 1, 1);
SELECT pg_temp.expect_integrity_error(
  $$UPDATE remote_session_clients SET project_id = pg_temp.fixture_id(2) WHERE id = pg_temp.fixture_id(1)$$,
  'principal_remote_session_bindings_client_scope_fkey');
SELECT pg_temp.expect_integrity_error(
  $$UPDATE user_session_issuers SET project_id = pg_temp.fixture_id(2) WHERE id = pg_temp.fixture_id(1)$$,
  'principal_remote_session_bindings_issuer_scope_fkey');
SELECT pg_temp.expect_integrity_error(
  $$UPDATE principal_remote_session_bindings SET remote_session_client_id = pg_temp.fixture_id(3), remote_session_id = pg_temp.fixture_id(13), client_attachment_scope = 'global'$$,
  'principal_remote_session_bindings_client_scope_check');
DELETE FROM principal_remote_session_bindings;

-- Each required reference hard-deletes its attachment. Roll each parent delete
-- back independently so every path is exercised with the same intact fixtures.
DO $$
DECLARE statement text;
BEGIN
  FOREACH statement IN ARRAY ARRAY[
    'DELETE FROM projects WHERE id = pg_temp.fixture_id(1)',
    'DELETE FROM agents WHERE id = pg_temp.fixture_id(1)',
    'DELETE FROM user_session_issuers WHERE id = pg_temp.fixture_id(1)',
    'DELETE FROM remote_session_clients WHERE id = pg_temp.fixture_id(1)',
    'DELETE FROM remote_sessions WHERE id = pg_temp.fixture_id(11)',
    'DELETE FROM remote_session_client_user_session_issuers WHERE user_session_issuer_id = pg_temp.fixture_id(1) AND remote_session_client_id = pg_temp.fixture_id(1)'
  ] LOOP
    BEGIN
      PERFORM pg_temp.attach(1, 'fixture-org-a', 1, 1);
      EXECUTE statement;
      IF EXISTS (SELECT FROM principal_remote_session_bindings) THEN
        RAISE EXCEPTION 'Attachment survived %', statement;
      END IF;
      RAISE SQLSTATE 'ZX001';
    EXCEPTION WHEN SQLSTATE 'ZX001' THEN NULL;
    END;
  END LOOP;
END;
$$;

ROLLBACK;
