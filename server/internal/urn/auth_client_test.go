package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestAuthClientRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	original := urn.NewAuthClient(id)

	require.Equal(t, "auth_client:33333333-3333-3333-3333-333333333333", original.String())

	parsed, err := urn.ParseAuthClient(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"auth_client:33333333-3333-3333-3333-333333333333"`, string(data))

	var fromJSON urn.AuthClient
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.AuthClient
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.AuthClient
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestAuthClientRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseAuthClient("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseAuthClient("toolset:33333333-3333-3333-3333-333333333333")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseAuthClient("auth_client:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseAuthClient("auth_client:" + uuid.Nil.String())
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewAuthClient(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
