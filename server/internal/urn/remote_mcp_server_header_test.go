package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestRemoteMcpServerHeaderRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	original := urn.NewRemoteMcpServerHeader(id)

	require.Equal(t, "remote-mcp-server-header:55555555-5555-5555-5555-555555555555", original.String())

	parsed, err := urn.ParseRemoteMcpServerHeader(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"remote-mcp-server-header:55555555-5555-5555-5555-555555555555"`, string(data))

	var fromJSON urn.RemoteMcpServerHeader
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.RemoteMcpServerHeader
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.RemoteMcpServerHeader
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestRemoteMcpServerHeaderRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseRemoteMcpServerHeader("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	// The parent server urn is a prefix of this one; it must not parse as a header.
	_, err = urn.ParseRemoteMcpServerHeader("remote-mcp-server:55555555-5555-5555-5555-555555555555")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseRemoteMcpServerHeader("remote-mcp-server-header:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewRemoteMcpServerHeader(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
