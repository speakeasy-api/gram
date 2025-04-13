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
  name VARCHAR(40) NOT NULL,
  slug VARCHAR(40) NOT NULL,

  organization_id TEXT NOT NULL,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT projects_pkey PRIMARY KEY (id)
);

CREATE UNIQUE INDEX IF NOT EXISTS projects_organization_id_slug_key
ON projects (organization_id, slug)
WHERE deleted IS FALSE;

CREATE TABLE IF NOT EXISTS deployments (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  seq BIGSERIAL NOT NULL, -- Use this to serialize the processing of deployments. Tools will be created from the latest deployment.
  user_id TEXT NOT NULL,
  project_id uuid NOT NULL,
  organization_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  cloned_from uuid,

  github_repo VARCHAR(100),
  github_pr VARCHAR(100),
  github_sha VARCHAR(100),
  external_id VARCHAR(100),
  external_url VARCHAR(100),

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),

  CONSTRAINT deployments_pkey PRIMARY KEY (id),
  CONSTRAINT deployments_project_id_idempotency_key UNIQUE (project_id, idempotency_key),
  CONSTRAINT deployments_seq_key UNIQUE (seq),
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
  message text NOT NULL,
  deployment_id uuid NOT NULL,
  project_id uuid NOT NULL,

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

  name VARCHAR(100) NOT NULL,
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

  name VARCHAR(40) NOT NULL,
  token TEXT NOT NULL,
  scopes TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT api_keys_pkey PRIMARY KEY (id),
  CONSTRAINT api_keys_token_key UNIQUE (token),
  CONSTRAINT api_keys_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE SET NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS api_keys_organization_id_name_key
ON api_keys (organization_id, name)
WHERE deleted IS FALSE;

CREATE TABLE IF NOT EXISTS deployments_openapiv3_assets (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  deployment_id uuid NOT NULL,
  asset_id uuid NOT NULL,
  name VARCHAR(60) NOT NULL,
  slug VARCHAR(60) NOT NULL,

  CONSTRAINT deployments_openapiv3_documents_pkey PRIMARY KEY (id),
  CONSTRAINT deployments_openapiv3_documents_deployment_id_fkey FOREIGN key (deployment_id) REFERENCES deployments (id) ON DELETE CASCADE,
  CONSTRAINT deployments_openapiv3_documents_asset_id_fkey FOREIGN key (asset_id) REFERENCES assets (id) ON DELETE CASCADE,
  CONSTRAINT deployments_openapiv3_documents_deployment_id_slug_key UNIQUE (deployment_id, slug)
);

CREATE TABLE IF NOT EXISTS http_tool_definitions (
  id uuid NOT NULL DEFAULT generate_uuidv7(),

  project_id uuid NOT NULL,
  deployment_id uuid NOT NULL,

  openapiv3_document_id uuid,

  name VARCHAR(100) NOT NULL,
  summary TEXT NOT NULL,
  description TEXT NOT NULL,
  openapiv3_operation VARCHAR(100),
  tags VARCHAR(40)[] NOT NULL DEFAULT ARRAY[]::VARCHAR(40)[],

  server_env_var TEXT NOT NULL,
  default_server_url TEXT,
  security jsonb,

  http_method VARCHAR(20) NOT NULL,
  path VARCHAR(140) NOT NULL,
  schema_version VARCHAR(20) NOT NULL,
  schema JSONB,
  header_settings JSONB,
  query_settings JSONB,
  path_settings JSONB,
  request_content_type TEXT,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT http_tool_definitions_pkey PRIMARY KEY (id),
  CONSTRAINT http_tool_definitions_deployment_id_fkey FOREIGN key (deployment_id) REFERENCES deployments (id) ON DELETE CASCADE,
  CONSTRAINT http_tool_definitions_openapiv3_document_id_fkey FOREIGN key (openapiv3_document_id) REFERENCES deployments_openapiv3_assets (id) ON DELETE RESTRICT,
  CONSTRAINT http_tool_definitions_project_id_fkey FOREIGN key (project_id) REFERENCES projects (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS http_tool_definitions_name_idx ON http_tool_definitions (name);

CREATE TABLE IF NOT EXISTS environments (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  organization_id TEXT NOT NULL,
  project_id uuid NOT NULL,
  name VARCHAR(40) NOT NULL,
  slug VARCHAR(40) NOT NULL,
  description VARCHAR(100),

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT environments_pkey PRIMARY KEY (id),
  CONSTRAINT environments_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE SET NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS environments_project_id_slug_key
ON environments (project_id, slug)
WHERE deleted IS FALSE;

CREATE TABLE IF NOT EXISTS environment_entries (
  name VARCHAR(60) NOT NULL,
  value VARCHAR(4000) NOT NULL,
  environment_id uuid NOT NULL,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT environments_entries_pkey PRIMARY KEY (environment_id, name),
  CONSTRAINT environments_entries_environment_id_fkey FOREIGN KEY (environment_id) REFERENCES environments (id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS toolsets (
  id uuid NOT NULL DEFAULT generate_uuidv7(),

  organization_id TEXT NOT NULL,
  project_id uuid NOT NULL,
  name VARCHAR(40) NOT NULL,
  slug VARCHAR(40) NOT NULL,
  description VARCHAR(100),
  default_environment_slug VARCHAR(40),
  http_tool_names VARCHAR(100)[],

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT toolsets_pkey PRIMARY KEY (id),
  CONSTRAINT toolsets_project_id_fkey FOREIGN key (project_id) REFERENCES projects (id) ON DELETE SET NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS toolsets_project_id_slug_key
ON toolsets (project_id, slug)
WHERE deleted IS FALSE;

CREATE TABLE IF NOT EXISTS http_security (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  
  key VARCHAR(60) NOT NULL,
  deployment_id uuid NOT NULL,
  type VARCHAR(20) NULL,
  name VARCHAR(60) NOT NULL,
  in_placement VARCHAR(10) NOT NULL, -- header, query, path
  scheme VARCHAR(20),
  bearer_format VARCHAR(20),

  env_variables VARCHAR(60)[] DEFAULT ARRAY[]::VARCHAR(60)[],
  
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,
  
  CONSTRAINT http_security_pkey PRIMARY KEY (id),
  CONSTRAINT http_security_deployment_id_fkey FOREIGN KEY (deployment_id) REFERENCES deployments (id) ON DELETE CASCADE,
  CONSTRAINT http_security_key_unique UNIQUE (deployment_id, key)
);