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
  -- Unreviewed rows are evidence dossiers nobody has asked about; they live
  -- on server pages, not in the queue, unless a caller names the status.
  AND (sqlc.narg(status)::text IS NOT NULL OR r.status <> 'unreviewed')
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

-- name: GetApprovalRequestByTarget :one
-- Resolves the review tracking a target within the caller's project, so the
-- read-side ensure path can return an existing dossier without re-admitting
-- it — a page view must not re-run evidence gathering or audit a create that
-- did not happen.
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
  AND r.target_kind = @target_kind
  AND r.target_key = @target_key
  AND r.deleted IS FALSE;

-- name: ListApprovalRequestsByTargetKeys :many
-- Resolves the approval request tracking each of a set of canonical server
-- URLs, so the Shadow MCP inventory can join approval state onto its rows.
-- target_key is unique per (project, kind), so this returns at most one row
-- per key.
SELECT
  r.id
  , r.target_key
  , r.status
  , (
      SELECT count(*)
      FROM mcp_approval_request_requesters req
      WHERE req.mcp_approval_request_id = r.id
        AND req.project_id = r.project_id
        AND req.deleted IS FALSE
    ) AS requester_count
FROM mcp_approval_requests r
WHERE r.project_id = @project_id
  AND r.target_kind = 'server_url'
  AND r.target_key = ANY (@target_keys::text[])
  AND r.deleted IS FALSE;

-- name: ListApprovalRequestTargets :many
-- Every review in a project, any kind and any status, with the requester
-- count the unified servers table displays. Bounded by the one-review-per-
-- target invariant, so the scan is as small as the project's server set.
SELECT
  r.id
  , r.target_kind
  , r.target_raw
  , r.target_key
  , r.status
  , r.created_at
  , r.updated_at
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
ORDER BY r.updated_at DESC, r.id DESC;

-- name: ListServerURLApprovalRequests :many
-- Every server_url review in a project, for resolving a server page slug to
-- the request tracking it. A server known only through a request has no
-- telemetry inventory row, so this is the page's fallback identity source.
-- The slug is a hash derived from target_key, so it cannot be matched in SQL;
-- the scan is bounded by project and carries only the columns the fallback
-- reads, with no per-row aggregates.
SELECT
  r.target_key
  , r.updated_at
FROM mcp_approval_requests r
WHERE r.project_id = @project_id
  AND r.target_kind = 'server_url'
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
-- target_key is what deduplicates; target_raw is the redacted display form of
-- the reference (URLs stripped of query and userinfo, commands stripped of
-- credential-shaped values), never the verbatim input — it reaches the queue,
-- the audit feed, and the webhook stream.
--
-- A real ask (incoming status 'requested') reopens a denied review and
-- upgrades an unreviewed evidence dossier: the history stays, and the request
-- joins the queue. An approved or still-pending request keeps its status —
-- re-asking changes nothing, and an admin can re-decide at any time. An
-- incoming dossier ('unreviewed') never downgrades an existing row.
INSERT INTO mcp_approval_requests (
  organization_id
  , project_id
  , target_kind
  , target_raw
  , target_key
  , artifact_ref
  , version_pinned
  , status
  , risk_policy_bypass_request_id
) VALUES (
  @organization_id
  , @project_id
  , @target_kind
  , @target_raw
  , @target_key
  , sqlc.narg(artifact_ref)::text
  , @version_pinned
  , @status
  , sqlc.narg(risk_policy_bypass_request_id)::uuid
)
ON CONFLICT (project_id, target_kind, target_key) WHERE deleted IS FALSE DO UPDATE
SET updated_at = clock_timestamp()
  , status = CASE
      WHEN EXCLUDED.status = 'requested'
        AND mcp_approval_requests.status IN ('denied', 'unreviewed')
        THEN EXCLUDED.status
      ELSE mcp_approval_requests.status
    END
  -- A later promotion links its bypass request onto an existing review; a
  -- proactive re-request never clears an existing link.
  , risk_policy_bypass_request_id = COALESCE(EXCLUDED.risk_policy_bypass_request_id, mcp_approval_requests.risk_policy_bypass_request_id)
-- inserted distinguishes a fresh row from a reused one (xmax is zero only for
-- rows this statement inserted), so the caller can avoid auditing a create
-- when concurrent dossier opens or a gather retry landed on an existing row.
RETURNING *, (xmax = 0) AS inserted;

-- name: UpsertApprovalRequestRequester :one
-- One row per person per request: ten people wanting the same server is one
-- review with ten requesters, and one person asking twice is still one. A
-- repeat ask keeps the freshest justification without erasing an earlier one
-- when the new ask carries none.
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
ON CONFLICT (mcp_approval_request_id, user_id) WHERE deleted IS FALSE DO UPDATE
SET note = COALESCE(EXCLUDED.note, mcp_approval_request_requesters.note)
  , user_email = COALESCE(EXCLUDED.user_email, mcp_approval_request_requesters.user_email)
  , requested_at = clock_timestamp()
  , updated_at = clock_timestamp()
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

-- name: SetApprovalRequestEvidenceIfUnchanged :execrows
-- Compare-and-set variant for explicit refreshes: writes only when the stored
-- gather is still the one the caller read before gathering, so two concurrent
-- refreshes cannot land an older gather over a newer one. Zero rows means a
-- concurrent write won and the caller's gather is discarded.
UPDATE mcp_approval_requests
SET current_evidence = @current_evidence
  , evidence_version = @evidence_version
  , evidence_collected_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND evidence_collected_at IS NOT DISTINCT FROM @previous_collected_at
  AND deleted IS FALSE;

-- name: RefreshApprovalRequestEvidence :execrows
-- Compare-and-set variant used by the read-path gap retry. The fresh document
-- lands only while the stored evidence is still the exact gather the caller
-- read and judged gapped: a slower gather losing a race to a concurrent
-- refresh matches zero rows instead of replacing the newer document, and the
-- loser re-reads the winner's evidence.
UPDATE mcp_approval_requests
SET current_evidence = @current_evidence
  , evidence_version = @evidence_version
  , evidence_collected_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND evidence_collected_at = sqlc.arg(observed_collected_at)::timestamptz
  AND deleted IS FALSE;

-- name: GetApprovalRequestForDecision :one
-- Locking read used inside the decision transaction. Serialises concurrent
-- decisions on the same request, so the request's status always matches the
-- newest decision rather than whichever transaction happened to commit last.
SELECT id, organization_id, target_kind, target_raw, target_key, status, current_evidence, evidence_version, risk_policy_bypass_request_id
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

-- name: GetBypassRequestForPromotion :one
-- Resolved under the caller's project, never by id alone: the id arrives from
-- the caller, and promotion of another project's bypass request into this
-- project's queue is the exact horizontal escalation the org standard forbids.
-- There is deliberately no database-level pin for this pair (see AIS-470), so
-- this predicate is the primary control.
SELECT id, organization_id, project_id, target_kind, target_label, target_key,
       target_dimensions, requester_user_id, requester_email, note
FROM risk_policy_bypass_requests
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: HasRunningResearchReport :one
SELECT EXISTS(
  SELECT 1
  FROM mcp_research_reports
  WHERE mcp_approval_request_id = @mcp_approval_request_id
    AND project_id = @project_id
    AND status = 'running'
    AND deleted IS FALSE
);

-- name: GetRunningResearchReport :one
SELECT *
FROM mcp_research_reports
WHERE mcp_approval_request_id = @mcp_approval_request_id
  AND project_id = @project_id
  AND status = 'running'
  AND deleted IS FALSE
ORDER BY created_at DESC
LIMIT 1;

-- name: GetResearchReport :one
SELECT *
FROM mcp_research_reports
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: CompleteResearchReport :one
UPDATE mcp_research_reports
SET status = 'completed'
  , report = @report
  , report_version = @report_version
  , model = sqlc.narg(model)::text
  , completed_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: FailResearchReport :one
-- Only a run still in flight can fail: a completed report must never be
-- retro-marked failed by a late compensation whose activity result got lost.
UPDATE mcp_research_reports
SET status = 'failed'
  , error = sqlc.narg(error)::text
  , completed_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND status = 'running'
  AND deleted IS FALSE
RETURNING *;
