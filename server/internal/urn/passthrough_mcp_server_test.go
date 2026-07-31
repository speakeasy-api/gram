package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestPassthroughMcpServerRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	original := urn.NewPassthroughMcpServer(id)

	require.Equal(t, "passthrough-mcp-server:44444444-4444-4444-4444-444444444444", original.String())

	parsed, err := urn.ParsePassthroughMcpServer(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"passthrough-mcp-server:44444444-4444-4444-4444-444444444444"`, string(data))

	var fromJSON urn.PassthroughMcpServer
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.PassthroughMcpServer
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.PassthroughMcpServer
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestPassthroughMcpServerRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParsePassthroughMcpServer("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParsePassthroughMcpServer("mcp-server:44444444-4444-4444-4444-444444444444")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParsePassthroughMcpServer("passthrough-mcp-server:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewPassthroughMcpServer(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
