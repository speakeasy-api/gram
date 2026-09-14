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
-- Exact equality on subject, compared as the platform minted it. No expression
-- around the column, which would make the lookup index unusable.
SELECT EXISTS (
  SELECT 1
  FROM workload_identity_admissions
  WHERE organization_id = @organization_id
    AND workload_issuer_id = @workload_issuer_id
    AND subject = @subject
    AND (project_id = @project_id OR project_id IS NULL)
    AND deleted IS FALSE
);
