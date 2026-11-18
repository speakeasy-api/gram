-- name: SnapshotAgentMemberAuthority :one
-- Capture the complete isolated test database authority before and after serving.
SELECT jsonb_build_object(
  'bindings', (SELECT jsonb_agg(to_jsonb(b) ORDER BY b.id) FROM principal_remote_session_bindings b WHERE principal_id = @agent_id::uuid),
  'agent', (SELECT to_jsonb(a) FROM agents a WHERE id = @agent_id::uuid),
  'grants', (SELECT jsonb_agg(to_jsonb(g) ORDER BY g.id) FROM principal_grants g),
  'sessions', (SELECT jsonb_agg(jsonb_build_array(s.id, s.grant_generation, s.deleted) ORDER BY s.id) FROM remote_sessions s)
)::text AS snapshot;

-- name: CreateWorkloadAgentAssignment :one
INSERT INTO workload_agent_assignments (organization_id, workload_issuer_id, subject, agent_id)
VALUES (@organization_id, @workload_issuer_id, @subject, @agent_id)
RETURNING id;

-- name: SoftDeleteWorkloadAgentAssignment :exec
UPDATE workload_agent_assignments SET deleted_at = clock_timestamp() WHERE id = @id;
