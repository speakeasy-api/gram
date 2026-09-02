package mcpendpoints_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	cdrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// seedHostedWrapper inserts a hosted toolset with a wrapper but no endpoint,
// the shape production carried before the mirror existed.
func seedHostedWrapper(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID) (toolsetsrepo.Toolset, mcpserversrepo.McpServer) {
	t.Helper()

	slug := "hosted-" + uuid.NewString()[:8]
	toolset, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         organizationID,
		ProjectID:              projectID,
		Name:                   slug,
		Slug:                   slug,
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "", Valid: false},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	serverID, err := uuid.NewV7()
	require.NoError(t, err)
	wrapper, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    serverID,
		ProjectID:             projectID,
		Name:                  conv.ToPGText(slug),
		Slug:                  conv.ToPGText(slug),
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
		UnproxiedMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            "private",
	})
	require.NoError(t, err)

	return toolset, wrapper
}

func seedActiveCustomDomain(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	domain, err := cdrepo.New(conn).CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  organizationID,
		Domain:          "mirror-" + uuid.NewString()[:8] + ".example.com",
		IngressName:     conv.ToPGText("ingress-mirror"),
		CertSecretName:  conv.ToPGText("cert-mirror"),
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	_, err = cdrepo.New(conn).SetCustomDomainVerified(ctx, domain.ID)
	require.NoError(t, err)
	activated, err := cdrepo.New(conn).ActivateVerifiedCustomDomain(ctx, cdrepo.ActivateVerifiedCustomDomainParams{
		IngressName:     conv.ToPGText("ingress-mirror"),
		CertSecretName:  conv.ToPGText("cert-mirror"),
		ProvisionerKind: "ingress",
		ID:              domain.ID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, activated)

	return domain.ID
}

func toolsetAddress(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, toolsetID uuid.UUID) (pgtype.Text, uuid.NullUUID) {
	t.Helper()

	toolset, err := toolsetsrepo.New(conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{
		ID:        toolsetID,
		ProjectID: projectID,
	})
	require.NoError(t, err)

	return toolset.McpSlug, toolset.CustomDomainID
}

func createEndpoint(t *testing.T, ctx context.Context, ti *testInstance, serverID uuid.UUID, customDomainID *uuid.UUID, slug string) *types.McpEndpoint {
	t.Helper()

	var domain *string
	if customDomainID != nil {
		domain = conv.PtrEmpty(customDomainID.String())
	}
	created, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   domain,
		McpServerID:      conv.PtrEmpty(serverID.String()),
		MetaMcpServerID:  nil,
		Slug:             types.McpEndpointSlug(slug),
	})
	require.NoError(t, err)

	return created
}

// The toolset columns follow the wrapper's primary endpoint: a custom-domain
// endpoint outranks a platform one, and the last endpoint leaving clears the
// address.
func TestMcpEndpoints_ToolsetBacked_ProjectPrimaryAddressOntoToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset, wrapper := seedHostedWrapper(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	domainID := seedActiveCustomDomain(t, ctx, ti.conn, authCtx.ActiveOrganizationID)

	platformSlug := authCtx.OrganizationSlug + "-mirror-platform"
	platform := createEndpoint(t, ctx, ti, wrapper.ID, nil, platformSlug)

	slug, domain := toolsetAddress(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.Equal(t, platformSlug, slug.String)
	require.False(t, domain.Valid)

	custom := createEndpoint(t, ctx, ti, wrapper.ID, &domainID, "mirror-custom")

	slug, domain = toolsetAddress(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.Equal(t, "mirror-custom", slug.String)
	require.Equal(t, uuid.NullUUID{UUID: domainID, Valid: true}, domain)

	require.NoError(t, ti.service.DeleteMcpEndpoint(ctx, &gen.DeleteMcpEndpointPayload{
		ID:               custom.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	slug, domain = toolsetAddress(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.Equal(t, platformSlug, slug.String)
	require.False(t, domain.Valid)

	require.NoError(t, ti.service.DeleteMcpEndpoint(ctx, &gen.DeleteMcpEndpointPayload{
		ID:               platform.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	slug, domain = toolsetAddress(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.False(t, slug.Valid)
	require.False(t, domain.Valid)
}

func TestUpdateMcpEndpoint_ToolsetBacked_RekeysToolsetAddress(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset, wrapper := seedHostedWrapper(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	endpoint := createEndpoint(t, ctx, ti, wrapper.ID, nil, authCtx.OrganizationSlug+"-before")

	renamed := authCtx.OrganizationSlug + "-after"
	_, err := ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               endpoint.ID,
		CustomDomainID:   nil,
		McpServerID:      conv.PtrEmpty(wrapper.ID.String()),
		MetaMcpServerID:  nil,
		Slug:             types.McpEndpointSlug(renamed),
	})
	require.NoError(t, err)

	slug, _ := toolsetAddress(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.Equal(t, renamed, slug.String)
}

func TestCreateMcpEndpoint_ToolsetBacked_RejectsSlugOverToolsetLimit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, wrapper := seedHostedWrapper(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)

	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		McpServerID: conv.PtrEmpty(wrapper.ID.String()),
		Slug:        types.McpEndpointSlug(authCtx.OrganizationSlug + "-" + strings.Repeat("a", hostedmcp.MaxToolsetSlugLength)),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

// Moving an endpoint between two hosted servers vacates the source toolset's
// slug before the target toolset claims it, whatever the servers' id order.
func TestUpdateMcpEndpoint_MoveBetweenHostedServers_ReleasesSlugFirst(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	olderToolset, older := seedHostedWrapper(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	newerToolset, newer := seedHostedWrapper(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	shared := authCtx.OrganizationSlug + "-shared"
	endpoint := createEndpoint(t, ctx, ti, newer.ID, nil, shared)

	_, err := ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{
		ID:          endpoint.ID,
		McpServerID: conv.PtrEmpty(older.ID.String()),
		Slug:        types.McpEndpointSlug(shared),
	})
	require.NoError(t, err)

	slug, _ := toolsetAddress(t, ctx, ti.conn, *authCtx.ProjectID, olderToolset.ID)
	require.Equal(t, shared, slug.String)
	slug, _ = toolsetAddress(t, ctx, ti.conn, *authCtx.ProjectID, newerToolset.ID)
	require.False(t, slug.Valid)
}
