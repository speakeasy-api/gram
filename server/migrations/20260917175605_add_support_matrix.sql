-- Create "support_matrix_capabilities" table
CREATE TABLE "support_matrix_capabilities" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "slug" text NOT NULL,
  "name" text NOT NULL,
  "category" text NOT NULL,
  "description" text NOT NULL DEFAULT '',
  "sort_order" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "support_matrix_capabilities_slug_key" to table: "support_matrix_capabilities"
CREATE UNIQUE INDEX "support_matrix_capabilities_slug_key" ON "support_matrix_capabilities" ("slug");
-- Set comment to table: "support_matrix_capabilities"
COMMENT ON TABLE "support_matrix_capabilities" IS 'Individual capabilities grouped by category; categories are not blanket support claims.';
-- Create "support_matrix_integration_methods" table
CREATE TABLE "support_matrix_integration_methods" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "slug" text NOT NULL,
  "name" text NOT NULL,
  "vendor" text NOT NULL,
  "description" text NOT NULL DEFAULT '',
  "plan_notes" text NOT NULL DEFAULT '',
  "sort_order" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "support_matrix_integration_methods_slug_key" to table: "support_matrix_integration_methods"
CREATE UNIQUE INDEX "support_matrix_integration_methods_slug_key" ON "support_matrix_integration_methods" ("slug");
-- Set comment to table: "support_matrix_integration_methods"
COMMENT ON TABLE "support_matrix_integration_methods" IS 'Integration methods available for assessing support; plan_notes preserve method-level eligibility claims.';
-- Create "support_matrix_platforms" table
CREATE TABLE "support_matrix_platforms" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "slug" text NOT NULL,
  "name" text NOT NULL,
  "vendor" text NOT NULL,
  "family" text NOT NULL,
  "surface" text NOT NULL,
  "description" text NOT NULL DEFAULT '',
  "sort_order" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "support_matrix_platforms_slug_key" to table: "support_matrix_platforms"
CREATE UNIQUE INDEX "support_matrix_platforms_slug_key" ON "support_matrix_platforms" ("slug");
-- Set comment to table: "support_matrix_platforms"
COMMENT ON TABLE "support_matrix_platforms" IS 'Global admin support catalog of upstream product surfaces, independent of customer installations.';
-- Create "support_matrix_method_platforms" table
CREATE TABLE "support_matrix_method_platforms" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "integration_method_id" uuid NOT NULL,
  "platform_id" uuid NOT NULL,
  "applicability" text NOT NULL DEFAULT 'unknown',
  "operating_systems" text[] NULL,
  "plan_types" text[] NULL,
  "conditions" text NOT NULL DEFAULT '',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "support_matrix_method_platforms_integration_method_id_fkey" FOREIGN KEY ("integration_method_id") REFERENCES "support_matrix_integration_methods" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "support_matrix_method_platforms_platform_id_fkey" FOREIGN KEY ("platform_id") REFERENCES "support_matrix_platforms" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "support_matrix_method_platforms_method_platform_key" to table: "support_matrix_method_platforms"
CREATE UNIQUE INDEX "support_matrix_method_platforms_method_platform_key" ON "support_matrix_method_platforms" ("integration_method_id", "platform_id");
-- Create index "support_matrix_method_platforms_platform_id_idx" to table: "support_matrix_method_platforms"
CREATE INDEX "support_matrix_method_platforms_platform_id_idx" ON "support_matrix_method_platforms" ("platform_id");
-- Set comment to table: "support_matrix_method_platforms"
COMMENT ON TABLE "support_matrix_method_platforms" IS 'Applicability of a method to a platform, assessed separately from its capability coverage. Missing rows are unknown.';
-- Set comment to column: "applicability" on table: "support_matrix_method_platforms"
COMMENT ON COLUMN "support_matrix_method_platforms"."applicability" IS 'Application-validated: unknown, applicable, na. Applicability alone never implies capability coverage.';
-- Set comment to column: "operating_systems" on table: "support_matrix_method_platforms"
COMMENT ON COLUMN "support_matrix_method_platforms"."operating_systems" IS 'NULL means unassessed; an empty array means unrestricted; otherwise lists eligible operating systems. Coverage restrictions supplement mapping restrictions.';
-- Set comment to column: "plan_types" on table: "support_matrix_method_platforms"
COMMENT ON COLUMN "support_matrix_method_platforms"."plan_types" IS 'NULL means unassessed; an empty array means unrestricted; otherwise lists eligible plan types. Coverage restrictions supplement mapping restrictions.';
-- Create "support_matrix_coverage" table
CREATE TABLE "support_matrix_coverage" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "method_platform_id" uuid NOT NULL,
  "capability_id" uuid NOT NULL,
  "status" text NOT NULL DEFAULT 'unknown',
  "notes" text NOT NULL DEFAULT '',
  "needs_verification" boolean NOT NULL DEFAULT true,
  "source_url" text NULL,
  "verified_at" timestamptz NULL,
  "operating_systems" text[] NULL,
  "plan_types" text[] NULL,
  "conditions" text NOT NULL DEFAULT '',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "support_matrix_coverage_capability_id_fkey" FOREIGN KEY ("capability_id") REFERENCES "support_matrix_capabilities" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "support_matrix_coverage_method_platform_id_fkey" FOREIGN KEY ("method_platform_id") REFERENCES "support_matrix_method_platforms" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "support_matrix_coverage_capability_id_idx" to table: "support_matrix_coverage"
CREATE INDEX "support_matrix_coverage_capability_id_idx" ON "support_matrix_coverage" ("capability_id");
-- Create index "support_matrix_coverage_mapping_capability_key" to table: "support_matrix_coverage"
CREATE UNIQUE INDEX "support_matrix_coverage_mapping_capability_key" ON "support_matrix_coverage" ("method_platform_id", "capability_id");
-- Set comment to table: "support_matrix_coverage"
COMMENT ON TABLE "support_matrix_coverage" IS 'Explicit method-platform-capability coverage. Missing rows are unknown; applicability must also be established before claiming support.';
-- Set comment to column: "status" on table: "support_matrix_coverage"
COMMENT ON COLUMN "support_matrix_coverage"."status" IS 'Application-validated: supported, partial, unimplemented, impossible, na, unknown. Partial coverage requires explanatory notes.';
-- Set comment to column: "operating_systems" on table: "support_matrix_coverage"
COMMENT ON COLUMN "support_matrix_coverage"."operating_systems" IS 'NULL means unassessed; an empty array means unrestricted; otherwise lists eligible operating systems. Coverage restrictions supplement mapping restrictions.';
-- Set comment to column: "plan_types" on table: "support_matrix_coverage"
COMMENT ON COLUMN "support_matrix_coverage"."plan_types" IS 'NULL means unassessed; an empty array means unrestricted; otherwise lists eligible plan types. Coverage restrictions supplement mapping restrictions.';
-- Create "support_matrix_method_capabilities" table
CREATE TABLE "support_matrix_method_capabilities" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "integration_method_id" uuid NOT NULL,
  "capability_id" uuid NOT NULL,
  "status" text NOT NULL DEFAULT 'unknown',
  "notes" text NOT NULL DEFAULT '',
  "needs_verification" boolean NOT NULL DEFAULT true,
  "source_url" text NULL,
  "verified_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "support_matrix_method_capabilities_capability_id_fkey" FOREIGN KEY ("capability_id") REFERENCES "support_matrix_capabilities" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "support_matrix_method_capabilities_integration_method_id_fkey" FOREIGN KEY ("integration_method_id") REFERENCES "support_matrix_integration_methods" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "support_matrix_method_capabilities_capability_id_idx" to table: "support_matrix_method_capabilities"
CREATE INDEX "support_matrix_method_capabilities_capability_id_idx" ON "support_matrix_method_capabilities" ("capability_id");
-- Create index "support_matrix_method_capabilities_method_capability_key" to table: "support_matrix_method_capabilities"
CREATE UNIQUE INDEX "support_matrix_method_capabilities_method_capability_key" ON "support_matrix_method_capabilities" ("integration_method_id", "capability_id");
-- Set comment to table: "support_matrix_method_capabilities"
COMMENT ON TABLE "support_matrix_method_capabilities" IS 'Method-level reference claims. These do not establish support for any specific platform.';
-- Set comment to column: "status" on table: "support_matrix_method_capabilities"
COMMENT ON COLUMN "support_matrix_method_capabilities"."status" IS 'Application-validated: supported, partial, unimplemented, impossible, na, unknown. Partial coverage requires explanatory notes.';
