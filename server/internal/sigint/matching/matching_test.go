package matching_test

import (
	"context"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/sigint/matching"
	"github.com/stretchr/testify/require"
)

func TestMatchingContract(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"user", "assistant", "tool", "system"} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()
			got, err := matching.Match(t.Context(), matching.DefaultExpression, matching.Message{Role: role})
			require.NoError(t, err)
			require.Equal(t, role == "user", got)
		})
	}
}

func TestRejectsInvalidMatchingContract(t *testing.T) {
	t.Parallel()
	for _, expression := range []string{"", "message.role", "message.missing == true", "unknown == 1"} {
		require.Error(t, matching.Validate(expression))
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := matching.Match(ctx, "true", matching.Message{Role: "user"})
	require.ErrorIs(t, err, context.Canceled)
}
