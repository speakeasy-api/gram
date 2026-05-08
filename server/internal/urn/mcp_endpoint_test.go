package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestMcpEndpointRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	original := urn.NewMcpEndpoint(id)

	require.Equal(t, "mcp-endpoint:33333333-3333-3333-3333-333333333333", original.String())

	parsed, err := urn.ParseMcpEndpoint(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"mcp-endpoint:33333333-3333-3333-3333-333333333333"`, string(data))

	var fromJSON urn.McpEndpoint
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.McpEndpoint
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.McpEndpoint
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestMcpEndpointRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseMcpEndpoint("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseMcpEndpoint("toolset:33333333-3333-3333-3333-333333333333")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseMcpEndpoint("mcp-endpoint:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewMcpEndpoint(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
