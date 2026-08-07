-- Fixture schema covering every tenancy shape the rule distinguishes.

-- project_id NOT NULL: project_id is required, organization_id never suffices.
CREATE TABLE toolsets (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES projects (id),
    name text NOT NULL
);

-- Both columns NOT NULL: project_id is still the requirement on its own, and a
-- second organization_id bind is optional narrowing.
CREATE TABLE deployments (
    id uuid PRIMARY KEY,
    organization_id text NOT NULL,
    project_id uuid NOT NULL REFERENCES projects (id)
);

-- organization_id NOT NULL, no project_id.
CREATE TABLE projects (
    id uuid PRIMARY KEY,
    organization_id text NOT NULL,
    slug text NOT NULL
);

-- organization_id NOT NULL with a nullable project_id: organization_id is the
-- guaranteed bound, so binding only project_id would drop the NULL rows.
CREATE TABLE api_keys (
    id uuid PRIMARY KEY,
    organization_id text NOT NULL,
    project_id uuid,
    key_hash text NOT NULL
);

-- Both nullable: neither is guaranteed, so either one is accepted.
CREATE TABLE external_credentials (
    id uuid PRIMARY KEY,
    organization_id text,
    project_id uuid
);

-- project_id nullable and no organization_id: the sole candidate is required.
CREATE TABLE chat_messages (
    id uuid PRIMARY KEY,
    project_id uuid,
    body text NOT NULL
);

-- No tenancy column, but a foreign key reaches one.
CREATE TABLE toolset_versions (
    id uuid PRIMARY KEY,
    toolset_id uuid NOT NULL REFERENCES toolsets (id),
    version integer NOT NULL
);

-- No tenancy column and no path to one: global.
CREATE TABLE global_roles (
    id uuid PRIMARY KEY,
    slug text NOT NULL
);

CREATE TABLE mcp_registries (
    id uuid PRIMARY KEY,
    name text NOT NULL
);

-- A foreign key cycle must not hang the classifier.
CREATE TABLE cycle_a (
    id uuid PRIMARY KEY,
    b_id uuid REFERENCES cycle_b (id)
);

CREATE TABLE cycle_b (
    id uuid PRIMARY KEY,
    a_id uuid REFERENCES cycle_a (id)
);
