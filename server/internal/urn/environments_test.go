package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	original := urn.NewEnvironment(id)

	require.Equal(t, "environment:22222222-2222-2222-2222-222222222222", original.String())

	parsed, err := urn.ParseEnvironment(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"environment:22222222-2222-2222-2222-222222222222"`, string(data))

	var fromJSON urn.Environment
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.Environment
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.Environment
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestEnvironmentRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseEnvironment("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseEnvironment("toolset:22222222-2222-2222-2222-222222222222")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseEnvironment("environment:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewEnvironment(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
