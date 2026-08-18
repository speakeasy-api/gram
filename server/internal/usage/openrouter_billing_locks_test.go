package usage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

func TestBillingTransitionWaitsForEveryPlatformInferenceKeyLock(t *testing.T) {
	t.Parallel()

	organizationID := "org-inference-billing-locks"
	_, db, _, _ := newTUMTestService(t, organizationID)

	for _, keyType := range openrouter.AllKeyTypes {
		connection, err := db.Acquire(t.Context())
		require.NoError(t, err)

		lockParams := activitiesrepo.AcquireOpenRouterKeyBillingLockParams{
			KeyType:        string(keyType),
			OrganizationID: organizationID,
		}
		require.NoError(t, activitiesrepo.New(connection).AcquireOpenRouterKeyBillingLock(t.Context(), lockParams))

		transition := testenv.BeginTx(t, t.Context(), db)

		acquired := make(chan error, 1)
		go func() {
			acquired <- acquireOpenRouterBillingLocks(t.Context(), repo.New(transition), organizationID)
		}()

		var earlyResult error
		returnedEarly := false
		select {
		case earlyResult = <-acquired:
			returnedEarly = true
		case <-time.After(100 * time.Millisecond):
		}

		unlocked, err := activitiesrepo.New(connection).ReleaseOpenRouterKeyBillingLock(t.Context(), activitiesrepo.ReleaseOpenRouterKeyBillingLockParams(lockParams))
		require.NoError(t, err)
		require.True(t, unlocked)
		connection.Release()
		if returnedEarly {
			require.FailNow(t, "billing transition bypassed inference-key lock", "key_type=%s err=%v", keyType, earlyResult)
		}
		require.NoError(t, <-acquired)
		require.NoError(t, transition.Rollback(t.Context()))
	}
}
