package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// A request header supplies a variable only where the server's settings and its configured values leave room for one.
func TestFilterRequestVariables(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		function   bool
		providedBy map[string]string
		system     map[string]string
		saved      map[string]string
		// request is what the headers set; presented is a token the request itself carried.
		request   map[string]string
		presented map[string]string
		want      map[string]string
	}{
		{
			name:     "function: header does not replace a system value",
			function: true,
			system:   map[string]string{"API_KEY": "system-key", "BASE_URL": "https://system.example"},
			request:  map[string]string{"base_url": "https://request.example"},
			want:     map[string]string{},
		},
		{
			name:     "function: header does not replace a saved value",
			function: true,
			saved:    map[string]string{"API_KEY": "saved-key", "BASE_URL": "https://saved.example"},
			request:  map[string]string{"base_url": "https://request.example"},
			want:     map[string]string{},
		},
		{
			name:     "function: header fills a variable nothing configures",
			function: true,
			system:   map[string]string{"BASE_URL": "https://system.example"},
			request:  map[string]string{"api_key": "request-key"},
			want:     map[string]string{"api_key": "request-key"},
		},
		{
			name:       "function: header replaces a configured value marked user-provided",
			function:   true,
			providedBy: map[string]string{"api_key": "user"},
			saved:      map[string]string{"API_KEY": "saved-key"},
			request:    map[string]string{"api_key": "request-key"},
			want:       map[string]string{"api_key": "request-key"},
		},
		{
			name:      "function: a presented token is not a header",
			function:  true,
			saved:     map[string]string{"OAUTH_TOKEN": "saved-token"},
			presented: map[string]string{"oauth_token": "presented-token"},
			want:      map[string]string{"oauth_token": "presented-token"},
		},
		{
			name:    "http: header replaces a system credential",
			system:  map[string]string{"API_KEY": "system-key"},
			request: map[string]string{"api_key": "request-key"},
			want:    map[string]string{"api_key": "request-key"},
		},
		{
			name:       "http: header for a system-provided variable is not used",
			providedBy: map[string]string{"api_key": "system"},
			system:     map[string]string{"API_KEY": "system-key"},
			request:    map[string]string{"api_key": "request-key", "region": "eu"},
			want:       map[string]string{"region": "eu"},
		},
		{
			name:       "http: header for a variable provided by no one is not used",
			providedBy: map[string]string{"api_key": "none"},
			request:    map[string]string{"api_key": "request-key"},
			want:       map[string]string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			user := toolconfig.NewCaseInsensitiveEnv()
			sources := map[string]credentialSource{}
			for name, value := range tc.request {
				user.Set(name, value)
				sources[name] = credentialSourceCaller
			}
			for name, value := range tc.presented {
				user.Set(name, value)
				sources[name] = credentialSourcePerson
			}
			env := toolconfig.ToolCallEnv{SystemEnv: toolconfig.CIEnvFrom(tc.system), SavedConfig: toolconfig.CIEnvFrom(tc.saved), UserConfig: user}

			filterRequestVariables(t.Context(), testenv.NewLogger(t), env, variableSettings{providedBy: tc.providedBy, complete: false}, sources, tc.function)

			require.Equal(t, tc.want, env.UserConfig.All())
			// Configured layers are left as they were.
			require.Equal(t, toolconfig.CIEnvFrom(tc.system).All(), env.SystemEnv.All())
			require.Equal(t, toolconfig.CIEnvFrom(tc.saved).All(), env.SavedConfig.All())
		})
	}
}
