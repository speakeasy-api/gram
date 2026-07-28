package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestRemoteMcpServerRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	original := urn.NewRemoteMcpServer(id)

	require.Equal(t, "remote-mcp-server:44444444-4444-4444-4444-444444444444", original.String())

	parsed, err := urn.ParseRemoteMcpServer(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"remote-mcp-server:44444444-4444-4444-4444-444444444444"`, string(data))

	var fromJSON urn.RemoteMcpServer
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.RemoteMcpServer
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.RemoteMcpServer
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestRemoteMcpServerRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseRemoteMcpServer("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseRemoteMcpServer("mcp-server:44444444-4444-4444-4444-444444444444")
	require.ErrorIs(t, err, urn.ErrInvalid)

	// The header urn shares this urn's prefix as a substring; it must not parse
	// as a remote mcp server.
	_, err = urn.ParseRemoteMcpServer("remote-mcp-server-header:44444444-4444-4444-4444-444444444444")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseRemoteMcpServer("remote-mcp-server:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewRemoteMcpServer(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
