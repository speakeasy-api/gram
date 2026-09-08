package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestSessionQuarantineRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	original := urn.NewSessionQuarantine(id)
	require.Equal(t, "session_quarantine:55555555-5555-5555-5555-555555555555", original.String())

	parsed, err := urn.ParseSessionQuarantine(original.String())
	require.NoError(t, err)
	require.Equal(t, original, parsed)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.JSONEq(t, `"session_quarantine:55555555-5555-5555-5555-555555555555"`, string(data))

	var fromJSON urn.SessionQuarantine
	require.NoError(t, json.Unmarshal(data, &fromJSON))
	require.Equal(t, original, fromJSON)

	text, err := original.MarshalText()
	require.NoError(t, err)
	var fromText urn.SessionQuarantine
	require.NoError(t, fromText.UnmarshalText(text))
	require.Equal(t, original, fromText)

	value, err := original.Value()
	require.NoError(t, err)
	var fromDB urn.SessionQuarantine
	require.NoError(t, fromDB.Scan(value))
	require.Equal(t, original, fromDB)
}

func TestSessionQuarantineRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"",
		"session_quarantine",
		"session_quarantine:not-a-uuid",
		"session_quarantine:00000000-0000-0000-0000-000000000000",
		"risk_policy:55555555-5555-5555-5555-555555555555",
	} {
		_, err := urn.ParseSessionQuarantine(value)
		require.ErrorIs(t, err, urn.ErrInvalid)
	}

	_, err := urn.NewSessionQuarantine(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
