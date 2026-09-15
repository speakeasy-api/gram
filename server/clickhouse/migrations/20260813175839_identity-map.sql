-- Create "identity_map" table
CREATE TABLE `identity_map` (
  `org_id` String COMMENT 'Organization the identity belongs to.',
  `email_lower` String COMMENT 'Normalized (lowercased, trimmed) email observed in telemetry.',
  `canonical_user_id` String COMMENT 'Directory user id the email resolves to.',
  `canonical_email` String COMMENT 'Normalized directory email of the owning user - the fold target for analytics.'
) ENGINE = Join(ANY, LEFT, org_id, email_lower) COMMENT 'Employee identity fold map synced from Postgres. Maps each unambiguous directory or linked-account email to its owning user so analytics reads can fold one employee into one identity via joinGet, with missing keys returning the empty string to signal fall-back-to-literal. Ambiguous emails are deliberately absent.';
-- Create "identity_map_staging" table
CREATE TABLE `identity_map_staging` (
  `org_id` String COMMENT 'Organization the identity belongs to.',
  `email_lower` String COMMENT 'Normalized (lowercased, trimmed) email observed in telemetry.',
  `canonical_user_id` String COMMENT 'Directory user id the email resolves to.',
  `canonical_email` String COMMENT 'Normalized directory email of the owning user - the fold target for analytics.'
) ENGINE = Join(ANY, LEFT, org_id, email_lower) COMMENT 'Staging twin of identity_map. The sync worker rebuilds this table then swaps it live with EXCHANGE TABLES so readers never observe a partial map.';
