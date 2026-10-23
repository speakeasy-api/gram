package admin

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/authenforcement"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestUpstreamAuthEnforcement_RefusedUntilCovered(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
	org := newMigrationOrg(t, ctx, db, "enforce-uncovered")
	issuer := org.issuer()
	org.client(issuer)
	org.server(issuer)
	org.session(issuer)

	status, err := svc.GetUpstreamOAuthMigrationStatus(ctx, &gen.GetUpstreamOAuthMigrationStatusPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.False(t, status.Enforcement.Ready)
	require.Contains(t, status.Enforcement.Blockers, "The upstream OAuth migration is incomplete.")

	_, err = svc.EnableUpstreamAuthEnforcement(ctx, &gen.EnableUpstreamAuthEnforcementPayload{OrganizationID: org.id, ShadowParityConfirmed: true, CrossOrganizationDenialCommunicated: true})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeInvalid, shareable.Code)

	enforced, err := authenforcement.NewGate(db).Enforced(ctx, org.id)
	require.NoError(t, err)
	require.False(t, enforced)
}

func TestUpstreamAuthEnforcement_EnablesOnceCoveredAndRollsBack(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
	org := newMigrationOrg(t, ctx, db, "enforce-covered")
	issuer := org.issuer()
	org.client(issuer)
	org.server(issuer)
	org.session(issuer)

	run, err := svc.RunUpstreamOAuthMigration(ctx, &gen.RunUpstreamOAuthMigrationPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.True(t, run.Status.Enforcement.Ready, run.Status.Enforcement.Blockers)

	_, err = svc.EnableUpstreamAuthEnforcement(ctx, &gen.EnableUpstreamAuthEnforcementPayload{OrganizationID: org.id, ShadowParityConfirmed: false, CrossOrganizationDenialCommunicated: true})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable, "shadow parity is confirmed explicitly")
	require.Equal(t, oops.CodeInvalid, shareable.Code)

	enabled, err := svc.EnableUpstreamAuthEnforcement(ctx, &gen.EnableUpstreamAuthEnforcementPayload{OrganizationID: org.id, ShadowParityConfirmed: true, CrossOrganizationDenialCommunicated: true})
	require.NoError(t, err)
	require.True(t, enabled.Enforcement.Enabled)
	enforced, err := authenforcement.NewGate(db).Enforced(ctx, org.id)
	require.NoError(t, err)
	require.True(t, enforced)

	disabled, err := svc.DisableUpstreamAuthEnforcement(ctx, &gen.DisableUpstreamAuthEnforcementPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.False(t, disabled.Enforcement.Enabled)
	enforced, err = authenforcement.NewGate(db).Enforced(ctx, org.id)
	require.NoError(t, err)
	require.False(t, enforced, "disabling is the organization's rollback")
}

func TestUpstreamAuthEnforcement_RequiresStaff(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	org := newMigrationOrg(t, ctx, db, "enforce-unauthorized")

	_, err := svc.EnableUpstreamAuthEnforcement(ctx, &gen.EnableUpstreamAuthEnforcementPayload{OrganizationID: org.id, ShadowParityConfirmed: true, CrossOrganizationDenialCommunicated: true})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnauthorized, shareable.Code)
	_, err = svc.DisableUpstreamAuthEnforcement(ctx, &gen.DisableUpstreamAuthEnforcementPayload{OrganizationID: org.id})
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnauthorized, shareable.Code)
}

func TestUpstreamAuthEnforcement_EachCoverageGapBlocks(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	covered := func() *gen.AdminUpstreamOAuthMigrationStatus {
		return &gen.AdminUpstreamOAuthMigrationStatus{
			OrganizationID: "org_enforce_blockers",
			Migrated:       true,
			AuthClients: &gen.AdminUpstreamOAuthMigrationAuthClients{
				Complete: true, UnresolvedAssociations: nil, Warnings: nil, Kinds: nil, FailedPhases: nil, SelectionClasses: nil, SelectionGaps: nil,
				PendingSelectionMappings: new(int64(0)), PendingGrants: new(int64(0)),
			},
			ExecutionProfiles: &gen.AdminUpstreamOAuthMigrationExecutionProfiles{Complete: true, Gaps: nil},
		}
	}
	enforcement, err := svc.upstreamAuthEnforcement(ctx, covered())
	require.NoError(t, err)
	require.True(t, enforcement.Ready, enforcement.Blockers)

	for name, tc := range map[string]struct {
		change  func(*gen.AdminUpstreamOAuthMigrationStatus)
		blocker string
	}{
		"pending selection mappings": {func(s *gen.AdminUpstreamOAuthMigrationStatus) { s.AuthClients.PendingSelectionMappings = new(int64(1)) }, "Credential choices still need their auth client named."},
		"pending default grants":     {func(s *gen.AdminUpstreamOAuthMigrationStatus) { s.AuthClients.PendingGrants = new(int64(1)) }, "Default auth permissions are not all granted."},
		"pending profile versions": {func(s *gen.AdminUpstreamOAuthMigrationStatus) {
			s.ExecutionProfiles.Complete, s.ExecutionProfiles.PendingVersions = false, 1
		}, "Assistant profiles are not all recorded."},
		"unchecked selection mappings": {func(s *gen.AdminUpstreamOAuthMigrationStatus) { s.AuthClients.PendingSelectionMappings = nil }, "Credential choices couldn't be checked."},
		"unchecked default grants":     {func(s *gen.AdminUpstreamOAuthMigrationStatus) { s.AuthClients.PendingGrants = nil }, "Default auth permissions couldn't be checked."},
		"unchecked profiles":           {func(s *gen.AdminUpstreamOAuthMigrationStatus) { s.ExecutionProfiles = nil }, "Assistant profiles couldn't be checked."},
	} {
		status := covered()
		tc.change(status)
		enforcement, err := svc.upstreamAuthEnforcement(ctx, status)
		require.NoError(t, err, name)
		require.False(t, enforcement.Ready, name)
		require.Equal(t, []string{tc.blocker}, enforcement.Blockers, name)
	}
}

func TestUpstreamAuthEnforcement_RequiresCrossOrganizationNotice(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
	org := newMigrationOrg(t, ctx, db, "enforce-notice")
	issuer := org.issuer()
	org.client(issuer)
	org.server(issuer)
	org.session(issuer)
	_, err := svc.RunUpstreamOAuthMigration(ctx, &gen.RunUpstreamOAuthMigrationPayload{OrganizationID: org.id})
	require.NoError(t, err)

	_, err = svc.EnableUpstreamAuthEnforcement(ctx, &gen.EnableUpstreamAuthEnforcementPayload{OrganizationID: org.id, ShadowParityConfirmed: true, CrossOrganizationDenialCommunicated: false})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeInvalid, shareable.Code)
	enforced, err := authenforcement.NewGate(db).Enforced(ctx, org.id)
	require.NoError(t, err)
	require.False(t, enforced)
}
