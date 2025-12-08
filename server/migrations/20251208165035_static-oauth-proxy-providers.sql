-- Modify "oauth_proxy_providers" table
ALTER TABLE "oauth_proxy_providers" ALTER COLUMN "authorization_endpoint" DROP NOT NULL, ALTER COLUMN "token_endpoint" DROP NOT NULL, ADD COLUMN "provider_type" text NOT NULL DEFAULT 'custom';
