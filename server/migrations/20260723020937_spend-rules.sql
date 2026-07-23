-- Create "spend_rules" table
CREATE TABLE "spend_rules" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "name" text NOT NULL,
  "slug" text NOT NULL,
  "description" text NOT NULL DEFAULT '',
  "target_expr" text NOT NULL,
  "limit_usd" double precision NOT NULL,
  "rule_expr" text NOT NULL DEFAULT 'spend_usd >= limit_usd',
  "window_kind" text NOT NULL,
  "warn_at_pct" integer NOT NULL DEFAULT 80,
  "action" text NOT NULL DEFAULT 'flag',
  "enabled" boolean NOT NULL DEFAULT true,
  "version" bigint NOT NULL DEFAULT 1,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "archived_at" timestamptz NULL,
  "archived" boolean NOT NULL GENERATED ALWAYS AS (archived_at IS NOT NULL) STORED,
  "superseded_by" uuid NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "spend_rules_organization_id_id_key" UNIQUE ("organization_id", "id"),
  CONSTRAINT "spend_rules_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "spend_rules_organization_id_superseded_by_fkey" FOREIGN KEY ("organization_id", "superseded_by") REFERENCES "spend_rules" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "spend_rules_organization_id_slug_live_key" to table: "spend_rules"
CREATE UNIQUE INDEX "spend_rules_organization_id_slug_live_key" ON "spend_rules" ("organization_id", "slug") WHERE (archived IS FALSE);
-- Create index "spend_rules_organization_id_slug_version_key" to table: "spend_rules"
CREATE UNIQUE INDEX "spend_rules_organization_id_slug_version_key" ON "spend_rules" ("organization_id", "slug", "version");
-- Create index "spend_rules_superseded_by_idx" to table: "spend_rules"
CREATE INDEX "spend_rules_superseded_by_idx" ON "spend_rules" ("superseded_by");
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
  CONSTRAINT "spend_rule_events_organization_id_spend_rule_id_fkey" FOREIGN KEY ("organization_id", "spend_rule_id") REFERENCES "spend_rules" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "spend_rule_events_event_type_check" CHECK (event_type = ANY (ARRAY['warning'::text, 'breach'::text]))
);
-- Create index "spend_rule_events_dedupe_key" to table: "spend_rule_events"
CREATE UNIQUE INDEX "spend_rule_events_dedupe_key" ON "spend_rule_events" ("spend_rule_id", "event_type", "email", "window_start");
-- Create index "spend_rule_events_organization_id_id_idx" to table: "spend_rule_events"
CREATE INDEX "spend_rule_events_organization_id_id_idx" ON "spend_rule_events" ("organization_id", "id" DESC);
