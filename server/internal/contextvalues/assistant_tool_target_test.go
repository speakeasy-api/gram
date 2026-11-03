package contextvalues

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAssistantToolTargetDoesNotAuthenticate(t *testing.T) {
	t.Parallel()
	target := AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()}
	ctx := SetAssistantToolTarget(t.Context(), target)
	_, ok := GetAssistantPrincipal(ctx)
	require.False(t, ok)
	got, ok := GetAssistantToolTarget(ctx)
	require.True(t, ok)
	require.Equal(t, target, got)
	automatic := AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()}
	ctx = SetAssistantPrincipal(ctx, automatic)
	got, ok = GetAssistantToolTarget(ctx)
	require.True(t, ok)
	require.Equal(t, automatic, got, "authenticated automatic context wins")
}
