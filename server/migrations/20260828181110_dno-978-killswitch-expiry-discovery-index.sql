-- atlas:txmode none

-- Create index "killswitch_prescription_versions_expiry_due_idx" to table: "killswitch_prescription_versions"
CREATE INDEX CONCURRENTLY "killswitch_prescription_versions_expiry_due_idx" ON "killswitch_prescription_versions" ("expires_at", "prescription_id", "version") WHERE ((state = 'active'::text) AND (expires_at IS NOT NULL) AND ((superseded_at IS NULL) OR (expires_at < superseded_at)));
