package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestMetaMcpServerMemberRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	original := urn.NewMetaMcpServerMember(id)

	require.Equal(t, "meta-mcp-server-member:55555555-5555-5555-5555-555555555555", original.String())

	parsed, err := urn.ParseMetaMcpServerMember(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"meta-mcp-server-member:55555555-5555-5555-5555-555555555555"`, string(data))

	var fromJSON urn.MetaMcpServerMember
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.MetaMcpServerMember
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.MetaMcpServerMember
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestMetaMcpServerMemberRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseMetaMcpServerMember("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseMetaMcpServerMember("mcp-server:55555555-5555-5555-5555-555555555555")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseMetaMcpServerMember("meta-mcp-server-member:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewMetaMcpServerMember(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
