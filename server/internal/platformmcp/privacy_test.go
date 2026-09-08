package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubjectCount_SuppressesSmallCounts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		//nolint:revive // The raw count is the subject of the test.
		value int64
		want  string
	}{
		// Zero identifies nobody and is the difference a diagnostic caller
		// needs between "no one used this" and "someone did".
		{name: "zero is reported exactly", value: 0, want: "0"},
		{name: "one is suppressed", value: 1, want: `"less_than_5"`},
		{name: "four is suppressed", value: 4, want: `"less_than_5"`},
		{name: "the threshold is reported", value: 5, want: "5"},
		{name: "large counts are reported", value: 4210, want: "4210"},
		{name: "negative counts floor at zero", value: -3, want: "0"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := json.Marshal(NewSubjectCount(test.value))
			require.NoError(t, err)
			require.Equal(t, test.want, string(encoded))
		})
	}
}

// TestSubjectCount_SuppressedValueNeverRoundTrips pins that a suppressed count
// cannot be recovered from what was transmitted. If decoding restored the real
// value, the suppression would only be cosmetic.
func TestSubjectCount_SuppressedValueNeverRoundTrips(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(NewSubjectCount(1))
	require.NoError(t, err)

	var decoded SubjectCount
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.True(t, decoded.Suppressed())
	require.Equal(t, int64(SubjectSuppressionThreshold-1), decoded.value)
}

// TestSubjectCount_RejectsUnknownSuppressionLabel pins that a label this build
// does not recognise is an error, not a zero. Decoding an unreadable value into
// "nobody" would turn ignorance into a positive claim about people.
func TestSubjectCount_RejectsUnknownSuppressionLabel(t *testing.T) {
	t.Parallel()

	var decoded SubjectCount
	require.Error(t, json.Unmarshal([]byte(`"less_than_50"`), &decoded))
	require.Error(t, json.Unmarshal([]byte(`"redacted"`), &decoded))
}

// TestSubjectCount_DecodedNegativeIsNormalized pins that decoding applies the
// same clamp the constructor does, so a negative count cannot survive a
// round-trip and re-serialize as a negative number.
func TestSubjectCount_DecodedNegativeIsNormalized(t *testing.T) {
	t.Parallel()

	var decoded SubjectCount
	require.NoError(t, json.Unmarshal([]byte("-1"), &decoded))

	encoded, err := json.Marshal(decoded)
	require.NoError(t, err)
	require.Equal(t, "0", string(encoded))
}

func TestSubjectCount_ReportedValueRoundTrips(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(NewSubjectCount(42))
	require.NoError(t, err)

	var decoded SubjectCount
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.False(t, decoded.Suppressed())
	require.Equal(t, int64(42), decoded.value)
}
