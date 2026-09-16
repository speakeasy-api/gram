package risk_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/analysisstatus"
)

// TestGetRiskAnalysisStatus_Idle asserts a closed coordinator run maps to
// idle with both timestamps rendered as RFC3339 UTC and the outcome label
// passed through.
func TestGetRiskAnalysisStatus_Idle(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskWatchdog, authCtx.ActiveOrganizationID, true)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	startedAt := time.Date(2026, 9, 15, 10, 0, 0, 0, time.FixedZone("plus2", 2*60*60))
	closedAt := startedAt.Add(45 * time.Second)
	ti.signaler.describeStatus = analysisstatus.Status{
		State:            analysisstatus.StateIdle,
		RunningSince:     nil,
		LastRunStartedAt: &startedAt,
		LastRunAt:        &closedAt,
		LastRunOutcome:   "completed",
	}

	res, err := ti.service.GetRiskAnalysisStatus(ctx, &gen.GetRiskAnalysisStatusPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	require.Equal(t, "idle", res.State)
	require.Nil(t, res.RunningSince)
	require.NotNil(t, res.LastRunStartedAt)
	require.Equal(t, "2026-09-15T08:00:00Z", *res.LastRunStartedAt)
	require.NotNil(t, res.LastRunAt)
	require.Equal(t, "2026-09-15T08:00:45Z", *res.LastRunAt)
	require.NotNil(t, res.LastRunOutcome)
	require.Equal(t, "completed", *res.LastRunOutcome)
}

// TestGetRiskAnalysisStatus_Running asserts an in-flight run reports only
// running_since and leaves the last-run fields unset.
func TestGetRiskAnalysisStatus_Running(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskWatchdog, authCtx.ActiveOrganizationID, true)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	runningSince := time.Date(2026, 9, 15, 10, 30, 0, 0, time.UTC)
	ti.signaler.describeStatus = analysisstatus.Status{
		State:            analysisstatus.StateRunning,
		RunningSince:     &runningSince,
		LastRunStartedAt: nil,
		LastRunAt:        nil,
		LastRunOutcome:   "",
	}

	res, err := ti.service.GetRiskAnalysisStatus(ctx, &gen.GetRiskAnalysisStatusPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	require.Equal(t, "running", res.State)
	require.NotNil(t, res.RunningSince)
	require.Equal(t, "2026-09-15T10:30:00Z", *res.RunningSince)
	require.Nil(t, res.LastRunStartedAt)
	require.Nil(t, res.LastRunAt)
	require.Nil(t, res.LastRunOutcome)
}

// TestGetRiskAnalysisStatus_Never asserts the stub's default (no visible
// run) maps to never with every optional field unset.
func TestGetRiskAnalysisStatus_Never(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskWatchdog, authCtx.ActiveOrganizationID, true)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	res, err := ti.service.GetRiskAnalysisStatus(ctx, &gen.GetRiskAnalysisStatusPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	require.Equal(t, "never", res.State)
	require.Nil(t, res.RunningSince)
	require.Nil(t, res.LastRunStartedAt)
	require.Nil(t, res.LastRunAt)
	require.Nil(t, res.LastRunOutcome)
}

// TestGetRiskAnalysisStatus_DescribeError asserts a Temporal describe
// failure surfaces as an unexpected error rather than a fabricated state.
func TestGetRiskAnalysisStatus_DescribeError(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskWatchdog, authCtx.ActiveOrganizationID, true)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	ti.signaler.describeErr = errors.New("temporal unavailable")

	_, err := ti.service.GetRiskAnalysisStatus(ctx, &gen.GetRiskAnalysisStatusPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeUnexpected)
}

// TestGetRiskAnalysisStatus_FlagDisabled asserts an org without the
// gram-risk-watchdog flag is refused even with the org:admin scope, matching
// the signals endpoint it accompanies.
func TestGetRiskAnalysisStatus_FlagDisabled(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	_, err := ti.service.GetRiskAnalysisStatus(ctx, &gen.GetRiskAnalysisStatusPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

// TestGetRiskAnalysisStatus_RequiresOrgAdmin asserts the endpoint denies
// callers without the org:admin scope.
func TestGetRiskAnalysisStatus_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskWatchdog, authCtx.ActiveOrganizationID, true)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.GetRiskAnalysisStatus(ctx, &gen.GetRiskAnalysisStatusPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
