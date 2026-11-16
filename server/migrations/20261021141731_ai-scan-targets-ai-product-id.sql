-- atlas:txmode none

-- Modify "ai_scan_targets" table
ALTER TABLE "ai_scan_targets" ADD COLUMN "ai_product_id" text NULL, ADD CONSTRAINT "ai_scan_targets_ai_product_id_fkey" FOREIGN KEY ("ai_product_id") REFERENCES "ai_products" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
-- Create index "ai_scan_targets_ai_product_id_idx" to table: "ai_scan_targets"
CREATE INDEX CONCURRENTLY "ai_scan_targets_ai_product_id_idx" ON "ai_scan_targets" ("ai_product_id") WHERE (ai_product_id IS NOT NULL);
-- Set comment to column: "ai_product_id" on table: "ai_scan_targets"
COMMENT ON COLUMN "ai_scan_targets"."ai_product_id" IS 'The catalog product a built-in''s row records a decision about, equal to id; null for a target the organization added. Makes a row''s kind durable rather than inferred from whether id happens to match a product. Deleting a referenced product is refused rather than nulling this, which would silently turn the decision into an empty target of the organization''s own.
@access: confidential';
