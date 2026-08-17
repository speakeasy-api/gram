-- atlas:txmode none

-- Create index "trials_ends_at_idx" to table: "trials"
CREATE INDEX CONCURRENTLY "trials_ends_at_idx" ON "trials" ("ends_at") WHERE ((converted_at IS NULL) AND (demoted_at IS NULL));
