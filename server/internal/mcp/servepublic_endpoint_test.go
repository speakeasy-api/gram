// servepublic_endpoint_test.go verifies that /mcp/{mcpSlug} resolves
// through mcp_endpoints → mcp_servers first and falls back to the
// legacy toolsets.mcp_slug lookup on miss.
package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// seedMcpServerForToolset writes the mcp_servers row pointing at a
// toolset and the mcp_endpoints row exposing it under slug. Mirrors
// the production wiring: visibility is set on the mcp_server, the
// endpoint is unscoped (custom_domain_id=NULL).
func seedMcpServerForToolset(
	t *testing.T,
	ctx context.Context,
	conn *pgxpool.Pool,
	projectID uuid.UUID,
	toolsetID uuid.UUID,
	slug string,
	visibility string,
) mcpserversrepo.McpServer {
	t.Helper()
	id, err := uuid.NewV7()
	require.NoError(t, err)
	mcpServer, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                id,
		ProjectID:         projectID,
		Name:              conv.ToPGText("test mcp server"),
		Slug:              conv.ToPGText("test-mcp-server-" + id.String()[len(id.String())-4:]),
		EnvironmentID:     uuid.NullUUID{},
		RemoteMcpServerID: uuid.NullUUID{},
		ToolsetID:         uuid.NullUUID{UUID: toolsetID, Valid: true},
		Visibility:        visibility,
	})
	require.NoError(t, err)

	_, err = mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      projectID,
		CustomDomainID: uuid.NullUUID{},
		McpServerID:    mcpServer.ID,
		Slug:           slug,
	})
	require.NoError(t, err)
	return mcpServer
}

// seedMcpServerForRemote writes a remote_mcp_servers row pointing at
// upstreamURL, an mcp_servers row pointing at it, and an mcp_endpoints
// row exposing it under slug.
func seedMcpServerForRemote(
	t *testing.T,
	ctx context.Context,
	conn *pgxpool.Pool,
	projectID uuid.UUID,
	upstreamURL, slug, visibility string,
) (mcpserversrepo.McpServer, remotemcprepo.RemoteMcpServer) {
	t.Helper()

	remoteServer := remotemcptest.SeedServer(t, ctx, conn, remotemcprepo.CreateServerParams{
		ProjectID:     projectID,
		TransportType: "streamable-http",
		Url:           upstreamURL,
	})

	id, err := uuid.NewV7()
	require.NoError(t, err)
	mcpServer, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                id,
		ProjectID:         projectID,
		Name:              conv.ToPGText("test mcp server"),
		Slug:              conv.ToPGText("test-mcp-server-" + id.String()[len(id.String())-4:]),
		EnvironmentID:     uuid.NullUUID{},
		RemoteMcpServerID: uuid.NullUUID{UUID: remoteServer.ID, Valid: true},
		ToolsetID:         uuid.NullUUID{},
		Visibility:        visibility,
	})
	require.NoError(t, err)

	_, err = mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      projectID,
		CustomDomainID: uuid.NullUUID{},
		McpServerID:    mcpServer.ID,
		Slug:           slug,
	})
	require.NoError(t, err)
	return mcpServer, remoteServer
}

// TestServePublic_McpEndpoint_ToolsetBacked_ResolvesViaEndpoints
// confirms /mcp/{slug} resolves via the new mcp_endpoints lookup when
// the endpoint is backed by a toolset, dispatching through
// ServeToolsetResolved just like the legacy mcp_slug path.
func TestServePublic_McpEndpoint_ToolsetBacked_ResolvesViaEndpoints(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsetsrepo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Toolset's mcp_slug is intentionally different from the endpoint
	// slug — confirms resolution flows through mcp_endpoints, not the
	// legacy toolset.mcp_slug query.
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "toolset-slug-"+uuid.NewString()[:8])

	endpointSlug := "endpoint-" + uuid.NewString()
	seedMcpServerForToolset(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, "public")

	w, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.NotEmpty(t, w.Header().Get("Mcp-Session-Id"))
}

// TestServePublic_NoMcpEndpoint_FallsBackToLegacyToolset confirms that
// when no mcp_endpoint matches the slug, /mcp/{slug} falls back to the
// legacy toolsets.mcp_slug lookup so existing customers without
// mcp_endpoints rows keep working unchanged.
func TestServePublic_NoMcpEndpoint_FallsBackToLegacyToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsetsrepo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// No mcp_endpoints row is created — the slug is only addressable
	// via the legacy toolsets.mcp_slug path.
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "legacy-only-"+uuid.NewString()[:8])

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
}

// TestServePublic_McpEndpoint_RemoteBacked_Proxies confirms /mcp/{slug}
// dispatches to the remote MCP proxy when the resolved mcp_server is
// backed by a remote_mcp_server. The proxy is exercised end-to-end:
// the test's httptest server captures the relayed initialize body.
func TestServePublic_McpEndpoint_RemoteBacked_Proxies(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	done := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"upstream","version":"1.0"}}}`))
		done <- struct{}{}
	}))
	t.Cleanup(upstream.Close)

	endpointSlug := "endpoint-" + uuid.NewString()
	seedMcpServerForRemote(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, endpointSlug, "public")

	w, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), "", nil)
	<-done
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Contains(t, w.Body.String(), "upstream", "upstream initialize response must be relayed back")
}

// TestServePublic_PlatformDomain_DoesNotResolveCustomDomainEndpoint
// regression-guards the platform-only resolution semantic: a
// custom-domain-scoped mcp_endpoint must not resolve for a request
// arriving on the platform domain, even when the slug matches and no
// legacy toolset claims the slug. Asymmetric to the toolsets fallback
// path, which DOES allow platform requests to resolve custom-domain
// toolsets (see loadToolset docstring).
func TestServePublic_PlatformDomain_DoesNotResolveCustomDomainEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsetsrepo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	customDomain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "custom.example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	_, err = customdomainsrepo.New(ti.conn).UpdateCustomDomain(ctx, customdomainsrepo.UpdateCustomDomainParams{
		ID:             customDomain.ID,
		Verified:       true,
		Activated:      true,
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "custom-only-"+uuid.NewString()[:8])

	endpointSlug := "endpoint-" + uuid.NewString()
	mcpServerID, err := uuid.NewV7()
	require.NoError(t, err)
	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                mcpServerID,
		ProjectID:         *authCtx.ProjectID,
		Name:              conv.ToPGText("custom-only mcp"),
		Slug:              conv.ToPGText("custom-only-" + mcpServerID.String()[len(mcpServerID.String())-4:]),
		EnvironmentID:     uuid.NullUUID{},
		RemoteMcpServerID: uuid.NullUUID{},
		ToolsetID:         uuid.NullUUID{UUID: toolset.ID, Valid: true},
		Visibility:        "public",
	})
	require.NoError(t, err)
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: uuid.NullUUID{UUID: customDomain.ID, Valid: true},
		McpServerID:    mcpServerID,
		Slug:           endpointSlug,
	})
	require.NoError(t, err)

	// Request arrives on the platform domain (no customdomains.Context).
	// The custom-domain mcp_endpoint must not resolve, and the legacy
	// toolset lookup must not find the slug either (toolset.mcp_slug
	// differs from endpointSlug). Expect a not-found error from
	// ServePublic. (Status assertions go through oops.ErrHandle in
	// production; the test calls ServePublic directly so we assert on
	// the returned error instead.)
	_, err = servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), "", nil)
	require.Error(t, err, "platform-domain request must not resolve a custom-domain mcp_endpoint")
	require.Contains(t, err.Error(), "not found")

	// Same request, this time with the custom-domain context bound,
	// resolves and succeeds — confirms the test isn't passing by accident.
	ctxWithDomain := customdomains.WithContext(ctx, &customdomains.Context{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         customDomain.Domain,
		DomainID:       customDomain.ID,
	})
	w2, err2 := servePublicHTTP(t, ctxWithDomain, ti, endpointSlug, makeInitializeBody(), "", nil)
	require.NoError(t, err2)
	require.Equal(t, http.StatusOK, w2.Code, "custom-domain-scoped request must resolve the endpoint; body=%s", w2.Body.String())
}
