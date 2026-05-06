-- /schemas/postgres.sql
--
-- Postgres SDL for the new usersessions / remotesessions surface.
-- See spike.md §4 for rationale.
--
-- Tables/columns this RFC removes from server/database/schema.sql (each migration
-- tracked as a ticket in project.md):
--
--   - oauth_proxy_servers                              -> user_session_issuers
--   - oauth_proxy_providers                            -> remote_session_issuers + remote_session_clients
--   - oauth_proxy_providers.secrets JSONB              (deprecated; structured columns instead)
--   - oauth_proxy_providers.security_key_names         (deprecated)
--   - oauth_proxy_providers.provider_type='custom'     (deprecated; behaviour collapses onto passthrough flag)
--   - external_oauth_server_metadata                   -> remote_session_issuer with passthrough=true
--   - toolsets.external_oauth_server_id                -> toolsets.user_session_issuer_id
--   - toolsets.oauth_proxy_server_id                   -> toolsets.user_session_issuer_id
--   - toolsets_oauth_exclusivity (CHECK)               (no longer needed)
--   - oauth_proxy_client_info                          -> user_session_clients

-- ---------------------------------------------------------------------------
-- User session issuers - the Gram-side AS configuration.
-- One per logical "thing that issues user sessions for a toolset."
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS user_session_issuers (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  project_id uuid NOT NULL,

  slug TEXT NOT NULL,
  authn_challenge_mode TEXT NOT NULL,  -- 'chain' | 'interactive' (enforced at app layer)
  session_duration INTERVAL NOT NULL,  -- policy: user_sessions.expires_at = created_at + session_duration

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT user_session_issuers_pkey PRIMARY KEY (id),
  CONSTRAINT user_session_issuers_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS user_session_issuers_project_slug_key
ON user_session_issuers (project_id, slug)
WHERE deleted IS FALSE;

-- ---------------------------------------------------------------------------
-- User session clients - DCR registry for MCP clients registering with Gram-as-AS.
-- Successor to legacy oauth_proxy_client_info. Symmetric counterpart to
-- remote_session_clients (which holds Gram's credentials at upstream issuers).
-- Fields dropped relative to legacy: grant_types, response_types, scope,
-- token_endpoint_auth_method, application_type — these are issuer-level policy
-- resolved at /authorize and /token time, not per-client state.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS user_session_clients (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  user_session_issuer_id uuid NOT NULL,

  client_id TEXT NOT NULL,                                    -- DCR-issued (RFC 7591)
  client_secret_hash TEXT,                                    -- bcrypt or equivalent; nullable for public PKCE clients
  client_name TEXT NOT NULL,
  redirect_uris TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],      -- validated on every /authorize

  client_id_issued_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  client_secret_expires_at timestamptz,                       -- nullable: null = doesn't expire (RFC 7591 expires_at=0 semantic)

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT user_session_clients_pkey PRIMARY KEY (id),
  CONSTRAINT user_session_clients_user_session_issuer_id_fkey FOREIGN KEY (user_session_issuer_id) REFERENCES user_session_issuers (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS user_session_clients_issuer_client_id_key
ON user_session_clients (user_session_issuer_id, client_id)
WHERE deleted IS FALSE;

-- ---------------------------------------------------------------------------
-- Remote session issuers - upstream Authorization Server identity records.
-- Successor to oauth_proxy_provider; behavioural diff from
-- external_oauth_server_metadata collapses onto the passthrough flag.
-- TODO: project_id is here for now but ultimately we want shared issuer
-- configurations across projects.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS remote_session_issuers (
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

  CONSTRAINT remote_session_issuers_pkey PRIMARY KEY (id),
  CONSTRAINT remote_session_issuers_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS remote_session_issuers_project_slug_key
ON remote_session_issuers (project_id, slug)
WHERE deleted IS FALSE;

-- ---------------------------------------------------------------------------
-- Remote session clients - the credentials Gram presents to a remote_session_issuer.
-- Jump-table edge between remote_session_issuer and user_session_issuer.
-- One issuer can have many clients in the schema; in initial scope we use 1:1.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS remote_session_clients (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  project_id uuid NOT NULL,
  remote_session_issuer_id uuid NOT NULL,
  user_session_issuer_id uuid NOT NULL,

  client_id TEXT NOT NULL,
  client_secret_encrypted TEXT,                               -- nullable for PKCE-only public clients
  client_id_issued_at timestamptz,
  client_secret_expires_at timestamptz,                       -- nullable for non-expiring secrets

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT remote_session_clients_pkey PRIMARY KEY (id),
  CONSTRAINT remote_session_clients_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE CASCADE,
  CONSTRAINT remote_session_clients_remote_session_issuer_id_fkey FOREIGN KEY (remote_session_issuer_id) REFERENCES remote_session_issuers (id) ON DELETE CASCADE,
  CONSTRAINT remote_session_clients_user_session_issuer_id_fkey FOREIGN KEY (user_session_issuer_id) REFERENCES user_session_issuers (id) ON DELETE CASCADE
);

-- ---------------------------------------------------------------------------
-- User session consent records.
-- Persistent record that a given principal has consented for a given
-- user_session_issuer to access ALL of its remote_session_tokens.
-- The /authorize endpoint may skip the consent prompt only when a matching
-- consent record exists.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS user_session_consents (
  id uuid NOT NULL DEFAULT generate_uuidv7(),

  principal_urn TEXT NOT NULL,                                -- user:<id> | apikey:<uuid> | anonymous:<mcp-session-id>
  user_session_client_id uuid NOT NULL,                       -- per-client; granting consent to MCP client X does NOT grant it to MCP client Y
  remote_set_hash TEXT NOT NULL,                              -- SHA-256 of sorted remote_session_issuer_id list (resolved via the client's issuer) at consent time

  consented_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT user_session_consents_pkey PRIMARY KEY (id),
  CONSTRAINT user_session_consents_user_session_client_id_fkey FOREIGN KEY (user_session_client_id) REFERENCES user_session_clients (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS user_session_consents_principal_client_set_key
ON user_session_consents (principal_urn, user_session_client_id, remote_set_hash)
WHERE deleted IS FALSE;

-- ---------------------------------------------------------------------------
-- User sessions - the durable issued user session.
-- Created lazily at /token exchange. Lookup at /token is by refresh_token_hash.
-- Bookkeeping ("what active sessions does this principal have at this issuer?")
-- is a (principal_urn, user_session_issuer_id) query.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS user_sessions (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  user_session_issuer_id uuid NOT NULL,
  principal_urn TEXT NOT NULL,                                -- user:<id> | apikey:<uuid> | anonymous:<mcp-session-id>
  jti TEXT NOT NULL,                                          -- current access-token JTI
  refresh_token_hash TEXT NOT NULL,                           -- SHA-256 of the refresh token; never persisted in the clear
  refresh_expires_at timestamptz NOT NULL,                    -- next refresh deadline; need not align with expires_at, but must be <= expires_at
  expires_at timestamptz NOT NULL,                            -- terminal session expiry; ceiling on refresh_expires_at; set from user_session_issuers.session_duration

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT user_sessions_pkey PRIMARY KEY (id),
  CONSTRAINT user_sessions_user_session_issuer_id_fkey FOREIGN KEY (user_session_issuer_id) REFERENCES user_session_issuers (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS user_sessions_refresh_token_hash_key
ON user_sessions (refresh_token_hash)
WHERE deleted IS FALSE;

CREATE INDEX IF NOT EXISTS user_sessions_principal_idx
ON user_sessions (principal_urn, user_session_issuer_id)
WHERE deleted IS FALSE;

-- ---------------------------------------------------------------------------
-- Remote sessions - the durable upstream OAuth session per (principal, remote_session_client).
-- Holds upstream access + refresh tokens with INDEPENDENT expiries. Created
-- when a remote auth dance completes; refreshed silently on the access-expiry path.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS remote_sessions (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  principal_urn TEXT NOT NULL,
  user_session_issuer_id uuid NOT NULL,
  remote_session_client_id uuid NOT NULL,                       -- one client implies one issuer

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
  CONSTRAINT remote_sessions_user_session_issuer_id_fkey FOREIGN KEY (user_session_issuer_id) REFERENCES user_session_issuers (id) ON DELETE CASCADE,
  CONSTRAINT remote_sessions_remote_session_client_id_fkey FOREIGN KEY (remote_session_client_id) REFERENCES remote_session_clients (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS remote_sessions_principal_client_key
ON remote_sessions (principal_urn, remote_session_client_id)
WHERE deleted IS FALSE;

-- ---------------------------------------------------------------------------
-- Toolset link to user_session_issuer.
-- Replaces toolsets.external_oauth_server_id and toolsets.oauth_proxy_server_id
-- (and the toolsets_oauth_exclusivity CHECK that gated them).
-- mcp_servers mirrors this column whenever its runtime migration lands (per spike.md §3.5).
-- ---------------------------------------------------------------------------

ALTER TABLE toolsets
  ADD COLUMN IF NOT EXISTS user_session_issuer_id uuid;

ALTER TABLE toolsets
  ADD CONSTRAINT toolsets_user_session_issuer_id_fkey
  FOREIGN KEY (user_session_issuer_id) REFERENCES user_session_issuers (id) ON DELETE SET NULL;
