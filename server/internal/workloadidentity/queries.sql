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
-- Whether this organization recognises one workload: a subject, vouched for by
-- one workload issuer row, admitted in the caller's own project or at the
-- organization tier above it.
--
-- EXISTS rather than the row, because the answer is the whole of what admission
-- needs and returning a row would invite a caller to read something else off it
-- and widen the security boundary by accident.
--
-- The tenancy predicate matches ListWorkloadIssuersByIssuerURL exactly, and for
-- the same reason: organization_id is checked unconditionally so a project-tier
-- row cannot answer outside its own organization, and the project arm reads the
-- caller's project or the organization tier. When @project_id is NULL the
-- project arm is not true, so an organization-scoped caller sees only
-- organization-tier admissions — a project's private decision never answers a
-- caller that named no project.
--
-- Exact equality on subject: it arrives from a verified assertion and is
-- compared as the platform minted it. No pattern, prefix or case folding, and
-- deliberately no expression around the column, which would make
-- workload_identity_admissions_lookup_idx unusable.
SELECT EXISTS (
  SELECT 1
  FROM workload_identity_admissions
  WHERE organization_id = @organization_id
    AND workload_issuer_id = @workload_issuer_id
    AND subject = @subject
    AND (project_id = @project_id OR project_id IS NULL)
    AND deleted IS FALSE
);
