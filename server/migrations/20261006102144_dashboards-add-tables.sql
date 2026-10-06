-- Create "dashboards" table
CREATE TABLE "dashboards" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "created_by_user_id" text NULL,
  "name" text NOT NULL,
  "description" text NULL,
  "filters" jsonb NOT NULL DEFAULT '{}',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "dashboards_organization_id_project_id_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "dashboards_description_check" CHECK (char_length(description) <= 2000),
  CONSTRAINT "dashboards_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 200))
);
-- Create index "dashboards_project_id_updated_at_idx" to table: "dashboards"
CREATE INDEX "dashboards_project_id_updated_at_idx" ON "dashboards" ("project_id", "updated_at" DESC) WHERE (deleted IS FALSE);
-- Create "dashboard_widgets" table
CREATE TABLE "dashboard_widgets" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "dashboard_id" uuid NOT NULL,
  "widget_id" uuid NOT NULL,
  "x" integer NOT NULL,
  "y" integer NOT NULL,
  "w" integer NOT NULL,
  "h" integer NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "dashboard_widgets_dashboard_id_fkey" FOREIGN KEY ("dashboard_id") REFERENCES "dashboards" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "dashboard_widgets_organization_id_project_id_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "dashboard_widgets_widget_id_fkey" FOREIGN KEY ("widget_id") REFERENCES "widgets" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "dashboard_widgets_h_check" CHECK (h > 0),
  CONSTRAINT "dashboard_widgets_w_check" CHECK (w > 0),
  CONSTRAINT "dashboard_widgets_x_check" CHECK (x >= 0),
  CONSTRAINT "dashboard_widgets_y_check" CHECK (y >= 0)
);
-- Create index "dashboard_widgets_dashboard_id_idx" to table: "dashboard_widgets"
CREATE INDEX "dashboard_widgets_dashboard_id_idx" ON "dashboard_widgets" ("dashboard_id");
-- Create index "dashboard_widgets_widget_id_idx" to table: "dashboard_widgets"
CREATE INDEX "dashboard_widgets_widget_id_idx" ON "dashboard_widgets" ("widget_id");
