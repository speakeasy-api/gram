-- Modify "toolsets" table
ALTER TABLE "toolsets" ADD COLUMN "user_session_issuer_id" uuid NULL, ADD CONSTRAINT "toolsets_user_session_issuer_id_fkey" FOREIGN KEY ("user_session_issuer_id") REFERENCES "user_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
