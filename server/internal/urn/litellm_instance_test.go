package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestLiteLLMInstanceRoundTrip(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	original := urn.NewLiteLLMInstance(id)
	require.Equal(t, "litellm-instance:33333333-3333-3333-3333-333333333333", original.String())
	parsed, err := urn.ParseLiteLLMInstance(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)
	data, err := json.Marshal(original)
	require.NoError(t, err)
	var decoded urn.LiteLLMInstance
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, original.ID, decoded.ID)
}

func TestLiteLLMInstanceRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	_, err := urn.ParseLiteLLMInstance("apikey:33333333-3333-3333-3333-333333333333")
	require.ErrorIs(t, err, urn.ErrInvalid)
	_, err = urn.NewLiteLLMInstance(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}

func TestLiteLLMInstanceZeroValue(t *testing.T) {
	t.Parallel()

	var zero urn.LiteLLMInstance
	require.True(t, zero.IsZero())

	_, err := zero.MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
	_, err = zero.MarshalText()
	require.ErrorIs(t, err, urn.ErrInvalid)
	value, err := zero.Value()
	require.ErrorIs(t, err, urn.ErrInvalid)
	require.Nil(t, value)
	require.NoError(t, zero.Scan(nil))
	require.True(t, zero.IsZero())

	mutated := urn.NewLiteLLMInstance(uuid.MustParse("33333333-3333-3333-3333-333333333333"))
	_, err = mutated.MarshalJSON()
	require.NoError(t, err)
	mutated.ID = uuid.Nil
	_, err = mutated.MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
	_, err = mutated.MarshalText()
	require.ErrorIs(t, err, urn.ErrInvalid)
	_, err = mutated.Value()
	require.ErrorIs(t, err, urn.ErrInvalid)
}

func TestLiteLLMInstanceDatabaseRoundTrip(t *testing.T) {
	t.Parallel()

	original := urn.NewLiteLLMInstance(uuid.MustParse("33333333-3333-3333-3333-333333333333"))
	value, err := original.Value()
	require.NoError(t, err)
	require.Equal(t, original.String(), value)

	var fromString urn.LiteLLMInstance
	require.NoError(t, fromString.Scan(value))
	require.Equal(t, original.ID, fromString.ID)

	var fromBytes urn.LiteLLMInstance
	require.NoError(t, fromBytes.Scan([]byte(original.String())))
	require.Equal(t, original.ID, fromBytes.ID)

	var invalid urn.LiteLLMInstance
	err = invalid.Scan("litellm-instance:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)
	require.Error(t, invalid.Scan(42))
}
