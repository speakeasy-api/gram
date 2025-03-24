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

CREATE TABLE IF NOT EXISTS organizations (
  id uuid NOT NULL DEFAULT generate_uuidv7(),

  name text NOT NULL,
  slug text NOT NULL,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT organizations_pkey PRIMARY KEY (id),
  CONSTRAINT organizations_slug_key UNIQUE (slug)
);

CREATE TABLE IF NOT EXISTS users (
  id uuid NOT NULL DEFAULT generate_uuidv7(),

  email text NOT NULL,
  verification uuid NOT NULL DEFAULT generate_uuidv7(),
  verified_at timestamptz,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT users_pkey PRIMARY KEY (id),
  CONSTRAINT users_email_key UNIQUE (email)
);

CREATE TABLE IF NOT EXISTS memberships (
  id uuid NOT NULL DEFAULT generate_uuidv7(),

  user_id uuid,
  organization_id uuid,
  role text NOT NULL,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT memberships_pkey PRIMARY KEY (id),
  CONSTRAINT memberships_organization_id_fkey FOREIGN key (organization_id) REFERENCES organizations (id) ON DELETE SET NULL,
  CONSTRAINT memberships_user_id_fkey FOREIGN key (user_id) REFERENCES users (id) ON DELETE SET NULL,
  CONSTRAINT memberships_user_id_organization_id_key UNIQUE (user_id, organization_id, deleted)
);

CREATE TABLE IF NOT EXISTS projects (
  id uuid NOT NULL DEFAULT generate_uuidv7(),

  organization_id uuid,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT projects_pkey PRIMARY KEY (id),
  CONSTRAINT projects_organization_id_fkey FOREIGN key (organization_id) REFERENCES organizations (id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS deployments (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  user_id uuid,
  project_id uuid,
  organization_id uuid,
  manifest_version text NOT NULL,
  manifest_url text NOT NULL,

  github_repo text,
  github_pr text CHECK (
    github_pr != ''
    AND length(github_pr) <= 10
  ),
  external_id text CHECK (
    external_id != ''
    AND length(external_id) <= 80
  ),
  external_url text CHECK (
    external_url != ''
    AND length(external_url) <= 150
  ),

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),

  CONSTRAINT deployments_pkey PRIMARY KEY (id),
  CONSTRAINT deployments_user_id_fkey FOREIGN key (user_id) REFERENCES users (id) ON DELETE SET NULL,
  CONSTRAINT deployments_project_id_fkey FOREIGN key (project_id) REFERENCES projects (id) ON DELETE SET NULL,
  CONSTRAINT deployments_organization_id_fkey FOREIGN key (organization_id) REFERENCES organizations (id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS deployment_statuses (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  seq BIGSERIAL NOT NULL,

  deployment_id uuid,
  status text NOT NULL,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),

  CONSTRAINT deployment_statuses_pkey PRIMARY KEY (id),
  CONSTRAINT deployment_statuses_seq_key UNIQUE (seq),
  CONSTRAINT deployment_statuses_deployment_id_fkey FOREIGN key (deployment_id) REFERENCES deployments (id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS deployment_logs (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  seq BIGSERIAL NOT NULL,

  event text NOT NULL,
  deployment_id uuid,
  project_id uuid,
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
  -- CONSTRAINT deployment_logs_collection_id_fkey FOREIGN key (collection_id) REFERENCES collections (id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS http_tool_definitions (
  id uuid NOT NULL DEFAULT generate_uuidv7(),

  organization_id uuid,
  project_id uuid,
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
  CONSTRAINT http_tool_definitions_organization_id_fkey FOREIGN key (organization_id) REFERENCES organizations (id) ON DELETE SET NULL,
  CONSTRAINT http_tool_definitions_project_id_fkey FOREIGN key (project_id) REFERENCES projects (id) ON DELETE SET NULL
);
