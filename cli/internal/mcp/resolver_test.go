package mcp

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"testing"

	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/stretchr/testify/require"
)

func TestLegacyServerName(t *testing.T) {
	t.Parallel()

	legacy, ok := LegacyServerName(DefaultServerName)
	require.True(t, ok)
	require.Equal(t, "gram-mcp", legacy)

	legacy, ok = LegacyServerName(DefaultServerName + "-server")
	require.True(t, ok)
	require.Equal(t, "gram-mcp-server", legacy)

	_, ok = LegacyServerName("my-toolset")
	require.False(t, ok)

	// A URL-derived name that only starts with the default never maps, so
	// installing it can't remove an unrelated gram-mcp-* entry.
	for _, name := range []string{DefaultServerName + "-foo", DefaultServerName + "-server-2"} {
		_, ok = LegacyServerName(name)
		require.False(t, ok, name)
	}
}

type stubEndpointLister struct {
	endpoints []*types.McpEndpoint
	err       error
	serverID  string
}

func (s *stubEndpointLister) ListMcpEndpoints(_ context.Context, _ secret.Secret, _ string, mcpServerID string) ([]*types.McpEndpoint, error) {
	s.serverID = mcpServerID
	return s.endpoints, s.err
}

func listing(endpoints ...*types.McpEndpoint) *stubEndpointLister {
	return &stubEndpointLister{endpoints: endpoints, err: nil, serverID: ""}
}

func endpoint(slug string, customDomainID *string) *types.McpEndpoint {
	return &types.McpEndpoint{
		ID:              "00000000-0000-0000-0000-0000000000e1",
		ProjectID:       "00000000-0000-0000-0000-0000000000b1",
		CustomDomainID:  customDomainID,
		McpServerID:     new("00000000-0000-0000-0000-0000000000a1"),
		MetaMcpServerID: nil,
		Slug:            types.McpEndpointSlug(slug),
		IsDomainRoot:    false,
		CreatedAt:       "",
		UpdatedAt:       "",
	}
}

func testToolset(mcpSlug, envSlug *types.Slug) *types.Toolset {
	return &types.Toolset{
		ID:                           "00000000-0000-0000-0000-0000000000a1",
		ProjectID:                    "00000000-0000-0000-0000-0000000000b1",
		OrganizationID:               "org-1",
		AccountType:                  "",
		Name:                         "Acme",
		Slug:                         "acme",
		Description:                  nil,
		DefaultEnvironmentSlug:       envSlug,
		SecurityVariables:            nil,
		ServerVariables:              nil,
		FunctionEnvironmentVariables: nil,
		ExternalMcpHeaderDefinitions: nil,
		OauthEnablementMetadata:      nil,
		Tools:                        nil,
		ToolUrns:                     nil,
		ToolsetVersion:               0,
		Resources:                    nil,
		ResourceUrns:                 nil,
		PromptTemplates:              nil,
		McpSlug:                      mcpSlug,
		McpIsPublic:                  nil,
		McpEnabled:                   nil,
		NetworkAccessMode:            nil,
		ToolSelectionMode:            "",
		CustomDomainID:               nil,
		Origin:                       nil,
		ExternalOauthServer:          nil,
		UserSessionIssuerID:          nil,
		UserSessionIssuerSlug:        nil,
		ToolVariationsGroupID:        nil,
		CreatedAt:                    "",
		UpdatedAt:                    "",
	}
}

func TestResolveHostedMCPURL(t *testing.T) {
	t.Parallel()

	apiURL, err := url.Parse("https://app.example.com")
	require.NoError(t, err)
	opts := &ResolverOptions{
		ProjectSlug:     "default",
		ToolsetSlug:     "acme",
		MCPURL:          "",
		ServerName:      "",
		APIKey:          secret.Secret(""),
		HeaderName:      "",
		EnvVar:          "",
		APIURL:          apiURL,
		Logger:          slog.New(slog.DiscardHandler),
		IsHeaderNameSet: false,
		IsEnvVarSet:     false,
	}

	mcpSlug := types.Slug("acme-tools")
	envSlug := types.Slug("default")
	customDomainID := "00000000-0000-0000-0000-0000000000d1"
	withSlug := testToolset(&mcpSlug, nil)
	withoutSlug := testToolset(nil, nil)

	tests := []struct {
		name    string
		lister  *stubEndpointLister
		toolset *types.Toolset
		want    string
		wantErr bool
	}{
		{
			name:    "platform endpoint matches the toolset URL",
			lister:  listing(endpoint("acme-tools", nil)),
			toolset: withSlug,
			want:    "https://app.example.com/mcp/acme-tools",
			wantErr: false,
		},
		{
			name:    "toolset slug wins over a newer platform endpoint",
			lister:  listing(endpoint("newer-tools", nil), endpoint("acme-tools", nil)),
			toolset: withSlug,
			want:    "https://app.example.com/mcp/acme-tools",
			wantErr: false,
		},
		{
			name:    "drifted endpoint keeps the toolset URL",
			lister:  listing(endpoint("drifted-tools", nil)),
			toolset: withSlug,
			want:    "https://app.example.com/mcp/acme-tools",
			wantErr: false,
		},
		{
			name:    "without toolset MCP fields the platform endpoint is used",
			lister:  listing(endpoint("custom-tools", &customDomainID), endpoint("platform-tools", nil)),
			toolset: withoutSlug,
			want:    "https://app.example.com/mcp/platform-tools",
			wantErr: false,
		},
		{
			name:    "without toolset MCP fields several platform endpoints are ambiguous",
			lister:  listing(endpoint("newer-tools", nil), endpoint("platform-tools", nil)),
			toolset: withoutSlug,
			want:    "",
			wantErr: true,
		},
		{
			name:    "custom domain only falls back to toolset slug",
			lister:  listing(endpoint("custom-tools", &customDomainID)),
			toolset: withSlug,
			want:    "https://app.example.com/mcp/acme-tools",
			wantErr: false,
		},
		{
			name:    "list error falls back to toolset slug",
			lister:  &stubEndpointLister{endpoints: nil, err: errors.New("forbidden"), serverID: ""},
			toolset: withSlug,
			want:    "https://app.example.com/mcp/acme-tools",
			wantErr: false,
		},
		{
			name:    "no endpoint or slug is an error even with an environment",
			lister:  listing(),
			toolset: testToolset(nil, &envSlug),
			want:    "",
			wantErr: true,
		},
		{
			name:    "no endpoint, slug, or environment is an error",
			lister:  listing(),
			toolset: withoutSlug,
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveHostedMCPURL(t.Context(), opts, tt.lister, tt.toolset)
			require.Equal(t, tt.toolset.ID, tt.lister.serverID)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
