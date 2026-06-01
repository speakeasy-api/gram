// custom_domain_lockdown_test.go verifies that when an org's custom domain
// carries a non-empty IP allowlist, its MCP servers are reachable ONLY via the
// custom domain. Public-host requests are rejected with 403 so the allowlist
// (enforced at the custom-domain ingress/gateway) cannot be bypassed by
// hitting the platform domain. See Service.enforceCustomDomainLockdown.
package mcp_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// TestServePublic_Lockdown_LegacyToolset_AllowlistBlocksPlatform is the
// primary regression: a custom-domain-bound toolset (legacy mcp_slug path)
// must not be served on the platform domain once its domain has an IP
// allowlist. Without the lockdown, GetToolsetByMcpSlug's fallback would serve
// it on getgram.ai with no IP filtering.
func TestServePublic_Lockdown_LegacyToolset_AllowlistBlocksPlatform(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset, domain := createPublicMCPToolsetWithCustomDomain(
		t, ctx, ti, authCtx,
		"lockdown-legacy-"+uuid.New().String()[:8],
		"lockdown-legacy.example.com",
	)

	// Configure a non-empty allowlist on the org's custom domain.
	_, err := customdomainsrepo.New(ti.conn).UpdateCustomDomainIPAllowlist(ctx, customdomainsrepo.UpdateCustomDomainIPAllowlistParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		IpAllowlist:    []string{"203.0.113.0/24"},
	})
	require.NoError(t, err)

	// Platform-domain request (no custom domain context) must be denied.
	_, err = servePublicHTTP(t, context.Background(), ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.Error(t, err, "platform request must be denied when the org's custom domain has an allowlist")
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	// Same request via the custom domain context resolves normally — the
	// ingress already enforced the allowlist, so the app does not block.
	customCtx := customdomains.WithContext(context.Background(), &customdomains.Context{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         domain.Domain,
		DomainID:       domain.ID,
	})
	w, err := servePublicHTTP(t, customCtx, ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "custom-domain request must serve; body=%s", w.Body.String())
}

// TestServePublic_Lockdown_McpEndpoint_AllowlistBlocksPlatform covers the
// new mcp_endpoints model: even a platform-scoped (custom_domain_id NULL)
// endpoint for an org whose custom domain has an allowlist must be denied on
// the platform domain, closing the duplicate-public-endpoint bypass.
func TestServePublic_Lockdown_McpEndpoint_AllowlistBlocksPlatform(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsetsrepo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	_, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "lockdown-endpoint.example.com",
		IpAllowlist:    []string{"203.0.113.7"},
	})
	require.NoError(t, err)

	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "lockdown-ep-ts-"+uuid.New().String()[:8])
	platformSlug := "platform-ep-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, platformSlug, "public", uuid.NullUUID{})

	// Platform endpoint resolves, but the lockdown denies it.
	_, err = servePublicHTTP(t, ctx, ti, platformSlug, makeInitializeBody(), "", nil)
	require.Error(t, err, "platform mcp_endpoint must be denied when the org has an allowlisted custom domain")
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

// TestServePublic_Lockdown_EmptyAllowlist_PlatformStillWorks guards against
// over-blocking: a custom domain with an empty allowlist must not restrict
// platform access (dual-serve remains the default).
func TestServePublic_Lockdown_EmptyAllowlist_PlatformStillWorks(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// createPublicMCPToolsetWithCustomDomain seeds an empty allowlist.
	toolset, _ := createPublicMCPToolsetWithCustomDomain(
		t, ctx, ti, authCtx,
		"lockdown-empty-"+uuid.New().String()[:8],
		"lockdown-empty.example.com",
	)

	w, err := servePublicHTTP(t, context.Background(), ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, err, "empty allowlist must not lock down the platform domain")
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
}
