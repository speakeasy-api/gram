package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestIdentityProviderConnectionRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	original := urn.NewIdentityProviderConnection(id)

	require.Equal(t, "identity_provider_connection:33333333-3333-3333-3333-333333333333", original.String())

	parsed, err := urn.ParseIdentityProviderConnection(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"identity_provider_connection:33333333-3333-3333-3333-333333333333"`, string(data))

	var fromJSON urn.IdentityProviderConnection
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.IdentityProviderConnection
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.IdentityProviderConnection
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestIdentityProviderConnectionValidatesCurrentID(t *testing.T) {
	t.Parallel()

	u := urn.NewIdentityProviderConnection(uuid.New())
	u.ID = uuid.Nil

	_, err := json.Marshal(u)
	require.ErrorIs(t, err, urn.ErrInvalid)
	_, err = u.MarshalText()
	require.ErrorIs(t, err, urn.ErrInvalid)
	_, err = u.Value()
	require.ErrorIs(t, err, urn.ErrInvalid)

	u = urn.NewIdentityProviderConnection(uuid.Nil)
	u.ID = uuid.New()
	_, err = json.Marshal(u)
	require.NoError(t, err)
	_, err = u.MarshalText()
	require.NoError(t, err)
	_, err = u.Value()
	require.NoError(t, err)
}

func TestIdentityProviderConnectionRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseIdentityProviderConnection("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseIdentityProviderConnection("toolset:33333333-3333-3333-3333-333333333333")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseIdentityProviderConnection("identity_provider_connection:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseIdentityProviderConnection("identity_provider_connection:00000000-0000-0000-0000-000000000000")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewIdentityProviderConnection(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
