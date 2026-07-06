package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestMcpServerToolMetadataRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	original := urn.NewMcpServerToolMetadata(id)

	require.Equal(t, "mcp-server-tool-metadata:44444444-4444-4444-4444-444444444444", original.String())

	parsed, err := urn.ParseMcpServerToolMetadata(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"mcp-server-tool-metadata:44444444-4444-4444-4444-444444444444"`, string(data))

	var fromJSON urn.McpServerToolMetadata
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.McpServerToolMetadata
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.McpServerToolMetadata
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestMcpServerToolMetadataRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseMcpServerToolMetadata("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseMcpServerToolMetadata("mcp-server:44444444-4444-4444-4444-444444444444")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseMcpServerToolMetadata("mcp-server-tool-metadata:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseMcpServerToolMetadata("mcp-server-tool-metadata:")
	require.ErrorIs(t, err, urn.ErrInvalid)
}
