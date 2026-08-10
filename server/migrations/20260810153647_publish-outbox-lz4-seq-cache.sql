-- MANUALLY AUTHORED MIGRATION
-- Atlas models neither Postgres TOAST compression nor identity sequence
-- options: it parses both from database/schema.sql and emits neither, in
-- either direction. Creating this migration by hand so the SDL and the database
-- agree. Both statements are metadata-only; neither rewrites the table.
ALTER TABLE "publish_outbox" ALTER COLUMN "message" SET COMPRESSION lz4;

-- The name of this sequence was confirmed on local, dev and prod databases.
ALTER SEQUENCE "publish_outbox_id_seq" CACHE 32;
