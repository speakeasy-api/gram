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
	result ratelimit.Result
	err    error
	keys   []string
}

func (l *recordingOperationLimiter) Allow(_ context.Context, key string) (ratelimit.Result, error) {
	l.keys = append(l.keys, key)
	return l.result, l.err
}
