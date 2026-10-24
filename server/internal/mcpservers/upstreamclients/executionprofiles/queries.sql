-- name: EnsureProfile :exec
-- Creates the profile for an assistant, root trigger and execution context
-- unless it exists. The assistant and trigger must belong to the profile's
-- project, which the foreign keys pin.
INSERT INTO execution_selection_profiles (organization_id, project_id, assistant_id, trigger_instance_id, execution_context)
VALUES (@organization_id, @project_id, @assistant_id, @trigger_instance_id, @execution_context)
ON CONFLICT (assistant_id, trigger_instance_id, execution_context) WHERE deleted IS FALSE DO NOTHING;

-- name: LockProfile :one
-- Locks the live profile for an assistant, root trigger and execution context
-- so one turn at a time decides its next version. Its latest version is read
-- by GetProfile after the lock is held. Each statement takes its own snapshot
-- under READ COMMITTED, which Bind runs at, so that read sees the version a
-- turn that held the lock before it committed.
SELECT p.id
FROM execution_selection_profiles AS p
WHERE p.organization_id = @organization_id
  AND p.project_id = @project_id
  AND p.assistant_id = @assistant_id
  AND p.trigger_instance_id = @trigger_instance_id
  AND p.execution_context = @execution_context
  AND p.deleted IS FALSE
FOR UPDATE OF p;

-- name: GetProfile :one
-- The live profile for an assistant, root trigger and execution context, read
-- without a lock, with its latest version: version 0 and the nil id when it
-- has none. A bind reads it once it holds LockProfile; a dry run reads it
-- alone, so it never holds a profile a turn binds.
SELECT p.id,
       COALESCE(v.id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS version_id,
       COALESCE(v.version, 0)::bigint AS version
FROM execution_selection_profiles AS p
LEFT JOIN LATERAL (
  SELECT pv.id, pv.version
  FROM execution_selection_profile_versions AS pv
  WHERE pv.profile_id = p.id
  ORDER BY pv.version DESC
  LIMIT 1
) AS v ON TRUE
WHERE p.organization_id = @organization_id
  AND p.project_id = @project_id
  AND p.assistant_id = @assistant_id
  AND p.trigger_instance_id = @trigger_instance_id
  AND p.execution_context = @execution_context
  AND p.deleted IS FALSE;

-- name: ListProfileServers :many
-- Every proxied (remote or tunneled) live MCP server the assistant reaches,
-- with whether the version already holds a choice for it and the evidence a
-- new choice is decided from. Hosted and unproxied servers keep serving from
-- their own configuration, so they need no choice. A server's upstream OAuth
-- client is the one it is associated with today; its typed auth clients are
-- the live, enabled clients linked to it.
SELECT s.id AS mcp_server_id,
       s.project_id,
       s.requires_upstream_auth,
       EXISTS (
         SELECT 1
         FROM upstream_client_selections AS sel
         WHERE sel.credential_kind = 'execution_profile_version'
           AND sel.credential_id = sqlc.narg('version_id')::uuid
           AND sel.mcp_server_id = s.id
           AND sel.deleted IS FALSE
       )::boolean AS has_choice,
       ARRAY(
         SELECT a.remote_session_client_id
         FROM mcp_server_remote_session_clients AS a
         JOIN remote_session_clients AS c
           ON c.id = a.remote_session_client_id
          AND c.deleted IS FALSE
         WHERE a.mcp_server_id = s.id
           AND a.deleted IS FALSE
         ORDER BY a.remote_session_client_id
       )::uuid[] AS registration_ids,
       ARRAY(
         SELECT ac.id
         FROM mcp_server_auth_clients AS l
         JOIN mcp_auth_clients AS ac
           ON ac.id = l.auth_client_id
          AND ac.organization_id = @organization_id
          AND ac.deleted IS FALSE
          AND ac.enabled
         WHERE l.mcp_server_id = s.id
           AND l.deleted IS FALSE
         ORDER BY ac.id
       )::uuid[] AS auth_client_ids,
       ARRAY(
         SELECT ac.client_type
         FROM mcp_server_auth_clients AS l
         JOIN mcp_auth_clients AS ac
           ON ac.id = l.auth_client_id
          AND ac.organization_id = @organization_id
          AND ac.deleted IS FALSE
          AND ac.enabled
         WHERE l.mcp_server_id = s.id
           AND l.deleted IS FALSE
         ORDER BY ac.id
       )::text[] AS auth_client_types,
       ARRAY(
         SELECT COALESCE(ac.remote_session_client_id, '00000000-0000-0000-0000-000000000000'::uuid)
         FROM mcp_server_auth_clients AS l
         JOIN mcp_auth_clients AS ac
           ON ac.id = l.auth_client_id
          AND ac.organization_id = @organization_id
          AND ac.deleted IS FALSE
          AND ac.enabled
         WHERE l.mcp_server_id = s.id
           AND l.deleted IS FALSE
         ORDER BY ac.id
       )::uuid[] AS auth_client_registration_ids
FROM assistant_mcp_servers AS am
JOIN mcp_servers AS s
  ON s.id = am.mcp_server_id
 AND s.project_id = am.project_id
 AND s.deleted IS FALSE
 AND (s.remote_mcp_server_id IS NOT NULL OR s.tunneled_mcp_server_id IS NOT NULL)
JOIN projects AS p
  ON p.id = s.project_id
 AND p.organization_id = @organization_id
 AND p.deleted IS FALSE
WHERE am.assistant_id = @assistant_id
  AND am.project_id = @project_id
ORDER BY s.id;

-- name: CreateProfileVersion :one
INSERT INTO execution_selection_profile_versions (profile_id, organization_id, project_id, version, source)
VALUES (@profile_id, @organization_id, @project_id, @version, @source)
RETURNING id;

-- name: CopyProfileVersionChoices :exec
-- Carries every choice of one version into the next, so a new version never
-- changes a choice a turn already used.
INSERT INTO upstream_client_selections (
    project_id, organization_id, credential_kind, credential_id, mcp_server_id,
    remote_session_client_id, client_attachment_scope, no_auth,
    auth_client_id, auth_client_attachment_scope, source
)
SELECT sel.project_id, sel.organization_id, sel.credential_kind, @to_version_id::uuid, sel.mcp_server_id,
       sel.remote_session_client_id, sel.client_attachment_scope, sel.no_auth,
       sel.auth_client_id, sel.auth_client_attachment_scope, sel.source
FROM upstream_client_selections AS sel
WHERE sel.credential_kind = 'execution_profile_version'
  AND sel.credential_id = @from_version_id::uuid
  AND sel.organization_id = @organization_id
  AND sel.project_id = @project_id
  AND sel.deleted IS FALSE;

-- name: RecordProfileChoice :execrows
-- Records one version's choice for one server: an auth client, with its
-- upstream OAuth client when it has one, or an explicit no-auth choice. The
-- auth client must still be live, enabled and linked to the server, and the
-- server must still be the organization's.
INSERT INTO upstream_client_selections (
    project_id, organization_id, credential_kind, credential_id, mcp_server_id,
    remote_session_client_id, client_attachment_scope, no_auth,
    auth_client_id, auth_client_attachment_scope, source
)
SELECT s.project_id,
       p.organization_id,
       'execution_profile_version',
       @version_id::uuid,
       s.id,
       reg.id,
       reg.attachment_scope,
       ac.id IS NULL,
       ac.id,
       ac.attachment_scope,
       @source::text
FROM mcp_servers AS s
JOIN projects AS p
  ON p.id = s.project_id
 AND p.organization_id = @organization_id
LEFT JOIN mcp_auth_clients AS ac
  ON ac.id = sqlc.narg('auth_client_id')::uuid
 AND ac.organization_id = p.organization_id
 AND ac.deleted IS FALSE
 AND ac.enabled
 AND EXISTS (
   SELECT 1
   FROM mcp_server_auth_clients AS l
   WHERE l.mcp_server_id = s.id
     AND l.auth_client_id = ac.id
     AND l.deleted IS FALSE
 )
LEFT JOIN remote_session_clients AS reg
  ON reg.id = ac.remote_session_client_id
 AND reg.deleted IS FALSE
WHERE s.id = @mcp_server_id
  AND s.deleted IS FALSE
  AND (sqlc.narg('auth_client_id')::uuid IS NULL OR ac.id IS NOT NULL)
ON CONFLICT (credential_kind, credential_id, mcp_server_id) WHERE deleted IS FALSE DO NOTHING;

-- name: ListProfileTargets :many
-- Every live assistant and root trigger pair a turn can run under in the
-- organization: each trigger bound to an assistant's workload identity.
SELECT DISTINCT b.project_id,
       ab.assistant_id,
       b.trigger_id
FROM trigger_workload_bindings AS b
JOIN assistant_agent_bindings AS ab
  ON ab.id = b.assistant_binding_id
 AND ab.deleted IS FALSE
 AND ab.assistant_id IS NOT NULL
JOIN assistants AS a
  ON a.id = ab.assistant_id
 AND a.project_id = b.project_id
 AND a.deleted IS FALSE
JOIN trigger_instances AS t
  ON t.id = b.trigger_id
 AND t.deleted IS FALSE
WHERE b.organization_id = @organization_id
  AND b.deleted IS FALSE
  AND b.trigger_id IS NOT NULL
ORDER BY b.project_id, ab.assistant_id, b.trigger_id;

-- name: ListAssistantProfileTargets :many
-- The root triggers one assistant's turns run under: each trigger bound to
-- its workload identity, and any trigger it already has a profile for.
SELECT t.id AS trigger_id,
       t.name AS trigger_name
FROM trigger_instances AS t
WHERE t.organization_id = @organization_id
  AND t.project_id = @project_id
  AND t.deleted IS FALSE
  AND (
    EXISTS (
      SELECT 1
      FROM trigger_workload_bindings AS b
      JOIN assistant_agent_bindings AS ab
        ON ab.id = b.assistant_binding_id
       AND ab.deleted IS FALSE
      WHERE b.trigger_id = t.id
        AND b.deleted IS FALSE
        AND ab.assistant_id = @assistant_id::uuid
    )
    OR EXISTS (
      SELECT 1
      FROM execution_selection_profiles AS p
      WHERE p.trigger_instance_id = t.id
        AND p.assistant_id = @assistant_id::uuid
        AND p.deleted IS FALSE
    )
  )
ORDER BY t.name, t.id;

-- name: ListVersionChoices :many
-- A version's choices: an auth client, or no upstream authentication.
SELECT sel.mcp_server_id,
       sel.auth_client_id,
       sel.no_auth,
       sel.source
FROM upstream_client_selections AS sel
WHERE sel.credential_kind = 'execution_profile_version'
  AND sel.credential_id = @version_id
  AND sel.organization_id = @organization_id
  AND sel.deleted IS FALSE
ORDER BY sel.mcp_server_id;

-- name: CopyProfileVersionChoicesExcept :exec
-- Carries every choice of one version into the next but one server's, which
-- an administrator is replacing or clearing.
INSERT INTO upstream_client_selections (
    project_id, organization_id, credential_kind, credential_id, mcp_server_id,
    remote_session_client_id, client_attachment_scope, no_auth,
    auth_client_id, auth_client_attachment_scope, source
)
SELECT sel.project_id, sel.organization_id, sel.credential_kind, @to_version_id::uuid, sel.mcp_server_id,
       sel.remote_session_client_id, sel.client_attachment_scope, sel.no_auth,
       sel.auth_client_id, sel.auth_client_attachment_scope, sel.source
FROM upstream_client_selections AS sel
WHERE sel.credential_kind = 'execution_profile_version'
  AND sel.credential_id = @from_version_id::uuid
  AND sel.organization_id = @organization_id
  AND sel.project_id = @project_id
  AND sel.mcp_server_id <> @except_mcp_server_id::uuid
  AND sel.deleted IS FALSE;

-- Test fixtures. Each seeds the one row its name says; nothing else uses them.

-- name: SeedOrganizationFixture :exec
INSERT INTO organization_metadata (id, name, slug) VALUES (@id, @id, @id);

-- name: SeedProjectFixture :exec
INSERT INTO projects (id, name, slug, organization_id) VALUES (@id, @slug, @slug, @organization_id);

-- name: SeedAssistantFixture :one
INSERT INTO assistants (organization_id, project_id, name, model, instructions)
VALUES (@organization_id, @project_id, @name, 'fixture-model', 'fixture')
RETURNING id;

-- name: SeedTriggerFixture :one
INSERT INTO trigger_instances (organization_id, project_id, definition_slug, name, target_kind, target_ref, target_display)
VALUES (@organization_id, @project_id, 'fixture', @name, 'assistant', 'fixture', 'fixture')
RETURNING id;

-- name: SeedRemoteMCPServerFixture :exec
INSERT INTO remote_mcp_servers (id, project_id, transport_type, url) VALUES (@id, @project_id, 'streamable-http', @url);

-- name: SeedMCPServerFixture :exec
INSERT INTO mcp_servers (id, project_id, slug, remote_mcp_server_id, visibility, requires_upstream_auth)
VALUES (@id, @project_id, @slug, @remote_mcp_server_id, 'private', sqlc.narg('requires_upstream_auth'));

-- name: SeedAssistantServerFixture :exec
INSERT INTO assistant_mcp_servers (assistant_id, mcp_server_id, project_id) VALUES (@assistant_id, @mcp_server_id, @project_id);

-- name: SeedRemoteSessionIssuerFixture :exec
INSERT INTO remote_session_issuers (id, project_id, slug, issuer) VALUES (@id, @project_id, @slug, @issuer);

-- name: SeedRemoteSessionClientFixture :exec
INSERT INTO remote_session_clients (id, project_id, organization_id, remote_session_issuer_id, client_id)
VALUES (@id, @project_id, @organization_id, @remote_session_issuer_id, @client_id);

-- name: SeedAssociationFixture :exec
INSERT INTO mcp_server_remote_session_clients (project_id, organization_id, mcp_server_id, remote_session_client_id, client_attachment_scope)
VALUES (
    @project_id, @organization_id, @mcp_server_id, @remote_session_client_id,
    (SELECT c.attachment_scope FROM remote_session_clients AS c WHERE c.id = @remote_session_client_id)
);

-- name: SeedAuthClientFixture :one
INSERT INTO mcp_auth_clients (organization_id, project_id, client_type, source, name, remote_session_client_id, client_attachment_scope)
VALUES (
    @organization_id, @project_id, @client_type, 'backfill', @client_type,
    sqlc.narg('remote_session_client_id'),
    (SELECT c.attachment_scope FROM remote_session_clients AS c WHERE c.id = sqlc.narg('remote_session_client_id'))
)
RETURNING id;

-- name: LinkAuthClientFixture :exec
INSERT INTO mcp_server_auth_clients (project_id, organization_id, mcp_server_id, auth_client_id, client_attachment_scope, source)
SELECT @project_id, @organization_id, @mcp_server_id, ac.id, ac.attachment_scope, 'backfill'
FROM mcp_auth_clients AS ac
WHERE ac.id = @auth_client_id;

-- name: ListVersionChoicesFixture :many
SELECT mcp_server_id, auth_client_id, remote_session_client_id, no_auth
FROM upstream_client_selections
WHERE credential_kind = 'execution_profile_version' AND credential_id = @version_id AND deleted IS FALSE
ORDER BY mcp_server_id;

-- name: SeedWorkloadBindingFixture :exec
WITH ab AS (
  INSERT INTO assistant_agent_bindings (
    organization_id, project_id, project_ref_organization_id, project_ref_id,
    original_assistant_id, assistant_ref_organization_id, assistant_ref_project_id, assistant_id,
    original_agent_id, generation
  )
  VALUES (@organization_id, @project_id, @organization_id, @project_id, @assistant_id, @organization_id, @project_id, @assistant_id, generate_uuidv7(), 1)
  RETURNING id
)
INSERT INTO trigger_workload_bindings (
  organization_id, project_id, project_ref_organization_id, project_ref_id,
  original_trigger_id, trigger_ref_organization_id, trigger_ref_project_id, trigger_id,
  original_assistant_binding_id, assistant_binding_ref_organization_id, assistant_binding_ref_project_id, assistant_binding_id,
  assistant_binding_generation, original_workload_issuer_id, subject, generation
)
SELECT @organization_id, @project_id, @organization_id, @project_id,
       @trigger_id, @organization_id, @project_id, @trigger_id,
       ab.id, @organization_id, @project_id, ab.id,
       1, generate_uuidv7(), 'workload', 1
FROM ab;

-- name: DisableAuthClientFixture :exec
UPDATE mcp_auth_clients SET enabled = FALSE WHERE id = @id AND organization_id = @organization_id;

-- name: SeedTriggerBindingFixture :exec
-- Binds a trigger to an assistant's existing agent binding.
INSERT INTO trigger_workload_bindings (
  organization_id, project_id, project_ref_organization_id, project_ref_id,
  original_trigger_id, trigger_ref_organization_id, trigger_ref_project_id, trigger_id,
  original_assistant_binding_id, assistant_binding_ref_organization_id, assistant_binding_ref_project_id, assistant_binding_id,
  assistant_binding_generation, original_workload_issuer_id, subject, generation
)
SELECT @organization_id, @project_id, @organization_id, @project_id,
       @trigger_id, @organization_id, @project_id, @trigger_id,
       ab.id, @organization_id, @project_id, ab.id,
       ab.generation, generate_uuidv7(), 'workload', 1
FROM assistant_agent_bindings AS ab
WHERE ab.assistant_id = @assistant_id AND ab.deleted IS FALSE;
