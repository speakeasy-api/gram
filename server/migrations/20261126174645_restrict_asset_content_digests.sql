-- Set comment to column: "name" on table: "assets"
COMMENT ON COLUMN "assets"."name" IS 'Stored asset name, which includes a content digest for chat attachments.
@access: restricted';
-- Set comment to column: "url" on table: "assets"
COMMENT ON COLUMN "assets"."url" IS 'Storage path for the asset, which includes a content digest for chat attachments.
@access: restricted';
-- Set comment to column: "sha256" on table: "assets"
COMMENT ON COLUMN "assets"."sha256" IS 'SHA-256 digest of the stored asset content.
@access: restricted';
-- Set comment to column: "content_asset_url" on table: "chat_content_parts"
COMMENT ON COLUMN "chat_content_parts"."content_asset_url" IS 'Storage path for the content part. Its file name is a digest of the content.
@access: restricted';
