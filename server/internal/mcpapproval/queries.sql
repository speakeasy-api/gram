-- name: ListApprovalRequests :many
-- Every query in this file is bounded by project_id without exception. A
-- request id appears in dashboard URLs and is not a secret, so a lookup keyed
-- on id alone would let any caller who learns one read another tenant's data.
SELECT
  r.*
  , (
      SELECT count(*)
      FROM mcp_approval_request_requesters req
      WHERE req.mcp_approval_request_id = r.id
        AND req.project_id = r.project_id
        AND req.deleted IS FALSE
    ) AS requester_count
FROM mcp_approval_requests r
WHERE r.project_id = @project_id
  AND r.deleted IS FALSE
  AND (sqlc.narg(status)::text IS NULL OR r.status = sqlc.narg(status)::text)
ORDER BY r.updated_at DESC
LIMIT sqlc.arg(page_limit)::int;

-- name: GetApprovalRequest :one
SELECT
  r.*
  , (
      SELECT count(*)
      FROM mcp_approval_request_requesters req
      WHERE req.mcp_approval_request_id = r.id
        AND req.project_id = r.project_id
        AND req.deleted IS FALSE
    ) AS requester_count
FROM mcp_approval_requests r
WHERE r.id = @id
  AND r.project_id = @project_id
  AND r.deleted IS FALSE;

-- name: ListRequestersForApprovalRequest :many
SELECT *
FROM mcp_approval_request_requesters
WHERE mcp_approval_request_id = @mcp_approval_request_id
  AND project_id = @project_id
  AND deleted IS FALSE
ORDER BY requested_at ASC;

-- name: ListDecisionsForApprovalRequest :many
SELECT *
FROM mcp_approval_decisions
WHERE mcp_approval_request_id = @mcp_approval_request_id
  AND project_id = @project_id
  AND deleted IS FALSE
ORDER BY decided_at DESC;

-- name: CreateApprovalDecision :one
-- evidence_version carries no default in the schema: the writer must copy the
-- version off the request it snapshotted, so a v2 payload cannot be silently
-- recorded as v1.
INSERT INTO mcp_approval_decisions (
  organization_id
  , project_id
  , mcp_approval_request_id
  , decision
  , decided_by
  , rationale
  , evidence_snapshot
  , evidence_version
  , granted_principal_urns
) VALUES (
  @organization_id
  , @project_id
  , @mcp_approval_request_id
  , @decision
  , @decided_by
  , sqlc.narg(rationale)::text
  , @evidence_snapshot
  , @evidence_version
  , @granted_principal_urns
)
RETURNING *;

-- name: SetApprovalRequestStatus :exec
UPDATE mcp_approval_requests
SET status = @status
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;
