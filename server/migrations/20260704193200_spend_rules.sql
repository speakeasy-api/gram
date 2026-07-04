-- Create "spend_rules" table
CREATE TABLE "spend_rules" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "name" text NOT NULL,
  "slug" text NOT NULL,
  "description" text NOT NULL DEFAULT '',
  "target_expr" text NOT NULL,
  "limit_usd" double precision NOT NULL,
  "window_kind" text NOT NULL,
  "warn_at_pct" integer NOT NULL DEFAULT 80,
  "action" text NOT NULL DEFAULT 'flag',
  "enabled" boolean NOT NULL DEFAULT true,
  "version" bigint NOT NULL DEFAULT 1,
  "evaluated_from" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "spend_rules_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "spend_rules_action_check" CHECK (action = ANY (ARRAY['flag'::text, 'block'::text])),
  CONSTRAINT "spend_rules_limit_usd_check" CHECK (limit_usd > (0)::double precision),
  CONSTRAINT "spend_rules_slug_check" CHECK (slug ~ '^[a-z0-9_-]{1,128}$'::text),
  CONSTRAINT "spend_rules_warn_at_pct_check" CHECK ((warn_at_pct >= 1) AND (warn_at_pct <= 100)),
  CONSTRAINT "spend_rules_window_kind_check" CHECK (window_kind = ANY (ARRAY['daily'::text, 'weekly'::text, 'monthly'::text]))
);
-- Create index "spend_rules_organization_id_idx" to table: "spend_rules"
CREATE INDEX "spend_rules_organization_id_idx" ON "spend_rules" ("organization_id") WHERE (deleted IS FALSE);
-- Create index "spend_rules_organization_id_slug_key" to table: "spend_rules"
CREATE UNIQUE INDEX "spend_rules_organization_id_slug_key" ON "spend_rules" ("organization_id", "slug") WHERE (deleted IS FALSE);
-- Create "spend_rule_events" table
CREATE TABLE "spend_rule_events" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "spend_rule_id" uuid NOT NULL,
  "rule_urn" text NOT NULL,
  "event_type" text NOT NULL,
  "user_id" text NULL,
  "email" text NOT NULL,
  "display_name" text NULL,
  "spend_usd" double precision NOT NULL,
  "limit_usd" double precision NOT NULL,
  "window_start" timestamptz NOT NULL,
  "window_end" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "spend_rule_events_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "spend_rule_events_spend_rule_id_fkey" FOREIGN KEY ("spend_rule_id") REFERENCES "spend_rules" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "spend_rule_events_event_type_check" CHECK (event_type = ANY (ARRAY['warning'::text, 'breach'::text]))
);
-- Create index "spend_rule_events_dedupe_key" to table: "spend_rule_events"
CREATE UNIQUE INDEX "spend_rule_events_dedupe_key" ON "spend_rule_events" ("spend_rule_id", "rule_urn", "event_type", "email", "window_start");
-- Create index "spend_rule_events_organization_id_created_at_idx" to table: "spend_rule_events"
CREATE INDEX "spend_rule_events_organization_id_created_at_idx" ON "spend_rule_events" ("organization_id", "created_at" DESC);
-- Create "spend_rule_versions" table
CREATE TABLE "spend_rule_versions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "spend_rule_id" uuid NOT NULL,
  "version" bigint NOT NULL,
  "target_expr" text NOT NULL,
  "limit_usd" double precision NOT NULL,
  "window_kind" text NOT NULL,
  "warn_at_pct" integer NOT NULL,
  "action" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "spend_rule_versions_spend_rule_id_version_key" UNIQUE ("spend_rule_id", "version"),
  CONSTRAINT "spend_rule_versions_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "spend_rule_versions_spend_rule_id_fkey" FOREIGN KEY ("spend_rule_id") REFERENCES "spend_rules" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "spend_rule_versions_action_check" CHECK (action = ANY (ARRAY['flag'::text, 'block'::text])),
  CONSTRAINT "spend_rule_versions_window_kind_check" CHECK (window_kind = ANY (ARRAY['daily'::text, 'weekly'::text, 'monthly'::text]))
);
