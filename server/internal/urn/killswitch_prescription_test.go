package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestKillswitchPrescriptionRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("6b1e4d2a-8f4c-4b3e-9a51-0c9d2e7f1a23")
	original := urn.NewKillswitchPrescription(id)
	require.Equal(t, "killswitch_prescription:"+id.String(), original.String())

	parsed, err := urn.ParseKillswitchPrescription(original.String())
	require.NoError(t, err)
	require.Equal(t, original, parsed)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"killswitch_prescription:`+id.String()+`"`, string(data))
	var decoded urn.KillswitchPrescription
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, original, decoded)

	text, err := original.MarshalText()
	require.NoError(t, err)
	var fromText urn.KillswitchPrescription
	require.NoError(t, fromText.UnmarshalText(text))
	require.Equal(t, original, fromText)

	value, err := original.Value()
	require.NoError(t, err)
	var fromDB urn.KillswitchPrescription
	require.NoError(t, fromDB.Scan(value))
	require.Equal(t, original, fromDB)
}

func TestKillswitchPrescriptionRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"",
		"killswitch_prescription",
		"killswitch_prescription:",
		"wrong:6b1e4d2a-8f4c-4b3e-9a51-0c9d2e7f1a23",
		"killswitch_prescription:not-a-uuid",
		"killswitch_prescription:00000000-0000-0000-0000-000000000000",
		"killswitch_prescription:6b1e4d2a-8f4c-4b3e-9a51-0c9d2e7f1a23:extra",
	} {
		_, err := urn.ParseKillswitchPrescription(value)
		require.ErrorIs(t, err, urn.ErrInvalid)
	}

	_, err := urn.NewKillswitchPrescription(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
	_, err = urn.NewKillswitchPrescription(uuid.Nil).Value()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
