-- Modify "ai_products" table
ALTER TABLE "ai_products" ADD COLUMN "family" text NULL;
-- Set comment to column: "family" on table: "ai_products"
COMMENT ON COLUMN "ai_products"."family" IS 'Groups a vendor''s related products, e.g. claude for Claude Code, Claude and Cowork. Null means the product is a family of its own.
@access: confidential';
-- Create "ai_product_aliases" table
CREATE TABLE "ai_product_aliases" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "ai_product_id" text NOT NULL,
  "name" text NOT NULL,
  "client" text NULL,
  "created_by" text NULL,
  "updated_by" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "ai_product_aliases_ai_product_id_fkey" FOREIGN KEY ("ai_product_id") REFERENCES "ai_products" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "ai_product_aliases_client_check" CHECK (client <> ''::text),
  CONSTRAINT "ai_product_aliases_name_check" CHECK (name <> ''::text)
);
-- Create index "ai_product_aliases_ai_product_id_idx" to table: "ai_product_aliases"
CREATE INDEX "ai_product_aliases_ai_product_id_idx" ON "ai_product_aliases" ("ai_product_id");
-- Create index "ai_product_aliases_name_key" to table: "ai_product_aliases"
CREATE UNIQUE INDEX "ai_product_aliases_name_key" ON "ai_product_aliases" ((translate(lower(name), ' 	_-'::text, ''::text))) WHERE (deleted IS FALSE);
-- Set comment to table: "ai_product_aliases"
COMMENT ON TABLE "ai_product_aliases" IS 'Other names each catalog product is reported under. Attribution only, never enforcement: the names are self-reported.';
-- Set comment to column: "id" on table: "ai_product_aliases"
COMMENT ON COLUMN "ai_product_aliases"."id" IS '@access: confidential';
-- Set comment to column: "ai_product_id" on table: "ai_product_aliases"
COMMENT ON COLUMN "ai_product_aliases"."ai_product_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "ai_product_aliases"
COMMENT ON COLUMN "ai_product_aliases"."name" IS 'The reported name, stored as written. Lookups ignore case, spaces, tabs, underscores and hyphens, so claude_code and Claude Code are the same name.
@access: opaque-confidential';
-- Set comment to column: "client" on table: "ai_product_aliases"
COMMENT ON COLUMN "ai_product_aliases"."client" IS 'Kind of client the name implies (cli, ide, desktop, web, mobile, cloud, slack); null when the name implies none. Validated in application code.
@access: confidential';
-- Set comment to column: "created_by" on table: "ai_product_aliases"
COMMENT ON COLUMN "ai_product_aliases"."created_by" IS 'Subject URN of the staff member who created the row; null for seeded rows.
@access: confidential-pii';
-- Set comment to column: "updated_by" on table: "ai_product_aliases"
COMMENT ON COLUMN "ai_product_aliases"."updated_by" IS 'Subject URN of the staff member who last changed the row; null for seeded rows.
@access: confidential-pii';
-- Set comment to column: "created_at" on table: "ai_product_aliases"
COMMENT ON COLUMN "ai_product_aliases"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "ai_product_aliases"
COMMENT ON COLUMN "ai_product_aliases"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "ai_product_aliases"
COMMENT ON COLUMN "ai_product_aliases"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "ai_product_aliases"
COMMENT ON COLUMN "ai_product_aliases"."deleted" IS '@access: confidential';
