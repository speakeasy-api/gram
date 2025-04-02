-- 🚨
-- 🚨 READ .cursor/rules/database-design.mdc BEFORE EDITING THIS FILE
-- 🚨

-- https://gist.github.com/kjmph/5bd772b2c2df145aa645b837da7eca74
create or replace function generate_uuidv7()
returns uuid
as $$
begin
  -- use random v4 uuid as starting point (which has the same variant we need)
  -- then overlay timestamp
  -- then set version 7 by flipping the 2 and 1 bit in the version 4 string
  return encode(
    set_bit(
      set_bit(
        overlay(uuid_send(gen_random_uuid())
                placing substring(int8send(floor(extract(epoch from clock_timestamp()) * 1000)::bigint) from 3)
                from 1 for 6
        ),
        52, 1
      ),
      53, 1
    ),
    'hex')::uuid;
end
$$
language plpgsql
volatile;

CREATE TABLE IF NOT EXISTS projects (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  name text NOT NULL,
  slug text NOT NULL,

  organization_id TEXT NOT NULL,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT projects_pkey PRIMARY KEY (id),
  CONSTRAINT projects_organization_id_slug_key UNIQUE (organization_id, slug)
);

CREATE TABLE IF NOT EXISTS deployments (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  user_id TEXT NOT NULL,
  project_id uuid NOT NULL,
  organization_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,

  github_repo TEXT,
  github_pr TEXT,
  github_sha TEXT,
  external_id TEXT,
  external_url TEXT,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),

  CONSTRAINT deployments_pkey PRIMARY KEY (id),
  CONSTRAINT deployments_project_id_idempotency_key UNIQUE (project_id, idempotency_key),
  CONSTRAINT deployments_project_id_fkey FOREIGN key (project_id) REFERENCES projects (id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS deployment_statuses (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  seq BIGSERIAL NOT NULL,

  deployment_id uuid NOT NULL,
  status text NOT NULL,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),

  CONSTRAINT deployment_statuses_pkey PRIMARY KEY (id),
  CONSTRAINT deployment_statuses_seq_key UNIQUE (seq)
);

CREATE TABLE IF NOT EXISTS deployment_logs (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  seq BIGSERIAL NOT NULL,

  event text NOT NULL,
  deployment_id uuid NOT NULL,
  project_id uuid NOT NULL,
  tooltemplate_id uuid,
  tooltemplate_type text CHECK (
    -- Cannot be null if tooltemplate_id is not null
    (tooltemplate_id IS NULL) OR (tooltemplate_type IS NOT NULL)
  ),
  collection_id uuid,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),

  CONSTRAINT deployment_logs_pkey PRIMARY KEY (id),
  CONSTRAINT deployment_logs_seq_key UNIQUE (seq),
  CONSTRAINT deployment_logs_deployment_id_fkey FOREIGN key (deployment_id) REFERENCES deployments (id) ON DELETE SET NULL,
  CONSTRAINT deployment_logs_project_id_fkey FOREIGN key (project_id) REFERENCES projects (id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS assets (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  project_id uuid NOT NULL,

  name text NOT NULL,
  url text NOT NULL,
  kind text NOT NULL,
  content_type text NOT NULL,
  content_length bigint NOT NULL,
  sha256 text NOT NULL,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT assets_pkey PRIMARY KEY (id),
  CONSTRAINT assets_project_id_sha256_key UNIQUE (project_id, sha256)
);

CREATE TABLE IF NOT EXISTS api_keys (
  id uuid NOT NULL DEFAULT generate_uuidv7(),

  organization_id TEXT NOT NULL,
  project_id uuid,
  created_by_user_id TEXT NOT NULL,

  name TEXT NOT NULL,
  token TEXT NOT NULL,
  scopes TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT api_keys_pkey PRIMARY KEY (id),
  CONSTRAINT api_keys_token_key UNIQUE (token),
  CONSTRAINT api_keys_organization_id_name_key UNIQUE (organization_id, name),
  CONSTRAINT api_keys_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS deployments_openapiv3_assets (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  deployment_id uuid NOT NULL,
  asset_id uuid NOT NULL,
  name text NOT NULL,
  slug text NOT NULL,

  CONSTRAINT deployments_openapiv3_documents_pkey PRIMARY KEY (id),
  CONSTRAINT deployments_openapiv3_documents_deployment_id_fkey FOREIGN key (deployment_id) REFERENCES deployments (id) ON DELETE CASCADE,
  CONSTRAINT deployments_openapiv3_documents_asset_id_fkey FOREIGN key (asset_id) REFERENCES assets (id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS openapiv3_documents (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  project_id uuid NOT NULL,
  deployment_id uuid,
  asset_id uuid NOT NULL,
  name text NOT NULL,
  slug text NOT NULL,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT openapiv3_documents_pkey PRIMARY KEY (id),
  CONSTRAINT openapiv3_documents_project_id_slug_key UNIQUE (project_id, slug),
  CONSTRAINT openapiv3_documents_deployment_id_fkey FOREIGN key (deployment_id) REFERENCES deployments (id) ON DELETE SET NULL,
  CONSTRAINT openapiv3_documents_asset_id_fkey FOREIGN key (asset_id) REFERENCES assets (id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS http_tool_definitions (
  id uuid NOT NULL DEFAULT generate_uuidv7(),

  organization_id TEXT NOT NULL,
  project_id uuid NOT NULL,

  openapiv3_document_id uuid,

  name text NOT NULL,
  description text NOT NULL,

  server_env_var text NOT NULL,
  security_type text NOT NULL CHECK (
    security_type IN ('http:bearer', 'http:basic', 'apikey')
  ),
  bearer_env_var text,
  apikey_env_var text,
  username_env_var text,
  password_env_var text,

  http_method text NOT NULL,
  path text NOT NULL,
  headers_schema jsonb,
  queries_schema jsonb,
  pathparams_schema jsonb,
  body_schema jsonb,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT http_tool_definitions_pkey PRIMARY KEY (id),
  CONSTRAINT http_tool_definitions_project_id_name_key UNIQUE (project_id, name),
  CONSTRAINT http_tool_definitions_openapiv3_document_id_fkey FOREIGN key (openapiv3_document_id) REFERENCES openapiv3_documents (id) ON DELETE RESTRICT,
  CONSTRAINT http_tool_definitions_project_id_fkey FOREIGN key (project_id) REFERENCES projects (id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS environments (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  organization_id TEXT NOT NULL,
  project_id uuid NOT NULL,
  name text NOT NULL,
  slug text NOT NULL,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT environments_pkey PRIMARY KEY (id),
  CONSTRAINT environments_project_id_slug_key UNIQUE (project_id, slug),
  CONSTRAINT environments_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS environment_entries (
  name text NOT NULL,
  value text NOT NULL,
  environment_id uuid NOT NULL,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,
  CONSTRAINT environments_entries_pkey PRIMARY KEY (environment_id, name),
  CONSTRAINT environments_entries_environment_id_fkey FOREIGN KEY (environment_id) REFERENCES environments (id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS toolsets (
  id uuid NOT NULL DEFAULT generate_uuidv7(),

  organization_id TEXT NOT NULL,
  project_id uuid NOT NULL,
  name text NOT NULL,
  slug text NOT NULL,
  description text,
  default_environment_id uuid,
  http_tool_ids uuid[],

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT toolsets_pkey PRIMARY KEY (id),
  CONSTRAINT toolsets_project_id_fkey FOREIGN key (project_id) REFERENCES projects (id) ON DELETE SET NULL,
  CONSTRAINT toolsets_project_id_slug_key UNIQUE (project_id, slug),
  CONSTRAINT toolsets_default_environment_id_fkey FOREIGN key (default_environment_id) REFERENCES environments (id) ON DELETE SET NULL
);