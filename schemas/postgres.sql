-- /schemas/postgres.sql
--
-- Postgres SDL for the new clientsessions / remotesessions surface.
-- See spike.md §4 for rationale.
--
-- Tables/columns this RFC removes from server/database/schema.sql (each migration
-- tracked as a ticket in project.md):
--
--   - oauth_proxy_servers                              -> client_session_issuers
--   - oauth_proxy_providers                            -> remote_oauth_issuers + remote_oauth_clients
--   - oauth_proxy_providers.secrets JSONB              (deprecated; structured columns instead)
--   - oauth_proxy_providers.security_key_names         (deprecated)
--   - oauth_proxy_providers.provider_type='custom'     (deprecated; behaviour collapses onto passthrough flag)
--   - external_oauth_server_metadata                   -> remote_oauth_issuer with passthrough=true
--   - toolsets.external_oauth_server_id                -> toolsets.client_session_issuer_id
--   - toolsets.oauth_proxy_server_id                   -> toolsets.client_session_issuer_id
--   - toolsets_oauth_exclusivity (CHECK)               (no longer needed)
--   - oauth_proxy_client_info                          (TBD: rename to client_session_dcr_registrations)

-- ---------------------------------------------------------------------------
-- Client session issuers - the Gram-side AS configuration.
-- One per logical "thing that issues client sessions for a toolset."
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS client_session_issuers (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  project_id uuid NOT NULL,

  slug TEXT NOT NULL,
  challenge_mode TEXT NOT NULL,  -- 'chain' | 'interactive' (enforced at app layer)

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT client_session_issuers_pkey PRIMARY KEY (id),
  CONSTRAINT client_session_issuers_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS client_session_issuers_project_slug_key
ON client_session_issuers (project_id, slug)
WHERE deleted IS FALSE;

-- ---------------------------------------------------------------------------
-- Remote OAuth issuers - upstream Authorization Server identity records.
-- Successor to oauth_proxy_provider; behavioural diff from
-- external_oauth_server_metadata collapses onto the passthrough flag.
-- TODO: project_id is here for now but ultimately we want shared issuer
-- configurations across projects.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS remote_oauth_issuers (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  project_id uuid NOT NULL,

  slug TEXT NOT NULL,
  issuer TEXT NOT NULL,                                       -- AS issuer URL; matches `iss` claim
  authorization_endpoint TEXT,
  token_endpoint TEXT,
  registration_endpoint TEXT,                                 -- nullable; absent for issuers without DCR
  jwks_uri TEXT,                                              -- nullable

  scopes_supported TEXT[] DEFAULT ARRAY[]::TEXT[],
  grant_types_supported TEXT[] DEFAULT ARRAY[]::TEXT[],
  response_types_supported TEXT[] DEFAULT ARRAY[]::TEXT[],
  token_endpoint_auth_methods_supported TEXT[] DEFAULT ARRAY[]::TEXT[],

  oidc BOOLEAN NOT NULL DEFAULT FALSE,                        -- true may unlock OIDC-aware behaviour
  passthrough BOOLEAN NOT NULL DEFAULT FALSE,                 -- when true, the MCP client transacts directly with this issuer

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT remote_oauth_issuers_pkey PRIMARY KEY (id),
  CONSTRAINT remote_oauth_issuers_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS remote_oauth_issuers_project_slug_key
ON remote_oauth_issuers (project_id, slug)
WHERE deleted IS FALSE;

-- ---------------------------------------------------------------------------
-- Remote OAuth clients - the credentials Gram presents to a remote_oauth_issuer.
-- Jump-table edge between remote_oauth_issuer and client_session_issuer.
-- One issuer can have many clients in the schema; in initial scope we use 1:1.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS remote_oauth_clients (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  project_id uuid NOT NULL,
  remote_oauth_issuer_id uuid NOT NULL,
  client_session_issuer_id uuid NOT NULL,

  client_id TEXT NOT NULL,
  client_secret_encrypted TEXT,                               -- nullable for PKCE-only public clients
  client_id_issued_at timestamptz,
  client_secret_expires_at timestamptz,                       -- nullable for non-expiring secrets

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT remote_oauth_clients_pkey PRIMARY KEY (id),
  CONSTRAINT remote_oauth_clients_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE CASCADE,
  CONSTRAINT remote_oauth_clients_remote_oauth_issuer_id_fkey FOREIGN KEY (remote_oauth_issuer_id) REFERENCES remote_oauth_issuers (id) ON DELETE CASCADE,
  CONSTRAINT remote_oauth_clients_client_session_issuer_id_fkey FOREIGN KEY (client_session_issuer_id) REFERENCES client_session_issuers (id) ON DELETE CASCADE
);

-- ---------------------------------------------------------------------------
-- Client session consent records.
-- Persistent record that a given user has consented for a given
-- client_session_issuer to access ALL of its remote_session_tokens.
-- The /authorize endpoint may skip the consent prompt only when a matching
-- consent record exists.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS client_session_consents (
  id uuid NOT NULL DEFAULT generate_uuidv7(),

  user_id TEXT NOT NULL,                                      -- user-scoped, NOT principal URN; consent persists across sessions
  client_session_issuer_id uuid NOT NULL,
  remote_set_hash TEXT NOT NULL,                              -- SHA-256 of sorted remote_oauth_issuer_id list at consent time

  consented_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT client_session_consents_pkey PRIMARY KEY (id),
  CONSTRAINT client_session_consents_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT client_session_consents_client_session_issuer_id_fkey FOREIGN KEY (client_session_issuer_id) REFERENCES client_session_issuers (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS client_session_consents_user_issuer_set_key
ON client_session_consents (user_id, client_session_issuer_id, remote_set_hash)
WHERE deleted IS FALSE;

-- ---------------------------------------------------------------------------
-- Client sessions - the durable issued client session.
-- Created lazily at /token exchange. Lookup at /token is by refresh_token_hash.
-- Bookkeeping ("what active sessions does this principal have at this issuer?")
-- is a (principal_urn, client_session_issuer_id) query.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS client_sessions (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  client_session_issuer_id uuid NOT NULL,
  principal_urn TEXT NOT NULL,                                -- user:<id> | apikey:<uuid> | anonymous:<mcp-session-id>
  jti TEXT NOT NULL,                                          -- current access-token JTI
  refresh_token_hash TEXT NOT NULL,                           -- SHA-256 of the refresh token; never persisted in the clear
  refresh_expires_at timestamptz NOT NULL,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT client_sessions_pkey PRIMARY KEY (id),
  CONSTRAINT client_sessions_client_session_issuer_id_fkey FOREIGN KEY (client_session_issuer_id) REFERENCES client_session_issuers (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS client_sessions_refresh_token_hash_key
ON client_sessions (refresh_token_hash)
WHERE deleted IS FALSE;

CREATE INDEX IF NOT EXISTS client_sessions_principal_idx
ON client_sessions (principal_urn, client_session_issuer_id)
WHERE deleted IS FALSE;

-- ---------------------------------------------------------------------------
-- Remote sessions - the durable upstream OAuth session per (principal, remote_oauth_client).
-- Holds upstream access + refresh tokens with INDEPENDENT expiries. Created
-- when a remote auth dance completes; refreshed silently on the access-expiry path.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS remote_sessions (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  principal_urn TEXT NOT NULL,
  client_session_issuer_id uuid NOT NULL,
  remote_oauth_client_id uuid NOT NULL,                       -- one client implies one issuer

  access_token_encrypted TEXT NOT NULL,
  access_expires_at timestamptz NOT NULL,                     -- independent of refresh expiry
  refresh_token_encrypted TEXT,
  refresh_expires_at timestamptz,
  scopes TEXT[] DEFAULT ARRAY[]::TEXT[],

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT remote_sessions_pkey PRIMARY KEY (id),
  CONSTRAINT remote_sessions_client_session_issuer_id_fkey FOREIGN KEY (client_session_issuer_id) REFERENCES client_session_issuers (id) ON DELETE CASCADE,
  CONSTRAINT remote_sessions_remote_oauth_client_id_fkey FOREIGN KEY (remote_oauth_client_id) REFERENCES remote_oauth_clients (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS remote_sessions_principal_client_key
ON remote_sessions (principal_urn, remote_oauth_client_id)
WHERE deleted IS FALSE;

-- ---------------------------------------------------------------------------
-- Toolset link to client_session_issuer.
-- Replaces toolsets.external_oauth_server_id and toolsets.oauth_proxy_server_id
-- (and the toolsets_oauth_exclusivity CHECK that gated them).
-- mcp_servers mirrors this column whenever its runtime migration lands (per spike.md §3.5).
-- ---------------------------------------------------------------------------

ALTER TABLE toolsets
  ADD COLUMN IF NOT EXISTS client_session_issuer_id uuid;

ALTER TABLE toolsets
  ADD CONSTRAINT toolsets_client_session_issuer_id_fkey
  FOREIGN KEY (client_session_issuer_id) REFERENCES client_session_issuers (id) ON DELETE SET NULL;
