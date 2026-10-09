package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestGetMcpServer_ByToolsetID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
		Name: "Selected hosted server", Slug: "selected-hosted-server", McpEnabled: true,
	})
	require.NoError(t, err)
	servers := mcpserversrepo.New(ti.conn)
	canonical, err := servers.CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: *authCtx.ProjectID,
		Name: conv.ToPGText("Disabled canonical"), Slug: conv.ToPGText("disabled-canonical"),
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "disabled",
	})
	require.NoError(t, err)
	alternate, err := servers.CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: *authCtx.ProjectID,
		Name: conv.ToPGText("Enabled alternate"), Slug: conv.ToPGText("enabled-alternate"),
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	// An addressless selected server is distinct from a missing wrapper.
	alternateID := alternate.ID.String()
	addressless, err := ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{ToolsetID: conv.PtrEmpty(toolset.ID.String())})
	require.NoError(t, err)
	require.Nil(t, addressless.PlatformEndpointSlug)

	for _, wrapper := range []mcpserversrepo.McpServer{canonical, alternate} {
		_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
			ProjectID:   *authCtx.ProjectID,
			McpServerID: uuid.NullUUID{UUID: wrapper.ID, Valid: true},
			Slug:        wrapper.Slug.String + "-endpoint",
		})
		require.NoError(t, err)
	}
	toolsetID := toolset.ID.String()
	payload := &gen.GetMcpServerPayload{ToolsetID: &toolsetID}
	got, err := ti.service.GetMcpServer(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, alternate.ID.String(), got.ID)
	require.Equal(t, types.McpServerVisibility("private"), got.Visibility)
	require.Equal(t, conv.PtrEmpty(alternate.Slug.String+"-endpoint"), got.PlatformEndpointSlug)

	canonicalID := canonical.ID.String()
	got, err = ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{ID: &canonicalID})
	require.NoError(t, err)
	require.Equal(t, canonicalID, got.ID)
	require.Equal(t, types.McpServerVisibility("disabled"), got.Visibility)
	require.Nil(t, got.PlatformEndpointSlug, "exact-ID reads do not resolve connection addresses")
	got, err = ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{Slug: &alternate.Slug.String})
	require.NoError(t, err)
	require.Equal(t, alternateID, got.ID)
	require.Nil(t, got.PlatformEndpointSlug, "exact-slug reads do not resolve connection addresses")

	// Management reads retain the existing backing-toolset grant resource.
	granted := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, toolsetID))
	got, err = ti.service.GetMcpServer(granted, payload)
	require.NoError(t, err)
	require.Equal(t, alternate.ID.String(), got.ID)
	// Target discovery must not require project-wide endpoint-list permission.
	require.Equal(t, conv.PtrEmpty(alternate.Slug.String+"-endpoint"), got.PlatformEndpointSlug)
	denied := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, alternate.ID.String()))
	_, err = ti.service.GetMcpServer(denied, payload)
	requireOopsCode(t, err, oops.CodeForbidden)

	otherProject := uuid.New()
	otherAuth := *authCtx
	otherAuth.ProjectID = &otherProject
	otherCtx := contextvalues.SetAuthContext(ctx, &otherAuth)
	_, err = ti.service.GetMcpServer(otherCtx, payload)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetMcpServer_ToolsetSelectorValidation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	id, slug, malformed := uuid.NewString(), "server-slug", "not-a-uuid"
	for _, tt := range []struct {
		name    string
		payload gen.GetMcpServerPayload
		code    oops.Code
	}{
		{name: "id and toolset", payload: gen.GetMcpServerPayload{ID: &id, ToolsetID: &id}, code: oops.CodeBadRequest},
		{name: "slug and toolset", payload: gen.GetMcpServerPayload{Slug: &slug, ToolsetID: &id}, code: oops.CodeBadRequest},
		{name: "all selectors", payload: gen.GetMcpServerPayload{ID: &id, Slug: &slug, ToolsetID: &id}, code: oops.CodeBadRequest},
		{name: "malformed toolset", payload: gen.GetMcpServerPayload{ToolsetID: &malformed}, code: oops.CodeBadRequest},
		{name: "missing toolset", payload: gen.GetMcpServerPayload{ToolsetID: &id}, code: oops.CodeNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ti.service.GetMcpServer(ctx, &tt.payload)
			requireOopsCode(t, err, tt.code)
		})
	}
}

func TestGetMcpServer_ExactSelectorsDoNotRequireEndpointLookup(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	server, toolsetID := createToolsetBackedServerFixture(t, ctx, ti, "endpoint lookup independence")
	require.NotNil(t, server.Slug)

	// This service has an isolated cloned test database. Make endpoint reads fail
	// without affecting the server row or its authorization data.
	_, err := ti.conn.Exec(ctx, "ALTER TABLE mcp_endpoints RENAME TO unavailable_mcp_endpoints") //nolint:glint // notestingrawsql: isolated test-database fault injection; never expose destructive DDL through production SQLc methods.
	require.NoError(t, err)
	for _, payload := range []*gen.GetMcpServerPayload{{ID: &server.ID}, {Slug: server.Slug}} {
		got, err := ti.service.GetMcpServer(ctx, payload)
		require.NoError(t, err)
		require.Equal(t, server.ID, got.ID)
		require.Nil(t, got.PlatformEndpointSlug)
	}
	_, err = ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{ToolsetID: conv.PtrEmpty(toolsetID.String())})
	requireOopsCode(t, err, oops.CodeUnexpected)
}

func TestGetMcpServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "test mcp server",
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("private"),
	})
	require.NoError(t, err)

	fetched, err := ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{
		ID:               &created.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.ID)
	require.Equal(t, types.McpServerVisibility("private"), fetched.Visibility)
}

func TestGetMcpServer_BySlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "fetch by slug",
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, created.Slug)

	fetched, err := ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{
		ID:               nil,
		Slug:             created.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.ID)
}

func TestGetMcpServer_MissingIDAndSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{
		ID:               nil,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestGetMcpServer_BothIDAndSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.NewString()
	slug := "some-slug"
	_, err := ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{
		ID:               &id,
		Slug:             &slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestGetMcpServer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.NewString()
	_, err := ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{
		ID:               &id,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetMcpServer_InvalidID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := "not-a-uuid"
	_, err := ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{
		ID:               &id,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestGetMcpServer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	fixture := createRemoteServerFixture(t, ctx, ti, "rbac forbidden get")

	denied := withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.GetMcpServer(denied, &gen.GetMcpServerPayload{
		ID:               &fixture.server.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
