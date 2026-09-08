package policycore

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func TestRequireFreshPolicyRejectsChangedLockedRow(t *testing.T) {
	t.Parallel()

	preparedAt := time.Date(2026, time.August, 26, 1, 2, 3, 0, time.UTC)
	current := repo.RiskPolicy{UpdatedAt: pgtype.Timestamptz{Time: preparedAt, Valid: true}}
	locked := current
	require.NoError(t, requireFreshPolicy(current, locked))

	locked.UpdatedAt = pgtype.Timestamptz{Time: preparedAt.Add(time.Second), Valid: true}
	err := requireFreshPolicy(current, locked)
	var stale *StalePolicyError
	require.ErrorAs(t, err, &stale)
}

func TestBlockingPolicyConflictExcludesCurrentPolicy(t *testing.T) {
	t.Parallel()

	currentID := uuid.New()
	policies := []repo.RiskPolicy{
		{ID: uuid.New(), Name: "flag-only", Action: "flag"},
		{ID: currentID, Name: "current", Action: "block"},
	}
	require.NoError(t, blockingPolicyConflict(policies, currentID))

	policies = append(policies, repo.RiskPolicy{ID: uuid.New(), Name: "existing blocker", Action: "block"})
	err := blockingPolicyConflict(policies, currentID)
	var conflict *BlockingPolicyConflictError
	require.ErrorAs(t, err, &conflict)
	require.Equal(t, "existing blocker", conflict.PolicyName)
}

func TestMutationErrorPreservesStepAndCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("database unavailable")
	err := mutationError("lock risk policy mutations", cause)

	var mutation *MutationError
	require.ErrorAs(t, err, &mutation)
	require.Equal(t, "lock risk policy mutations", mutation.Message)
	require.ErrorIs(t, err, cause)
}
