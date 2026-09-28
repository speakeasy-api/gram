-- Create "trigger_thread_routes" table
CREATE TABLE "trigger_thread_routes" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "target_kind" text NOT NULL,
  "target_ref" text NOT NULL,
  "correlation_id" text NOT NULL,
  "route_to_correlation_id" text NULL,
  "state" text NOT NULL,
  "last_seen_cursor" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "trigger_thread_routes_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "trigger_thread_routes_correlation_id_check" CHECK ((correlation_id <> ''::text) AND (char_length(correlation_id) <= 300)),
  CONSTRAINT "trigger_thread_routes_last_seen_cursor_check" CHECK ((last_seen_cursor <> ''::text) AND (char_length(last_seen_cursor) <= 64)),
  CONSTRAINT "trigger_thread_routes_route_to_correlation_id_check" CHECK ((route_to_correlation_id <> ''::text) AND (char_length(route_to_correlation_id) <= 300)),
  CONSTRAINT "trigger_thread_routes_target_kind_check" CHECK ((target_kind <> ''::text) AND (char_length(target_kind) <= 60)),
  CONSTRAINT "trigger_thread_routes_target_ref_check" CHECK ((target_ref <> ''::text) AND (char_length(target_ref) <= 255))
);
-- Create index "trigger_thread_routes_project_id_idx" to table: "trigger_thread_routes"
CREATE INDEX "trigger_thread_routes_project_id_idx" ON "trigger_thread_routes" ("project_id");
-- Create index "trigger_thread_routes_route_to_correlation_id_idx" to table: "trigger_thread_routes"
CREATE INDEX "trigger_thread_routes_route_to_correlation_id_idx" ON "trigger_thread_routes" ("project_id", "target_kind", "target_ref", "route_to_correlation_id") WHERE ((route_to_correlation_id IS NOT NULL) AND (deleted IS FALSE));
-- Create index "trigger_thread_routes_target_correlation_id_key" to table: "trigger_thread_routes"
CREATE UNIQUE INDEX "trigger_thread_routes_target_correlation_id_key" ON "trigger_thread_routes" ("project_id", "target_kind", "target_ref", "correlation_id") WHERE (deleted IS FALSE);
