package adminmcp

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	adminrepo "github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

func newOrganizationWhitelistFixture(t *testing.T, name string) writeFixture {
	t.Helper()
	f := newWriteFixture(t, name)
	for _, id := range []string{f.orgA, f.orgB} {
		require.NoError(t, adminrepo.New(f.db).AdminUpdateOrganization(t.Context(), adminrepo.AdminUpdateOrganizationParams{ID: id, Whitelisted: conv.PtrToPGBool(new(false))}))
	}
	return f
}

func newOrganizationWhitelistWriter(f writeFixture) *organizationWhitelistWriter {
	writes := WriteConfig{Enabled: true}
	return &organizationWhitelistWriter{runner: f.runner, audit: audit.NewLogger(), writes: writes}
}

func TestOrganizationWhitelistApplies(t *testing.T) {
	t.Parallel()
	f := newOrganizationWhitelistFixture(t, "admin_mcp_whitelist_execute")
	writer := newOrganizationWhitelistWriter(f)
	ctx := writeContext(t, f)
	before, err := orgrepo.New(f.db).GetOrganizationMetadata(ctx, f.orgA)
	require.NoError(t, err)
	other, err := orgrepo.New(f.db).GetOrganizationMetadata(ctx, f.orgB)
	require.NoError(t, err)
	input := OrganizationWhitelistInput{OrganizationID: f.orgA, Whitelisted: !before.Whitelisted}
	receipt, err := writer.apply(ctx, input)
	require.NoError(t, err)
	require.Equal(t, f.orgA, receipt.Target.OrganizationID)
	require.Equal(t, before.Whitelisted, receipt.Target.Whitelisted)
	require.Equal(t, input.Whitelisted, receipt.After)
	require.Contains(t, receipt.SideEffects, "not credential revocation")
	after, err := orgrepo.New(f.db).GetOrganizationMetadata(ctx, f.orgA)
	require.NoError(t, err)
	require.Equal(t, !before.Whitelisted, after.Whitelisted)
	after.Whitelisted, after.UpdatedAt = before.Whitelisted, before.UpdatedAt
	require.Equal(t, before, after, "only whitelisted and its update timestamp change")
	otherAfter, err := orgrepo.New(f.db).GetOrganizationMetadata(ctx, f.orgB)
	require.NoError(t, err)
	require.Equal(t, other, otherAfter)
	entry, err := audittest.LatestAuditLogByAction(ctx, f.db, audit.ActionOrganizationWhitelistUpdated)
	require.NoError(t, err)
	require.Equal(t, f.orgA, entry.OrganizationID)
	require.Equal(t, "staff-subject", entry.ActorID)
	require.Equal(t, string(audit.SurfaceAdminMCP), *entry.ActingSurface)
	require.Equal(t, "test-client", *entry.ActingClientID)
	require.JSONEq(t, `{"whitelisted":false}`, string(entry.BeforeSnapshot))
	require.JSONEq(t, `{"whitelisted":true}`, string(entry.AfterSnapshot))
	_, err = writer.apply(ctx, input)
	require.ErrorContains(t, err, "nothing would change", "repeating the call changes nothing")
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationWhitelistUpdated))
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationSetOrganizationWhitelist, f.orgA, "executed"))
}

func TestOrganizationWhitelistRemovalPreservesDisabledState(t *testing.T) {
	t.Parallel()
	f := newOrganizationWhitelistFixture(t, "admin_mcp_whitelist_remove")
	q := adminrepo.New(f.db)
	require.NoError(t, q.AdminUpdateOrganization(t.Context(), adminrepo.AdminUpdateOrganizationParams{ID: f.orgA, Whitelisted: conv.PtrToPGBool(new(true))}))
	_, err := q.AdminDisableOrganization(t.Context(), f.orgA)
	require.NoError(t, err)
	before, err := orgrepo.New(f.db).GetOrganizationMetadata(t.Context(), f.orgA)
	require.NoError(t, err)
	_, err = newOrganizationWhitelistWriter(f).apply(writeContext(t, f), OrganizationWhitelistInput{OrganizationID: f.orgA, Whitelisted: false})
	require.NoError(t, err)
	after, err := orgrepo.New(f.db).GetOrganizationMetadata(t.Context(), f.orgA)
	require.NoError(t, err)
	require.False(t, after.Whitelisted)
	require.Equal(t, before.DisabledAt, after.DisabledAt)
	require.Equal(t, before.GramAccountType, after.GramAccountType)
}

func TestOrganizationWhitelistAuditFailureRollsBack(t *testing.T) {
	t.Parallel()
	f := newOrganizationWhitelistFixture(t, "admin_mcp_whitelist_rollback")
	writer := newOrganizationWhitelistWriter(f)
	require.NoError(t, audittest.RejectAction(t.Context(), f.db, audit.ActionOrganizationWhitelistUpdated))
	_, err := writer.apply(writeContext(t, f), OrganizationWhitelistInput{OrganizationID: f.orgA, Whitelisted: true})
	require.Error(t, err)
	after, err := orgrepo.New(f.db).GetOrganizationMetadata(t.Context(), f.orgA)
	require.NoError(t, err)
	require.False(t, after.Whitelisted)
	require.Zero(t, countWriteEvents(t, f.db, OperationSetOrganizationWhitelist, f.orgA, "executed"))
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationSetOrganizationWhitelist, f.orgA, "failed"))
}

func TestOrganizationWhitelistConcurrentCallsWriteOnce(t *testing.T) {
	t.Parallel()
	f := newOrganizationWhitelistFixture(t, "admin_mcp_whitelist_concurrent")
	writer := newOrganizationWhitelistWriter(f)
	ctx := writeContext(t, f)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = writer.apply(ctx, OrganizationWhitelistInput{OrganizationID: f.orgA, Whitelisted: true})
		})
	}
	wg.Wait()
	require.NotEqual(t, errs[0] == nil, errs[1] == nil, "exactly one call changes the setting")
	require.ErrorContains(t, errors.Join(errs[0], errs[1]), "nothing would change", "the other call finds nothing to do")
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationWhitelistUpdated))
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationSetOrganizationWhitelist, f.orgA, "executed"))
}

func TestOrganizationWhitelistRefusesInvalidTargetsAndNoop(t *testing.T) {
	t.Parallel()
	f := newOrganizationWhitelistFixture(t, "admin_mcp_whitelist_invalid")
	writer := newOrganizationWhitelistWriter(f)
	for _, input := range []OrganizationWhitelistInput{
		{OrganizationID: "synthetic-a", Whitelisted: true},
		{OrganizationID: "", Whitelisted: true},
		{OrganizationID: f.orgA, Whitelisted: false},
	} {
		_, err := writer.apply(writeContext(t, f), input)
		require.Error(t, err)
	}
	require.Zero(t, auditCount(t, f, audit.ActionOrganizationWhitelistUpdated))
}

func TestOrganizationWhitelistRefusesTrialTargets(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		tier      string
		converted bool
		expired   bool
	}{
		{name: "running", tier: "enterprise"},
		{name: "expired", tier: "enterprise", expired: true},
		{name: "converted", tier: "enterprise", converted: true},
		{name: "non_enterprise", tier: "pro"},
		{name: "unknown_tier", tier: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newOrganizationWhitelistFixture(t, "admin_mcp_whitelist_trial_"+tc.name)
			endsAt := time.Now().Add(time.Hour)
			if tc.expired {
				endsAt = time.Now().Add(-time.Hour)
			}
			q := trialsrepo.New(f.db)
			require.NoError(t, q.CreateTrial(t.Context(), trialsrepo.CreateTrialParams{OrganizationID: f.orgA, Tier: tc.tier, EndsAt: conv.ToPGTimestamptz(endsAt)}))
			if tc.converted {
				_, err := q.MarkTrialConverted(t.Context(), f.orgA)
				require.NoError(t, err)
			}
			trialBefore, err := q.GetTrial(t.Context(), f.orgA)
			require.NoError(t, err)
			_, err = newOrganizationWhitelistWriter(f).apply(writeContext(t, f), OrganizationWhitelistInput{OrganizationID: f.orgA, Whitelisted: true})
			require.ErrorIs(t, err, errWhitelistTrial)
			trialAfter, err := trialsrepo.New(f.db).GetTrial(t.Context(), f.orgA)
			require.NoError(t, err)
			require.Equal(t, trialBefore, trialAfter)
			org, err := orgrepo.New(f.db).GetOrganizationMetadata(t.Context(), f.orgA)
			require.NoError(t, err)
			require.False(t, org.Whitelisted)
			require.Zero(t, auditCount(t, f, audit.ActionOrganizationWhitelistUpdated))
		})
	}
}

func TestOrganizationWhitelistRequiresAuthority(t *testing.T) {
	t.Parallel()
	writer := &organizationWhitelistWriter{}
	_, err := writer.apply(t.Context(), OrganizationWhitelistInput{})
	require.ErrorIs(t, err, ErrWriteDisabled)
	writer.writes = WriteConfig{Enabled: true}
	_, err = writer.apply(t.Context(), OrganizationWhitelistInput{})
	require.ErrorIs(t, err, ErrWriteIdentity)
	_, err = writer.apply(writePrincipalContext(t, []string{ScopeRead}), OrganizationWhitelistInput{})
	require.ErrorIs(t, err, ErrWriteScope)
}
