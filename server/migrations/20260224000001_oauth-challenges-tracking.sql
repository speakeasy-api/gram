-- OAuth challenges tracking for billing and analytics
-- Records each OAuth authorization flow (challenge) initiated via the DCR proxy.
-- Used for tracking usage per organization for enterprise billing.
CREATE TABLE IF NOT EXISTS oauth_challenges (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  organization_id TEXT NOT NULL,
  project_id uuid NOT NULL,
  user_id TEXT NOT NULL,
  toolset_id uuid NOT NULL,

  -- OAuth provider information
  oauth_server_issuer TEXT NOT NULL,
  provider_name TEXT,

  -- Challenge status: 'initiated', 'completed', 'failed', 'expired'
  status TEXT NOT NULL DEFAULT 'initiated' CHECK (status IN ('initiated', 'completed', 'failed', 'expired')),

  -- Error information (populated on failure)
  error_code TEXT,
  error_description TEXT,

  -- Timestamps
  initiated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  completed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),

  CONSTRAINT oauth_challenges_pkey PRIMARY KEY (id),
  CONSTRAINT oauth_challenges_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES organization_metadata (id) ON DELETE CASCADE,
  CONSTRAINT oauth_challenges_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE CASCADE,
  CONSTRAINT oauth_challenges_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT oauth_challenges_toolset_id_fkey FOREIGN KEY (toolset_id) REFERENCES toolsets (id) ON DELETE CASCADE
);

-- Index for querying challenges by organization (for billing)
CREATE INDEX IF NOT EXISTS oauth_challenges_organization_id_initiated_at_idx
ON oauth_challenges (organization_id, initiated_at DESC);

-- Index for querying challenges by project
CREATE INDEX IF NOT EXISTS oauth_challenges_project_id_initiated_at_idx
ON oauth_challenges (project_id, initiated_at DESC);
