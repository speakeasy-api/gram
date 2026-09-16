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
VALUES (pg_temp.fixture_id(1), 'Fixture', 'fixture', 'fixture-org-a');
INSERT INTO identity_provider_connections (id, organization_id, provider) VALUES
  (pg_temp.fixture_id(1), 'fixture-org-a', 'okta'),
  (pg_temp.fixture_id(2), 'fixture-org-b', 'okta');

-- Two org-A issuers, a project issuer with BOTH ownership columns populated,
-- an org-B issuer, and a global issuer.
INSERT INTO remote_session_issuers (id, project_id, organization_id, slug, issuer)
SELECT pg_temp.fixture_id(n), CASE WHEN n = 3 THEN pg_temp.fixture_id(1) END,
  CASE WHEN n <= 3 THEN 'fixture-org-a' WHEN n = 4 THEN 'fixture-org-b' END,
  'fixture-' || n, 'https://issuer-' || n || '.example.invalid'
FROM generate_series(1, 5) AS n;
INSERT INTO remote_session_clients (id, project_id, organization_id, remote_session_issuer_id, client_id)
SELECT pg_temp.fixture_id(n), CASE WHEN n = 3 THEN pg_temp.fixture_id(1) END,
  CASE WHEN n IN (4, 9) THEN 'fixture-org-b' WHEN n <> 5 THEN 'fixture-org-a' END,
  pg_temp.fixture_id(CASE n WHEN 2 THEN 2 WHEN 6 THEN 3 WHEN 7 THEN 4 WHEN 8 THEN 5 WHEN 9 THEN 4 ELSE 1 END),
  'fixture-' || n
FROM generate_series(1, 9) AS n;

CREATE FUNCTION pg_temp.attach(i integer, c integer, connection integer DEFAULT 1, org text DEFAULT 'fixture-org-a', url text DEFAULT NULL, reason text DEFAULT NULL)
RETURNS void LANGUAGE sql AS $$
  INSERT INTO okta_identity_provider_connections
    (identity_provider_connection_id, organization_id, org_url, issuer_url,
     remote_session_issuer_id, remote_session_client_id, issuer_url_override_reason)
  VALUES (pg_temp.fixture_id(connection), org, 'https://org.example.invalid',
    COALESCE(url, 'https://issuer-' || i || '.example.invalid'), pg_temp.fixture_id(i), pg_temp.fixture_id(c), reason);
$$;

-- Reject project, other-org, and global issuers even when their selected
-- client is org-A-owned and correctly references that issuer.
SELECT pg_temp.expect_integrity_error(format('SELECT pg_temp.attach(%s, %s)', i, c),
  'okta_identity_provider_connections_issuer_scope_fkey')
FROM (VALUES (3, 6), (4, 7), (5, 8)) AS cases(i, c);
-- Reject wrong-issuer, project (with organization_id populated), other-org,
-- and global clients. Each case isolates the client FK from the issuer FK.
SELECT pg_temp.expect_integrity_error(format('SELECT pg_temp.attach(1, %s)', c),
  'okta_identity_provider_connections_client_issuer_scope_fkey')
FROM (VALUES (2), (3), (4), (5)) AS cases(c);
-- Connection tenant pinning remains enforced independently of remote scope.
SELECT pg_temp.expect_integrity_error($q$SELECT pg_temp.attach(4, 9, 1, 'fixture-org-b')$q$,
  'okta_identity_provider_connections_connection_tenant_fkey');

-- A caller cannot claim another URL for an otherwise valid pair.
SELECT pg_temp.expect_integrity_error(
  $q$SELECT pg_temp.attach(1, 1, 1, 'fixture-org-a', 'https://wrong.example.invalid')$q$,
  'okta_identity_provider_connections_issuer_scope_fkey');

-- A valid org-owned pair is accepted.
SELECT pg_temp.attach(1, 1);

-- The subtype cannot be switched to a project/global/other-org issuer pair.
SELECT pg_temp.expect_integrity_error(format(
  'UPDATE okta_identity_provider_connections SET remote_session_issuer_id = pg_temp.fixture_id(%s), remote_session_client_id = pg_temp.fixture_id(%s), issuer_url = %L WHERE identity_provider_connection_id = pg_temp.fixture_id(1)', i, c, 'https://issuer-' || i || '.example.invalid'),
  'okta_identity_provider_connections_issuer_scope_fkey')
FROM (VALUES (3, 6), (4, 7), (5, 8)) AS cases(i, c);
SELECT pg_temp.expect_integrity_error(format(
  'UPDATE okta_identity_provider_connections SET remote_session_client_id = pg_temp.fixture_id(%s) WHERE identity_provider_connection_id = pg_temp.fixture_id(1)', c),
  'okta_identity_provider_connections_client_issuer_scope_fkey')
FROM (VALUES (2), (3), (4), (5)) AS cases(c);
SELECT pg_temp.expect_integrity_error(
  $q$UPDATE okta_identity_provider_connections SET remote_session_issuer_id = pg_temp.fixture_id(2), issuer_url = 'https://issuer-2.example.invalid' WHERE identity_provider_connection_id = pg_temp.fixture_id(1)$q$,
  'okta_identity_provider_connections_client_issuer_scope_fkey');

-- Referenced parents cannot migrate to a project, another org, or global
-- scope. Updating the client's issuer must not silently break the pair either.
SELECT pg_temp.expect_integrity_error(
  'UPDATE remote_session_clients SET remote_session_issuer_id = pg_temp.fixture_id(2) WHERE id = pg_temp.fixture_id(1)',
  'okta_identity_provider_connections_client_issuer_scope_fkey');
SELECT pg_temp.expect_integrity_error(
  'UPDATE remote_session_clients SET ' || assignment || ' WHERE id = pg_temp.fixture_id(1)',
  'okta_identity_provider_connections_client_issuer_scope_fkey')
FROM (VALUES ('project_id = pg_temp.fixture_id(1)'),
  ($q$organization_id = 'fixture-org-b'$q$), ('organization_id = NULL')) AS cases(assignment);
SELECT pg_temp.expect_integrity_error(
  'UPDATE remote_session_issuers SET ' || assignment || ' WHERE id = pg_temp.fixture_id(1)',
  'okta_identity_provider_connections_issuer_scope_fkey')
FROM (VALUES ('project_id = pg_temp.fixture_id(1)'),
  ($q$organization_id = 'fixture-org-b'$q$), ('organization_id = NULL')) AS cases(assignment);

-- Neither side can change the pinned URL independently.
SELECT pg_temp.expect_integrity_error(
  $q$UPDATE remote_session_issuers SET issuer = 'https://changed.example.invalid' WHERE id = pg_temp.fixture_id(1)$q$,
  'okta_identity_provider_connections_issuer_scope_fkey');
SELECT pg_temp.expect_integrity_error(
  $q$UPDATE okta_identity_provider_connections SET issuer_url = 'https://changed.example.invalid' WHERE identity_provider_connection_id = pg_temp.fixture_id(1)$q$,
  'okta_identity_provider_connections_issuer_scope_fkey');
-- Empty and whitespace-only overrides are invalid on UPDATE too.
SELECT pg_temp.expect_integrity_error(format(
  'UPDATE okta_identity_provider_connections SET issuer_url_override_reason = %L WHERE identity_provider_connection_id = pg_temp.fixture_id(1)', reason),
  'okta_identity_provider_connections_override_reason_check')
FROM (VALUES (''), ('   '), (E'\t\n\r ')) AS cases(reason);

-- Switching the subtype to another valid pair is allowed. An unreferenced
-- client's issuer may still be changed: this is not a blanket mutation ban.
UPDATE okta_identity_provider_connections
SET remote_session_issuer_id = pg_temp.fixture_id(2), remote_session_client_id = pg_temp.fixture_id(2),
    issuer_url = 'https://issuer-2.example.invalid'
WHERE identity_provider_connection_id = pg_temp.fixture_id(1);
UPDATE remote_session_clients SET remote_session_issuer_id = pg_temp.fixture_id(2)
WHERE id = pg_temp.fixture_id(1);

-- A second org can connect to a distinct issuer without an override.
SELECT pg_temp.attach(4, 9, 2, 'fixture-org-b');
DELETE FROM okta_identity_provider_connections WHERE identity_provider_connection_id = pg_temp.fixture_id(2);

-- The same upstream URL in two orgs requires a meaningful override. Duplicate
-- issuer rows are allowed; the subtype owns the cross-org uniqueness policy.
INSERT INTO remote_session_issuers (id, organization_id, slug, issuer)
VALUES (pg_temp.fixture_id(6), 'fixture-org-b', 'fixture-6', 'https://issuer-2.example.invalid');
INSERT INTO remote_session_clients (id, organization_id, remote_session_issuer_id, client_id)
VALUES (pg_temp.fixture_id(10), 'fixture-org-b', pg_temp.fixture_id(6), 'fixture-10');
SELECT pg_temp.expect_integrity_error(
  $q$SELECT pg_temp.attach(6, 10, 2, 'fixture-org-b', 'https://issuer-2.example.invalid')$q$,
  'okta_identity_provider_connections_issuer_url_key');
SELECT pg_temp.expect_integrity_error(format(
  'SELECT pg_temp.attach(6, 10, 2, %L, %L, %L)', 'fixture-org-b', 'https://issuer-2.example.invalid', reason),
  'okta_identity_provider_connections_override_reason_check')
FROM (VALUES (''), ('   '), (E'\t\n\r ')) AS cases(reason);
SELECT pg_temp.attach(6, 10, 2, 'fixture-org-b', 'https://issuer-2.example.invalid', E' \tApproved fixture override\n');

ROLLBACK;
