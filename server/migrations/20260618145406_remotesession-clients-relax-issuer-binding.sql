-- atlas:txmode none

-- Drop index "remote_session_client_user_session_issuers_one_per_issuer" from table: "remote_session_client_user_session_issuers"
DROP INDEX CONCURRENTLY "remote_session_client_user_session_issuers_one_per_issuer";
-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ALTER COLUMN "user_session_issuer_id" DROP NOT NULL;
