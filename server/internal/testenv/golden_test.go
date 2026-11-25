package testenv

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestGoldenYAMLNumericPrecision(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		fixture []byte
	}{
		{name: "read YAML", fixture: []byte(`
fraction: 0.12345678901234567890123456789
nested:
  - 123456789012345678901234567890
  - -1.234567890123456789e-200
  - "0.12345678901234567890123456789"
`)},
		{name: "generate YAML from JSON", fixture: encodeGoldenYAML(t, []byte(`{"fraction":0.12345678901234567890123456789,"nested":[123456789012345678901234567890,-1.234567890123456789e-200,"0.12345678901234567890123456789"]}`))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var document yaml.Node
			require.NoError(t, yaml.Unmarshal(tc.fixture, &document))

			// These literals cannot round-trip through float64. Quoted numbers must
			// remain strings, while numeric values retain every digit and their exponent.
			require.Equal(t, map[string]any{
				"fraction": json.Number("0.12345678901234567890123456789"),
				"nested": []any{
					json.Number("123456789012345678901234567890"),
					json.Number("-1.234567890123456789e-200"),
					"0.12345678901234567890123456789",
				},
			}, goldenYAMLValue(t, &document))
		})
	}
}
