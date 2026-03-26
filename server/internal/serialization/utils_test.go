package serialization

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValToString(t *testing.T) {
	t.Parallel()
	t.Run("time.Time values", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name     string
			input    time.Time
			expected string
		}{
			{
				name:     "zero time",
				input:    time.Time{},
				expected: "0001-01-01T00:00:00Z",
			},
			{
				name:     "unix epoch",
				input:    time.Unix(0, 0).UTC(),
				expected: "1970-01-01T00:00:00Z",
			},
			{
				name:     "specific time with nanoseconds",
				input:    time.Date(2023, 12, 25, 15, 30, 45, 123456789, time.UTC),
				expected: "2023-12-25T15:30:45.123456789Z",
			},
			{
				name:     "time with timezone",
				input:    time.Date(2023, 6, 15, 10, 0, 0, 0, time.FixedZone("EST", -5*3600)),
				expected: "2023-06-15T10:00:00-05:00",
			},
			{
				name:     "time with microseconds",
				input:    time.Date(2023, 1, 1, 12, 0, 0, 123456000, time.UTC),
				expected: "2023-01-01T12:00:00.123456Z",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				result := valToString(tt.input)
				require.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("json.Number values", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name     string
			input    json.Number
			expected string
		}{
			// integers
			{"zero", json.Number("0"), "0"},
			{"positive int", json.Number("42"), "42"},
			{"negative int", json.Number("-123"), "-123"},
			{"large int (18 digits)", json.Number("123456789123456789"), "123456789123456789"},
			{"another int (was float64 case)", json.Number("36282114"), "36282114"},

			// decimals
			{"positive decimal", json.Number("3.141592653589793"), "3.141592653589793"},
			{"negative decimal", json.Number("-2.718281828459045"), "-2.718281828459045"},
			{"very small positive", json.Number("0.0000000001"), "0.0000000001"},
			{"very small negative", json.Number("-0.0000000001"), "-0.0000000001"},
			{"decimal with trailing zeros", json.Number("1.50000000000000"), "1.50000000000000"},
			{"high precision decimal", json.Number("0.123456789012345"), "0.123456789012345"},

			// scientific notation
			{"scientific notation positive", json.Number("1.23e10"), "1.23e10"},
			{"scientific notation negative", json.Number("-4.56e-7"), "-4.56e-7"},
			{"scientific notation capital E", json.Number("2.5E+3"), "2.5E+3"},

			// edge cases for precision
			{"many decimal places", json.Number("1.123456789012345678901234567890"), "1.123456789012345678901234567890"},
			{"integer with decimal point", json.Number("42.0"), "42.0"},
			{"leading zeros", json.Number("007"), "007"},
			{"leading zeros with decimal", json.Number("00.123"), "00.123"},

			// boundary values
			{"max safe integer", json.Number("9007199254740991"), "9007199254740991"},
			{"beyond safe integer", json.Number("9007199254740992"), "9007199254740992"},
			{"very large number", json.Number("999999999999999999999999999999"), "999999999999999999999999999999"},

			// special decimal cases
			{"decimal starting with zero", json.Number("0.123"), "0.123"},
			{"negative decimal starting with zero", json.Number("-0.456"), "-0.456"},
			{"only decimal point", json.Number("0.0"), "0.0"},
			{"multiple trailing zeros", json.Number("1.000000"), "1.000000"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				result := valToString(tt.input)
				require.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("string values", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name     string
			input    string
			expected string
		}{
			{"empty string", "", ""},
			{"simple string", "hello", "hello"},
			{"string with spaces", "hello world", "hello world"},
			{"string with newlines", "line1\nline2", "line1\nline2"},
			{"string with tabs", "col1\tcol2", "col1\tcol2"},
			{"string with special characters", "hello\nworld\t!@#$%", "hello\nworld\t!@#$%"},
			{"unicode string", "héllo 世界 🌍", "héllo 世界 🌍"},
			{"numeric string", "12345", "12345"},
			{"json-like string", `{"key": "value"}`, `{"key": "value"}`},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				result := valToString(tt.input)
				require.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("boolean values", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name     string
			input    bool
			expected string
		}{
			{"true", true, "true"},
			{"false", false, "false"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				result := valToString(tt.input)
				require.Equal(t, tt.expected, result)
			})
		}
	})
}
