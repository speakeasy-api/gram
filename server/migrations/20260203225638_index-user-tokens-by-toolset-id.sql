-- atlas:txmode none

-- Drop index "user_oauth_tokens_user_org_issuer_key" from table: "user_oauth_tokens"
DROP INDEX CONCURRENTLY "user_oauth_tokens_user_org_issuer_key";
-- Create index "user_oauth_tokens_user_org_issuer_key" to table: "user_oauth_tokens"
CREATE UNIQUE INDEX CONCURRENTLY "user_oauth_tokens_user_org_issuer_key" ON "user_oauth_tokens" ("user_id", "organization_id", "toolset_id") WHERE (deleted IS FALSE);
