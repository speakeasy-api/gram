-- atlas:txmode none

-- Create "onboarding_use_cases" table
CREATE TABLE "onboarding_use_cases" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "slug" text NOT NULL,
  "name" text NOT NULL,
  "description" text NOT NULL DEFAULT '',
  "sort_order" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "onboarding_use_cases_slug_key" to table: "onboarding_use_cases"
CREATE UNIQUE INDEX "onboarding_use_cases_slug_key" ON "onboarding_use_cases" ("slug") WHERE (deleted_at IS NULL);
-- Create "onboarding_playbooks" table
CREATE TABLE "onboarding_playbooks" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "use_case_id" uuid NULL,
  "organization_id" text NULL,
  "name" text NOT NULL,
  "description" text NOT NULL DEFAULT '',
  "is_default" boolean NOT NULL DEFAULT false,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "onboarding_playbooks_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "onboarding_playbooks_use_case_id_fkey" FOREIGN KEY ("use_case_id") REFERENCES "onboarding_use_cases" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "onboarding_playbooks_owner_check" CHECK ((use_case_id IS NULL) <> (organization_id IS NULL))
);
-- Create index "onboarding_playbooks_default_key" to table: "onboarding_playbooks"
CREATE UNIQUE INDEX "onboarding_playbooks_default_key" ON "onboarding_playbooks" ("use_case_id") WHERE (is_default AND (organization_id IS NULL) AND (deleted_at IS NULL));
-- Create index "onboarding_playbooks_organization_id_idx" to table: "onboarding_playbooks"
CREATE INDEX "onboarding_playbooks_organization_id_idx" ON "onboarding_playbooks" ("organization_id") WHERE (organization_id IS NOT NULL);
-- Create index "onboarding_playbooks_use_case_id_idx" to table: "onboarding_playbooks"
CREATE INDEX "onboarding_playbooks_use_case_id_idx" ON "onboarding_playbooks" ("use_case_id");
-- Create "onboarding_steps" table
CREATE TABLE "onboarding_steps" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "slug" text NOT NULL,
  "title" text NOT NULL,
  "description" text NOT NULL DEFAULT '',
  "parent_step_id" uuid NULL,
  "completion" text NOT NULL DEFAULT 'manual',
  "hidden_by_default" boolean NOT NULL DEFAULT true,
  "sort_order" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "onboarding_steps_parent_step_id_fkey" FOREIGN KEY ("parent_step_id") REFERENCES "onboarding_steps" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "onboarding_steps_parent_step_id_idx" to table: "onboarding_steps"
CREATE INDEX "onboarding_steps_parent_step_id_idx" ON "onboarding_steps" ("parent_step_id") WHERE (parent_step_id IS NOT NULL);
-- Create index "onboarding_steps_slug_key" to table: "onboarding_steps"
CREATE UNIQUE INDEX "onboarding_steps_slug_key" ON "onboarding_steps" ("slug");
-- Set comment to table: "onboarding_steps"
COMMENT ON TABLE "onboarding_steps" IS 'Onboarding steps mirrored from application code; deleted_at marks a step the code no longer defines.';
-- Create "onboarding_playbook_steps" table
CREATE TABLE "onboarding_playbook_steps" (
  "playbook_id" uuid NOT NULL,
  "step_id" uuid NOT NULL,
  "position" integer NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("playbook_id", "step_id"),
  CONSTRAINT "onboarding_playbook_steps_playbook_id_fkey" FOREIGN KEY ("playbook_id") REFERENCES "onboarding_playbooks" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "onboarding_playbook_steps_step_id_fkey" FOREIGN KEY ("step_id") REFERENCES "onboarding_steps" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "onboarding_playbook_steps_position_key" to table: "onboarding_playbook_steps"
CREATE UNIQUE INDEX "onboarding_playbook_steps_position_key" ON "onboarding_playbook_steps" ("playbook_id", "position");
-- Create index "onboarding_playbook_steps_step_id_idx" to table: "onboarding_playbook_steps"
CREATE INDEX "onboarding_playbook_steps_step_id_idx" ON "onboarding_playbook_steps" ("step_id");
-- Create "onboarding_step_dependencies" table
CREATE TABLE "onboarding_step_dependencies" (
  "step_id" uuid NOT NULL,
  "requires_step_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("step_id", "requires_step_id"),
  CONSTRAINT "onboarding_step_dependencies_requires_step_id_fkey" FOREIGN KEY ("requires_step_id") REFERENCES "onboarding_steps" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "onboarding_step_dependencies_step_id_fkey" FOREIGN KEY ("step_id") REFERENCES "onboarding_steps" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "onboarding_step_dependencies_step_id_check" CHECK (step_id <> requires_step_id)
);
-- Create index "onboarding_step_dependencies_requires_step_id_idx" to table: "onboarding_step_dependencies"
CREATE INDEX "onboarding_step_dependencies_requires_step_id_idx" ON "onboarding_step_dependencies" ("requires_step_id");
-- Create "onboarding_step_methods" table
CREATE TABLE "onboarding_step_methods" (
  "step_id" uuid NOT NULL,
  "integration_method_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("step_id", "integration_method_id"),
  CONSTRAINT "onboarding_step_methods_integration_method_id_fkey" FOREIGN KEY ("integration_method_id") REFERENCES "support_matrix_integration_methods" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "onboarding_step_methods_step_id_fkey" FOREIGN KEY ("step_id") REFERENCES "onboarding_steps" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "onboarding_step_methods_integration_method_id_idx" to table: "onboarding_step_methods"
CREATE INDEX "onboarding_step_methods_integration_method_id_idx" ON "onboarding_step_methods" ("integration_method_id");
-- Modify "organization_onboarding" table
ALTER TABLE "organization_onboarding" ADD COLUMN "mdm_vendor" text NULL, ADD COLUMN "mdm_vendor_name" text NULL, ADD COLUMN "playbook_id" uuid NULL, ADD CONSTRAINT "organization_onboarding_playbook_id_fkey" FOREIGN KEY ("playbook_id") REFERENCES "onboarding_playbooks" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Create index "organization_onboarding_playbook_id_idx" to table: "organization_onboarding"
CREATE INDEX CONCURRENTLY "organization_onboarding_playbook_id_idx" ON "organization_onboarding" ("playbook_id") WHERE (playbook_id IS NOT NULL);
-- Create "support_matrix_plans" table
CREATE TABLE "support_matrix_plans" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "slug" text NOT NULL,
  "vendor" text NOT NULL,
  "name" text NOT NULL,
  "sort_order" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "support_matrix_plans_slug_key" to table: "support_matrix_plans"
CREATE UNIQUE INDEX "support_matrix_plans_slug_key" ON "support_matrix_plans" ("slug");
-- Create index "support_matrix_plans_vendor_idx" to table: "support_matrix_plans"
CREATE INDEX "support_matrix_plans_vendor_idx" ON "support_matrix_plans" ("vendor");
-- Set comment to table: "support_matrix_plans"
COMMENT ON TABLE "support_matrix_plans" IS 'Plans each vendor sells, as the support matrix names them; an organization declares the one it is on per vendor.';
-- Create "organization_onboarding_vendors" table
CREATE TABLE "organization_onboarding_vendors" (
  "organization_id" text NOT NULL,
  "vendor" text NOT NULL,
  "plan_id" uuid NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id", "vendor"),
  CONSTRAINT "organization_onboarding_vendors_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_onboarding" ("organization_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "organization_onboarding_vendors_plan_id_fkey" FOREIGN KEY ("plan_id") REFERENCES "support_matrix_plans" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "organization_onboarding_vendors_plan_id_idx" to table: "organization_onboarding_vendors"
CREATE INDEX "organization_onboarding_vendors_plan_id_idx" ON "organization_onboarding_vendors" ("plan_id") WHERE (plan_id IS NOT NULL);
