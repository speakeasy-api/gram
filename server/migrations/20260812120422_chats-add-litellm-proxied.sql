-- Modify "chats" table
ALTER TABLE "chats" ADD COLUMN "litellm_proxied" boolean NOT NULL DEFAULT false;
