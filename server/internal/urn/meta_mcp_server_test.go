package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestMetaMcpServerRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	original := urn.NewMetaMcpServer(id)

	require.Equal(t, "meta-mcp-server:44444444-4444-4444-4444-444444444444", original.String())

	parsed, err := urn.ParseMetaMcpServer(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"meta-mcp-server:44444444-4444-4444-4444-444444444444"`, string(data))

	var fromJSON urn.MetaMcpServer
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.MetaMcpServer
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.MetaMcpServer
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestMetaMcpServerRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseMetaMcpServer("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseMetaMcpServer("mcp-server:44444444-4444-4444-4444-444444444444")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseMetaMcpServer("meta-mcp-server:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewMetaMcpServer(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
