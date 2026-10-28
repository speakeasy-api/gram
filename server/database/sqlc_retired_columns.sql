-- Columns that application code no longer reads or writes but that are still in
-- schema.sql until their drop migration ships. sqlc packages that list this file
-- after schema.sql generate code as if these columns were already gone, so their
-- expanded `SELECT *` and `RETURNING *` never name them and keep working once the
-- columns are dropped. Remove each statement in the migration that drops its
-- column.

ALTER TABLE okta_identity_provider_connections DROP COLUMN issuer_url_override_reason;
