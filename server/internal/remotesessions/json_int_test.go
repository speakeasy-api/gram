package remotesessions

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJSONInt(t *testing.T) {
	t.Parallel()
	maxInt := int(^uint(0) >> 1)
	for _, want := range []int{0, 60, 7200, -1, maxInt, -maxInt - 1} {
		number := strconv.Itoa(want)
		for _, raw := range []string{number, strconv.Quote(number)} {
			t.Run(raw, func(t *testing.T) {
				var got jsonInt
				require.NoError(t, json.Unmarshal([]byte(raw), &got))
				require.Equal(t, want, int(got))
				encoded, err := json.Marshal(got)
				require.NoError(t, err)
				require.JSONEq(t, number, string(encoded))
			})
		}
	}
	for _, raw := range []string{`""`, `"never"`, `1.5`, `"1.5"`, `1e3`, `"1e3"`, `true`, `{}`, `[]`, `999999999999999999999999`, `"999999999999999999999999"`} {
		t.Run("invalid_"+raw, func(t *testing.T) {
			got := jsonInt(42)
			require.Error(t, json.Unmarshal([]byte(raw), &got))
			require.Equal(t, jsonInt(42), got)
		})
	}
	got := jsonInt(42)
	require.NoError(t, json.Unmarshal([]byte(`null`), &got))
	require.Equal(t, jsonInt(42), got)
}
