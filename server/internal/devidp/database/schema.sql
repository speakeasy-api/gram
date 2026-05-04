-- dev-idp Postgres schema. Declarative SDL — applied via
-- `mise db:devidp:apply` (atlas schema apply). No migration files are ever
-- emitted for this database. See idp-design.md §5 / §5.4 for the design.
--
-- This database is dev-only, isolated from the production Gram database. Do
-- NOT point `mise db:devidp:apply` at any non-local Postgres — atlas
-- declarative apply will reshape it to match this file.

CREATE TABLE IF NOT EXISTS users (
  id uuid NOT NULL DEFAULT gen_random_uuid(),
  email TEXT NOT NULL,
  display_name TEXT NOT NULL,
  photo_url TEXT,
  github_handle TEXT,
  admin boolean NOT NULL DEFAULT FALSE,
  whitelisted boolean NOT NULL DEFAULT TRUE,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),

  CONSTRAINT users_pkey PRIMARY KEY (id)
);

CREATE UNIQUE INDEX IF NOT EXISTS users_email_key ON users (email);

CREATE TABLE IF NOT EXISTS organizations (
  id uuid NOT NULL DEFAULT gen_random_uuid(),
  name TEXT NOT NULL,
  slug TEXT NOT NULL,
  account_type TEXT NOT NULL DEFAULT 'free',
  workos_id TEXT,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),

  CONSTRAINT organizations_pkey PRIMARY KEY (id)
);

CREATE UNIQUE INDEX IF NOT EXISTS organizations_slug_key ON organizations (slug);

CREATE TABLE IF NOT EXISTS memberships (
  id uuid NOT NULL DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  organization_id uuid NOT NULL,
  role TEXT NOT NULL DEFAULT 'member',

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),

  CONSTRAINT memberships_pkey PRIMARY KEY (id),
  CONSTRAINT memberships_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT memberships_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS memberships_user_id_organization_id_key
  ON memberships (user_id, organization_id);

-- Per-mode currentUser. `subject_ref` is mode-specific (idp-design.md §3):
-- a `users.id` UUID for `local-speakeasy`/`oauth2-1`/`oauth2`, a WorkOS
-- `sub` for `workos`. Stored as TEXT with no FK because the workos value
-- is external. Cascading deletes for local users are handled in the
-- service layer.
CREATE TABLE IF NOT EXISTS current_users (
  mode TEXT NOT NULL,
  subject_ref TEXT NOT NULL,

  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),

  CONSTRAINT current_users_pkey PRIMARY KEY (mode)
);

-- Short-TTL `/authorize` codes. `client_id` is recorded for inspection only
-- (idp-design.md §5.2) — never validated against any registered list.
CREATE TABLE IF NOT EXISTS auth_codes (
  code TEXT NOT NULL,
  mode TEXT NOT NULL,
  user_id uuid NOT NULL,
  client_id TEXT NOT NULL,
  redirect_uri TEXT NOT NULL,
  code_challenge TEXT,
  code_challenge_method TEXT,
  scope TEXT,
  expires_at timestamptz NOT NULL,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),

  CONSTRAINT auth_codes_pkey PRIMARY KEY (code),
  CONSTRAINT auth_codes_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS auth_codes_expires_at_idx ON auth_codes (expires_at);

-- Issued tokens (access / refresh / id). Opaque random strings looked up by
-- value (idp-design.md §5.3). `client_id` recorded for inspection only.
CREATE TABLE IF NOT EXISTS tokens (
  token TEXT NOT NULL,
  mode TEXT NOT NULL,
  user_id uuid NOT NULL,
  client_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  scope TEXT,
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),

  CONSTRAINT tokens_pkey PRIMARY KEY (token),
  CONSTRAINT tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS tokens_user_id_idx ON tokens (user_id);
CREATE INDEX IF NOT EXISTS tokens_expires_at_idx ON tokens (expires_at);
