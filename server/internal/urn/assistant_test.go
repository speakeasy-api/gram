package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestAssistantRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	original := urn.NewAssistant(id)

	require.Equal(t, "assistant:55555555-5555-5555-5555-555555555555", original.String())

	parsed, err := urn.ParseAssistant(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"assistant:55555555-5555-5555-5555-555555555555"`, string(data))

	var fromJSON urn.Assistant
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.Assistant
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.Assistant
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestAssistantRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseAssistant("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseAssistant("toolset:55555555-5555-5555-5555-555555555555")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseAssistant("assistant:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewAssistant(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
