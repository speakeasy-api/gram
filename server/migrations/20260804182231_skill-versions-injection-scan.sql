-- atlas:txmode none

-- Modify "skill_versions" table
ALTER TABLE "skill_versions" ADD COLUMN "injection_scanned_at" timestamptz NULL, ADD COLUMN "injection_flagged" boolean NULL, ADD COLUMN "injection_rationale" text NULL;
-- Create index "skill_versions_injection_scan_idx" to table: "skill_versions"
CREATE INDEX CONCURRENTLY "skill_versions_injection_scan_idx" ON "skill_versions" ("id") WHERE (injection_scanned_at IS NULL);
