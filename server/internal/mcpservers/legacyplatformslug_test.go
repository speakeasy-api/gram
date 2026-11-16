package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestGetMcpServer_LegacyPlatformSlug(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "legacy-" + uuid.NewString()[:8]
	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
		Name: slug, Slug: slug,
	})
	require.NoError(t, err)
	testenv.SetLegacyAlias(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, slug, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: *authCtx.ProjectID, Name: conv.ToPGText(slug), Slug: conv.ToPGText(slug),
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "public",
	})
	require.NoError(t, err)
	id := toolset.ID.String()
	get := func() *string {
		got, err := ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{ID: &id})
		require.NoError(t, err)
		return got.LegacyPlatformSlug
	}
	require.Nil(t, get(), "a platform alias is a platform endpoint's slug, not a legacy one")

	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID, Domain: slug + ".example.com",
		IngressName: pgtype.Text{String: "", Valid: false}, CertSecretName: pgtype.Text{String: "", Valid: false}, IpAllowlist: []string{},
	})
	require.NoError(t, err)
	testenv.SetLegacyAlias(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, slug, uuid.NullUUID{UUID: domain.ID, Valid: true})
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: *authCtx.ProjectID, CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true},
		McpServerID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Slug: slug,
	})
	require.NoError(t, err)
	require.Equal(t, conv.PtrEmpty(slug), get(), "the platform host serves the custom-domain alias")

	nonCanonical, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: *authCtx.ProjectID, Name: conv.ToPGText(slug + "-other"), Slug: conv.ToPGText(slug + "-other"),
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "public",
	})
	require.NoError(t, err)
	otherID := nonCanonical.ID.String()
	got, err := ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{ID: &otherID})
	require.NoError(t, err)
	require.Nil(t, got.LegacyPlatformSlug, "only the canonical server reports the alias")

	// An organization holds one live domain, so the second domain is filed under another organization id.
	otherDomain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: "org-other-" + slug, Domain: slug + "-other.example.com",
		IngressName: pgtype.Text{String: "", Valid: false}, CertSecretName: pgtype.Text{String: "", Valid: false}, IpAllowlist: []string{},
	})
	require.NoError(t, err)
	twin, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
		Name: slug + "-twin", Slug: slug + "-twin",
	})
	require.NoError(t, err)
	testenv.SetLegacyAlias(t, ctx, ti.conn, *authCtx.ProjectID, twin.ID, slug, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	require.Nil(t, get(), "a platform alias of another toolset wins the slug")
	testenv.SetLegacyAlias(t, ctx, ti.conn, *authCtx.ProjectID, twin.ID, slug, uuid.NullUUID{UUID: otherDomain.ID, Valid: true})
	require.Nil(t, get(), "two custom-domain aliases share the slug, so neither is reported")
	_, err = toolsetsrepo.New(ti.conn).DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{Slug: twin.Slug, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Equal(t, conv.PtrEmpty(slug), get())

	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: *authCtx.ProjectID, McpServerID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Slug: slug,
	})
	require.NoError(t, err)
	require.Nil(t, get(), "a platform endpoint with the slug serves it instead")
}
