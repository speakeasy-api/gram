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

-- name: UpsertApprovalRequest :one
-- Re-requesting a server reopens the same row rather than starting a second
-- review, so decisions accumulate as history against one target per project.
-- target_key is what deduplicates; target_raw stays as the requester wrote it.
INSERT INTO mcp_approval_requests (
  organization_id
  , project_id
  , target_kind
  , target_raw
  , target_key
  , artifact_ref
  , version_pinned
  , status
) VALUES (
  @organization_id
  , @project_id
  , @target_kind
  , @target_raw
  , @target_key
  , sqlc.narg(artifact_ref)::text
  , @version_pinned
  , @status
)
ON CONFLICT (project_id, target_kind, target_key) WHERE deleted IS FALSE DO UPDATE
SET updated_at = clock_timestamp()
RETURNING *;

-- name: CreateApprovalRequestRequester :one
INSERT INTO mcp_approval_request_requesters (
  organization_id
  , project_id
  , mcp_approval_request_id
  , user_id
  , user_email
  , note
) VALUES (
  @organization_id
  , @project_id
  , @mcp_approval_request_id
  , @user_id
  , sqlc.narg(user_email)::text
  , sqlc.narg(note)::text
)
RETURNING *;

-- name: SetApprovalRequestEvidence :exec
-- Overwrites the current gather. The copy a decision rested on is frozen onto
-- the decision, so refreshing this loses nothing.
UPDATE mcp_approval_requests
SET current_evidence = @current_evidence
  , evidence_version = @evidence_version
  , evidence_collected_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;
