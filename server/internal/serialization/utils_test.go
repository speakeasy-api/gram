package serialization

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValToString(t *testing.T) {
	t.Run("time.Time values", func(t *testing.T) {
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
				result := valToString(tt.input)
				require.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("float32 values", func(t *testing.T) {
		tests := []struct {
			name     string
			input    float32
			expected string
		}{
			{
				name:     "zero",
				input:    0,
				expected: "0",
			},
			{
				name:     "positive integer",
				input:    42.0,
				expected: "42",
			},
			{
				name:     "negative integer",
				input:    -123.0,
				expected: "-123",
			},
			{
				name:     "positive decimal",
				input:    3.14159,
				expected: "3.14159",
			},
			{
				name:     "negative decimal",
				input:    -2.71828,
				expected: "-2.71828",
			},
			{
				name:     "very small positive",
				input:    0.000001,
				expected: "0.000001",
			},
			{
				name:     "very small negative",
				input:    -0.000001,
				expected: "-0.000001",
			},
			{
				name:     "decimal with trailing zeros",
				input:    1.5000,
				expected: "1.5",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := valToString(tt.input)
				require.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("float64 values", func(t *testing.T) {
		tests := []struct {
			name     string
			input    float64
			expected string
		}{
			{
				name:     "zero",
				input:    0.0,
				expected: "0",
			},
			{
				name:     "positive integer",
				input:    42.0,
				expected: "42",
			},
			{
				name:     "positive integer large",
				input:    36282114,
				expected: "36282114",
			},
			{
				name:     "negative integer",
				input:    -123.0,
				expected: "-123",
			},
			{
				name:     "positive decimal",
				input:    3.141592653589793,
				expected: "3.141592653589793",
			},
			{
				name:     "negative decimal",
				input:    -2.718281828459045,
				expected: "-2.718281828459045",
			},
			{
				name:     "very small positive",
				input:    0.0000000001,
				expected: "0.0000000001",
			},
			{
				name:     "very small negative",
				input:    -0.0000000001,
				expected: "-0.0000000001",
			},
			{
				name:     "decimal with trailing zeros",
				input:    1.50000000000000,
				expected: "1.5",
			},
			{
				name:     "high precision decimal",
				input:    0.123456789012345,
				expected: "0.123456789012345",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := valToString(tt.input)
				require.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("string values", func(t *testing.T) {
		tests := []struct {
			name     string
			input    string
			expected string
		}{
			{
				name:     "empty string",
				input:    "",
				expected: "",
			},
			{
				name:     "simple string",
				input:    "hello",
				expected: "hello",
			},
			{
				name:     "string with spaces",
				input:    "hello world",
				expected: "hello world",
			},
			{
				name:     "string with newlines",
				input:    "line1\nline2",
				expected: "line1\nline2",
			},
			{
				name:     "string with tabs",
				input:    "col1\tcol2",
				expected: "col1\tcol2",
			},
			{
				name:     "string with special characters",
				input:    "hello\nworld\t!@#$%",
				expected: "hello\nworld\t!@#$%",
			},
			{
				name:     "unicode string",
				input:    "héllo 世界 🌍",
				expected: "héllo 世界 🌍",
			},
			{
				name:     "numeric string",
				input:    "12345",
				expected: "12345",
			},
			{
				name:     "json-like string",
				input:    `{"key": "value"}`,
				expected: `{"key": "value"}`,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := valToString(tt.input)
				require.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("boolean values", func(t *testing.T) {
		tests := []struct {
			name     string
			input    bool
			expected string
		}{
			{
				name:     "true",
				input:    true,
				expected: "true",
			},
			{
				name:     "false",
				input:    false,
				expected: "false",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := valToString(tt.input)
				require.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("integer values", func(t *testing.T) {
		tests := []struct {
			name     string
			input    any
			expected string
		}{
			{
				name:     "int zero",
				input:    int(0),
				expected: "0",
			},
			{
				name:     "positive int",
				input:    int(42),
				expected: "42",
			},
			{
				name:     "negative int",
				input:    int(-123),
				expected: "-123",
			},
			{
				name:     "int8 max",
				input:    int8(127),
				expected: "127",
			},
			{
				name:     "int8 min",
				input:    int8(-128),
				expected: "-128",
			},
			{
				name:     "int16 max",
				input:    int16(32767),
				expected: "32767",
			},
			{
				name:     "int16 min",
				input:    int16(-32768),
				expected: "-32768",
			},
			{
				name:     "int32 max",
				input:    int32(2147483647),
				expected: "2147483647",
			},
			{
				name:     "int32 min",
				input:    int32(-2147483648),
				expected: "-2147483648",
			},
			{
				name:     "int64 max",
				input:    int64(9223372036854775807),
				expected: "9223372036854775807",
			},
			{
				name:     "int64 min",
				input:    int64(-9223372036854775808),
				expected: "-9223372036854775808",
			},
			{
				name:     "uint zero",
				input:    uint(0),
				expected: "0",
			},
			{
				name:     "uint max 32-bit",
				input:    uint(4294967295),
				expected: "4294967295",
			},
			{
				name:     "uint8 max",
				input:    uint8(255),
				expected: "255",
			},
			{
				name:     "uint16 max",
				input:    uint16(65535),
				expected: "65535",
			},
			{
				name:     "uint32 max",
				input:    uint32(4294967295),
				expected: "4294967295",
			},
			{
				name:     "uint64 max",
				input:    uint64(18446744073709551615),
				expected: "18446744073709551615",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := valToString(tt.input)
				require.Equal(t, tt.expected, result)
			})
		}
	})
}
