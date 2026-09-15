package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestIdentityProviderConnectionIDRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	original := urn.NewIdentityProviderConnectionID(id)
	require.Equal(t, "identityproviderconnection:11111111-1111-1111-1111-111111111111", original.String())

	parsed, err := urn.ParseIdentityProviderConnectionID(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	var fromJSON urn.IdentityProviderConnectionID
	require.NoError(t, json.Unmarshal(data, &fromJSON))
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)
	var fromText urn.IdentityProviderConnectionID
	require.NoError(t, fromText.UnmarshalText(text))
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)
	var fromDB urn.IdentityProviderConnectionID
	require.NoError(t, fromDB.Scan(value))
	require.Equal(t, original.ID, fromDB.ID)
}

func TestIdentityProviderConnectionIDRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseIdentityProviderConnectionID("")
	require.ErrorIs(t, err, urn.ErrInvalid)
	_, err = urn.ParseIdentityProviderConnectionID("environment:11111111-1111-1111-1111-111111111111")
	require.ErrorIs(t, err, urn.ErrInvalid)
	_, err = urn.ParseIdentityProviderConnectionID("identityproviderconnection:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)
	_, err = urn.NewIdentityProviderConnectionID(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
