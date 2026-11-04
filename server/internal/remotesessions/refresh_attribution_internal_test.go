package remotesessions

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
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
