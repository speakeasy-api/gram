package customdomains_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/domains"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	cdrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// seedCanonicalHostedServer creates a hosted toolset and its canonical wrapper
// (id = toolset id) with its single endpoint on customDomainID, or the platform when invalid.
func seedCanonicalHostedServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID, slug string, customDomainID uuid.NullUUID) uuid.UUID {
	t.Helper()

	toolset, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         organizationID,
		ProjectID:              projectID,
		Name:                   slug,
		Slug:                   slug,
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: slug, Valid: true},
		McpEnabled:             true,
	})
	require.NoError(t, err)
	_, err = mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:         toolset.ID,
		ProjectID:  projectID,
		Name:       conv.ToPGText(slug),
		Slug:       conv.ToPGText(slug),
		ToolsetID:  uuid.NullUUID{UUID: toolset.ID, Valid: true},
		Visibility: "private",
	})
	require.NoError(t, err)
	_, err = mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      projectID,
		CustomDomainID: customDomainID,
		McpServerID:    uuid.NullUUID{UUID: toolset.ID, Valid: true},
		Slug:           slug,
	})
	require.NoError(t, err)
	return toolset.ID
}

func TestSetRootMcpEndpoint_ByServerHostedOnDomainReusesItsEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "hosted-root.example.com",
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	serverID := seedCanonicalHostedServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "hosted-on-domain", uuid.NullUUID{UUID: domain.ID, Valid: true})
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	options, err := ti.service.ListRootMcpServers(ctx, &gen.ListRootMcpServersPayload{})
	require.NoError(t, err)
	require.True(t, rootOptionListed(options, serverID), "a hosted server served on the domain is a root option")

	result, err := ti.service.SetRootMcpEndpoint(ctx, &gen.SetRootMcpEndpointPayload{
		CustomDomainID: domain.ID.String(),
		McpServerID:    new(serverID.String()),
	})
	require.NoError(t, err)

	endpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{
		ProjectID: *authCtx.ProjectID, McpServerID: serverID,
	})
	require.NoError(t, err)
	require.Len(t, endpoints, 1, "the hosted server keeps exactly one endpoint")
	require.Equal(t, endpoints[0].ID.String(), requireValue(t, result.RootMcpEndpointID))
	require.True(t, endpoints[0].IsDomainRoot.Valid && endpoints[0].IsDomainRoot.Bool)
}

func TestSetRootMcpEndpoint_ByServerRejectsHostedServerOnAnotherAddress(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "hosted-elsewhere.example.com",
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	serverID := seedCanonicalHostedServer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "hosted-on-platform", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	options, err := ti.service.ListRootMcpServers(ctx, &gen.ListRootMcpServersPayload{})
	require.NoError(t, err)
	require.False(t, rootOptionListed(options, serverID), "a hosted server served elsewhere is not offered")

	_, err = ti.service.SetRootMcpEndpoint(ctx, &gen.SetRootMcpEndpointPayload{
		CustomDomainID: domain.ID.String(),
		McpServerID:    new(serverID.String()),
	})
	requireOopsCode(t, err, oops.CodeInvalid)

	_, err = ti.repo.GetMcpEndpointByCustomDomainAndServer(ctx, cdrepo.GetMcpEndpointByCustomDomainAndServerParams{
		CustomDomainID: domain.ID,
		McpServerID:    serverID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "no second endpoint may be created for a hosted server")
}

func rootOptionListed(options *gen.ListRootMcpServersResult, serverID uuid.UUID) bool {
	for _, option := range options.McpServers {
		if option.McpServerID == serverID.String() {
			return true
		}
	}
	return false
}
