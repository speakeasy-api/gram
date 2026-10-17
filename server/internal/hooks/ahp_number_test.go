package hooks

import (
	"encoding/json"
	"strconv"
	"testing"

	ahp "github.com/agenthooksprotocol/go-sdk"
	"github.com/stretchr/testify/require"
)

func TestAHPTokenCountExactNumbers(t *testing.T) {
	t.Parallel()
	maxInt := int(^uint(0) >> 1)
	for _, tc := range []struct {
		value string
		want  int
	}{
		{"0", 0}, {"12", 12}, {"12.0", 12}, {"12.000", 12},
		{"1e3", 1000}, {"1E+3", 1000}, {"120e-1", 12},
		{"1.20e1", 12}, {"0.001e3", 1}, {"1000.000e-3", 1},
		{"-0", 0}, {"-0.0e-999999999", 0}, {"0e999999999999999999999999999", 0},
		{strconv.Itoa(maxInt), maxInt},
		{strconv.Itoa(maxInt) + ".0", maxInt},
		{strconv.Itoa(maxInt) + "0e-1", maxInt},
	} {
		t.Run(tc.value, func(t *testing.T) {
			got := ahpTokenCount(ahp.Some(json.Number(tc.value)))
			require.NotNil(t, got)
			require.Equal(t, tc.want, *got)
		})
	}
	// Above 2^53: a float64 conversion would silently round this count.
	if strconv.IntSize == 64 {
		got := ahpTokenCount(ahp.Some(json.Number("9007199254740993.0")))
		require.NotNil(t, got)
		require.Equal(t, int64(9007199254740993), int64(*got))
	}
}

func TestAHPTokenCountRejectsInvalidNumbers(t *testing.T) {
	t.Parallel()
	maxInt := uint64(^uint(0) >> 1)
	for _, raw := range []string{
		"-1", "-12.0", "-1e3", "0.1", "12.01", "1e-3", "1201e-2",
		"9007199254740992.1", // Must not round a fraction into an integer.
		strconv.FormatUint(maxInt+1, 10), strconv.FormatUint(maxInt+1, 10) + ".0",
		"1e19", "1e999999999", "1e-999999999",
		"1e999999999999999999999999999", "1e-999999999999999999999999999",
		"", "null", "true", "[]", "{}", "NaN", "Infinity", "01", "+1", "1.", ".1", "1e", "1 2",
	} {
		t.Run(raw, func(t *testing.T) {
			require.Nil(t, ahpTokenCount(ahp.Some(json.Number(raw))))
		})
	}
	require.Nil(t, ahpTokenCount(ahp.Optional[json.Number]{}))
}
