//nolint:glint // Integration fixtures intentionally seed, corrupt, and inspect isolated migration rows.
package agentmanagementgrants

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type grantSnapshot struct {
	ID             uuid.UUID
	OrganizationID string
	PrincipalURN   string
	Scope          string
	Selectors      string
	Effect         *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func TestRunnerBackfillsRecognizedAdminRolesIdempotently(t *testing.T) {
	t.Parallel()
	pool := newTestDB(t)
	ctx := t.Context()

	for _, organizationID := range []string{"org-global-admin", "org-local-admin", "org-partial-admin"} {
		seedOrganization(t, pool, organizationID)
	}
	globalAdminURN := seedGlobalRole(t, pool, "admin")
	localAdminURN := seedOrganizationRole(t, pool, "org-local-admin", "admin")
	customRoleURN := seedOrganizationRole(t, pool, "org-local-admin", "operator")

	unrelatedID := seedGrant(t, pool, "org-global-admin", globalAdminURN, "project:read", `{"resource_kind":"project","resource_id":"project-test"}`, nil)
	customID := seedGrant(t, pool, "org-local-admin", customRoleURN, "agent:unexpected", canonicalSelector, nil)
	for _, scope := range requiredScopes[:2] {
		seedGrant(t, pool, "org-partial-admin", globalAdminURN, scope, canonicalSelector, nil)
	}
	unrelatedBefore := loadGrant(t, pool, unrelatedID)
	customBefore := loadGrant(t, pool, customID)

	runner := NewRunner(pool, Options{BatchSize: 2, SampleLimit: 2})
	dryRun, err := runner.Run(ctx, ModeDryRun)
	require.NoError(t, err)
	require.False(t, dryRun.Verification.ReadyForEnforcement)
	require.Equal(t, int64(14), dryRun.Verification.MissingRequiredGrants)
	require.Len(t, dryRun.Verification.MissingGrantSamples, 2)

	first, err := runner.Run(ctx, ModeApply)
	require.NoError(t, err)
	require.Equal(t, int64(3), first.OrganizationsScanned)
	require.Equal(t, int64(14), first.GrantRowsChanged)
	require.Equal(t, int64(2), first.Batches)
	require.True(t, first.Verification.ReadyForEnforcement)
	require.Equal(t, int64(4), countCanonicalGrants(t, pool, "org-global-admin", globalAdminURN))
	require.Equal(t, int64(4), countCanonicalGrants(t, pool, "org-local-admin", localAdminURN))
	require.Equal(t, int64(4), countCanonicalGrants(t, pool, "org-local-admin", globalAdminURN))
	require.Equal(t, int64(4), countCanonicalGrants(t, pool, "org-partial-admin", globalAdminURN))
	require.Equal(t, unrelatedBefore, loadGrant(t, pool, unrelatedID))
	require.Equal(t, customBefore, loadGrant(t, pool, customID))

	second, err := runner.Run(ctx, ModeApply)
	require.NoError(t, err)
	require.Zero(t, second.GrantRowsChanged)
	require.True(t, second.Verification.ReadyForEnforcement)
	require.Equal(t, unrelatedBefore, loadGrant(t, pool, unrelatedID))
	require.Equal(t, customBefore, loadGrant(t, pool, customID))
}

func TestRunnerResumesAfterCommittedBatchFailure(t *testing.T) {
	t.Parallel()
	pool := newTestDB(t)
	for _, organizationID := range []string{"org-resume-a", "org-resume-b", "org-resume-c"} {
		seedOrganization(t, pool, organizationID)
	}
	adminURN := seedGlobalRole(t, pool, "admin")

	runner := NewRunner(pool, Options{BatchSize: 2})
	runner.afterBatch = func() error { return errors.New("simulated interruption") }
	partial, err := runner.Run(t.Context(), ModeApply)
	require.ErrorContains(t, err, "simulated interruption")
	require.Equal(t, int64(1), partial.Batches)
	require.Equal(t, int64(8), partial.GrantRowsChanged)
	require.Equal(t, "org-resume-b", partial.LastCursor)
	require.Equal(t, int64(4), countCanonicalGrants(t, pool, "org-resume-a", adminURN))
	require.Equal(t, int64(4), countCanonicalGrants(t, pool, "org-resume-b", adminURN))
	require.Zero(t, countCanonicalGrants(t, pool, "org-resume-c", adminURN))

	runner.afterBatch = nil
	resumed, err := runner.Run(t.Context(), ModeApply)
	require.NoError(t, err)
	require.Equal(t, int64(4), resumed.GrantRowsChanged)
	require.True(t, resumed.Verification.ReadyForEnforcement)
}

func TestRunnerVerificationIndependentlyDetectsMissingAndUnexpectedGrants(t *testing.T) {
	t.Parallel()
	pool := newTestDB(t)
	seedOrganization(t, pool, "org-verification")
	adminURN := seedGlobalRole(t, pool, "admin")
	runner := NewRunner(pool, Options{SampleLimit: 1})

	_, err := runner.Run(t.Context(), ModeApply)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `
DELETE FROM principal_grants
WHERE organization_id = $1 AND principal_urn = $2 AND scope = $3 AND selectors = $4::jsonb`,
		"org-verification", adminURN, requiredScopes[0], canonicalSelector)
	require.NoError(t, err)
	seedGrant(t, pool, "org-verification", adminURN, "agent:unexpected", canonicalSelector, nil)

	summary, err := runner.Run(t.Context(), ModeVerify)
	require.ErrorIs(t, err, ErrVerificationFailed)
	require.False(t, summary.Verification.ReadyForEnforcement)
	require.Equal(t, int64(1), summary.Verification.MissingRequiredGrants)
	require.Equal(t, int64(1), summary.Verification.MissingGrantOrganizations)
	require.Equal(t, []DefectSample{{OrganizationID: "org-verification", PrincipalURN: adminURN, Scope: requiredScopes[0]}}, summary.Verification.MissingGrantSamples)
	require.Equal(t, int64(1), summary.Verification.UnexpectedAgentGrants)
	require.Equal(t, int64(1), summary.Verification.UnexpectedOrganizations)
	require.Equal(t, []DefectSample{{OrganizationID: "org-verification", PrincipalURN: adminURN, Scope: "agent:unexpected"}}, summary.Verification.UnexpectedGrantSamples)
}

func TestRunnerVerificationBlocksOrganizationsWithoutRecognizedAdminRole(t *testing.T) {
	t.Parallel()
	pool := newTestDB(t)
	seedOrganization(t, pool, "org-missing-admin")

	runner := NewRunner(pool, Options{SampleLimit: 1})
	summary, err := runner.Run(t.Context(), ModeVerify)
	require.ErrorIs(t, err, ErrVerificationFailed)
	require.Equal(t, int64(1), summary.Verification.TotalOrganizations)
	require.Zero(t, summary.Verification.TargetAdminRoles)
	require.Equal(t, int64(1), summary.Verification.MissingAdminRoles)
	require.Equal(t, []DefectSample{{OrganizationID: "org-missing-admin"}}, summary.Verification.MissingAdminRoleSamples)
}

func TestRunnerApplyNormalizesExistingRequiredDenyGrant(t *testing.T) {
	t.Parallel()
	pool := newTestDB(t)
	seedOrganization(t, pool, "org-deny-admin")
	adminURN := seedGlobalRole(t, pool, "admin")
	deny := "deny"
	seedGrant(t, pool, "org-deny-admin", adminURN, requiredScopes[0], canonicalSelector, &deny)

	summary, err := NewRunner(pool, Options{}).Run(t.Context(), ModeApply)
	require.NoError(t, err)
	require.Equal(t, int64(4), summary.GrantRowsChanged)
	require.True(t, summary.Verification.ReadyForEnforcement)
}

func seedGlobalRole(t *testing.T, pool *pgxpool.Pool, slug string) string {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(t.Context(), `
INSERT INTO global_roles (workos_slug, workos_name, workos_description, workos_created_at, workos_updated_at)
VALUES ($1, $1, '', clock_timestamp(), clock_timestamp())
RETURNING id`, slug).Scan(&id)
	require.NoError(t, err)
	return "role:global:" + id.String()
}

func seedOrganizationRole(t *testing.T, pool *pgxpool.Pool, organizationID, slug string) string {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(t.Context(), `
INSERT INTO organization_roles (organization_id, workos_slug, workos_name, workos_description, workos_created_at, workos_updated_at)
VALUES ($1, $2, $2, '', clock_timestamp(), clock_timestamp())
RETURNING id`, organizationID, slug).Scan(&id)
	require.NoError(t, err)
	return "role:organization:" + id.String()
}

func seedGrant(t *testing.T, pool *pgxpool.Pool, organizationID, principalURN, scope, selectors string, effect *string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(t.Context(), `
INSERT INTO principal_grants (organization_id, principal_urn, scope, selectors, effect)
VALUES ($1, $2, $3, $4::jsonb, $5)
RETURNING id`, organizationID, principalURN, scope, selectors, effect).Scan(&id)
	require.NoError(t, err)
	return id
}

func loadGrant(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) grantSnapshot {
	t.Helper()
	var snapshot grantSnapshot
	err := pool.QueryRow(t.Context(), `
SELECT id, organization_id, principal_urn, scope, selectors::text, effect, created_at, updated_at
FROM principal_grants
WHERE id = $1`, id).Scan(
		&snapshot.ID, &snapshot.OrganizationID, &snapshot.PrincipalURN, &snapshot.Scope,
		&snapshot.Selectors, &snapshot.Effect, &snapshot.CreatedAt, &snapshot.UpdatedAt,
	)
	require.NoError(t, err)
	return snapshot
}

func countCanonicalGrants(t *testing.T, pool *pgxpool.Pool, organizationID, principalURN string) int64 {
	t.Helper()
	var count int64
	err := pool.QueryRow(t.Context(), `
SELECT COUNT(*)
FROM principal_grants
WHERE organization_id = $1
  AND principal_urn = $2
  AND scope = ANY($3::text[])
  AND COALESCE(effect, 'allow') = 'allow'
  AND selectors = $4::jsonb`, organizationID, principalURN, requiredScopes, canonicalSelector).Scan(&count)
	require.NoError(t, err)
	return count
}

func TestUnsupportedMode(t *testing.T) {
	t.Parallel()
	pool := newTestDB(t)
	_, err := NewRunner(pool, Options{}).Run(t.Context(), Mode("unknown"))
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrVerificationFailed)
}
