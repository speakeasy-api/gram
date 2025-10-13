package conv_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/stretchr/testify/require"
)

func TestSecret_String(t *testing.T) {
	s := conv.NewSecret([]byte("my-secret-value"))

	require.Equal(t, "****", s.String())
	require.Equal(t, "****", fmt.Sprintf("%s", s))
	require.Equal(t, "****", fmt.Sprintf("%v", s))
	require.Equal(t, "****", fmt.Sprintf("%+v", s))
}

func TestSecret_Reveal(t *testing.T) {
	s := conv.NewSecret([]byte("my-secret-value"))

	// Reveal should return the actual secret value
	require.Equal(t, []byte("my-secret-value"), s.Reveal())
}

func TestSecret_MarshalText(t *testing.T) {
	s := conv.NewSecret([]byte("my-secret-value"))

	// MarshalText should return "****" to hide the secret
	text, err := s.MarshalText()
	require.NoError(t, err)
	require.Equal(t, []byte("****"), text)
}

func TestSecret_UnmarshalText(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "non-empty value",
			input:    []byte("my-secret-value"),
			expected: []byte("my-secret-value"),
		},
		{
			name:     "empty value",
			input:    []byte(""),
			expected: []byte(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := conv.Secret{}
			err := s.UnmarshalText(tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.expected, s.Reveal())
		})
	}
}

func TestSecret_MarshalJSON(t *testing.T) {
	s := conv.NewSecret([]byte("my-secret-value"))

	// MarshalJSON should return "****" to hide the secret
	data, err := s.MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, `"****"`, string(data))
}

func TestSecret_MarshalJSON_InStruct(t *testing.T) {
	type TestStruct struct {
		APIKey conv.Secret `json:"api_key"`
		Name   string      `json:"name"`
	}

	ts := TestStruct{
		Name:   "test-service",
		APIKey: conv.NewSecret([]byte("secret-api-key")),
	}

	// When marshaled as part of a struct, the secret should be hidden
	data, err := json.Marshal(ts)
	require.NoError(t, err)
	require.Equal(t, `{"api_key":"****","name":"test-service"}`, string(data))
}

func TestSecret_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []byte
		wantErr  bool
	}{
		{
			name:     "non-empty value",
			input:    fmt.Sprintf(`"%s"`, base64.StdEncoding.EncodeToString([]byte("my-secret-value"))),
			expected: []byte("my-secret-value"),
			wantErr:  false,
		},
		{
			name:     "empty value",
			input:    `""`,
			expected: []byte{},
			wantErr:  false,
		},
		{
			name:     "invalid json",
			input:    `{invalid}`,
			expected: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := conv.Secret{}
			err := s.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, s.Reveal())
			}
		})
	}
}

func TestSecret_UnmarshalJSON_InStruct(t *testing.T) {
	val := []byte("secret-api-key")
	valB64 := base64.StdEncoding.EncodeToString(val)

	type TestStruct struct {
		APIKey conv.Secret `json:"api_key"`
		Name   string      `json:"name"`
	}

	input := fmt.Sprintf(`{"api_key":"%s","name":"test-service"}`, valB64)

	var ts TestStruct
	err := json.Unmarshal([]byte(input), &ts)
	require.NoError(t, err)
	require.Equal(t, []byte("secret-api-key"), ts.APIKey.Reveal())
	require.Equal(t, "test-service", ts.Name)
}

func TestSecret_Scan(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected []byte
		wantErr  bool
	}{
		{
			name:     "string value",
			value:    "my-secret-value",
			expected: []byte("my-secret-value"),
			wantErr:  false,
		},
		{
			name:     "byte slice value",
			value:    []byte("my-secret-value"),
			expected: []byte("my-secret-value"),
			wantErr:  false,
		},
		{
			name:     "nil value",
			value:    nil,
			expected: nil,
			wantErr:  false,
		},
		{
			name:     "unsupported type",
			value:    123,
			expected: []byte{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := conv.Secret{}
			err := s.Scan(tt.value)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, s.Reveal())
			}
		})
	}
}

func TestSecret_Value(t *testing.T) {
	s := conv.NewSecret([]byte("my-secret-value"))

	// Value should return the actual secret for database storage
	val, err := s.Value()
	require.NoError(t, err)
	require.Equal(t, []byte("my-secret-value"), val)
}

func TestSecret_Value_DatabaseRoundtrip(t *testing.T) {
	// Simulate database write
	s1 := conv.NewSecret([]byte("my-secret-value"))

	val, err := s1.Value()
	require.NoError(t, err)

	// Simulate database read
	s2 := conv.Secret{}
	err = s2.Scan(val)
	require.NoError(t, err)

	// The secret should be preserved through the roundtrip
	require.Equal(t, s1.Reveal(), s2.Reveal())
}

func TestSecret_LogValue(t *testing.T) {
	s := conv.NewSecret([]byte("my-secret-value"))

	// LogValue should return a masked value
	logVal := s.LogValue()
	require.Equal(t, slog.StringValue("****"), logVal)
}

func TestSecret_LogValue_InLogger(t *testing.T) {
	s := conv.NewSecret([]byte("my-secret-value"))

	// Create a logger that writes to a buffer
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	// Log the secret
	logger.Info("test message", "api_key", s)

	// The secret should be masked in the log output
	output := buf.String()
	require.Contains(t, output, "****")
	require.NotContains(t, output, "my-secret-value")
}
