-- Hosted chat instances accessible at chat.getgram.ai
CREATE TABLE IF NOT EXISTS hosted_chats (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  organization_id TEXT NOT NULL,
  project_id uuid NOT NULL,
  created_by_user_id TEXT NOT NULL,

  name TEXT NOT NULL CHECK (name <> '' AND CHAR_LENGTH(name) <= 100),
  slug TEXT NOT NULL CHECK (slug <> '' AND CHAR_LENGTH(slug) <= 60),

  -- Elements configuration
  mcp_slug TEXT CHECK (mcp_slug IS NULL OR (mcp_slug <> '' AND CHAR_LENGTH(mcp_slug) <= 60)),
  system_prompt TEXT,
  welcome_title TEXT CHECK (welcome_title IS NULL OR CHAR_LENGTH(welcome_title) <= 200),
  welcome_subtitle TEXT CHECK (welcome_subtitle IS NULL OR CHAR_LENGTH(welcome_subtitle) <= 500),
  theme_color_scheme TEXT NOT NULL DEFAULT 'system' CHECK (theme_color_scheme IN ('light', 'dark', 'system')),

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT hosted_chats_pkey PRIMARY KEY (id),
  CONSTRAINT hosted_chats_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE SET NULL,
  CONSTRAINT hosted_chats_created_by_user_id_fkey FOREIGN KEY (created_by_user_id) REFERENCES users (id) ON DELETE SET NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS hosted_chats_project_id_slug_key
ON hosted_chats (project_id, slug)
WHERE deleted IS FALSE;

CREATE INDEX IF NOT EXISTS hosted_chats_organization_id_idx
ON hosted_chats (organization_id)
WHERE deleted IS FALSE;
