package plugins

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateClaudePluginUserConfigSchema(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		server      PluginServerInfo
		apiKey      string
		wantSetting string
		wantTitle   string
	}{
		{
			name: "environment variable without a display name",
			server: PluginServerInfo{IsPublic: true, EnvConfigs: []ServerEnvConfig{
				{VariableName: "AUTH_TOKEN"},
			}},
			wantSetting: "AUTH_TOKEN",
			wantTitle:   "AUTH_TOKEN",
		},
		{
			name: "environment variable with a display name",
			server: PluginServerInfo{IsPublic: true, EnvConfigs: []ServerEnvConfig{
				{VariableName: "AUTH_TOKEN", DisplayName: "Authentication token"},
			}},
			wantSetting: "AUTH_TOKEN",
			wantTitle:   "Authentication token",
		},
		{
			name: "environment variable with a blank display name",
			server: PluginServerInfo{IsPublic: true, EnvConfigs: []ServerEnvConfig{
				{VariableName: "AUTH_TOKEN", DisplayName: " \t "},
			}},
			wantSetting: "AUTH_TOKEN",
			wantTitle:   "AUTH_TOKEN",
		},
		{
			name:        "private server API key prompt",
			server:      PluginServerInfo{},
			wantSetting: "SPEAKEASY_AI_API_KEY",
			wantTitle:   "Speakeasy API key",
		},
		{
			name:   "private server with an embedded API key",
			server: PluginServerInfo{},
			apiKey: "EXAMPLE_TEST_KEY",
		},
		{
			name:   "OAuth server",
			server: PluginServerInfo{IsOAuth: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := tc.server
			server.DisplayName = "example-server"
			server.MCPURL = "https://example.com/mcp"
			files, err := GeneratePluginPackages([]PluginInfo{{
				Name: "Example", Slug: "example", Servers: []PluginServerInfo{server},
			}}, GenerateConfig{OrgName: "Example", ServerURL: "https://example.com", APIKey: tc.apiKey})
			require.NoError(t, err)

			// Decode the wire format independently of the generator's metadata types.
			var manifest map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(files["example/.claude-plugin/plugin.json"], &manifest))
			if tc.wantSetting == "" {
				require.NotContains(t, manifest, "userConfig")
				return
			}
			var settings map[string]map[string]any
			require.NoError(t, json.Unmarshal(manifest["userConfig"], &settings))
			require.Len(t, settings, 1)
			setting := settings[tc.wantSetting]
			require.Equal(t, "string", setting["type"])
			require.Equal(t, tc.wantTitle, setting["title"])
			require.Equal(t, true, setting["sensitive"])
			require.Contains(t, setting, "description")
		})
	}
}
