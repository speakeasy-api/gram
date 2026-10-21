package mcp_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// A server behind a shared authorization server resolves to the issuer-wide
// grant: no resource, and an exchange at the resolved endpoint with the
// resolved audience mints a session.
func TestWorkloadTokenTargetFor_SharedAuthorizationServer(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	issuerURL := f.sharedIssuer(t)

	target, ok, err := f.ti.service.WorkloadTokenTargetFor(t.Context(), f.resource)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, issuerURL+"/token", target.TokenEndpoint)
	require.Equal(t, target.TokenEndpoint, target.Audience)
	require.Empty(t, target.Resource)

	w := sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), issuerWorkloadForm(f.assertion(t, target.Audience)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// A server on its own authorization server resolves to that server's token
// endpoint and names itself as the resource, and the exchange succeeds.
func TestWorkloadTokenTargetFor_PerEndpointAuthorizationServer(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)

	target, ok, err := f.ti.service.WorkloadTokenTargetFor(t.Context(), f.resource)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, f.resource+"/token", target.TokenEndpoint)
	require.Equal(t, target.TokenEndpoint, target.Audience)
	require.Equal(t, f.resource, target.Resource)

	w := f.exchange(t, f.assertion(t, target.Audience), target.Resource)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// A poll that arrived on a custom domain still resolves a platform-hosted
// server: the poll's domain must not scope the server lookup.
func TestWorkloadTokenTargetFor_PollOnCustomDomainResolvesPlatformServer(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	ctx := customdomains.WithContext(t.Context(), &customdomains.Context{
		OrganizationID: "org-of-the-poll",
		Domain:         "mcp.customer.example",
		DomainID:       uuid.New(),
	})

	target, ok, err := f.ti.service.WorkloadTokenTargetFor(ctx, f.resource)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, f.resource+"/token", target.TokenEndpoint)
	require.Equal(t, f.resource, target.Resource)
}

// A server that is not issuer-gated has no authorization server, so no
// workload grant reaches it.
func TestWorkloadTokenTargetFor_NoWorkloadGrant(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	slug := "no-workload-" + uuid.NewString()[:8]
	_, err := toolsets_repo.New(f.ti.conn).CreateToolset(f.fixtureContext(t), toolsets_repo.CreateToolsetParams{
		OrganizationID:         f.fx.orgID,
		ProjectID:              f.fx.target.ProjectID,
		Name:                   "Ungated MCP " + slug,
		Slug:                   slug,
		Description:            conv.ToPGText("an MCP server with no user session issuer"),
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                conv.ToPGText(slug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	base := strings.TrimSuffix(f.ti.serverURL.String(), "/")
	for _, mcpURL := range []string{
		base + "/mcp/" + slug,
		base + "/mcp/does-not-exist-" + uuid.NewString()[:8],
		base + "/x/mcp/" + slug,
		"https://unregistered.example.com/mcp/" + slug,
	} {
		target, ok, err := f.ti.service.WorkloadTokenTargetFor(t.Context(), mcpURL)
		require.NoError(t, err, mcpURL)
		require.False(t, ok, mcpURL)
		require.Empty(t, target.TokenEndpoint, mcpURL)
	}
}
