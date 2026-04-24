package mcpslugs_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_slugs"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpfrontendsrepo "github.com/speakeasy-api/gram/server/internal/mcpfrontends/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// seedOtherProjectMcpFrontend creates an additional project in the caller's
// organization and inserts an mcp_frontend under that *other* project.
// Used to exercise cross-tenant ownership rejection.
func seedOtherProjectMcpFrontend(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	slug := "other-" + uuid.New().String()[:8]
	otherProject, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	server, err := remotemcprepo.New(conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ProjectID:     otherProject.ID,
		TransportType: "streamable-http",
		Url:           "https://other.example.com/mcp/" + uuid.NewString(),
	})
	require.NoError(t, err)

	frontend, err := mcpfrontendsrepo.New(conn).CreateMCPFrontend(ctx, mcpfrontendsrepo.CreateMCPFrontendParams{
		ProjectID:             otherProject.ID,
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExternalOauthServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OauthProxyServerID:    uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     uuid.NullUUID{UUID: server.ID, Valid: true},
		ToolsetID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            "disabled",
	})
	require.NoError(t, err)

	return frontend.ID
}

func TestCreateMcpSlug_RejectsCrossTenantMcpFrontend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Frontend lives in a different project in the same org.
	otherFrontendID := seedOtherProjectMcpFrontend(t, ctx, ti.conn, authCtx.ActiveOrganizationID).String()

	_, err := ti.service.CreateMcpSlug(ctx, &gen.CreateMcpSlugPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpFrontendID:    otherFrontendID,
		Slug:             types.McpSlugString(authCtx.OrganizationSlug + "-leak"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUpdateMcpSlug_RejectsCrossTenantMcpFrontend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ownFrontendID := seedMcpFrontend(t, ctx, ti.conn, *authCtx.ProjectID).String()
	created, err := ti.service.CreateMcpSlug(ctx, &gen.CreateMcpSlugPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpFrontendID:    ownFrontendID,
		Slug:             types.McpSlugString(authCtx.OrganizationSlug + "-legit"),
	})
	require.NoError(t, err)

	otherFrontendID := seedOtherProjectMcpFrontend(t, ctx, ti.conn, authCtx.ActiveOrganizationID).String()

	_, err = ti.service.UpdateMcpSlug(ctx, &gen.UpdateMcpSlugPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		CustomDomainID:   nil,
		McpFrontendID:    otherFrontendID,
		Slug:             types.McpSlugString(authCtx.OrganizationSlug + "-legit"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpSlug_ConflictOnDuplicateSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	frontendID := seedMcpFrontend(t, ctx, ti.conn, *authCtx.ProjectID).String()
	slugVal := authCtx.OrganizationSlug + "-taken"

	_, err := ti.service.CreateMcpSlug(ctx, &gen.CreateMcpSlugPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpFrontendID:    frontendID,
		Slug:             types.McpSlugString(slugVal),
	})
	require.NoError(t, err)

	// Second create with the same (NULL custom_domain_id, slug) must conflict.
	_, err = ti.service.CreateMcpSlug(ctx, &gen.CreateMcpSlugPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpFrontendID:    frontendID,
		Slug:             types.McpSlugString(slugVal),
	})
	requireOopsCode(t, err, oops.CodeConflict)
}
