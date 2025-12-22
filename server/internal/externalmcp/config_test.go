package externalmcp

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/stretchr/testify/require"
)

func TestBuildHeaders(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		systemEnv   map[string]string
		userConfig  map[string]string
		headerDefs  []HeaderDefinition
		expected    map[string]string
	}{
		{
			name: "all system env flows through with ToHTTPHeader derivation for keys without definitions",
			systemEnv: map[string]string{
				"api_key":       "secret123",
				"api_secret":    "mysecret",
				"service_token": "token456",
			},
			userConfig:  map[string]string{},
			headerDefs:  []HeaderDefinition{},
			expected: map[string]string{
				"Api-Key":       "secret123",
				"Api-Secret":    "mysecret",
				"Service-Token": "token456",
			},
		},
		{
			name: "header definitions rename keys to custom header names",
			systemEnv: map[string]string{
				"api_key":    "secret123",
				"auth_token": "token789",
			},
			userConfig: map[string]string{},
			headerDefs: []HeaderDefinition{
				{Name: "api_key", HeaderName: "X-API-Key"},
				{Name: "auth_token", HeaderName: "Authorization"},
			},
			expected: map[string]string{
				"X-API-Key":     "secret123",
				"Authorization": "token789",
			},
		},
		{
			name: "user config overrides defined keys only",
			systemEnv: map[string]string{
				"api_key":      "system_secret",
				"api_version":  "v1",
				"custom_field": "original",
			},
			userConfig: map[string]string{
				"api_key":      "user_override",
				"api_version":  "v2",
				"custom_field": "ignored",
			},
			headerDefs: []HeaderDefinition{
				{Name: "api_key", HeaderName: "X-API-Key"},
				{Name: "api_version", HeaderName: "X-API-Version"},
			},
			expected: map[string]string{
				"X-API-Key":     "user_override",
				"X-API-Version": "v2",
				"Custom-Field":  "original",
			},
		},
		{
			name: "user config is ignored for keys without definitions",
			systemEnv: map[string]string{
				"api_key": "system_secret",
			},
			userConfig: map[string]string{
				"api_key":        "user_override",
				"undefined_key":  "some_value",
				"another_custom": "data",
			},
			headerDefs: []HeaderDefinition{},
			expected: map[string]string{
				"Api-Key": "system_secret",
			},
		},
		{
			name: "case insensitive lookups work",
			systemEnv: map[string]string{
				"API_KEY":       "value1",
				"Auth_Token":    "value2",
				"SERVICE_TOKEN": "value3",
			},
			userConfig: map[string]string{
				"api_key":    "override1",
				"AUTH_token": "override2",
			},
			headerDefs: []HeaderDefinition{
				{Name: "API_KEY", HeaderName: "X-Key"},
				{Name: "auth_token", HeaderName: "X-Auth"},
			},
			expected: map[string]string{
				"X-Key":         "override1",
				"X-Auth":        "override2",
				"Service-Token": "value3",
			},
		},
		{
			name: "empty values are skipped",
			systemEnv: map[string]string{
				"api_key":     "secret123",
				"empty_field": "",
				"another_key": "value",
			},
			userConfig: map[string]string{
				"api_key":      "override",
				"empty_user":   "",
				"another_key":  "",
			},
			headerDefs: []HeaderDefinition{
				{Name: "api_key", HeaderName: "X-Key"},
				{Name: "another_key", HeaderName: "X-Other"},
			},
			expected: map[string]string{
				"X-Key":   "override",
				"X-Other": "value",
			},
		},
		{
			name:       "empty inputs nil env",
			systemEnv:  map[string]string{},
			userConfig: map[string]string{},
			headerDefs: []HeaderDefinition{},
			expected:   map[string]string{},
		},
		{
			name: "nil header definitions uses ToHTTPHeader for all",
			systemEnv: map[string]string{
				"db_host": "localhost",
				"db_port": "5432",
			},
			userConfig:  map[string]string{},
			headerDefs:  nil,
			expected: map[string]string{
				"Db-Host": "localhost",
				"Db-Port": "5432",
			},
		},
		{
			name: "mixed defined and undefined keys",
			systemEnv: map[string]string{
				"defined_key":   "def_value",
				"undefined_key": "undef_value",
				"another_def":   "another_value",
			},
			userConfig: map[string]string{
				"defined_key": "user_def",
				"undefined_key": "user_undef",
			},
			headerDefs: []HeaderDefinition{
				{Name: "defined_key", HeaderName: "X-Defined"},
				{Name: "another_def", HeaderName: "X-Another"},
			},
			expected: map[string]string{
				"X-Defined":     "user_def",
				"X-Another":     "another_value",
				"Undefined-Key": "undef_value",
			},
		},
		{
			name: "user config completely overrides system env for defined keys",
			systemEnv: map[string]string{
				"auth_token": "old_token",
			},
			userConfig: map[string]string{
				"auth_token": "new_token",
			},
			headerDefs: []HeaderDefinition{
				{Name: "auth_token", HeaderName: "Authorization"},
			},
			expected: map[string]string{
				"Authorization": "new_token",
			},
		},
		{
			name: "only user config override with no system env",
			systemEnv: map[string]string{},
			userConfig: map[string]string{
				"api_key": "user_value",
			},
			headerDefs: []HeaderDefinition{
				{Name: "api_key", HeaderName: "X-API-Key"},
			},
			expected: map[string]string{
				"X-API-Key": "user_value",
			},
		},
		{
			name: "system env with user empty string override",
			systemEnv: map[string]string{
				"api_key": "system_value",
			},
			userConfig: map[string]string{
				"api_key": "",
			},
			headerDefs: []HeaderDefinition{
				{Name: "api_key", HeaderName: "X-API-Key"},
			},
			expected: map[string]string{
				"X-API-Key": "system_value",
			},
		},
		{
			name: "multiple header definitions with one override",
			systemEnv: map[string]string{
				"key1": "value1",
				"key2": "value2",
				"key3": "value3",
			},
			userConfig: map[string]string{
				"key2": "override2",
			},
			headerDefs: []HeaderDefinition{
				{Name: "key1", HeaderName: "X-Key1"},
				{Name: "key2", HeaderName: "X-Key2"},
				{Name: "key3", HeaderName: "X-Key3"},
			},
			expected: map[string]string{
				"X-Key1": "value1",
				"X-Key2": "override2",
				"X-Key3": "value3",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			systemEnv := toolconfig.CIEnvFrom(tt.systemEnv)
			userConfig := toolconfig.CIEnvFrom(tt.userConfig)

			result := BuildHeaders(systemEnv, userConfig, tt.headerDefs, "")

			require.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildHeadersEdgeCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		systemEnv  map[string]string
		userConfig map[string]string
		headerDefs []HeaderDefinition
		expected   map[string]string
	}{
		{
			name: "header definition with same name different case",
			systemEnv: map[string]string{
				"API_KEY": "value1",
			},
			userConfig: map[string]string{},
			headerDefs: []HeaderDefinition{
				{Name: "api_key", HeaderName: "X-API-Key"},
			},
			expected: map[string]string{
				"X-API-Key": "value1",
			},
		},
		{
			name: "multiple definitions with overlapping case",
			systemEnv: map[string]string{
				"db_host":  "localhost",
				"db_port":  "5432",
				"db_user":  "admin",
			},
			userConfig: map[string]string{
				"DB_HOST": "remotehost",
			},
			headerDefs: []HeaderDefinition{
				{Name: "DB_Host", HeaderName: "X-DB-Host"},
				{Name: "db_port", HeaderName: "X-DB-Port"},
				{Name: "DB_USER", HeaderName: "X-DB-User"},
			},
			expected: map[string]string{
				"X-DB-Host": "remotehost",
				"X-DB-Port": "5432",
				"X-DB-User": "admin",
			},
		},
		{
			name: "special characters in env key converted by ToHTTPHeader",
			systemEnv: map[string]string{
				"api_v2_key":     "value1",
				"service_2_name": "value2",
			},
			userConfig: map[string]string{},
			headerDefs: []HeaderDefinition{},
			expected: map[string]string{
				"Api-V2-Key":     "value1",
				"Service-2-Name": "value2",
			},
		},
		{
			name: "user config all empty strings",
			systemEnv: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			userConfig: map[string]string{
				"key1": "",
				"key2": "",
			},
			headerDefs: []HeaderDefinition{
				{Name: "key1", HeaderName: "X-Key1"},
				{Name: "key2", HeaderName: "X-Key2"},
			},
			expected: map[string]string{
				"X-Key1": "value1",
				"X-Key2": "value2",
			},
		},
		{
			name: "system env all empty strings",
			systemEnv: map[string]string{
				"key1": "",
				"key2": "",
			},
			userConfig: map[string]string{
				"key1": "override1",
				"key2": "override2",
			},
			headerDefs: []HeaderDefinition{
				{Name: "key1", HeaderName: "X-Key1"},
				{Name: "key2", HeaderName: "X-Key2"},
			},
			expected: map[string]string{
				"X-Key1": "override1",
				"X-Key2": "override2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			systemEnv := toolconfig.CIEnvFrom(tt.systemEnv)
			userConfig := toolconfig.CIEnvFrom(tt.userConfig)

			result := BuildHeaders(systemEnv, userConfig, tt.headerDefs, "")

			require.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildHeadersWithOAuthToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		systemEnv  map[string]string
		userConfig map[string]string
		headerDefs []HeaderDefinition
		oauthToken string
		expected   map[string]string
	}{
		{
			name:       "oauth token sets Authorization header",
			systemEnv:  map[string]string{},
			userConfig: map[string]string{},
			headerDefs: []HeaderDefinition{},
			oauthToken: "my-token-123",
			expected: map[string]string{
				"Authorization": "Bearer my-token-123",
			},
		},
		{
			name:       "empty oauth token does not set Authorization header",
			systemEnv:  map[string]string{},
			userConfig: map[string]string{},
			headerDefs: []HeaderDefinition{},
			oauthToken: "",
			expected:   map[string]string{},
		},
		{
			name:       "oauth token combines with other headers",
			systemEnv:  map[string]string{"x_api_key": "system-key"},
			userConfig: map[string]string{},
			headerDefs: []HeaderDefinition{},
			oauthToken: "oauth-token",
			expected: map[string]string{
				"X-Api-Key":     "system-key",
				"Authorization": "Bearer oauth-token",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			systemEnv := toolconfig.CIEnvFrom(tt.systemEnv)
			userConfig := toolconfig.CIEnvFrom(tt.userConfig)

			result := BuildHeaders(systemEnv, userConfig, tt.headerDefs, tt.oauthToken)

			require.Equal(t, tt.expected, result)
		})
	}
}
