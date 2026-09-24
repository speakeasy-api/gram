-- Create "onboarding_providers" table
CREATE TABLE "onboarding_providers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "slug" text NOT NULL,
  "name" text NOT NULL,
  "sort_order" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "onboarding_providers_slug_check" CHECK ((slug <> ''::text) AND (char_length(slug) <= 64))
);
-- Create index "onboarding_providers_slug_key" to table: "onboarding_providers"
CREATE UNIQUE INDEX "onboarding_providers_slug_key" ON "onboarding_providers" ("slug");
-- Create "onboarding_plans" table
CREATE TABLE "onboarding_plans" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "provider_id" uuid NOT NULL,
  "slug" text NOT NULL,
  "name" text NOT NULL,
  "sort_order" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "onboarding_plans_provider_id_fkey" FOREIGN KEY ("provider_id") REFERENCES "onboarding_providers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "onboarding_plans_slug_check" CHECK ((slug <> ''::text) AND (char_length(slug) <= 64))
);
-- Create index "onboarding_plans_slug_key" to table: "onboarding_plans"
CREATE UNIQUE INDEX "onboarding_plans_slug_key" ON "onboarding_plans" ("slug");
-- Create "onboarding_products" table
CREATE TABLE "onboarding_products" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "provider_id" uuid NOT NULL,
  "slug" text NOT NULL,
  "name" text NOT NULL,
  "source_ids" text[] NOT NULL DEFAULT '{}',
  "sort_order" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "onboarding_products_provider_id_fkey" FOREIGN KEY ("provider_id") REFERENCES "onboarding_providers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "onboarding_products_slug_check" CHECK ((slug <> ''::text) AND (char_length(slug) <= 64))
);
-- Create index "onboarding_products_slug_key" to table: "onboarding_products"
CREATE UNIQUE INDEX "onboarding_products_slug_key" ON "onboarding_products" ("slug");
-- Create "onboarding_techniques" table
CREATE TABLE "onboarding_techniques" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "slug" text NOT NULL,
  "name" text NOT NULL,
  "description" text NOT NULL DEFAULT '',
  "sort_order" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "onboarding_techniques_slug_check" CHECK ((slug <> ''::text) AND (char_length(slug) <= 64))
);
-- Create index "onboarding_techniques_slug_key" to table: "onboarding_techniques"
CREATE UNIQUE INDEX "onboarding_techniques_slug_key" ON "onboarding_techniques" ("slug");
-- Create "onboarding_product_techniques" table
CREATE TABLE "onboarding_product_techniques" (
  "product_id" uuid NOT NULL,
  "technique_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("product_id", "technique_id"),
  CONSTRAINT "onboarding_product_techniques_product_id_fkey" FOREIGN KEY ("product_id") REFERENCES "onboarding_products" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "onboarding_product_techniques_technique_id_fkey" FOREIGN KEY ("technique_id") REFERENCES "onboarding_techniques" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "onboarding_product_technique_plans" table
CREATE TABLE "onboarding_product_technique_plans" (
  "product_id" uuid NOT NULL,
  "technique_id" uuid NOT NULL,
  "plan_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("product_id", "technique_id", "plan_id"),
  CONSTRAINT "onboarding_product_technique_plans_plan_id_fkey" FOREIGN KEY ("plan_id") REFERENCES "onboarding_plans" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "onboarding_product_technique_plans_product_id_technique_id_fkey" FOREIGN KEY ("product_id", "technique_id") REFERENCES "onboarding_product_techniques" ("product_id", "technique_id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "onboarding_capabilities" table
CREATE TABLE "onboarding_capabilities" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "slug" text NOT NULL,
  "name" text NOT NULL,
  "use_case" text NOT NULL,
  "sort_order" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "onboarding_capabilities_slug_check" CHECK ((slug <> ''::text) AND (char_length(slug) <= 64))
);
-- Create index "onboarding_capabilities_slug_key" to table: "onboarding_capabilities"
CREATE UNIQUE INDEX "onboarding_capabilities_slug_key" ON "onboarding_capabilities" ("slug");
-- Create "onboarding_technique_capabilities" table
CREATE TABLE "onboarding_technique_capabilities" (
  "technique_id" uuid NOT NULL,
  "capability_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("technique_id", "capability_id"),
  CONSTRAINT "onboarding_technique_capabilities_capability_id_fkey" FOREIGN KEY ("capability_id") REFERENCES "onboarding_capabilities" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "onboarding_technique_capabilities_technique_id_fkey" FOREIGN KEY ("technique_id") REFERENCES "onboarding_techniques" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "organization_onboarding_answers" table
CREATE TABLE "organization_onboarding_answers" (
  "organization_id" text NOT NULL,
  "mdm_vendor" text NULL,
  "use_case" text NULL,
  "completed_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id"),
  CONSTRAINT "organization_onboarding_answers_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "organization_onboarding_products" table
CREATE TABLE "organization_onboarding_products" (
  "organization_id" text NOT NULL,
  "product_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id", "product_id"),
  CONSTRAINT "organization_onboarding_products_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_onboarding_answers" ("organization_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "organization_onboarding_products_product_id_fkey" FOREIGN KEY ("product_id") REFERENCES "onboarding_products" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "organization_onboarding_providers" table
CREATE TABLE "organization_onboarding_providers" (
  "organization_id" text NOT NULL,
  "provider_id" uuid NOT NULL,
  "plan_id" uuid NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id", "provider_id"),
  CONSTRAINT "organization_onboarding_providers_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_onboarding_answers" ("organization_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "organization_onboarding_providers_plan_id_fkey" FOREIGN KEY ("plan_id") REFERENCES "onboarding_plans" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "organization_onboarding_providers_provider_id_fkey" FOREIGN KEY ("provider_id") REFERENCES "onboarding_providers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "organization_onboarding_steps" table
CREATE TABLE "organization_onboarding_steps" (
  "organization_id" text NOT NULL,
  "step_slug" text NOT NULL,
  "verified_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id", "step_slug"),
  CONSTRAINT "organization_onboarding_steps_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_onboarding_answers" ("organization_id") ON UPDATE NO ACTION ON DELETE CASCADE
);
