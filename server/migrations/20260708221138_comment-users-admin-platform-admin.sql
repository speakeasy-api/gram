-- Set comment to column: "admin" on table: "users"
COMMENT ON COLUMN "users"."admin" IS 'Maps to the application''s platform_admin concept: TRUE marks a Gram/Speakeasy platform admin. Distinct from the org-level admin role.';
