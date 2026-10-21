-- Create "ai_products" table
CREATE TABLE "ai_products" (
  "id" text NOT NULL,
  "vendor_key" text NOT NULL,
  "display_name" text NOT NULL,
  "category" text NULL,
  "bundle_ids" text[] NOT NULL DEFAULT '{}',
  "binaries" text[] NOT NULL DEFAULT '{}',
  "config_dirs" text[] NOT NULL DEFAULT '{}',
  "process_names" text[] NOT NULL DEFAULT '{}',
  "version_plist_key" text NULL,
  "client_info_names" text[] NOT NULL DEFAULT '{}',
  "created_by" text NULL,
  "updated_by" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "ai_products_display_name_check" CHECK (display_name <> ''::text),
  CONSTRAINT "ai_products_id_check" CHECK (id <> ''::text),
  CONSTRAINT "ai_products_vendor_key_check" CHECK (vendor_key <> ''::text)
);
-- Set comment to table: "ai_products"
COMMENT ON TABLE "ai_products" IS 'Global AI client catalog of products Speakeasy knows, curated by staff. Shadow AI scan targets and CIMD documents reference it.';
-- Set comment to column: "id" on table: "ai_products"
COMMENT ON COLUMN "ai_products"."id" IS 'Stable product identifier, e.g. claude-code. Shadow AI scan reports key on it and decisions are recorded against it, so it must never change once used. Soft deletion keeps it reserved.
@access: confidential';
-- Set comment to column: "vendor_key" on table: "ai_products"
COMMENT ON COLUMN "ai_products"."vendor_key" IS 'Groups products by vendor. Not unique: a vendor may ship several products. Stable: renaming one is a breaking change. Names a product at the MCP gateway only when exactly one live product carries it.
@access: confidential';
-- Set comment to column: "display_name" on table: "ai_products"
COMMENT ON COLUMN "ai_products"."display_name" IS '@access: confidential';
-- Set comment to column: "category" on table: "ai_products"
COMMENT ON COLUMN "ai_products"."category" IS 'Product category (harness, assistant, local_model). Required when any signature is set; null for a product known only as an OAuth client.
@access: confidential';
-- Set comment to column: "bundle_ids" on table: "ai_products"
COMMENT ON COLUMN "ai_products"."bundle_ids" IS 'macOS CFBundleIdentifier values that identify the product on a device.
@access: confidential';
-- Set comment to column: "binaries" on table: "ai_products"
COMMENT ON COLUMN "ai_products"."binaries" IS 'Bare command names resolved on the device PATH.
@access: confidential';
-- Set comment to column: "config_dirs" on table: "ai_products"
COMMENT ON COLUMN "ai_products"."config_dirs" IS 'Directories whose existence marks the product installed.
@access: confidential';
-- Set comment to column: "process_names" on table: "ai_products"
COMMENT ON COLUMN "ai_products"."process_names" IS 'Process names checked for the running signal.
@access: confidential';
-- Set comment to column: "version_plist_key" on table: "ai_products"
COMMENT ON COLUMN "ai_products"."version_plist_key" IS 'Info.plist key read for the version on a bundle match; null means CFBundleShortVersionString.
@access: confidential';
-- Set comment to column: "client_info_names" on table: "ai_products"
COMMENT ON COLUMN "ai_products"."client_info_names" IS 'Names the product reports at MCP initialize. Attribution only, never enforcement: they are self-reported, so any client can claim any name.
@access: confidential';
-- Set comment to column: "created_by" on table: "ai_products"
COMMENT ON COLUMN "ai_products"."created_by" IS 'Subject URN of the staff member who created the row; null for seeded rows.
@access: confidential-pii';
-- Set comment to column: "updated_by" on table: "ai_products"
COMMENT ON COLUMN "ai_products"."updated_by" IS 'Subject URN of the staff member who last changed the row; null for seeded rows.
@access: confidential-pii';
-- Set comment to column: "created_at" on table: "ai_products"
COMMENT ON COLUMN "ai_products"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "ai_products"
COMMENT ON COLUMN "ai_products"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "ai_products"
COMMENT ON COLUMN "ai_products"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "ai_products"
COMMENT ON COLUMN "ai_products"."deleted" IS '@access: confidential';
-- Create "ai_product_cimd_documents" table
CREATE TABLE "ai_product_cimd_documents" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "ai_product_id" text NOT NULL,
  "url" text NOT NULL,
  "display_name" text NOT NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "is_pattern" boolean NOT NULL DEFAULT false,
  "verified_at" timestamptz NULL,
  "verification_error" text NULL,
  "created_by" text NULL,
  "updated_by" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "ai_product_cimd_documents_ai_product_id_fkey" FOREIGN KEY ("ai_product_id") REFERENCES "ai_products" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "ai_product_cimd_documents_display_name_check" CHECK (display_name <> ''::text),
  CONSTRAINT "ai_product_cimd_documents_url_check" CHECK (url <> ''::text)
);
-- Create index "ai_product_cimd_documents_ai_product_id_idx" to table: "ai_product_cimd_documents"
CREATE INDEX "ai_product_cimd_documents_ai_product_id_idx" ON "ai_product_cimd_documents" ("ai_product_id");
-- Create index "ai_product_cimd_documents_url_key" to table: "ai_product_cimd_documents"
CREATE UNIQUE INDEX "ai_product_cimd_documents_url_key" ON "ai_product_cimd_documents" ("url") WHERE (deleted IS FALSE);
-- Set comment to table: "ai_product_cimd_documents"
COMMENT ON TABLE "ai_product_cimd_documents" IS 'CIMD documents each catalog product publishes. Every enabled live row admits its client_id on presets-mode issuers.';
-- Set comment to column: "id" on table: "ai_product_cimd_documents"
COMMENT ON COLUMN "ai_product_cimd_documents"."id" IS '@access: confidential';
-- Set comment to column: "ai_product_id" on table: "ai_product_cimd_documents"
COMMENT ON COLUMN "ai_product_cimd_documents"."ai_product_id" IS '@access: confidential';
-- Set comment to column: "url" on table: "ai_product_cimd_documents"
COMMENT ON COLUMN "ai_product_cimd_documents"."url" IS 'The client_id the product presents. Matching is exact, case-sensitive string equality with no normalization (so a trailing slash is a different document), and never origin matching. When is_pattern is set, * stands for exactly one complete path segment, and the scheme and host stay literal.
@access: confidential';
-- Set comment to column: "display_name" on table: "ai_product_cimd_documents"
COMMENT ON COLUMN "ai_product_cimd_documents"."display_name" IS 'How the admission catalog lists this document; per document, since a vendor''s connector and shared documents differ.
@access: confidential';
-- Set comment to column: "enabled" on table: "ai_product_cimd_documents"
COMMENT ON COLUMN "ai_product_cimd_documents"."enabled" IS 'Gates the document without deleting it. A disabled document is inert for admission but still listed, and still holds its URL. Admission is an OR over documents, so disabling one does not de-admit a URL another enabled document still matches.
@access: confidential';
-- Set comment to column: "is_pattern" on table: "ai_product_cimd_documents"
COMMENT ON COLUMN "ai_product_cimd_documents"."is_pattern" IS 'Whether url is a wildcard pattern rather than a literal client_id. Explicit rather than inferred from *, so an exact URL is never read as a pattern.
@access: confidential';
-- Set comment to column: "verified_at" on table: "ai_product_cimd_documents"
COMMENT ON COLUMN "ai_product_cimd_documents"."verified_at" IS 'When the live document was last checked; null if never.
@access: confidential';
-- Set comment to column: "verification_error" on table: "ai_product_cimd_documents"
COMMENT ON COLUMN "ai_product_cimd_documents"."verification_error" IS 'Reason code from the last check; null when it passed or never ran.
@access: confidential';
-- Set comment to column: "created_by" on table: "ai_product_cimd_documents"
COMMENT ON COLUMN "ai_product_cimd_documents"."created_by" IS 'Subject URN of the staff member who created the row; null for seeded rows.
@access: confidential-pii';
-- Set comment to column: "updated_by" on table: "ai_product_cimd_documents"
COMMENT ON COLUMN "ai_product_cimd_documents"."updated_by" IS 'Subject URN of the staff member who last changed the row; null for seeded rows.
@access: confidential-pii';
-- Set comment to column: "created_at" on table: "ai_product_cimd_documents"
COMMENT ON COLUMN "ai_product_cimd_documents"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "ai_product_cimd_documents"
COMMENT ON COLUMN "ai_product_cimd_documents"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "ai_product_cimd_documents"
COMMENT ON COLUMN "ai_product_cimd_documents"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "ai_product_cimd_documents"
COMMENT ON COLUMN "ai_product_cimd_documents"."deleted" IS '@access: confidential';
