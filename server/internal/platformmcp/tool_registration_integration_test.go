package platformmcp

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

func newBrowserCatalogRegistrationHarness(t *testing.T, ctx context.Context, conn *pgxpool.Pool, providerKey string) (Principal, ResolvedProject, *Registrar) {
	t.Helper()

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	dashboardURL, err := url.Parse("https://dashboard.example.test")
	require.NoError(t, err)
	registrations := newRegistrationService(testCatalog{details: CatalogDetails{
		CatalogCandidate: CatalogCandidate{Name: "Reviewed MCP", ProviderKey: providerKey, CatalogRef: "app.example/mcp", SetupIntent: "dashboard_source_settings"},
		Transport:        "streamable-http",
		remoteURL:        "https://reviewed.example.test/mcp",
	}}, &testRegistrationGate{enabled: true}, store).WithDashboardURL(dashboardURL)

	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "platform-mcp-test"}, nil))
	registerCatalogRegistrationTool(reg, registrations, NewOnboardingService(conn))
	return principal, project, reg
}

// A browser-catalogue registration returns its dashboard setup link in the same
// call that commits it. The setup-link step reads the just-committed
// registration back, so it must accept what registration itself wrote.
func TestCatalogRegistrationToolReturnsDashboardSetupURLForBrowserCatalogServer(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_browser_catalog_setup_url")
	require.NoError(t, err)

	providerKey := "browser-catalog-registry-" + uuid.NewString()
	principal, project, reg := newBrowserCatalogRegistrationHarness(t, ctx, conn, providerKey)

	registered, err := invokeTool[RegisterCatalogMCPToolOutput](t, ContextWithPrincipal(ctx, principal), reg, "register_catalog_mcp", map[string]any{
		"project_slug":    project.Slug,
		"provider_key":    providerKey,
		"catalog_ref":     "app.example/mcp",
		"idempotency_key": "browser-catalog-setup-url",
	})
	require.NoError(t, err)
	require.Equal(t, "continue_dashboard_setup", registered.NextAction)
	require.Regexp(t, `^https://dashboard\.example\.test/.+/settings#authentication$`, registered.DashboardSetupURL)
}

// Deleting the MCP server in the dashboard leaves its catalogue registration
// behind. Adding the same catalogue server again must not fail on that stale
// registration.
func TestCatalogRegistrationToolReAddsBrowserCatalogServerAfterDashboardDelete(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_browser_catalog_readd")
	require.NoError(t, err)

	providerKey := "browser-catalog-registry-" + uuid.NewString()
	principal, project, reg := newBrowserCatalogRegistrationHarness(t, ctx, conn, providerKey)
	callCtx := ContextWithPrincipal(ctx, principal)

	first, err := invokeTool[RegisterCatalogMCPToolOutput](t, callCtx, reg, "register_catalog_mcp", map[string]any{
		"project_slug":    project.Slug,
		"provider_key":    providerKey,
		"catalog_ref":     "app.example/mcp",
		"idempotency_key": "browser-catalog-first-add",
	})
	require.NoError(t, err)

	registrationID, err := uuid.Parse(first.RegistrationID)
	require.NoError(t, err)
	registration, err := platformrepo.New(conn).GetPlatformMCPCatalogRegistrationByID(ctx, platformrepo.GetPlatformMCPCatalogRegistrationByIDParams{
		ID:             registrationID,
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	require.True(t, registration.McpServerID.Valid)
	_, err = mcpserversrepo.New(conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{
		ID:        registration.McpServerID.UUID,
		ProjectID: project.ID,
	})
	require.NoError(t, err)

	second, err := invokeTool[RegisterCatalogMCPToolOutput](t, callCtx, reg, "register_catalog_mcp", map[string]any{
		"project_slug":    project.Slug,
		"provider_key":    providerKey,
		"catalog_ref":     "app.example/mcp",
		"idempotency_key": "browser-catalog-second-add",
	})
	require.NoError(t, err)
	require.NotEqual(t, first.RegistrationID, second.RegistrationID, "the stale registration must be replaced, not reused")
	require.Equal(t, "continue_dashboard_setup", second.NextAction)
	require.Regexp(t, `^https://dashboard\.example\.test/.+/settings#authentication$`, second.DashboardSetupURL)
}

// Over MCP, a registration result carries the card in _meta and keeps the same
// text and structured content as a client without MCP Apps support reads.
func TestCatalogRegistrationToolResultCarriesServerCard(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_registration_card")
	require.NoError(t, err)

	providerKey := "browser-catalog-registry-" + uuid.NewString()
	principal, project, reg := newBrowserCatalogRegistrationHarness(t, ctx, conn, providerKey)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	reg.server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			return next(contextWithPrincipal(ctx, principal), method, req)
		}
	})

	result, err := connectTestClient(t, reg.server).CallTool(ctx, &mcp.CallToolParams{Name: "register_catalog_mcp", Arguments: map[string]any{
		"project_slug":    project.Slug,
		"provider_key":    providerKey,
		"catalog_ref":     "app.example/mcp",
		"idempotency_key": "browser-catalog-card",
	}})
	require.NoError(t, err)
	require.False(t, result.IsError)

	structured, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.JSONEq(t, string(structured), text.Text, "the text content is still the structured output")
	var output RegisterCatalogMCPToolOutput
	require.NoError(t, json.Unmarshal(structured, &output))
	require.Equal(t, project.Slug, output.ProjectSlug)
	require.Equal(t, "continue_dashboard_setup", output.NextAction)
	require.NotContains(t, text.Text, "Private until added to a plugin", "the card is not part of the model-visible result")

	card, ok := result.Meta[cardMetaKey].(string)
	require.True(t, ok)
	require.Contains(t, card, "<h1>Reviewed MCP</h1>")
	require.Contains(t, card, "Added to project "+project.Name)
	require.Contains(t, card, "Private until added to a plugin")
	require.Contains(t, card, `href="`+output.DashboardSetupURL+`"`)
}
