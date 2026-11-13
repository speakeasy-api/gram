-- Option B keeps today's tables. These mirror them (x_ prefix) plus the one
-- additive change the plan needs: validity columns on principal_grants.
DROP TABLE IF EXISTS x_principal_grants, x_role_assignments, x_directory_memberships,
  x_directory_role_mappings, x_agents, x_api_keys, x_chats CASCADE;

CREATE TABLE x_principal_grants (
  id TEXT PRIMARY KEY,
  organization_id TEXT NOT NULL,
  principal_urn TEXT NOT NULL,
  scope TEXT NOT NULL,
  selectors JSONB NOT NULL,
  valid_from timestamptz,   -- new (D3)
  valid_until timestamptz,  -- new (D3), end-exclusive
  created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX x_principal_grants_principal_idx ON x_principal_grants (organization_id, principal_urn);

CREATE TABLE x_role_assignments (organization_id TEXT NOT NULL, user_id TEXT NOT NULL, role_urn TEXT NOT NULL);
CREATE INDEX x_role_assignments_user_idx ON x_role_assignments (organization_id, user_id);
CREATE TABLE x_directory_memberships (organization_id TEXT NOT NULL, user_id TEXT NOT NULL, group_id TEXT NOT NULL);
CREATE INDEX x_directory_memberships_user_idx ON x_directory_memberships (organization_id, user_id);
CREATE TABLE x_directory_role_mappings (organization_id TEXT NOT NULL, group_id TEXT NOT NULL, role_urn TEXT NOT NULL);

CREATE TABLE x_agents (
  id TEXT PRIMARY KEY, organization_id TEXT NOT NULL, project_id TEXT NOT NULL,
  owner_user_id TEXT NOT NULL, status TEXT NOT NULL
);
CREATE INDEX x_agents_owner_idx ON x_agents (organization_id, owner_user_id);
CREATE TABLE x_api_keys (
  id TEXT PRIMARY KEY, organization_id TEXT NOT NULL, agent_id TEXT NOT NULL,
  revoked_at timestamptz, expires_at timestamptz,
  delegated_grants JSONB NOT NULL  -- stored credential policy, as api_keys.delegated_grants today
);
CREATE TABLE x_chats (
  id TEXT PRIMARY KEY, organization_id TEXT NOT NULL, project_id TEXT NOT NULL,
  user_id TEXT, agent_id TEXT
);
