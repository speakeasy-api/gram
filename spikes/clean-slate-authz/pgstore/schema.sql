DROP TABLE IF EXISTS authz_relationships, authz_revisions, spike_agents, spike_api_keys, spike_chats CASCADE;

CREATE TABLE authz_relationships (
  id uuid NOT NULL PRIMARY KEY,
  organization_id TEXT NOT NULL,
  object_type TEXT NOT NULL,
  object_id TEXT NOT NULL,
  relation TEXT NOT NULL,
  subject_type TEXT NOT NULL,
  subject_id TEXT NOT NULL,
  subject_relation TEXT NOT NULL DEFAULT '',
  condition_name TEXT,
  condition_params JSONB,
  expires_at timestamptz,
  source TEXT NOT NULL,
  source_ref TEXT,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT authz_relationships_condition_check CHECK ((condition_name IS NULL) = (condition_params IS NULL))
);

CREATE INDEX authz_relationships_subject_idx
ON authz_relationships (organization_id, subject_type, subject_id, subject_relation);

CREATE UNIQUE INDEX authz_relationships_object_key
ON authz_relationships (organization_id, object_type, object_id, relation,
                        subject_type, subject_id, subject_relation,
                        COALESCE(condition_name, ''), COALESCE(condition_params, 'null'::jsonb));

CREATE TABLE authz_revisions (
  organization_id TEXT NOT NULL PRIMARY KEY,
  revision BIGINT NOT NULL DEFAULT 0
);

-- Statement-level: one bump per statement, covering inserts, updates, deletes
-- and cascades. Application code cannot forget it.
CREATE OR REPLACE FUNCTION authz_bump_revision() RETURNS trigger AS $$
BEGIN
  INSERT INTO authz_revisions (organization_id, revision)
  SELECT DISTINCT organization_id, 1 FROM changed
  ON CONFLICT (organization_id) DO UPDATE SET revision = authz_revisions.revision + 1;
  RETURN NULL;
END $$ LANGUAGE plpgsql;

CREATE TRIGGER authz_relationships_bump_ins AFTER INSERT ON authz_relationships
REFERENCING NEW TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION authz_bump_revision();
CREATE TRIGGER authz_relationships_bump_upd AFTER UPDATE ON authz_relationships
REFERENCING NEW TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION authz_bump_revision();
CREATE TRIGGER authz_relationships_bump_del AFTER DELETE ON authz_relationships
REFERENCING OLD TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION authz_bump_revision();

-- Stand-ins for domain tables. Their facts are read live, never copied.
CREATE TABLE spike_agents (
  id TEXT PRIMARY KEY, organization_id TEXT NOT NULL, project_id TEXT NOT NULL,
  owner_user_id TEXT NOT NULL, status TEXT NOT NULL
);
CREATE TABLE spike_api_keys (
  id TEXT PRIMARY KEY, organization_id TEXT NOT NULL, agent_id TEXT NOT NULL,
  revoked_at timestamptz, expires_at timestamptz
);
CREATE TABLE spike_chats (
  id TEXT PRIMARY KEY, organization_id TEXT NOT NULL, project_id TEXT NOT NULL,
  user_id TEXT, agent_id TEXT REFERENCES spike_agents (id)
);
