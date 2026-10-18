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
			t.Parallel()
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
			t.Parallel()
			require.Nil(t, ahpTokenCount(ahp.Some(json.Number(raw))))
		})
	}
	require.Nil(t, ahpTokenCount(ahp.Optional[json.Number]{}))
}

func TestAHPUsageNormalizesCachedInput(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		input  *int
		cached *int
		want   *int
	}{
		{"cached input", new(100), new(80), new(20)},
		{"fully cached", new(100), new(100), new(0)},
		{"cache exceeds input", new(10), new(20), new(0)},
		{"zero cache", new(100), new(0), new(100)},
		{"missing cache", new(100), nil, new(100)},
		{"missing input", nil, new(80), nil},
		{"missing both", nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := ahpTestResponse()
			usage, ok := raw["usage"].(map[string]any)
			require.True(t, ok)
			delete(usage, "inputTokens")
			if tc.input != nil {
				usage["inputTokens"] = *tc.input
			}
			if tc.cached != nil {
				usage["cacheReadTokens"] = *tc.cached
			}
			usage["cacheWriteTokens"] = 7
			data, err := json.Marshal(raw)
			require.NoError(t, err)
			var event ahp.ModelResponseAfterEvent
			require.NoError(t, json.Unmarshal(data, &event))
			payloads := ahpModelResponsePayloads(&event)
			got := payloads[0].Data.Usage
			require.NotNil(t, got)
			require.Equal(t, tc.want, got.InputTokens)
			require.Equal(t, tc.cached, got.CacheReadTokens)
			require.Equal(t, new(7), got.CacheWriteTokens)
			require.Equal(t, new(3), got.OutputTokens)
			require.Equal(t, new(0.01), got.Cost)
			for _, payload := range payloads[1:] {
				require.Nil(t, payload.Data.Usage)
			}
		})
	}
}
