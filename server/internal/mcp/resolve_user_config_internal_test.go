package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type stubEnvLoader struct {
	vars map[string]string
}

func (s stubEnvLoader) Load(context.Context, uuid.UUID, toolconfig.SlugOrID) (map[string]string, error) {
	return s.vars, nil
}

func (s stubEnvLoader) LoadSystemEnv(context.Context, uuid.UUID, uuid.UUID, string, string) (*toolconfig.CaseInsensitiveEnv, error) {
	return toolconfig.NewCaseInsensitiveEnv(), nil
}

func TestResolveUserConfiguration_SeparatesSavedAndUserLayers(t *testing.T) {
	t.Parallel()

	plan := &gateway.ToolCallPlan{
		Kind: gateway.ToolKindHTTP,
		HTTP: &gateway.HTTPToolCallPlan{
			Security: []*gateway.HTTPToolSecurity{{
				Key:          "oauth",
				Type:         gateway.NullString{Value: "oauth2", Valid: true},
				OAuthTypes:   []string{"authorization_code"},
				EnvVariables: []string{"ACME_OAUTH_ACCESS_TOKEN"},
			}},
		},
	}

	payload := &mcpInputs{
		projectID:     uuid.New(),
		environment:   "default",
		authenticated: true,
		mcpEnvVariables: map[string]string{
			"acme_server_url":         "https://header.example",
			"acme_oauth_access_token": "header-token",
		},
		oauthTokenInputs: []oauthTokenInputs{{
			Token:  "resolved-oauth-token",
			shared: true,
		}},
	}

	savedConfig, userConfig, sources, err := resolveUserConfiguration(
		t.Context(),
		testenv.NewLogger(t),
		stubEnvLoader{vars: map[string]string{
			"ACME_API_KEY":    "saved-api-key",
			"ACME_SERVER_URL": "https://saved.example",
		}},
		payload,
		plan,
	)
	require.NoError(t, err)

	// Saved environment values stay in SavedConfig.
	require.Equal(t, "saved-api-key", savedConfig.Get("ACME_API_KEY"))
	require.Equal(t, "https://saved.example", savedConfig.Get("ACME_SERVER_URL"))
	require.Empty(t, userConfig.Get("ACME_API_KEY"))

	// Request headers stay in UserConfig.
	require.Equal(t, "https://header.example", userConfig.Get("ACME_SERVER_URL"))

	// Resolved OAuth tokens stay in SavedConfig and clear any header override.
	require.Equal(t, "resolved-oauth-token", savedConfig.Get("ACME_OAUTH_ACCESS_TOKEN"))
	require.Empty(t, userConfig.Get("ACME_OAUTH_ACCESS_TOKEN"))

	require.Equal(t, credentialSourceSaved, sources["acme_api_key"])
	require.Equal(t, credentialSourceCaller, sources["acme_server_url"])
	require.Equal(t, credentialSourceShared, sources["acme_oauth_access_token"])
}
