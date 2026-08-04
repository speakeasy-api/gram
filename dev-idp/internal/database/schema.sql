-- dev-idp SQLite schema. Applied at app boot via internal/bootstrap. Every
-- statement is idempotent (CREATE TABLE / CREATE INDEX IF NOT EXISTS) so
-- re-applying on every start is a no-op once the schema is in place.

CREATE TABLE IF NOT EXISTS users (
  id TEXT NOT NULL PRIMARY KEY,
  email TEXT NOT NULL,
  display_name TEXT NOT NULL,
  photo_url TEXT,
  github_handle TEXT,
  admin INTEGER NOT NULL DEFAULT 0,
  whitelisted INTEGER NOT NULL DEFAULT 1,

  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS users_email_key ON users (email);

CREATE TABLE IF NOT EXISTS organizations (
  id TEXT NOT NULL PRIMARY KEY,
  name TEXT NOT NULL,
  slug TEXT NOT NULL,
  account_type TEXT NOT NULL DEFAULT 'enterprise',
  workos_id TEXT,
  -- The caller's own identifier for this organization, echoed back on every
  -- organization response. Gram writes its organization id here and then reads
  -- it back through the WorkOS event sync.
  external_id TEXT,

  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS organizations_slug_key ON organizations (slug);

CREATE TABLE IF NOT EXISTS memberships (
  id TEXT NOT NULL PRIMARY KEY,
  user_id TEXT NOT NULL,
  organization_id TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'admin',

  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

  FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS memberships_user_id_organization_id_key
  ON memberships (user_id, organization_id);

-- currentUser per identity slot. `subject_ref` is slot-specific: a `users.id`
-- for the `oauth2-1` slot, an external WorkOS `sub` for the `workos` slot.
-- Which slot is authoritative follows GRAM_DEVIDP_BACKEND. Stored as TEXT
-- with no FK because the workos value is external.
CREATE TABLE IF NOT EXISTS current_users (
  mode TEXT NOT NULL PRIMARY KEY,
  subject_ref TEXT NOT NULL,

  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Dynamically registered OAuth 2.1 clients. The dev-idp only needs enough
-- state to reject /authorize redirect_uri values that were not registered.
--
-- `rotate_refresh_tokens` defaults on (OAuth 2.1 recommends rotation). Clients
-- register with it off to emulate upstreams that reuse refresh tokens, which
-- Gram has to tolerate in the wild.
CREATE TABLE IF NOT EXISTS oauth_clients (
  client_id TEXT NOT NULL PRIMARY KEY,
  client_secret TEXT NOT NULL,
  redirect_uris TEXT NOT NULL DEFAULT '[]',
  rotate_refresh_tokens INTEGER NOT NULL DEFAULT 1,

  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Short-TTL `/authorize` codes.
CREATE TABLE IF NOT EXISTS auth_codes (
  code TEXT NOT NULL PRIMARY KEY,
  user_id TEXT NOT NULL,
  client_id TEXT NOT NULL,
  redirect_uri TEXT NOT NULL,
  code_challenge TEXT,
  code_challenge_method TEXT,
  scope TEXT,
  expires_at DATETIME NOT NULL,

  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

  FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS auth_codes_expires_at_idx ON auth_codes (expires_at);

-- Issued tokens (access / refresh / id). Opaque random strings looked up by
-- value. `client_id` recorded for inspection only.
CREATE TABLE IF NOT EXISTS tokens (
  token TEXT NOT NULL PRIMARY KEY,
  user_id TEXT NOT NULL,
  client_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  scope TEXT,
  expires_at DATETIME NOT NULL,
  revoked_at DATETIME,

  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

  FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS tokens_user_id_idx ON tokens (user_id);
CREATE INDEX IF NOT EXISTS tokens_expires_at_idx ON tokens (expires_at);

-- =============================================================================
-- WorkOS-emulation tables (consumed by the local backend's
-- /user_management/* and /authorization/organizations/* endpoints).
-- =============================================================================

-- Invitations mirror the WorkOS user_management invitation lifecycle:
-- pending / accepted / revoked / expired. Local dev never delivers the
-- invite email; tests progress invitations by hitting the dashboard's
-- accept-flow UI.
CREATE TABLE IF NOT EXISTS invitations (
  id TEXT NOT NULL PRIMARY KEY,
  email TEXT NOT NULL,
  organization_id TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending',
  token TEXT NOT NULL,
  inviter_user_id TEXT,

  accepted_at DATETIME,
  revoked_at DATETIME,
  expires_at DATETIME NOT NULL,

  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

  FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE CASCADE,
  FOREIGN KEY (inviter_user_id) REFERENCES users (id) ON DELETE SET NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS invitations_token_key ON invitations (token);
CREATE INDEX IF NOT EXISTS invitations_organization_id_idx ON invitations (organization_id);
CREATE INDEX IF NOT EXISTS invitations_email_idx ON invitations (email);

-- Per-org roles. Mirrors WorkOS's authorization role surface.
-- (admin, member) seed by default; tests can add more.
CREATE TABLE IF NOT EXISTS organization_roles (
  id TEXT NOT NULL PRIMARY KEY,
  organization_id TEXT NOT NULL,
  slug TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',

  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

  FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS organization_roles_organization_id_slug_key
  ON organization_roles (organization_id, slug);

-- =============================================================================
-- Enterprise-Managed Authorization (EMA) tables. Two independent policy surfaces:
--
--   * mint side (the IdP) -- ema_apps + ema_resources + ema_app_assignments
--     decide whether the oauth2-1 server will issue an ID-JAG at all;
--   * redeem side (each resource authorization server) -- ema_trust_rules
--     decide whether an ID-JAG that was issued somewhere is accepted here.
--
-- They are deliberately separate: an ID-JAG this dev-idp minted can still be
-- rejected at redemption, and a resource can be configured to trust a foreign
-- issuer this dev-idp never mints for. Both are things worth testing.
-- =============================================================================

-- Requesting apps: the clients allowed to ask the IdP for an ID-JAG on a
-- user's behalf. `client_id` is the id they authenticate to /token with.
--
-- How an app authenticates follows from which credential column is set, so
-- there is no separate auth-method discriminator to keep in sync:
--
--   jwks set           -> private_key_jwt, the method real enterprise IdPs
--                         mandate for this exchange. A JWKS document holding
--                         the app's public key; it signs a client assertion.
--   client_secret set  -> client_secret_post.
--   neither            -> a public client, authenticating by client_id alone.
--
-- jwks wins when both are set, so an app can be migrated to private_key_jwt
-- without a window where its old secret still works.
CREATE TABLE IF NOT EXISTS ema_apps (
  id TEXT NOT NULL PRIMARY KEY,
  client_id TEXT NOT NULL,
  client_secret TEXT NOT NULL DEFAULT '',
  jwks TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,

  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS ema_apps_client_id_key ON ema_apps (client_id);

-- Resource apps. One row is one resource authorization server, mounted at
-- /resource-as/<slug>, guarding the MCP server named by `resource_identifier`.
-- The slug-derived issuer URL is what an ID-JAG carries in `aud`;
-- `resource_identifier` is what it carries in `resource`.
CREATE TABLE IF NOT EXISTS ema_resources (
  id TEXT NOT NULL PRIMARY KEY,
  slug TEXT NOT NULL,
  name TEXT NOT NULL,
  resource_identifier TEXT NOT NULL,

  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS ema_resources_slug_key ON ema_resources (slug);

-- Which user may drive which app against which resource, and for what scopes.
-- The absence of a row IS the denial -- there is no disabled state here, so
-- "revoke this user's access" is a delete.
CREATE TABLE IF NOT EXISTS ema_app_assignments (
  id TEXT NOT NULL PRIMARY KEY,
  app_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  resource_id TEXT NOT NULL,
  granted_scopes TEXT NOT NULL DEFAULT '',

  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

  FOREIGN KEY (app_id) REFERENCES ema_apps (id) ON DELETE CASCADE,
  FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  FOREIGN KEY (resource_id) REFERENCES ema_resources (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS ema_app_assignments_app_user_resource_key
  ON ema_app_assignments (app_id, user_id, resource_id);

CREATE INDEX IF NOT EXISTS ema_app_assignments_user_id_idx
  ON ema_app_assignments (user_id);

-- Trust domain rules. `trusted_issuer` is an ID-JAG `iss` value the resource
-- accepts; the dev-idp's own oauth2-1 issuer is just one possible value, so a
-- resource can be pointed at a foreign IdP. `allowed_client_ids` is a JSON
-- array ('[]' means any client) and `allowed_scopes` is a space-delimited
-- ceiling ('' means no ceiling) applied on top of whatever the ID-JAG carries.
CREATE TABLE IF NOT EXISTS ema_trust_rules (
  id TEXT NOT NULL PRIMARY KEY,
  resource_id TEXT NOT NULL,
  trusted_issuer TEXT NOT NULL,
  allowed_client_ids TEXT NOT NULL DEFAULT '[]',
  allowed_scopes TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,

  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

  FOREIGN KEY (resource_id) REFERENCES ema_resources (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS ema_trust_rules_resource_id_trusted_issuer_key
  ON ema_trust_rules (resource_id, trusted_issuer);

-- Ledger of every ID-JAG this IdP minted. Inspection only -- the dashboard
-- reads it to show what policy actually allowed. Replay is enforced by
-- ema_redeemed_jags, not here, because a resource may accept ID-JAGs this
-- dev-idp never issued.
CREATE TABLE IF NOT EXISTS ema_issued_jags (
  jti TEXT NOT NULL PRIMARY KEY,
  app_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  resource_id TEXT NOT NULL,
  scope TEXT NOT NULL DEFAULT '',
  expires_at DATETIME NOT NULL,

  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

  FOREIGN KEY (app_id) REFERENCES ema_apps (id) ON DELETE CASCADE,
  FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  FOREIGN KEY (resource_id) REFERENCES ema_resources (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS ema_issued_jags_user_id_idx ON ema_issued_jags (user_id);
CREATE INDEX IF NOT EXISTS ema_issued_jags_expires_at_idx ON ema_issued_jags (expires_at);

-- Redemption ledger, keyed by (issuer, jti) so an ID-JAG is single-use no
-- matter which issuer minted it. An insert that conflicts IS the replay
-- signal; there is no separate lookup.
CREATE TABLE IF NOT EXISTS ema_redeemed_jags (
  issuer TEXT NOT NULL,
  jti TEXT NOT NULL,
  resource_id TEXT NOT NULL,
  expires_at DATETIME NOT NULL,

  redeemed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

  PRIMARY KEY (issuer, jti),
  FOREIGN KEY (resource_id) REFERENCES ema_resources (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS ema_redeemed_jags_expires_at_idx
  ON ema_redeemed_jags (expires_at);

-- Access tokens issued by a resource authorization server. Deliberately not
-- the `tokens` table: these are minted by a different authorization server,
-- under a different grant, and are audience-restricted to one MCP server --
-- so `audience` is a column here rather than something inferred. Keeping them
-- apart also means the whole EMA surface drops in one piece.
--
-- The token itself is a signed JWT and is not stored; the row is keyed on its
-- `jti`. A verifier does not need this table at all -- that is the point of
-- issuing a JWT -- so what it is for is revocation and showing an operator
-- what was issued.
CREATE TABLE IF NOT EXISTS ema_resource_tokens (
  jti TEXT NOT NULL PRIMARY KEY,
  resource_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  client_id TEXT NOT NULL,
  audience TEXT NOT NULL,
  scope TEXT NOT NULL DEFAULT '',
  expires_at DATETIME NOT NULL,
  revoked_at DATETIME,

  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

  FOREIGN KEY (resource_id) REFERENCES ema_resources (id) ON DELETE CASCADE,
  FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS ema_resource_tokens_user_id_idx
  ON ema_resource_tokens (user_id);
CREATE INDEX IF NOT EXISTS ema_resource_tokens_expires_at_idx
  ON ema_resource_tokens (expires_at);
