package urn_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestPlatformMcpRegistrationRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	original := urn.NewPlatformMcpRegistration(id)

	require.Equal(t, "platform-mcp-registration:44444444-4444-4444-4444-444444444444", original.String())

	parsed, err := urn.ParsePlatformMcpRegistration(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	marshaled, err := original.MarshalJSON()
	require.NoError(t, err)
	require.JSONEq(t, `"platform-mcp-registration:44444444-4444-4444-4444-444444444444"`, string(marshaled))

	var unmarshaled urn.PlatformMcpRegistration
	require.NoError(t, unmarshaled.UnmarshalJSON(marshaled))
	require.Equal(t, original.ID, unmarshaled.ID)
}

func TestPlatformMcpRegistrationRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"",
		"not-a-urn",
		"mcp-server:44444444-4444-4444-4444-444444444444",
		"platform-mcp-registration:not-a-uuid",
		"platform-mcp-registration:44444444-4444-4444-4444-444444444444:extra",
	} {
		_, err := urn.ParsePlatformMcpRegistration(value)
		require.Error(t, err, value)
	}
}
