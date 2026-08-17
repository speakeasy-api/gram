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

type recordingOperationLimiter struct {
	result ratelimit.Result
	err    error
	keys   []string
}

func (l *recordingOperationLimiter) Allow(_ context.Context, key string) (ratelimit.Result, error) {
	l.keys = append(l.keys, key)
	return l.result, l.err
}

// A surface with no OAuth connection still has to be metered. Refusing it the
// budget outright would make every budgeted tool fail for the assistant, which
// is indistinguishable to a user from the feature being broken.
func TestOperationBudgetMetersAConnectionlessCallerPerUser(t *testing.T) {
	t.Parallel()

	caller := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	organization := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	assistant := Principal{
		UserID:         "user-1",
		OrganizationID: "organization",
		Surface:        SurfaceProjectAssistant,
	}
	require.False(t, assistant.HasConnection())

	require.NoError(t, (OperationBudget{Connection: caller, Organization: organization}).Allow(t.Context(), assistant))
	require.Equal(t, []string{userSubjectURN("user-1")}, caller.keys, "a connection-less caller is metered on its own subject, not on an empty key")
	require.Equal(t, []string{"organization"}, organization.keys)
}

// Without a connection and without a user there is nothing to meter, so the
// budget must refuse rather than share one unbounded bucket.
func TestOperationBudgetRefusesAnUnidentifiedCaller(t *testing.T) {
	t.Parallel()

	caller := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	organization := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}

	err := (OperationBudget{Connection: caller, Organization: organization}).Allow(t.Context(), Principal{OrganizationID: "organization"})
	require.ErrorIs(t, err, ErrOperationBudgetUnavailable)
	require.Empty(t, caller.keys)
	require.Empty(t, organization.keys)
}
