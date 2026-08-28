-- atlas:txmode none

-- Modify "killswitch_prescription_versions" table
ALTER TABLE "killswitch_prescription_versions" ADD COLUMN "expiry_event_recorded_at" timestamptz NULL;
-- Create index "killswitch_prescription_versions_expiry_due_idx" to table: "killswitch_prescription_versions"
CREATE INDEX CONCURRENTLY "killswitch_prescription_versions_expiry_due_idx" ON "killswitch_prescription_versions" ("expires_at", "prescription_id", "version") WHERE ((state = 'active'::text) AND (expires_at IS NOT NULL) AND (expiry_event_recorded_at IS NULL) AND ((superseded_at IS NULL) OR (expires_at < superseded_at)));
