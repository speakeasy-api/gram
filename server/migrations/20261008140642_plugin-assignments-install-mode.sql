-- Modify "plugin_assignments" table
ALTER TABLE "plugin_assignments" ADD COLUMN "install_mode" text NOT NULL DEFAULT 'default';
