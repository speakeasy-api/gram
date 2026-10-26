package platformmcp

import (
	"context"
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
	require.Equal(t, "continue_dashboard_setup", second.NextAction)
	require.Regexp(t, `^https://dashboard\.example\.test/.+/settings#authentication$`, second.DashboardSetupURL)
}
