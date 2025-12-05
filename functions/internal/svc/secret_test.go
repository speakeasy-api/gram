package svc

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecret_String(t *testing.T) {
	t.Parallel()

	t.Run("string secret", func(t *testing.T) {
		t.Parallel()
		secret := NewSecret("super-secret-password")
		result := secret.String()
		require.Equal(t, "[REDACTED]", result)
		require.NotContains(t, result, "super-secret-password")
	})

	t.Run("empty string secret", func(t *testing.T) {
		t.Parallel()
		secret := NewSecret("")
		result := secret.String()
		require.Equal(t, "[REDACTED]", result)
	})
}

func TestSecret_GoString(t *testing.T) {
	t.Parallel()

	t.Run("string secret", func(t *testing.T) {
		t.Parallel()
		secret := NewSecret("super-secret-password")
		result := secret.GoString()
		require.Equal(t, "Secret{[REDACTED]}", result)
		require.NotContains(t, result, "super-secret-password")
	})

	t.Run("empty string secret", func(t *testing.T) {
		t.Parallel()
		secret := NewSecret("")
		result := secret.GoString()
		require.Equal(t, "Secret{[REDACTED]}", result)
	})
}

func TestSecret_Printf(t *testing.T) {
	t.Parallel()

	secret := NewSecret("my-api-key-12345")

	tests := []struct {
		name   string
		format string
	}{
		{
			name:   "percent v",
			format: "%v",
		},
		{
			name:   "percent s",
			format: "%s",
		},
		{
			name:   "percent plus v",
			format: "%+v",
		},
		{
			name:   "percent sharp v",
			format: "%#v",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := fmt.Sprintf(tt.format, secret)
			require.NotContains(t, result, "my-api-key-12345")
			require.Contains(t, result, "[REDACTED]")
		})
	}
}

func TestSecret_MarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("string secret", func(t *testing.T) {
		t.Parallel()
		secret := NewSecret("super-secret-api-key")
		data, err := json.Marshal(secret)
		require.NoError(t, err)

		require.Equal(t, `"[REDACTED]"`, string(data))
		require.NotContains(t, string(data), "super-secret-api-key")
	})

	t.Run("empty string secret", func(t *testing.T) {
		t.Parallel()
		secret := NewSecret("")
		data, err := json.Marshal(secret)
		require.NoError(t, err)

		require.Equal(t, `"[REDACTED]"`, string(data))
	})
}

func TestSecret_MarshalJSON_InStruct(t *testing.T) {
	t.Parallel()

	type Config struct {
		Username string        `json:"username"`
		Password Secret[string] `json:"password"`
		APIKey   Secret[string] `json:"api_key"`
	}

	config := Config{
		Username: "admin",
		Password: NewSecret("super-secret-password"),
		APIKey:   NewSecret("sk-1234567890"),
	}

	data, err := json.Marshal(config)
	require.NoError(t, err)

	jsonStr := string(data)
	require.Contains(t, jsonStr, `"username":"admin"`)
	require.Contains(t, jsonStr, `"password":"[REDACTED]"`)
	require.Contains(t, jsonStr, `"api_key":"[REDACTED]"`)
	require.NotContains(t, jsonStr, "super-secret-password")
	require.NotContains(t, jsonStr, "sk-1234567890")
}

func TestSecret_MarshalText(t *testing.T) {
	t.Parallel()

	secret := NewSecret("text-secret-value")

	data, err := secret.MarshalText()
	require.NoError(t, err)

	require.Equal(t, []byte("[REDACTED]"), data)
	require.NotContains(t, string(data), "text-secret-value")
}

func TestSecret_Reveal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    any
		expected any
	}{
		{
			name:     "string secret",
			value:    "my-secret-password",
			expected: "my-secret-password",
		},
		{
			name:     "int secret",
			value:    42,
			expected: 42,
		},
		{
			name:     "struct secret",
			value:    struct{ Key string }{Key: "value"},
			expected: struct{ Key string }{Key: "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			secret := NewSecret(tt.value)
			revealed := secret.Reveal()
			require.Equal(t, tt.expected, revealed)
		})
	}
}

func TestSecret_DifferentTypes(t *testing.T) {
	t.Parallel()

	t.Run("int secret", func(t *testing.T) {
		t.Parallel()
		secret := NewSecret(12345)
		require.Equal(t, "[REDACTED]", secret.String())
		require.Equal(t, 12345, secret.Reveal())
	})

	t.Run("byte slice secret", func(t *testing.T) {
		t.Parallel()
		secret := NewSecret([]byte("secret-bytes"))
		require.Equal(t, "[REDACTED]", secret.String())
		require.Equal(t, []byte("secret-bytes"), secret.Reveal())
	})

	t.Run("struct secret", func(t *testing.T) {
		t.Parallel()
		type Credentials struct {
			Username string
			Password string
		}
		creds := Credentials{Username: "admin", Password: "pass123"}
		secret := NewSecret(creds)
		require.Equal(t, "[REDACTED]", secret.String())
		require.Equal(t, creds, secret.Reveal())
	})
}
