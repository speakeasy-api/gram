package assistanttokens

import (
	"context"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestKitProviderSessionID(t *testing.T) {
	t.Parallel()
	// Released Kit 2c4a35e adapter.rs sends this UUIDv5 as the native chat header.
	require.Equal(t, "15f428a5-735e-5088-9e50-5210f6365e50", kitProviderSessionID("speakeasy-contract").String())
	require.Equal(t, "0b3a60e2-f08b-5ddd-9bcb-f3732f6a3322", kitProviderSessionID("claude-session-1").String())
	const id = "550e8400-e29b-41d4-a716-446655440000"
	require.Equal(t, uuid.MustParse(id), kitProviderSessionID(id))
	require.NotEqual(t, kitProviderSessionID("session-a"), kitProviderSessionID("session-b"))
}

func TestBindKitSessionRequiresRuntimeAuthority(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(t.Context(), inferenceContextKey{}, assistantidentity.Identity{ProjectID: uuid.New(), AssistantID: uuid.New()})
	_, err := new(Manager).BindKitSession(ctx, uuid.New())
	var forbidden *oops.ShareableError
	require.ErrorAs(t, err, &forbidden)
	require.Equal(t, oops.CodeForbidden, forbidden.Code)
}
