package platformmcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

func TestOperationBudgetChargesConnectionBeforeOrganization(t *testing.T) {
	t.Parallel()

	connection := &recordingOperationLimiter{result: ratelimit.Result{Allowed: false}}
	organization := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	err := (OperationBudget{Connection: connection, Organization: organization}).Allow(t.Context(), Principal{ConnectionID: "connection", OrganizationID: "organization"})

	require.ErrorIs(t, err, ErrOperationRateLimited)
	require.Equal(t, []string{"connection"}, connection.keys)
	require.Empty(t, organization.keys)
}

func TestOperationBudgetStopsAfterOrganizationDenial(t *testing.T) {
	t.Parallel()

	connection := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	organization := &recordingOperationLimiter{result: ratelimit.Result{Allowed: false}}
	err := (OperationBudget{Connection: connection, Organization: organization}).Allow(t.Context(), Principal{ConnectionID: "connection", OrganizationID: "organization"})

	require.ErrorIs(t, err, ErrOperationRateLimited)
	require.Equal(t, []string{"connection"}, connection.keys)
	require.Equal(t, []string{"organization"}, organization.keys)
}

func TestOperationBudgetDistinguishesLimiterFailureFromThrottle(t *testing.T) {
	t.Parallel()

	unavailable := errors.New("redis unavailable")
	err := (OperationBudget{
		Connection:   &recordingOperationLimiter{err: unavailable},
		Organization: &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
	}).Allow(t.Context(), Principal{ConnectionID: "connection", OrganizationID: "organization"})

	require.ErrorIs(t, err, ErrOperationBudgetUnavailable)
	require.NotErrorIs(t, err, ErrOperationRateLimited)
}

func TestOperationBudgetMetersAConnectionlessAssistantByUser(t *testing.T) {
	t.Parallel()

	connection := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	organization := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	err := (OperationBudget{Connection: connection, Organization: organization}).Allow(t.Context(), Principal{UserID: "user", OrganizationID: "organization", ClientID: AssistantClientID, Surface: SurfaceProjectAssistant})

	require.NoError(t, err)
	require.Equal(t, []string{"assistant:" + AssistantClientID + ":user"}, connection.keys)
	require.Equal(t, []string{"organization"}, organization.keys)
}

func TestOperationBudgetMetersAConnectionlessDashboardByUser(t *testing.T) {
	t.Parallel()

	connection := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	organization := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	err := (OperationBudget{Connection: connection, Organization: organization}).Allow(t.Context(), Principal{UserID: "user", OrganizationID: "organization", Surface: SurfaceDashboard})

	require.NoError(t, err)
	require.Equal(t, []string{"dashboard:user"}, connection.keys)
	require.Equal(t, []string{"organization"}, organization.keys)
}

func TestOperationBudgetThrottlesAConnectionlessAssistantBeforeOrganization(t *testing.T) {
	t.Parallel()

	connection := &recordingOperationLimiter{result: ratelimit.Result{Allowed: false}}
	organization := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	err := (OperationBudget{Connection: connection, Organization: organization}).Allow(t.Context(), Principal{UserID: "user", OrganizationID: "organization", ClientID: AssistantClientID, Surface: SurfaceProjectAssistant})

	require.ErrorIs(t, err, ErrOperationRateLimited)
	require.Equal(t, []string{"assistant:" + AssistantClientID + ":user"}, connection.keys)
	require.Empty(t, organization.keys)
}

func TestOperationBudgetRejectsAConnectionClaimedByGenerationAlone(t *testing.T) {
	t.Parallel()

	connection := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	organization := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	err := (OperationBudget{Connection: connection, Organization: organization}).Allow(t.Context(), Principal{ConnectionID: "", Generation: "generation", OrganizationID: "organization"})

	require.ErrorIs(t, err, ErrOperationBudgetUnavailable)
	require.Empty(t, connection.keys)
	require.Empty(t, organization.keys)
}

func TestOperationBudgetStillRequiresAnOrganization(t *testing.T) {
	t.Parallel()

	err := (OperationBudget{
		Connection:   &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
		Organization: &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
	}).Allow(t.Context(), Principal{UserID: "user", ClientID: AssistantClientID, Surface: SurfaceProjectAssistant, OrganizationID: ""})

	require.ErrorIs(t, err, ErrOperationBudgetUnavailable)
}

type recordingOperationLimiter struct {
	result  ratelimit.Result
	err     error
	keys    []string
	charges []int
}

func (l *recordingOperationLimiter) Allow(_ context.Context, key string) (ratelimit.Result, error) {
	l.keys = append(l.keys, key)
	return l.result, l.err
}

func (l *recordingOperationLimiter) AllowN(_ context.Context, key string, n int) (ratelimit.Result, error) {
	l.keys = append(l.keys, key)
	l.charges = append(l.charges, n)
	return l.result, l.err
}

// TestDrilldownVolumeBudget_ChargesRealVolume pins that the second drill-down
// cap meters what a page returns rather than the call that returned it. A
// per-call budget alone lets a caller page steadily under the rate and still
// accumulate a whole window.
func TestDrilldownVolumeBudget_ChargesRealVolume(t *testing.T) {
	t.Parallel()

	rows := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	metrics := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	budget := DrilldownVolumeBudget{Rows: rows, MetricQueries: metrics}
	principal := Principal{OrganizationID: "org-1", ConnectionID: "connection-1", Generation: "generation-1"}

	require.NoError(t, budget.AllowRows(t.Context(), principal, 20))
	require.NoError(t, budget.AllowMetricQuery(t.Context(), principal))

	require.Equal(t, []string{"connection-1"}, rows.keys)
	require.Equal(t, []int{20}, rows.charges)
	require.Equal(t, []int{1}, metrics.charges)
}

// TestDrilldownVolumeBudget_ExhaustionIsRateLimited pins that a refused volume
// charge stops the read rather than returning a short page.
func TestDrilldownVolumeBudget_ExhaustionIsRateLimited(t *testing.T) {
	t.Parallel()

	budget := DrilldownVolumeBudget{
		Rows:          &recordingOperationLimiter{result: ratelimit.Result{Allowed: false}},
		MetricQueries: &recordingOperationLimiter{result: ratelimit.Result{Allowed: false}},
	}
	principal := Principal{OrganizationID: "org-1", ConnectionID: "connection-1", Generation: "generation-1"}

	require.ErrorIs(t, budget.AllowRows(t.Context(), principal, 20), ErrOperationRateLimited)
	require.ErrorIs(t, budget.AllowMetricQuery(t.Context(), principal), ErrOperationRateLimited)
}

// TestDrilldownVolumeBudget_ConnectionlessCallerIsNotMetered pins that a
// surface holding no OAuth connection passes the volume cap rather than being
// refused: it has no connection key to charge, and keying the empty string
// would pool every such caller into one bucket.
func TestDrilldownVolumeBudget_ConnectionlessCallerIsNotMetered(t *testing.T) {
	t.Parallel()

	rows := &recordingOperationLimiter{result: ratelimit.Result{Allowed: false}}
	budget := DrilldownVolumeBudget{Rows: rows, MetricQueries: rows}

	require.NoError(t, budget.AllowRows(t.Context(), Principal{OrganizationID: "org-1"}, 20))
	require.Empty(t, rows.keys)

	// A principal claiming a connection through its generation alone has no key
	// to charge, so the budget is unavailable to it rather than free.
	require.ErrorIs(t,
		budget.AllowRows(t.Context(), Principal{OrganizationID: "org-1", Generation: "generation-1"}, 20),
		ErrOperationBudgetUnavailable,
	)
}
