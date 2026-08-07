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

-- name: GetResearchReportForDecision :one
-- Resolves a report a decision wants to cite, pinned to the request being
-- decided and to the caller's project, so a decision can never attribute
-- research about one server to another.
SELECT id
FROM mcp_research_reports
WHERE id = @id
  AND mcp_approval_request_id = @mcp_approval_request_id
  AND project_id = @project_id
  AND deleted IS FALSE;

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
  , mcp_research_report_id
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
  , sqlc.narg(mcp_research_report_id)::uuid
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
-- Re-requesting a server reuses the same row rather than starting a second
-- review, so decisions accumulate as history against one target per project.
-- target_key is what deduplicates; target_raw stays as the requester wrote it.
--
-- A re-request reopens a denied review: the denial stays in the decision
-- history, and the request returns to the queue. An approved or still-pending
-- request keeps its status — re-asking for an approved server changes
-- nothing, and an admin can re-decide at any time.
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
  , status = CASE
      WHEN mcp_approval_requests.status = 'denied' THEN EXCLUDED.status
      ELSE mcp_approval_requests.status
    END
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

-- name: GetApprovalRequestForDecision :one
-- Locking read used inside the decision transaction. Serialises concurrent
-- decisions on the same request, so the request's status always matches the
-- newest decision rather than whichever transaction happened to commit last.
SELECT id, organization_id, target_raw, status, current_evidence, evidence_version
FROM mcp_approval_requests
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
FOR UPDATE;

-- name: ListResearchReportsForApprovalRequest :many
SELECT *
FROM mcp_research_reports
WHERE mcp_approval_request_id = @mcp_approval_request_id
  AND project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: CreateResearchReport :one
INSERT INTO mcp_research_reports (
  organization_id
  , project_id
  , mcp_approval_request_id
  , status
  , report
  , report_version
  , model
  , prompt_version
  , requested_by
  , started_at
  , completed_at
  , error
) VALUES (
  @organization_id
  , @project_id
  , @mcp_approval_request_id
  , @status
  , @report
  , @report_version
  , sqlc.narg(model)::text
  , sqlc.narg(prompt_version)::text
  , sqlc.narg(requested_by)::text
  , sqlc.narg(started_at)::timestamptz
  , sqlc.narg(completed_at)::timestamptz
  , sqlc.narg(error)::text
)
RETURNING *;
