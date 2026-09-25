-- name: ListWorkloadIssuersByIssuerURL :many
-- Every workload issuer a caller may resolve an assertion's `iss` onto: its own
-- project tier plus the organization tier above it.
--
-- Workload trust has no platform tier, so a row in the caller's own tenancy is
-- the whole of the trust decision. organization_id is checked unconditionally
-- rather than only on the organization arm, which is what the NOT NULL
-- organization_id on project-tier rows exists for.
--
-- Matching is literal equality against a caller-supplied candidate set rather
-- than a normalizing expression, because the index this rides on
-- (workload_issuers_issuer_idx) is on the raw column: any expression
-- around `issuer` would make it unusable and turn this into a sequential scan.
-- The caller canonicalizes in Go and expands back into the closed set of raw
-- spellings that canonicalize to the same thing. See issuerurl.MatchCandidates
-- for the exact set and for the spellings deliberately left unmatched.
--
-- Returns every candidate rather than picking one: precedence is project over
-- organization and cannot be expressed by row order here, so the caller applies
-- it through resolveByPrecedence.
--
-- Ordered by created_at, NOT by id. generate_uuidv7 overlays only a
-- millisecond-resolution timestamp onto an otherwise random gen_random_uuid, so
-- two rows written in the same millisecond sort randomly by id. created_at is
-- clock_timestamp() at microsecond resolution, which makes "oldest" mean what
-- it says. id remains the final tie-break so the order is still total.
SELECT *
FROM workload_issuers
WHERE issuer = ANY(@issuers::text[])
  AND organization_id = @organization_id
  AND (project_id = @project_id OR project_id IS NULL)
  AND deleted IS FALSE
ORDER BY created_at ASC, id ASC;

-- name: WorkloadIdentityIsAdmitted :one
-- Whether this tenant recognises one workload: a subject vouched for by one
-- issuer row, admitted in the caller's own project or the organization above.
--
-- EXISTS rather than the row, so a caller cannot read anything else off it and
-- widen the security boundary by accident.
--
-- Tenancy matches ListWorkloadIssuersByIssuerURL: organization_id
-- unconditionally, so a project-tier row cannot answer outside its
-- organization, and the project arm is not true for a NULL @project_id, so an
-- organization-scoped caller sees only organization-tier rows.
--
-- Two ways to match, chosen per row by match_kind. The exact arm compares the
-- subject as the platform minted it, with no expression around the column, so it
-- still rides workload_identity_admissions_lookup_idx. The wildcard arm strips
-- the stored value's trailing `*` and asks whether the remaining stem leads the
-- presented subject, which is the opposite of what a btree on subject answers, so
-- it scans instead — bounded by the (organization, issuer) columns above and by
-- how few admissions an issuer has.
--
-- left(subject, length(subject) - 1) is the stem. Storing the `*` rather than
-- stripping it before the insert is deliberate: a row then states its own breadth
-- to anyone reading the table, which a bare stem does not.
--
-- The shape is re-checked here rather than trusted from write time, for the same
-- reason the issuer's permission is. Nothing in the database enforces it, and
-- stripping a last character that is not a `*` silently widens the rule: a row
-- reading `repo:org` would admit every subject under `repo:or`. Requiring the
-- terminator and a non-empty stem on read makes a malformed row inert instead of
-- over-broad, which is the safe direction for a row no writer should have made.
--
-- The wildcard arm additionally requires the issuer to permit wildcard matching,
-- and that is checked HERE rather than trusted from write time. A write-side gate
-- alone would be advisory: any future writer, a seed or a hand-run statement
-- could leave a wildcard row behind, and turning the issuer's permission off
-- would not revoke rows already written. Enforced on read, clearing
-- allow_wildcard_admission is an immediate and complete kill switch for every
-- wildcard rule under that issuer.
--
-- The join also pins the issuer live. Resolution upstream already excludes a
-- soft-deleted issuer, so this changes no outcome today; it means a caller that
-- ever reaches admission another way cannot be admitted by an issuer the
-- organization has stopped trusting.
SELECT EXISTS (
  SELECT 1
  FROM workload_identity_admissions a
  JOIN workload_issuers i
    ON i.organization_id = a.organization_id
    AND i.id = a.workload_issuer_id
    -- The admission's tier may not out-reach its issuer's. An organization-tier
    -- admission has to name an organization-tier issuer; a project-tier one may
    -- name its own project's or the organization's. The write path refuses any
    -- other pairing, and this makes a row that reached the table another way
    -- inert rather than letting one project's trust decide an organization-wide
    -- admission.
    AND (i.project_id IS NULL OR i.project_id = a.project_id)
  WHERE a.organization_id = @organization_id
    AND a.workload_issuer_id = @workload_issuer_id
    AND (a.project_id = @project_id OR a.project_id IS NULL)
    AND a.deleted IS FALSE
    AND i.deleted IS FALSE
    AND (
      (a.match_kind = 'exact' AND a.subject = @subject)
      OR (
        a.match_kind = 'wildcard'
        AND i.allow_wildcard_admission
        AND a.subject LIKE '%*'
        AND length(a.subject) > 1
        AND starts_with(@subject, left(a.subject, length(a.subject) - 1))
      )
    )
);

-- name: ResolveWorkloadAgentAssignment :one
-- The agent a workload principal inherits its permission policy from.
--
-- Keyed on the workload principal itself — (organization, issuer row, subject) —
-- and deliberately not on an admission row. Admissions are tiered by project
-- while the principal is organization-scoped, so withdrawing one tier's
-- admission must not change what the workload may do under another. That is
-- also why no project arm appears here, unlike WorkloadIdentityIsAdmitted.
--
-- More than one row can match, so "one agent per workload" is RESOLVED here
-- rather than enforced by uniqueness. A wildcard assignment covering an issuer's
-- whole fleet and an exact assignment naming one principal can both cover the
-- same subject, which is the point: a default for everything, with individual
-- principals pinned elsewhere. The ORDER BY picks the most specific — exact
-- before wildcard, and a longer stem before a shorter one — and LIMIT 1 makes
-- the answer deterministic. workload_agent_assignments_workload_key still stops
-- the same rule being written twice.
--
-- Unassigning is a soft delete, so deleted rows are excluded or the workload
-- would keep the authority an administrator believes they removed.
--
-- The wildcard arm requires the issuer to permit wildcard matching, checked here
-- for the same reason as in WorkloadIdentityIsAdmitted: clearing
-- allow_wildcard_admission must revoke wildcard rules already written, not just
-- stop new ones. Admission and assignment have to agree on this, or a subject
-- admitted by wildcard would resolve to no agent and be refused for the wrong
-- reason.
--
-- The issuer must be live too. Deleting an issuer soft-deletes only its own
-- row, so without the join a session minted before the delete would keep the
-- authority of an identity the organization no longer trusts.
SELECT a.agent_id
FROM workload_agent_assignments a
JOIN workload_issuers i
  ON i.organization_id = a.organization_id
  AND i.id = a.workload_issuer_id
WHERE a.organization_id = @organization_id
  AND a.workload_issuer_id = @workload_issuer_id
  AND a.deleted IS FALSE
  AND i.deleted IS FALSE
  AND (
    (a.match_kind = 'exact' AND a.subject = @subject)
    OR (
      a.match_kind = 'wildcard'
      AND i.allow_wildcard_admission
      AND a.subject LIKE '%*'
      AND length(a.subject) > 1
      AND starts_with(@subject, left(a.subject, length(a.subject) - 1))
    )
  )
ORDER BY (a.match_kind = 'exact') DESC, length(a.subject) DESC
LIMIT 1;
