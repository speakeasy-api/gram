package remotesessions

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
)

// One member's outage must not read as another's on the same issuer.
func TestSkippedClients_DoNotLeakBetweenClients(t *testing.T) {
	t.Parallel()

	projectA, projectB, issuer, clientA, clientB := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	skipped := new(SkippedClients)
	skipped.Note(projectA, issuer, clientA, ErrRemoteSessionUnavailable)

	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	require.ErrorIs(t, skipped.Reason(projectA, issuer, none), ErrRemoteSessionUnavailable)
	require.NoError(t, skipped.Reason(projectB, issuer, none), "another project's member")
	require.NoError(t, skipped.Reason(projectA, issuer, uuid.NullUUID{UUID: clientB, Valid: true}), "another chosen client")
	require.ErrorIs(t, skipped.Reason(projectA, issuer, uuid.NullUUID{UUID: clientA, Valid: true}), ErrRemoteSessionUnavailable)
}

func TestRefreshAttribution_InAttemptIgnoresZero(t *testing.T) {
	t.Parallel()
	scope := mcpconnectionattempts.ProviderSessionScope("test-org", uuid.New(), mcpconnectionattempts.AppFirstParty)
	attempt := mcpconnectionattempts.Begin(scope)
	attribution := RefreshIn(scope).InAttempt(attempt)
	require.Equal(t, attribution, attribution.InAttempt(mcpconnectionattempts.Attempt{}))
	replacement := mcpconnectionattempts.Begin(scope)
	require.Equal(t, replacement.ID(), attribution.InAttempt(replacement).attempt.ID())
	require.True(t, RefreshIn(scope).InAttempt(mcpconnectionattempts.Attempt{}).attempt.IsZero())
}
