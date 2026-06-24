-- Modify "chats" table
ALTER TABLE "chats" ADD COLUMN "title_manually_set" boolean NOT NULL DEFAULT false;
