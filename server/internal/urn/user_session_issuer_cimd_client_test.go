package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestUserSessionIssuerCimdClientRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	original := urn.NewUserSessionIssuerCimdClient(id)

	require.Equal(t, "user-session-issuer-cimd-client:55555555-5555-5555-5555-555555555555", original.String())

	parsed, err := urn.ParseUserSessionIssuerCimdClient(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"user-session-issuer-cimd-client:55555555-5555-5555-5555-555555555555"`, string(data))

	var fromJSON urn.UserSessionIssuerCimdClient
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.UserSessionIssuerCimdClient
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.UserSessionIssuerCimdClient
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestUserSessionIssuerCimdClientRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseUserSessionIssuerCimdClient("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	// The parent server urn is a prefix of this one; it must not parse as a header.
	_, err = urn.ParseUserSessionIssuerCimdClient("remote-mcp-server:55555555-5555-5555-5555-555555555555")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseUserSessionIssuerCimdClient("user-session-issuer-cimd-client:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewUserSessionIssuerCimdClient(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
