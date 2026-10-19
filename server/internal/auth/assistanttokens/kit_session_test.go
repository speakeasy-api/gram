package assistanttokens

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestKitProviderSessionID(t *testing.T) {
	t.Parallel()
	require.Equal(t, "0b3a60e2-f08b-5ddd-9bcb-f3732f6a3322", kitProviderSessionID("claude-session-1").String())
	const id = "550e8400-e29b-41d4-a716-446655440000"
	require.Equal(t, uuid.MustParse(id), kitProviderSessionID(id))
	require.NotEqual(t, kitProviderSessionID("session-a"), kitProviderSessionID("session-b"))
}

func TestBindKitSessionRequiresRuntimeAuthority(t *testing.T) {
	t.Parallel()
	_, err := new(Manager).BindKitSession(t.Context(), uuid.New())
	require.Error(t, err)
}
