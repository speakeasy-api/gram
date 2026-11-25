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

func TestAssistantToolTargetPrefersExplicitThreadOverThreadlessPrincipal(t *testing.T) {
	t.Parallel()
	threadless := AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.Nil}
	ctx := SetAssistantPrincipal(t.Context(), threadless)
	got, ok := GetAssistantToolTarget(ctx)
	require.True(t, ok)
	require.Equal(t, threadless, got, "an assistant-scoped principal still targets its assistant")
	explicit := AssistantPrincipal{AssistantID: threadless.AssistantID, ThreadID: uuid.New()}
	got, ok = GetAssistantToolTarget(SetAssistantToolTarget(ctx, explicit))
	require.True(t, ok)
	require.Equal(t, explicit, got, "an explicit thread wins over a principal without one")
}
