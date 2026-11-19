-- Modify "ai_products" table
ALTER TABLE "ai_products" ADD CONSTRAINT "ai_products_family_check" CHECK (family <> ''::text), ADD COLUMN "family" text NULL;
-- Set comment to column: "family" on table: "ai_products"
COMMENT ON COLUMN "ai_products"."family" IS 'Groups a vendor''s related products, e.g. claude for Claude Code, Claude and Cowork. Null means the product is a family of its own.
@access: confidential';
