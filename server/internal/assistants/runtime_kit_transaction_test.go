package assistants

import (
	"testing"

	"github.com/google/uuid"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/stretchr/testify/require"
)

func TestKitLifecycleTransactionPreservesIsolationAcrossPublish(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "kit_lifecycle_isolation")
	require.NoError(t, err)
	tx, cleanup, err := beginKitLifecycleTransaction(t.Context(), db, uuid.NewString())
	require.NoError(t, err)
	defer cleanup()
	for phase := range 3 {
		isolation, err := assistantrepo.New(tx).GetAssistantTransactionIsolationForTest(t.Context())
		require.NoError(t, err)
		require.Equal(t, "repeatable read", isolation, "lifecycle phase %d", phase)
		require.NoError(t, tx.Publish(t.Context()))
	}
}
