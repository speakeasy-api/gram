-- Modify "toolsets" table for draft/staging workflow
ALTER TABLE "toolsets" ADD COLUMN "iteration_mode" boolean NOT NULL DEFAULT false;
ALTER TABLE "toolsets" ADD COLUMN "has_draft_changes" boolean NOT NULL DEFAULT false;

-- Create "draft_toolset_versions" table
CREATE TABLE "draft_toolset_versions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "toolset_id" uuid NOT NULL,
  "tool_urns" text[] NOT NULL DEFAULT ARRAY[]::TEXT[],
  "resource_urns" text[] NOT NULL DEFAULT ARRAY[]::TEXT[],
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  CONSTRAINT "draft_toolset_versions_pkey" PRIMARY KEY ("id"),
  CONSTRAINT "draft_toolset_versions_toolset_id_fkey" FOREIGN KEY ("toolset_id") REFERENCES "toolsets" ("id") ON DELETE CASCADE
);

-- Create unique index for one active draft per toolset
CREATE UNIQUE INDEX "draft_toolset_versions_toolset_id_key" ON "draft_toolset_versions" ("toolset_id") WHERE (deleted IS FALSE);

-- Create "draft_tool_variations" table
CREATE TABLE "draft_tool_variations" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "draft_version_id" uuid NOT NULL,
  "src_tool_urn" text NOT NULL,
  "src_tool_name" text NOT NULL,
  "confirm" text NULL,
  "confirm_prompt" text NULL,
  "name" text NULL,
  "summary" text NULL,
  "description" text NULL,
  "tags" text[] NULL,
  "summarizer" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  CONSTRAINT "draft_tool_variations_pkey" PRIMARY KEY ("id"),
  CONSTRAINT "draft_tool_variations_draft_version_id_fkey" FOREIGN KEY ("draft_version_id") REFERENCES "draft_toolset_versions" ("id") ON DELETE CASCADE
);

-- Create unique index for one variation per tool URN per draft
CREATE UNIQUE INDEX "draft_tool_variations_version_urn_key" ON "draft_tool_variations" ("draft_version_id", "src_tool_urn") WHERE (deleted IS FALSE);
