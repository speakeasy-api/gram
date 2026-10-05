package relay

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestGovernedGateBudgetStopsShortOfCallerDeadline: agenthooks turns an
// expired handler context into no decision, which providers treat as allow,
// so a governed verdict must always resolve before the caller's deadline.
func TestGovernedGateBudgetStopsShortOfCallerDeadline(t *testing.T) {
	t.Parallel()

	t.Run("no deadline adds the mint allowance", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, gateSendBudget+gateMintBudget, governedGateBudget(context.Background(), gateSendBudget))
	})

	t.Run("ample deadline adds the mint allowance", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		t.Cleanup(cancel)
		require.Equal(t, gateSendBudget+gateMintBudget, governedGateBudget(ctx, gateSendBudget))
	})

	t.Run("tight deadline keeps the margin", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		t.Cleanup(cancel)
		budget := governedGateBudget(ctx, gateSendBudget)
		require.Positive(t, budget)
		require.LessOrEqual(t, budget, 3*time.Second-governedDeadlineMargin)
	})

	t.Run("spent deadline yields no budget", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), governedDeadlineMargin/2)
		t.Cleanup(cancel)
		require.Zero(t, governedGateBudget(ctx, gateSendBudget))
	})
}
