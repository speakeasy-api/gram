package mcpendpoints_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp/visibility"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestResolveNetworkAccessModeMatrix(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		mode    networkaccess.Mode
		surface networkaccess.Surface
		allowed bool
	}{
		{name: "public_only_public", mode: networkaccess.ModePublicOnly, surface: networkaccess.SurfacePublic, allowed: true},
		{name: "public_only_private", mode: networkaccess.ModePublicOnly, surface: networkaccess.SurfacePrivate, allowed: false},
		{name: "dual_public", mode: networkaccess.ModeDual, surface: networkaccess.SurfacePublic, allowed: true},
		{name: "dual_private", mode: networkaccess.ModeDual, surface: networkaccess.SurfacePrivate, allowed: true},
		{name: "private_only_public", mode: networkaccess.ModePrivateOnly, surface: networkaccess.SurfacePublic, allowed: false},
		{name: "private_only_private", mode: networkaccess.ModePrivateOnly, surface: networkaccess.SurfacePrivate, allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)

			serverID := seedMcpServerWithMode(t, ctx, ti.conn, *authCtx.ProjectID, "public", tc.mode)
			slug := authCtx.OrganizationSlug + "-" + tc.name
			_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
				SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
				CustomDomainID: nil, McpServerID: conv.PtrEmpty(serverID.String()), MetaMcpServerID: nil,
				Slug: types.McpEndpointSlug(slug),
			})
			require.NoError(t, err)

			result, err := mcpendpoints.Resolve(ctx, ti.conn, testenv.NewLogger(t), mcpendpoints.ResolutionInput{
				Slug:                 slug,
				NamespaceKind:        mcpendpoints.NamespacePlatform,
				CustomDomainID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				ExpectedOrganization: authCtx.ActiveOrganizationID,
				Surface:              tc.surface,
			})
			require.NoError(t, err)
			require.True(t, result.Found)
			require.Equal(t, tc.allowed, result.Allowed)
			require.Equal(t, tc.mode, result.Mode)
		})
	}
}

func TestResolveMetaNetworkAccessModeMatrix(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		mode    networkaccess.Mode
		surface networkaccess.Surface
		allowed bool
	}{
		{name: "public_only_public", mode: networkaccess.ModePublicOnly, surface: networkaccess.SurfacePublic, allowed: true},
		{name: "public_only_private", mode: networkaccess.ModePublicOnly, surface: networkaccess.SurfacePrivate, allowed: false},
		{name: "dual_public", mode: networkaccess.ModeDual, surface: networkaccess.SurfacePublic, allowed: true},
		{name: "dual_private", mode: networkaccess.ModeDual, surface: networkaccess.SurfacePrivate, allowed: true},
		{name: "private_only_public", mode: networkaccess.ModePrivateOnly, surface: networkaccess.SurfacePublic, allowed: false},
		{name: "private_only_private", mode: networkaccess.ModePrivateOnly, surface: networkaccess.SurfacePrivate, allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			metaID := seedMetaMcpServerWithMode(t, ctx, ti, *authCtx.ProjectID, tc.mode)
			slug := authCtx.OrganizationSlug + "-meta-" + tc.name
			_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
				SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
				CustomDomainID: nil, McpServerID: nil, MetaMcpServerID: conv.PtrEmpty(metaID.String()),
				Slug: types.McpEndpointSlug(slug),
			})
			require.NoError(t, err)

			result, err := mcpendpoints.Resolve(ctx, ti.conn, testenv.NewLogger(t), mcpendpoints.ResolutionInput{
				Slug: slug, NamespaceKind: mcpendpoints.NamespacePlatform,
				CustomDomainID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				ExpectedOrganization: authCtx.ActiveOrganizationID, Surface: tc.surface,
			})
			require.NoError(t, err)
			require.True(t, result.Found)
			require.Equal(t, tc.allowed, result.Allowed)
			require.Equal(t, tc.mode, result.Mode)
		})
	}
}

func TestResolveWrongOrganizationIsAuthoritativeDenial(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	serverID := seedMcpServerWithMode(t, ctx, ti.conn, *authCtx.ProjectID, "public", networkaccess.ModeDual)
	slug := authCtx.OrganizationSlug + "-wrong-org"
	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		CustomDomainID: nil, McpServerID: conv.PtrEmpty(serverID.String()), MetaMcpServerID: nil,
		Slug: types.McpEndpointSlug(slug),
	})
	require.NoError(t, err)

	result, err := mcpendpoints.Resolve(ctx, ti.conn, testenv.NewLogger(t), mcpendpoints.ResolutionInput{
		Slug: slug, NamespaceKind: mcpendpoints.NamespacePlatform,
		CustomDomainID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExpectedOrganization: "other-org", Surface: networkaccess.SurfacePrivate,
	})
	require.NoError(t, err)
	require.True(t, result.Found)
	require.False(t, result.Allowed)
}

func TestResolveDisabledServersDenyEverySurface(t *testing.T) {
	t.Parallel()

	for _, backend := range []string{"generic", "meta"} {
		t.Run(backend, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			slug := authCtx.OrganizationSlug + "-disabled-" + backend

			var mcpServerID, metaMcpServerID *string
			if backend == "generic" {
				id := seedMcpServerWithMode(t, ctx, ti.conn, *authCtx.ProjectID, "disabled", networkaccess.ModeDual).String()
				mcpServerID = &id
			} else {
				meta, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
					OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
					Name: "disabled meta", UserSessionIssuerID: uuid.NullUUID{},
					Visibility: visibility.Disabled, NetworkAccessMode: networkaccess.Storage(networkaccess.ModeDual),
				})
				require.NoError(t, err)
				id := meta.ID.String()
				metaMcpServerID = &id
			}
			_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
				SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
				CustomDomainID: nil, McpServerID: mcpServerID, MetaMcpServerID: metaMcpServerID,
				Slug: types.McpEndpointSlug(slug),
			})
			require.NoError(t, err)

			for _, surface := range []networkaccess.Surface{networkaccess.SurfacePublic, networkaccess.SurfacePrivate} {
				result, err := mcpendpoints.Resolve(ctx, ti.conn, testenv.NewLogger(t), mcpendpoints.ResolutionInput{
					Slug: slug, NamespaceKind: mcpendpoints.NamespacePlatform,
					CustomDomainID: uuid.NullUUID{}, ExpectedOrganization: authCtx.ActiveOrganizationID, Surface: surface,
				})
				require.NoError(t, err)
				require.True(t, result.Found)
				require.False(t, result.Allowed)
			}
		})
	}
}

func TestResolveUnknownStoredModeDeniesEverySurface(t *testing.T) {
	t.Parallel()

	for _, surface := range []networkaccess.Surface{networkaccess.SurfacePublic, networkaccess.SurfacePrivate} {
		t.Run(string(surface), func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			serverID := seedMcpServerWithMode(t, ctx, ti.conn, *authCtx.ProjectID, "public", networkaccess.ModeDual)
			slug := authCtx.OrganizationSlug + "-unknown-" + string(surface)
			_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
				SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
				CustomDomainID: nil, McpServerID: conv.PtrEmpty(serverID.String()), MetaMcpServerID: nil,
				Slug: types.McpEndpointSlug(slug),
			})
			require.NoError(t, err)
			rows, err := testrepo.New(ti.conn).SetMCPServerNetworkAccessModeFixture(ctx, testrepo.SetMCPServerNetworkAccessModeFixtureParams{
				NetworkAccessMode: pgtype.Text{String: "future_mode", Valid: true},
				ID:                serverID, ProjectID: *authCtx.ProjectID,
			})
			require.NoError(t, err)
			require.EqualValues(t, 1, rows)

			result, err := mcpendpoints.Resolve(ctx, ti.conn, testenv.NewLogger(t), mcpendpoints.ResolutionInput{
				Slug: slug, NamespaceKind: mcpendpoints.NamespacePlatform,
				CustomDomainID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				ExpectedOrganization: authCtx.ActiveOrganizationID, Surface: surface,
			})
			require.NoError(t, err)
			require.True(t, result.Found)
			require.False(t, result.Allowed)
		})
	}
}

func TestResolveMetaUnknownStoredModeDeniesEverySurface(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	metaID := seedMetaMcpServerWithMode(t, ctx, ti, *authCtx.ProjectID, networkaccess.ModeDual)
	slug := authCtx.OrganizationSlug + "-meta-unknown"
	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		CustomDomainID: nil, McpServerID: nil, MetaMcpServerID: conv.PtrEmpty(metaID.String()),
		Slug: types.McpEndpointSlug(slug),
	})
	require.NoError(t, err)
	rows, err := testrepo.New(ti.conn).SetMetaMCPServerNetworkAccessModeFixture(ctx, testrepo.SetMetaMCPServerNetworkAccessModeFixtureParams{
		NetworkAccessMode: pgtype.Text{String: "future_mode", Valid: true}, ID: metaID,
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)

	for _, surface := range []networkaccess.Surface{networkaccess.SurfacePublic, networkaccess.SurfacePrivate} {
		result, err := mcpendpoints.Resolve(ctx, ti.conn, testenv.NewLogger(t), mcpendpoints.ResolutionInput{
			Slug: slug, NamespaceKind: mcpendpoints.NamespacePlatform, CustomDomainID: uuid.NullUUID{},
			ExpectedOrganization: authCtx.ActiveOrganizationID, Surface: surface,
		})
		require.NoError(t, err)
		require.True(t, result.Found)
		require.False(t, result.Allowed)
	}
}

func TestResolvePrivateSurfaceWithoutOrganizationFailsClosed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	result, err := mcpendpoints.Resolve(ctx, ti.conn, testenv.NewLogger(t), mcpendpoints.ResolutionInput{
		Slug: "does-not-matter", NamespaceKind: mcpendpoints.NamespacePlatform,
		CustomDomainID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExpectedOrganization: "", Surface: networkaccess.SurfacePrivate,
	})
	require.NoError(t, err)
	require.True(t, result.Found)
	require.False(t, result.Allowed)
}

func TestResolveClearedCustomDomainNamespaceFailsClosed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	result, err := mcpendpoints.Resolve(ctx, ti.conn, testenv.NewLogger(t), mcpendpoints.ResolutionInput{
		Slug:                 "does-not-matter",
		NamespaceKind:        mcpendpoints.NamespaceCustomDomain,
		CustomDomainID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExpectedOrganization: "org",
		Surface:              networkaccess.SurfacePrivate,
	})
	require.NoError(t, err)
	require.True(t, result.Found)
	require.False(t, result.Allowed)
}

func TestBySlugAndCustomDomainMarksMalformedOriginAsPolicyDenied(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = requestorigin.WithContext(ctx, requestorigin.Origin{
		Surface: requestorigin.SurfaceCustomDomain, BaseURL: "https://custom.example.com",
		OrganizationID: "org", NetworkIngressID: uuid.Nil, NetworkIdentity: nil,
	})
	_, _, _, err := mcpendpoints.BySlugAndCustomDomain(ctx, ti.conn, testenv.NewLogger(t), "slug")
	require.Error(t, err)
	require.True(t, mcpendpoints.IsPolicyDenied(err))
}

func TestBySlugAndCustomDomainMarksPrivateOnlyPublicDenial(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	serverID := seedMcpServerWithMode(t, ctx, ti.conn, *authCtx.ProjectID, "public", networkaccess.ModePrivateOnly)
	slug := authCtx.OrganizationSlug + "-terminal-policy"
	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		CustomDomainID: nil, McpServerID: conv.PtrEmpty(serverID.String()), MetaMcpServerID: nil,
		Slug: types.McpEndpointSlug(slug),
	})
	require.NoError(t, err)

	ctx = requestorigin.WithContext(ctx, requestorigin.Origin{
		Surface: requestorigin.SurfacePlatform, BaseURL: "https://api.example.com",
		OrganizationID: "", NetworkIngressID: uuid.Nil, NetworkIdentity: nil,
	})
	_, _, _, err = mcpendpoints.BySlugAndCustomDomain(ctx, ti.conn, testenv.NewLogger(t), slug)
	require.Error(t, err)
	require.True(t, mcpendpoints.IsPolicyDenied(err))
}
