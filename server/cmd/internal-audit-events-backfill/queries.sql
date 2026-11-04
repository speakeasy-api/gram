-- name: SeedAuditLogs :exec
INSERT INTO audit_logs (organization_id, actor_id, actor_type, subject_id, subject_type, action, created_at, is_internal, metadata) VALUES
 ('test-org','actor','user','one','catalog_promotion','other',@older::timestamptz,false,'{"keep":true}'),
 ('test-org','actor','user','two','mcp_server','mcp-server:promote-catalogue',@older::timestamptz,false,NULL),
 ('test-org','actor','user','three','catalog_promotion','other',@older::timestamptz,false,NULL),
 ('test-org','actor','user','unrelated','mcp_server','mcp-server:update',@older::timestamptz,false,NULL),
 ('test-org','actor','user','exact-cutoff','catalog_promotion','other',@cutoff::timestamptz,false,NULL),
 ('test-org','actor','user','new','catalog_promotion','other',@newer::timestamptz,false,NULL),
 ('test-org','actor','user','already','catalog_promotion','other',@older::timestamptz,true,NULL);

-- name: SeedSecurityLogs :exec
INSERT INTO security_audit_logs (organization_id, actor_id, actor_type, subject_id, subject_type, action, created_at, is_internal, metadata) VALUES
 ('test-org','actor','user','one','catalog_promotion','other',@older::timestamptz,false,'{"keep":true}'),
 ('test-org','actor','user','two','mcp_server','mcp-server:promote-catalogue',@older::timestamptz,false,NULL),
 ('test-org','actor','user','three','catalog_promotion','other',@older::timestamptz,false,NULL),
 ('test-org','actor','user','unrelated','mcp_server','mcp-server:update',@older::timestamptz,false,NULL),
 ('test-org','actor','user','exact-cutoff','catalog_promotion','other',@cutoff::timestamptz,false,NULL),
 ('test-org','actor','user','new','catalog_promotion','other',@newer::timestamptz,false,NULL),
 ('test-org','actor','user','already','catalog_promotion','other',@older::timestamptz,true,NULL);

-- name: Snapshot :one
SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM audit_logs a),(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM security_audit_logs a),(SELECT count(*) FROM outbox),(SELECT count(*) FROM publish_outbox))::text;

-- name: Data :one
SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(a)-'is_internal' ORDER BY id) FROM audit_logs a),(SELECT jsonb_agg(to_jsonb(a)-'is_internal' ORDER BY id) FROM security_audit_logs a))::text;

-- name: CountUnchangedAuditLogs :one
SELECT count(*) FROM audit_logs WHERE NOT is_internal AND subject_id IN ('unrelated','exact-cutoff','new');

-- name: CountUnchangedSecurityLogs :one
SELECT count(*) FROM security_audit_logs WHERE NOT is_internal AND subject_id IN ('unrelated','exact-cutoff','new');

-- name: CountOutbox :one
SELECT (SELECT count(*) FROM outbox)+(SELECT count(*) FROM publish_outbox);

-- name: ResetAuditLogs :exec
UPDATE audit_logs SET is_internal=false WHERE subject_id IN ('one','two','three');

-- name: CountUpdatedAuditLogs :one
SELECT count(*) FROM audit_logs WHERE is_internal AND subject_id IN ('one','two','three');
