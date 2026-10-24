-- name: UpsertBlockedApproval :one
-- Records the block of a call held by a require_approval policy. A retry of
-- the same call while its request is still open lands on the existing row, so
-- the agent sees the same token again; @token is used only for a new row.
-- mcp_server_id and chat_id are kept only when they name a row in the project:
-- a Platform MCP call carries a platform toolset id in mcp_server_id, and the
-- chat id comes from a client header that can name an unsaved or foreign chat.
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
  , mcp_server_id
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
  , (SELECT c.id FROM chats c WHERE c.id = sqlc.narg(chat_id)::uuid AND c.project_id = @project_id)
  , sqlc.narg(toolset_id)::uuid
  , (SELECT m.id FROM mcp_servers m WHERE m.id = sqlc.narg(mcp_server_id)::uuid AND m.project_id = @project_id)
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
-- Lists a project's approval requests, newest first. @status filters on the
-- status a viewer sees: an open request or an unused approval past its window
-- counts as expired.
-- @visible_policy_ids, when set, narrows the list to those policies plus the
-- requests @viewer_user_id made; NULL lists every request.
SELECT *
FROM risk_policy_approvals
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
  AND (
    sqlc.narg(status)::text IS NULL
    OR (
      sqlc.narg(status)::text = 'expired'
      AND (
        status = 'expired'
        OR (status IN ('blocked', 'pending', 'approved') AND expires_at <= clock_timestamp())
      )
    )
    OR (
      sqlc.narg(status)::text IN ('blocked', 'pending', 'approved')
      AND status = sqlc.narg(status)::text
      AND (expires_at IS NULL OR expires_at > clock_timestamp())
    )
    OR (
      sqlc.narg(status)::text NOT IN ('expired', 'blocked', 'pending', 'approved')
      AND status = sqlc.narg(status)::text
    )
  )
  AND (
    sqlc.narg(visible_policy_ids)::uuid[] IS NULL
    OR risk_policy_id = ANY(sqlc.narg(visible_policy_ids)::uuid[])
    OR requester_user_id = sqlc.narg(viewer_user_id)::text
  )
  AND (sqlc.narg(cursor)::uuid IS NULL OR id < sqlc.narg(cursor)::uuid)
ORDER BY id DESC
LIMIT @page_limit;

-- name: ListApprovalRecipientMembers :many
-- Active organization members who can receive approval requests: the members
-- in @user_ids, or every member when @all_members is set, which is how role
-- approvers expand to people. The members in @user_ids come first, so a
-- capped list keeps them, and then the order is by id, so it is stable.
SELECT u.id AS user_id, u.email, u.display_name
FROM organization_user_relationships our
JOIN users u ON u.id = our.user_id
WHERE our.organization_id = @organization_id
  AND our.deleted_at IS NULL
  AND u.deleted_at IS NULL
  AND (@all_members::boolean OR u.id = ANY(@user_ids::text[]))
ORDER BY u.id = ANY(@user_ids::text[]) DESC, u.id
LIMIT @max_rows;

-- name: GetApprovalProjectSlugs :one
-- What a link to the request's project needs: the slugs, and the
-- organization's default host for links sent without a request to follow.
SELECT o.slug AS organization_slug, p.slug AS project_slug, o.default_host
FROM projects p
JOIN organization_metadata o ON o.id = p.organization_id
WHERE p.organization_id = @organization_id
  AND p.id = @project_id;

-- name: InsertApprovalMessage :one
-- Records a notification sent for a request. One per recipient and provider,
-- so a retried send that already recorded its message inserts nothing.
INSERT INTO risk_policy_approval_messages (
  organization_id
  , project_id
  , approval_id
  , provider
  , kind
  , recipient_user_id
  , slack_team_id
  , slack_user_id
  , slack_channel_id
  , slack_message_ts
) VALUES (
  @organization_id
  , @project_id
  , @approval_id
  , @provider
  , @kind
  , @recipient_user_id
  , @slack_team_id
  , @slack_user_id
  , @slack_channel_id
  , @slack_message_ts
)
ON CONFLICT (approval_id, provider, recipient_user_id) WHERE deleted IS FALSE DO NOTHING
RETURNING *;

-- name: ListApprovalMessages :many
SELECT *
FROM risk_policy_approval_messages
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND approval_id = @approval_id
  AND deleted IS FALSE
ORDER BY created_at, id;

-- name: GetApprovalMessageBySlackMessage :one
-- Resolves a Slack click to the DM it was made on. Slack signs the click, and
-- the click names its workspace, channel and message, so this lookup is not
-- tenant-scoped; the row it returns carries the tenant. A DM in a workspace
-- the organization has since disconnected finds no row.
SELECT sqlc.embed(m)
FROM risk_policy_approval_messages m
JOIN slack_directory_connections c
  ON c.organization_id = m.organization_id
  AND c.slack_team_id = m.slack_team_id
  AND c.disconnected_at IS NULL
WHERE m.provider = 'slack'
  AND m.slack_team_id = @slack_team_id
  AND m.slack_channel_id = @slack_channel_id
  AND m.slack_message_ts = @slack_message_ts
  AND m.deleted IS FALSE
ORDER BY m.created_at, m.id
LIMIT 1;

-- name: GetApprovalServer :one
-- The MCP server a held call went to, named on the approver's card. A deleted
-- server finds no row.
SELECT name, slug
FROM mcp_servers
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;
