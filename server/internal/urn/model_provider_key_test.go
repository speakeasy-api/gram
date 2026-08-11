package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestModelProviderKeyRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	original := urn.NewModelProviderKey(id)

	require.Equal(t, "model_provider_key:33333333-3333-3333-3333-333333333333", original.String())

	parsed, err := urn.ParseModelProviderKey(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"model_provider_key:33333333-3333-3333-3333-333333333333"`, string(data))

	var fromJSON urn.ModelProviderKey
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.ModelProviderKey
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.ModelProviderKey
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestModelProviderKeyRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseModelProviderKey("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseModelProviderKey("toolset:33333333-3333-3333-3333-333333333333")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseModelProviderKey("model_provider_key:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewModelProviderKey(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
