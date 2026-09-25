-- Create "sigint_custom_signals" table
CREATE TABLE "sigint_custom_signals" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "name" text NOT NULL,
  "description" text NULL,
  "classifier_criteria" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "sigint_custom_signals_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "sigint_custom_signals_project_id_id_idx" to table: "sigint_custom_signals"
CREATE INDEX "sigint_custom_signals_project_id_id_idx" ON "sigint_custom_signals" ("project_id", "id") WHERE (deleted IS FALSE);
-- Create index "sigint_custom_signals_project_id_id_key" to table: "sigint_custom_signals"
CREATE UNIQUE INDEX "sigint_custom_signals_project_id_id_key" ON "sigint_custom_signals" ("project_id", "id");
-- Create "sigint_sensors" table
CREATE TABLE "sigint_sensors" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "name" text NOT NULL,
  "description" text NULL,
  "instructions" text NULL,
  "mode" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "sigint_sensors_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "sigint_sensors_project_id_id_idx" to table: "sigint_sensors"
CREATE INDEX "sigint_sensors_project_id_id_idx" ON "sigint_sensors" ("project_id", "id") WHERE (deleted IS FALSE);
-- Create index "sigint_sensors_project_id_id_key" to table: "sigint_sensors"
CREATE UNIQUE INDEX "sigint_sensors_project_id_id_key" ON "sigint_sensors" ("project_id", "id");
-- Create "sigint_sensor_signals" table
CREATE TABLE "sigint_sensor_signals" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "sensor_id" uuid NOT NULL,
  "signal_id" uuid NOT NULL,
  "sort_order" integer NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "sigint_sensor_signals_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "sigint_sensor_signals_project_id_sensor_id_fkey" FOREIGN KEY ("project_id", "sensor_id") REFERENCES "sigint_sensors" ("project_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "sigint_sensor_signals_project_id_signal_id_fkey" FOREIGN KEY ("project_id", "signal_id") REFERENCES "sigint_custom_signals" ("project_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "sigint_sensor_signals_project_id_sensor_id_signal_id_key" to table: "sigint_sensor_signals"
CREATE UNIQUE INDEX "sigint_sensor_signals_project_id_sensor_id_signal_id_key" ON "sigint_sensor_signals" ("project_id", "sensor_id", "signal_id") WHERE (deleted IS FALSE);
-- Create index "sigint_sensor_signals_project_id_sensor_id_sort_order_idx" to table: "sigint_sensor_signals"
CREATE INDEX "sigint_sensor_signals_project_id_sensor_id_sort_order_idx" ON "sigint_sensor_signals" ("project_id", "sensor_id", "sort_order", "id") WHERE (deleted IS FALSE);
-- Create index "sigint_sensor_signals_project_id_signal_id_idx" to table: "sigint_sensor_signals"
CREATE INDEX "sigint_sensor_signals_project_id_signal_id_idx" ON "sigint_sensor_signals" ("project_id", "signal_id") WHERE (deleted IS FALSE);
-- Create index "sigint_sensor_signals_sensor_reference_idx" to table: "sigint_sensor_signals"
CREATE INDEX "sigint_sensor_signals_sensor_reference_idx" ON "sigint_sensor_signals" ("project_id", "sensor_id");
-- Create index "sigint_sensor_signals_signal_reference_idx" to table: "sigint_sensor_signals"
CREATE INDEX "sigint_sensor_signals_signal_reference_idx" ON "sigint_sensor_signals" ("project_id", "signal_id");
