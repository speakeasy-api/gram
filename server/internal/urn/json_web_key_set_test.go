package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestJsonWebKeySetRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	original := urn.NewJsonWebKeySet(id)

	require.Equal(t, "json_web_key_set:44444444-4444-4444-4444-444444444444", original.String())

	parsed, err := urn.ParseJsonWebKeySet(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"json_web_key_set:44444444-4444-4444-4444-444444444444"`, string(data))

	var fromJSON urn.JsonWebKeySet
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.JsonWebKeySet
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.JsonWebKeySet
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestJsonWebKeySetRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseJsonWebKeySet("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseJsonWebKeySet("toolset:44444444-4444-4444-4444-444444444444")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseJsonWebKeySet("json_web_key_set:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewJsonWebKeySet(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
