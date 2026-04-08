package mcp_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomains_repo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// createPublicMCPToolsetWithCustomDomain creates a public MCP toolset, then creates
// a verified+activated custom domain and links it to the toolset.
func createPublicMCPToolsetWithCustomDomain(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	authCtx *contextvalues.AuthContext,
	slug string,
	domainName string,
) (toolsets_repo.Toolset, customdomains_repo.CustomDomain) {
	t.Helper()

	toolsetsRepo := toolsets_repo.New(ti.conn)
	domainsRepo := customdomains_repo.New(ti.conn)

	// Create a public MCP toolset (without custom domain initially)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, slug)

	// Create and activate a custom domain
	domain, err := domainsRepo.CreateCustomDomain(ctx, customdomains_repo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         domainName,
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	domain, err = domainsRepo.UpdateCustomDomain(ctx, customdomains_repo.UpdateCustomDomainParams{
		ID:             domain.ID,
		Verified:       true,
		Activated:      true,
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	// Link the custom domain to the toolset
	toolset, err = toolsetsRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            toolset.McpIsPublic,
		McpEnabled:             toolset.McpEnabled,
		CustomDomainID:         uuid.NullUUID{UUID: domain.ID, Valid: true},
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)

	return toolset, domain
}

func TestServePublic_CustomDomain_PlatformDomainStillWorks(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset, _ := createPublicMCPToolsetWithCustomDomain(
		t, ctx, ti, authCtx,
		"cd-platform-"+uuid.New().String()[:8],
		"custom-platform.example.com",
	)

	// Request via the platform domain (no custom domain context) should still work
	unauthCtx := context.Background()
	w, err := servePublicHTTP(t, unauthCtx, ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestServePublic_CustomDomain_CustomDomainAlsoWorks(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset, domain := createPublicMCPToolsetWithCustomDomain(
		t, ctx, ti, authCtx,
		"cd-custom-"+uuid.New().String()[:8],
		"custom-domain.example.com",
	)

	// Request via the custom domain (with custom domain context) should work
	customCtx := customdomains.WithContext(context.Background(), &customdomains.Context{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         domain.Domain,
		DomainID:       domain.ID,
	})
	w, err := servePublicHTTP(t, customCtx, ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestServePublic_NoCustomDomain_PlatformDomainWorks(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Create a standard toolset without any custom domain
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "no-cd-platform-"+uuid.New().String()[:8])

	// Platform domain should work as before
	unauthCtx := context.Background()
	w, err := servePublicHTTP(t, unauthCtx, ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestServePublic_CustomDomain_WrongDomainReturnsNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	domainsRepo := customdomains_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset, _ := createPublicMCPToolsetWithCustomDomain(
		t, ctx, ti, authCtx,
		"cd-wrong-"+uuid.New().String()[:8],
		"correct-domain.example.com",
	)

	// Create a different verified domain (not linked to this toolset)
	otherDomain, err := domainsRepo.CreateCustomDomain(ctx, customdomains_repo.CreateCustomDomainParams{
		OrganizationID: "other-org",
		Domain:         "wrong-domain.example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	otherDomain, err = domainsRepo.UpdateCustomDomain(ctx, customdomains_repo.UpdateCustomDomainParams{
		ID:             otherDomain.ID,
		Verified:       true,
		Activated:      true,
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	// Request via the wrong custom domain should fail
	wrongCtx := customdomains.WithContext(context.Background(), &customdomains.Context{
		OrganizationID: "other-org",
		Domain:         otherDomain.Domain,
		DomainID:       otherDomain.ID,
	})
	_, err = servePublicHTTP(t, wrongCtx, ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

// serveWellKnownHTTP makes a GET request to the OAuth well-known endpoint for the given MCP slug.
func serveWellKnownHTTP(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	mcpSlug string,
) (*httptest.ResponseRecorder, error) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/"+mcpSlug, nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	if err := ti.service.HandleWellKnownOAuthServerMetadata(w, req); err != nil {
		return w, fmt.Errorf("well-known oauth: %w", err)
	}
	return w, nil
}

func TestWellKnownOAuth_CustomDomain_PlatformDomainStillWorks(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset, _ := createPublicMCPToolsetWithCustomDomain(
		t, ctx, ti, authCtx,
		"cd-wellknown-"+uuid.New().String()[:8],
		"wellknown-domain.example.com",
	)

	// The well-known endpoint should find the toolset via the platform domain.
	// It will return an error because no OAuth is configured, but crucially it
	// should NOT return "not found" — proving the toolset lookup succeeded.
	_, err := serveWellKnownHTTP(t, context.Background(), ti, toolset.McpSlug.String)
	require.Error(t, err)
	require.Contains(t, err.Error(), "OAuth")
	require.NotContains(t, err.Error(), "not found")
}
