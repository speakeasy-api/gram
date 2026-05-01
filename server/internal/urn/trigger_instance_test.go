package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestTriggerInstanceRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	original := urn.NewTriggerInstance(id)

	require.Equal(t, "trigger-instance:44444444-4444-4444-4444-444444444444", original.String())

	parsed, err := urn.ParseTriggerInstance(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"trigger-instance:44444444-4444-4444-4444-444444444444"`, string(data))

	var fromJSON urn.TriggerInstance
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.TriggerInstance
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.TriggerInstance
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestTriggerInstanceRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseTriggerInstance("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseTriggerInstance("toolset:44444444-4444-4444-4444-444444444444")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseTriggerInstance("trigger-instance:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewTriggerInstance(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
