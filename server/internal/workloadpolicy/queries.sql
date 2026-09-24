-- Every query here is scoped to organization_id, and to project_id wherever a
-- row can carry one. The trust policy has an organization tier whose rows hold
-- project_id IS NULL by design, so organization_id is the tenancy boundary and
-- project_id narrows within it rather than replacing it.

-- name: ListWorkloadIssuers :many
-- Issuers the caller can see: the organization tier plus the selected
-- project's. Organization tier first, then by name, so a list reads the same
-- way every time.
SELECT *
FROM workload_issuers
WHERE organization_id = @organization_id
  AND (project_id IS NULL OR project_id = @project_id)
  AND deleted IS FALSE
ORDER BY (project_id IS NULL) DESC, name;

-- name: GetWorkloadIssuer :one
-- Scoped exactly as ListWorkloadIssuers is, so the set a caller can name by id
-- is the set it can see. Organization alone is not enough: a sibling project's
-- issuer is invisible in the list, so reading or withdrawing one by supplying
-- its UUID would let a project-scoped caller act on a row it cannot observe.
SELECT *
FROM workload_issuers
WHERE organization_id = @organization_id
  AND (project_id IS NULL OR project_id = @project_id)
  AND id = @id
  AND deleted IS FALSE;

-- name: FindWorkloadIssuersByIssuer :many
-- Resolves the issuer named on a write path. Tenancy-scoped, so naming a row
-- the caller cannot see is a not-found rather than something checked
-- afterwards. Returns every match because the issuer column is not unique: the
-- caller refuses an ambiguous spelling rather than picking one.
--
-- Matched against a caller-built candidate set, exactly as the verification path
-- matches an assertion's iss. The column stays raw so the issuer index remains
-- usable; canonicalization applies to the supplied URL only. Registering under a
-- spelling that admission would never match is the failure this avoids.
SELECT *
FROM workload_issuers
WHERE organization_id = @organization_id
  AND (project_id IS NULL OR project_id = @project_id)
  AND issuer = ANY(@issuers::text[])
  AND deleted IS FALSE
ORDER BY (project_id IS NULL) DESC, name;

-- name: CreateWorkloadIssuer :one
INSERT INTO workload_issuers (organization_id, project_id, name, issuer, jwks_uri, allow_wildcard_admission)
VALUES (@organization_id, @project_id, @name, @issuer, @jwks_uri, @allow_wildcard_admission)
RETURNING *;

-- name: SoftDeleteWorkloadIssuer :one
-- Carries the same project predicate as the read above rather than trusting the
-- caller to have gone through it: the withdrawal is the destructive half, and a
-- row the caller cannot list is a row it cannot withdraw.
UPDATE workload_issuers
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND (project_id IS NULL OR project_id = @project_id)
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: ListWorkloadAdmissions :many
-- The admitted set as an operator reads it: the subject, the issuer that must
-- assert it, and the agent it inherits its policy from. The assignment is keyed
-- on (issuer, match_kind, subject) rather than on the admission row, because an
-- admission is tiered and an assignment is not.
SELECT
  a.*,
  i.issuer AS issuer_url,
  i.name AS issuer_name,
  i.allow_wildcard_admission,
  g.agent_id,
  ag.name AS agent_name
FROM workload_identity_admissions a
JOIN workload_issuers i
  ON i.organization_id = a.organization_id
 AND i.id = a.workload_issuer_id
 AND i.deleted IS FALSE
 -- The same visibility predicate the issuer list uses. Without it an admission
 -- naming another project's issuer would surface here, and the issuer column
 -- would name a row this caller cannot otherwise see.
 AND (i.project_id IS NULL OR i.project_id = @project_id)
LEFT JOIN workload_agent_assignments g
  ON g.organization_id = a.organization_id
 AND g.workload_issuer_id = a.workload_issuer_id
 AND g.match_kind = a.match_kind
 AND g.subject = a.subject
 AND g.deleted IS FALSE
LEFT JOIN agents ag
  ON ag.organization_id = g.organization_id
 AND ag.id = g.agent_id
 AND ag.deleted IS FALSE
WHERE a.organization_id = @organization_id
  AND (a.project_id IS NULL OR a.project_id = @project_id)
  AND a.deleted IS FALSE
ORDER BY (a.project_id IS NULL) DESC, i.name, a.subject;

-- name: GetWorkloadAdmission :one
SELECT *
FROM workload_identity_admissions
WHERE organization_id = @organization_id
  AND id = @id
  AND (project_id IS NULL OR project_id = @project_id)
  AND deleted IS FALSE;

-- name: CreateWorkloadAdmission :one
INSERT INTO workload_identity_admissions (organization_id, project_id, workload_issuer_id, subject, match_kind, name)
VALUES (@organization_id, @project_id, @workload_issuer_id, @subject, @match_kind, @name)
RETURNING *;

-- name: SoftDeleteWorkloadAdmission :one
UPDATE workload_identity_admissions
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND (project_id IS NULL OR project_id = @project_id)
  AND deleted IS FALSE
RETURNING *;

-- name: SoftDeleteWorkloadAdmissionsByIssuer :many
-- ON DELETE CASCADE only fires on a hard delete, so withdrawing an issuer has
-- to tombstone its admissions explicitly or they stay in the active set
-- pointing at a tombstone. Returns the rows so each one can be audited.
UPDATE workload_identity_admissions
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND workload_issuer_id = @workload_issuer_id
  AND deleted IS FALSE
RETURNING *;

-- name: UpsertWorkloadAgentAssignment :one
-- One assignment per live (issuer, match_kind, subject). Re-admitting a subject
-- repoints its agent rather than colliding. The unique index is partial on
-- deleted IS FALSE, so a withdrawn assignment does not conflict and a fresh row
-- is inserted beside the tombstone, which is what keeps the withdrawal auditable.
INSERT INTO workload_agent_assignments (organization_id, workload_issuer_id, subject, match_kind, agent_id)
VALUES (@organization_id, @workload_issuer_id, @subject, @match_kind, @agent_id)
ON CONFLICT (organization_id, workload_issuer_id, match_kind, subject) WHERE deleted IS FALSE
DO UPDATE SET agent_id = EXCLUDED.agent_id, updated_at = clock_timestamp()
RETURNING *;

-- name: LockWorkloadAdmissionsForSubject :many
-- Locks every live admission naming this tuple, both tiers, before the tier being
-- withdrawn is tombstoned. Two withdrawals of the same workload otherwise race:
-- under READ COMMITTED neither transaction sees the other's uncommitted delete, so
-- both count the sibling tier as live and both leave the shared assignment behind,
-- stranding an assignment with no admission. A NOT EXISTS in the delete closes the
-- window inside one transaction but not between two, which is why this locks
-- instead. Ordered by id so concurrent callers take the rows in the same
-- sequence.
SELECT id
FROM workload_identity_admissions
WHERE organization_id = @organization_id
  AND workload_issuer_id = @workload_issuer_id
  AND match_kind = @match_kind
  AND subject = @subject
  AND deleted IS FALSE
ORDER BY id
FOR UPDATE;

-- name: CountLiveAdmissionsForSubject :one
-- How many live admissions still name this tuple, across both tiers. The agent
-- assignment is keyed on (issuer, match_kind, subject) and is therefore shared
-- by them, so withdrawing one tier must not strip the agent from the other:
-- that admission would survive with no policy and be refused at the token
-- endpoint, which reads as a broken rule rather than a withdrawn one.
SELECT count(*)
FROM workload_identity_admissions
WHERE organization_id = @organization_id
  AND workload_issuer_id = @workload_issuer_id
  AND match_kind = @match_kind
  AND subject = @subject
  AND deleted IS FALSE;

-- name: SoftDeleteWorkloadAgentAssignmentForSubject :many
-- Keyed the way the assignment is, not by admission id. Returns rows so the
-- withdrawal is auditable, and is a no-op when nothing was assigned.
UPDATE workload_agent_assignments
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND workload_issuer_id = @workload_issuer_id
  AND match_kind = @match_kind
  AND subject = @subject
  AND deleted IS FALSE
RETURNING *;

-- name: SoftDeleteWorkloadAgentAssignmentsByIssuer :many
UPDATE workload_agent_assignments
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND workload_issuer_id = @workload_issuer_id
  AND deleted IS FALSE
RETURNING *;

-- name: GetOrganizationAgent :one
-- Resolves the agent an admission assigns, within the caller's organization, so
-- naming another tenant's agent is a not-found rather than a rejected insert.
SELECT id, name
FROM agents
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE;
