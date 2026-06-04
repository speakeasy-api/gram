-- Queries for the risk-policy access request workflow.
-- policy_access_requests is current state for one requester+policy+target.
-- Active bypasses themselves live in principal_grants and are listed/revoked
-- from there.

-- name: CreatePolicyAccessRequest :one
-- Create or refresh a policy access request. Repeat blocks for the same
-- org/requester/policy/target update the existing row instead of appending.
INSERT INTO policy_access_requests (
  organization_id, project_id, policy_id,
  target_kind, target_label, target_key, target_dimensions,
  requester_user_id, requester_email, note
)
VALUES (
  @organization_id, @project_id, @policy_id,
  @target_kind, @target_label, @target_key, @target_dimensions,
  @requester_user_id, @requester_email, @note
)
ON CONFLICT (organization_id, requester_user_id, policy_id, target_key)
DO UPDATE SET
  project_id = EXCLUDED.project_id,
  target_kind = EXCLUDED.target_kind,
  target_label = EXCLUDED.target_label,
  target_dimensions = EXCLUDED.target_dimensions,
  requester_email = EXCLUDED.requester_email,
  note = EXCLUDED.note,
  status = 'requested',
  decided_by = '',
  granted_principal_urns = ARRAY[]::TEXT[],
  decided_at = NULL,
  deleted_at = NULL,
  updated_at = clock_timestamp()
RETURNING *;

-- name: ListPolicyAccessRequests :many
SELECT
  policy_access_requests.*,
  COALESCE(risk_policies.name, '')::text AS policy_name
FROM policy_access_requests
LEFT JOIN risk_policies
  ON risk_policies.id = policy_access_requests.policy_id
  AND risk_policies.organization_id = policy_access_requests.organization_id
  AND risk_policies.deleted IS FALSE
WHERE policy_access_requests.organization_id = @organization_id
  AND policy_access_requests.deleted IS FALSE
  AND (sqlc.narg(status)::text IS NULL OR policy_access_requests.status = sqlc.narg(status)::text)
ORDER BY policy_access_requests.updated_at DESC;

-- name: GetPolicyAccessRequest :one
SELECT *
FROM policy_access_requests
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE;

-- name: GetRequestedPolicyAccessRequestForUpdate :one
SELECT *
FROM policy_access_requests
WHERE organization_id = @organization_id
  AND id = @id
  AND status = 'requested'
  AND deleted IS FALSE
FOR UPDATE;

-- name: DecidePolicyAccessRequest :one
-- Transition a pending request to approved/denied. Only mutates a row that is
-- still requested so concurrent decisions do not clobber each other.
UPDATE policy_access_requests
SET status = @status,
    decided_by = @decided_by,
    granted_principal_urns = @granted_principal_urns,
    decided_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND status = 'requested'
  AND deleted IS FALSE
RETURNING *;

-- name: ListPolicyBypasses :many
SELECT
  principal_grants.id,
  principal_grants.principal_urn,
  principal_grants.principal_type,
  principal_grants.selectors,
  principal_grants.created_at,
  principal_grants.updated_at,
  (principal_grants.selectors->>'resource_id')::uuid AS policy_id,
  COALESCE(risk_policies.name, '')::text AS policy_name
FROM principal_grants
LEFT JOIN risk_policies
  ON risk_policies.id = (principal_grants.selectors->>'resource_id')::uuid
  AND risk_policies.organization_id = principal_grants.organization_id
  AND risk_policies.deleted IS FALSE
WHERE principal_grants.organization_id = @organization_id
  AND principal_grants.scope = 'risk_policy:bypass'
  AND COALESCE(principal_grants.effect, 'allow') = 'allow'
  AND principal_grants.selectors @> jsonb_build_object('resource_kind', 'risk_policy')
ORDER BY principal_grants.created_at DESC;

-- name: DeletePolicyBypass :one
DELETE FROM principal_grants
WHERE id = @grant_id
  AND organization_id = @organization_id
  AND scope = 'risk_policy:bypass'
  AND COALESCE(effect, 'allow') = 'allow'
RETURNING id, principal_urn, selectors;

-- name: UpdatePolicyAccessRequestAfterBypassRevoked :many
UPDATE policy_access_requests
SET granted_principal_urns = array_remove(granted_principal_urns, @principal_urn::text),
    deleted_at = CASE
      WHEN cardinality(array_remove(granted_principal_urns, @principal_urn::text)) = 0
        THEN clock_timestamp()
      ELSE deleted_at
    END,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND policy_id = @policy_id
  AND target_key = @target_key
  AND status = 'approved'
  AND deleted IS FALSE
  AND @principal_urn::text = ANY(granted_principal_urns)
RETURNING *;
