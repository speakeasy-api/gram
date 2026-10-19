-- name: UpsertBlockedApproval :one
-- Records the block of a call held by a require_approval policy. A retry of
-- the same call while its request is still open lands on the existing row, so
-- the agent sees the same token again; @token is used only for a new row.
INSERT INTO risk_policy_approvals (
    organization_id
  , project_id
  , risk_policy_id
  , policy_version
  , token
  , requester_urn
  , requester_user_id
  , surface
  , source_adapter
  , session_id
  , chat_id
  , toolset_id
  , server_url
  , tool_name
  , call_fingerprint
  , arguments_preview
  , status
)
VALUES (
    @organization_id
  , @project_id
  , @risk_policy_id
  , @policy_version
  , @token
  , @requester_urn
  , sqlc.narg(requester_user_id)::text
  , @surface
  , sqlc.narg(source_adapter)::text
  , sqlc.narg(session_id)::text
  , sqlc.narg(chat_id)::uuid
  , sqlc.narg(toolset_id)::uuid
  , sqlc.narg(server_url)::text
  , @tool_name
  , @call_fingerprint
  , @arguments_preview
  , 'blocked'
)
ON CONFLICT (project_id, risk_policy_id, requester_urn, tool_name, call_fingerprint)
WHERE deleted IS FALSE AND status IN ('blocked', 'pending')
DO UPDATE
SET updated_at = clock_timestamp()
RETURNING *;

-- name: ExpireLapsedOpenApproval :exec
-- Closes a held call's open request whose window has passed, so a retry of
-- the same call starts a new request instead of landing on the lapsed one.
UPDATE risk_policy_approvals
SET status = 'expired'
  , updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND risk_policy_id = @risk_policy_id
  AND requester_urn = @requester_urn
  AND tool_name = @tool_name
  AND call_fingerprint = @call_fingerprint
  AND status IN ('blocked', 'pending')
  AND expires_at IS NOT NULL
  AND expires_at <= clock_timestamp()
  AND deleted IS FALSE;

-- name: ConsumeApprovedApproval :one
-- Uses an exact-once approval for a retry of the same call by the same
-- principal. The status predicate on the outer UPDATE makes consumption
-- atomic: two concurrent retries cannot both use one approval.
UPDATE risk_policy_approvals
SET status = 'consumed'
  , consumed_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE id = (
    SELECT a.id
    FROM risk_policy_approvals a
    WHERE a.organization_id = @organization_id
      AND a.project_id = @project_id
      AND a.risk_policy_id = @risk_policy_id
      AND a.requester_urn = @requester_urn
      AND a.tool_name = @tool_name
      AND a.call_fingerprint = @call_fingerprint
      AND a.status = 'approved'
      AND a.deleted IS FALSE
      AND (a.expires_at IS NULL OR a.expires_at > clock_timestamp())
    ORDER BY a.decided_at DESC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
  )
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND status = 'approved'
RETURNING *;

-- name: GetApprovalByToken :one
-- Resolves an agent-facing request token. The token is the only handle agents
-- hold, so this lookup is not project-scoped; every write that follows is.
SELECT *
FROM risk_policy_approvals
WHERE token = @token
  AND deleted IS FALSE;

-- name: GetApproval :one
SELECT *
FROM risk_policy_approvals
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND id = @id
  AND deleted IS FALSE;

-- name: MarkApprovalRequested :one
-- Moves a blocked request to pending once the agent asks for approval. A
-- request that is already pending is returned unchanged by the caller, so
-- repeating request_approval never re-notifies approvers.
UPDATE risk_policy_approvals
SET status = 'pending'
  , reason = sqlc.narg(reason)::text
  , requested_scope = @requested_scope
  , requested_at = clock_timestamp()
  , expires_at = @expires_at
  , updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND id = @id
  AND status = 'blocked'
  AND deleted IS FALSE
RETURNING *;

-- name: DecideApproval :one
-- Records an approver's decision on a pending request. Only a pending,
-- unexpired request moves, so a replayed or late decision changes nothing.
UPDATE risk_policy_approvals
SET status = @status
  , decided_by_urn = @decided_by_urn
  , decided_via = @decided_via
  , decision_note = sqlc.narg(decision_note)::text
  , granted_scope = @granted_scope
  , decided_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND id = @id
  AND status = 'pending'
  AND (expires_at IS NULL OR expires_at > clock_timestamp())
  AND deleted IS FALSE
RETURNING *;

-- name: ListApprovals :many
-- Lists a project's approval requests, newest first, optionally narrowed to
-- one status.
SELECT *
FROM risk_policy_approvals
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  AND (sqlc.narg(cursor)::uuid IS NULL OR id < sqlc.narg(cursor)::uuid)
ORDER BY id DESC
LIMIT @page_limit;
