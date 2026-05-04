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

-- Per-mode currentUser. `subject_ref` is mode-specific: a `users.id` for
-- local-speakeasy/oauth2-1/oauth2, a WorkOS `sub` for `workos`. Stored as
-- TEXT with no FK because the workos value is external.
CREATE TABLE IF NOT EXISTS current_users (
  mode TEXT NOT NULL PRIMARY KEY,
  subject_ref TEXT NOT NULL,

  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Short-TTL `/authorize` codes. `client_id` is recorded for inspection only —
-- never validated against any registered list.
CREATE TABLE IF NOT EXISTS auth_codes (
  code TEXT NOT NULL PRIMARY KEY,
  mode TEXT NOT NULL,
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
  mode TEXT NOT NULL,
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
-- WorkOS-emulation tables (consumed by the local-speakeasy mode's
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
