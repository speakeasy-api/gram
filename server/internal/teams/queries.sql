-- name: CreateTeamInvite :one
INSERT INTO team_invites (organization_id, email, invited_by_user_id, status, token, expires_at)
VALUES (@organization_id, @email, @invited_by_user_id, 'pending', @token, @expires_at)
RETURNING *;

-- name: GetTeamInviteByID :one
SELECT * FROM team_invites
WHERE id = @id AND deleted IS FALSE;

-- name: GetTeamInviteByToken :one
SELECT * FROM team_invites
WHERE token = @token AND deleted IS FALSE;

-- name: ListPendingTeamInvites :many
SELECT ti.*, u.display_name as invited_by_name
FROM team_invites ti
JOIN users u ON u.id = ti.invited_by_user_id
WHERE ti.organization_id = @organization_id
  AND ti.status = 'pending'
  AND ti.deleted IS FALSE
ORDER BY ti.created_at DESC;

-- name: CancelTeamInvite :exec
UPDATE team_invites
SET status = 'cancelled', deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = @id AND deleted IS FALSE;

-- name: UpdateTeamInviteExpiryAndToken :one
UPDATE team_invites
SET expires_at = @expires_at, token = @token, updated_at = clock_timestamp()
WHERE id = @id AND deleted IS FALSE
RETURNING *;

-- name: AcceptTeamInvite :one
UPDATE team_invites
SET status = 'accepted', updated_at = clock_timestamp()
WHERE id = @id AND status = 'pending' AND deleted IS FALSE
RETURNING *;

-- name: GetPendingInviteByEmail :one
SELECT * FROM team_invites
WHERE organization_id = @organization_id
  AND lower(email) = lower(@email)
  AND status = 'pending'
  AND deleted IS FALSE;

-- name: ListOrganizationMembers :many
SELECT
  u.id,
  u.email,
  u.display_name,
  u.photo_url,
  our.created_at as joined_at
FROM organization_user_relationships our
JOIN users u ON u.id = our.user_id
WHERE our.organization_id = @organization_id
  AND our.deleted IS FALSE
ORDER BY our.created_at ASC;

-- name: GetOrganizationSlug :one
SELECT slug FROM organization_metadata WHERE id = @id;

-- name: GetInviteInfoByToken :one
SELECT
  ti.id,
  ti.email,
  ti.status,
  ti.expires_at,
  u.display_name as inviter_name,
  om.name as organization_name
FROM team_invites ti
JOIN users u ON u.id = ti.invited_by_user_id
JOIN organization_metadata om ON om.id = ti.organization_id
WHERE ti.token = @token AND ti.deleted IS FALSE;

-- name: CountRecentInvitesByOrg :one
SELECT count(*) FROM team_invites
WHERE organization_id = @organization_id
  AND created_at > now() - interval '24 hours'
  AND deleted IS FALSE;
