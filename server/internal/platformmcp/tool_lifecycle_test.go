package platformmcp

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestSetupHandoffToolReturnsExactIdentityTargetWithoutCredentials(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		providerKey string
		catalogRef  string
	}{
		{"catalog", "browser-catalog-registry-7e966bfa-4df0-43ef-a54c-9c8c2e5f1b0d", "reviewed/slack"},
		{"direct remote", directRemoteProviderKey, "https://mcp.slack.com/mcp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			registrationID := uuid.NewString()
			candidate := CatalogCandidate{ProviderKey: tc.providerKey, CatalogRef: tc.catalogRef, SetupIntent: "dashboard_source_settings"}
			store := &recordingRegistrationStore{
				project:   ResolvedProject{ID: uuid.New(), Slug: "selected-project"},
				candidate: candidate,
				dashboard: RegistrationDashboardSetup{OrganizationSlug: "selected-organization", MCPServerRoute: "selected slack"},
			}
			service := newRegistrationService(testCatalog{details: CatalogDetails{CatalogCandidate: candidate, Transport: "streamable-http"}}, &testRegistrationGate{enabled: true}, store).
				WithDashboardURL(&url.URL{Scheme: "https", Host: "dashboard.example.test"})
			server := mcp.NewServer(&mcp.Implementation{Name: "handoff-test", Version: "1"}, nil)
			bindExternalTestPrincipal(server)
			reg := newRegistrar(server)
			reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
			registerSetupHandoffTool(reg, service)
			descriptor := reg.Descriptors()[0]
			require.Equal(t, bothAudiences, descriptor.Meta.Audiences)
			require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope)
			require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization)
			clientTransport, serverTransport := mcp.NewInMemoryTransports()
			serverSession, err := server.Connect(t.Context(), serverTransport, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = serverSession.Close() })
			session, err := mcp.NewClient(&mcp.Implementation{Name: "handoff-client", Version: "1"}, nil).Connect(t.Context(), clientTransport, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = session.Close() })
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
				Name:      "get_setup_handoff",
				Arguments: GetSetupHandoffToolInput{ProjectSlug: "selected-project", RegistrationID: registrationID, ProviderKey: tc.providerKey, CatalogRef: tc.catalogRef},
			})
			require.NoError(t, err)
			require.False(t, result.IsError)
			require.Len(t, result.Content, 1)
			text, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok)
			var output GetSetupHandoffToolOutput
			require.NoError(t, json.Unmarshal([]byte(text.Text), &output))
			require.Equal(t, registrationID, output.RegistrationID)
			require.Equal(t, tc.providerKey, output.ProviderKey)
			require.Equal(t, tc.catalogRef, output.CatalogRef)
			require.Equal(t, "https://dashboard.example.test/selected-organization/projects/selected-project/mcp/x/selected%20slack/settings#authentication", output.SetupURL)
			require.Equal(t, "dashboard_source_settings", output.Intent)
			require.Empty(t, output.Handoff)
			require.Empty(t, output.ExpiresAt)
			require.Zero(t, store.handoffCalls)
			parsed, err := url.Parse(output.SetupURL)
			require.NoError(t, err)
			require.Empty(t, parsed.RawQuery)
			require.Nil(t, parsed.User)
			for _, forbidden := range []string{"client_secret", "access_token", "refresh_token", "authorization_code", "Bearer"} {
				require.NotContains(t, text.Text, forbidden)
				require.NotContains(t, string(descriptor.InputSchema), forbidden)
			}
		})
	}
}
