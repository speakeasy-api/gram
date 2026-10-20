package usersessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	sessionsgen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

func seedMintCustomDomain(t *testing.T, ctx context.Context, ti *testInstance, verified, activated bool) customdomainsrepo.CustomDomain {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	domains := customdomainsrepo.New(ti.conn)
	domain, err := domains.CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "mint-" + uuid.NewString()[:8] + ".example.com",
		IngressName:     pgtype.Text{String: "", Valid: false},
		CertSecretName:  pgtype.Text{String: "", Valid: false},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	updated, err := domains.UpdateCustomDomain(ctx, customdomainsrepo.UpdateCustomDomainParams{
		Verified:        verified,
		Activated:       activated,
		IngressName:     pgtype.Text{String: "", Valid: false},
		CertSecretName:  pgtype.Text{String: "", Valid: false},
		ProvisionerKind: "ingress",
		ID:              domain.ID,
	})
	require.NoError(t, err)
	return updated
}

func seedMintEndpoint(t *testing.T, ctx context.Context, ti *testInstance, serverID uuid.UUID, domainID uuid.NullUUID) string {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	slug := "mint-ep-" + uuid.NewString()[:8]
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  domainID,
		McpServerID:     uuid.NullUUID{UUID: serverID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            slug,
	})
	require.NoError(t, err)
	return slug
}

// The iss claim names a custom-domain endpoint only when the custom-domain
// middleware would serve it: verified, activated, and not deleted.
func TestMintUserSessionIssuerSkipsUnservedCustomDomain(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		verified         bool
		activated        bool
		deleted          bool
		withPlatform     bool
		wantCustomIssuer bool
	}{
		"verified and activated":   {verified: true, activated: true, withPlatform: true, wantCustomIssuer: true},
		"unverified":               {verified: false, activated: false, withPlatform: true},
		"verified, not activated":  {verified: true, activated: false, withPlatform: true},
		"activated, not verified":  {verified: false, activated: true, withPlatform: true},
		"deleted":                  {verified: true, activated: true, deleted: true, withPlatform: true},
		"unserved, no platform ep": {verified: true, activated: false, withPlatform: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			server := createIssuerGatedMintServer(t, ctx, ti, "mint-cd-"+uuid.NewString()[:8])

			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, authCtx)

			domain := seedMintCustomDomain(t, ctx, ti, tt.verified, tt.activated)
			customSlug := seedMintEndpoint(t, ctx, ti, server.ID, uuid.NullUUID{UUID: domain.ID, Valid: true})
			platformSlug := ""
			if tt.withPlatform {
				platformSlug = seedMintEndpoint(t, ctx, ti, server.ID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
			}
			if tt.deleted {
				require.NoError(t, customdomainsrepo.New(ti.conn).DeleteCustomDomain(ctx, authCtx.ActiveOrganizationID))
			}

			ctx = withExactAuthzGrants(t, ctx, ti.conn,
				authz.NewGrant(authz.ScopeMCPConnect, server.ID.String()),
			)
			got, err := ti.service.MintUserSession(ctx, &sessionsgen.MintUserSessionPayload{
				ToolsetID:        nil,
				McpServerID:      conv.PtrEmpty(server.ID.String()),
				SessionToken:     nil,
				ProjectSlugInput: nil,
			})
			require.NoError(t, err)

			claims, err := usersessions.NewSigner("test-jwt-secret").Validate(
				got.AccessToken,
				urn.NewUserSessionIssuer(server.UserSessionIssuerID.UUID).String(),
			)
			require.NoError(t, err)

			switch {
			case tt.wantCustomIssuer:
				require.Equal(t, "https://"+domain.Domain+"/mcp/"+customSlug, claims.Issuer)
			case tt.withPlatform:
				require.Equal(t, "http://0.0.0.0/mcp/"+platformSlug, claims.Issuer)
			default:
				require.Equal(t, "http://0.0.0.0/mcp/"+server.Slug.String, claims.Issuer,
					"no servable endpoint falls back to the server slug")
			}
		})
	}
}

// A wrapper whose only endpoint sits on an unserved domain still governs the
// toolset mint; only the iss URL falls back to the wrapper's slug.
func TestMintUserSessionForToolsetWrapperOnUnservedDomain(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"toolset issuer absent":    "absent",
		"toolset issuer matching":  "matching",
		"toolset issuer differing": "differing",
	}

	for name, toolsetIssuer := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, authCtx)

			slug := "mint-unserved-" + uuid.NewString()[:8]
			wrapperIssuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
				SessionToken:         nil,
				ApikeyToken:          nil,
				ProjectSlugInput:     nil,
				Slug:                 slug + "-srv-issuer",
				AuthnChallengeMode:   "chain",
				SessionDurationHours: 24,
			})
			require.NoError(t, err)
			wrapperIssuerID := uuid.MustParse(wrapperIssuer.ID)

			var toolset toolsetsrepo.Toolset
			switch toolsetIssuer {
			case "absent":
				toolset = createBackingToolset(t, ctx, ti, slug)
			case "matching":
				toolset = createBackingToolset(t, ctx, ti, slug)
				toolset, err = toolsetsrepo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsetsrepo.UpdateToolsetUserSessionIssuerParams{
					UserSessionIssuerID: uuid.NullUUID{UUID: wrapperIssuerID, Valid: true},
					Slug:                toolset.Slug,
					ProjectID:           toolset.ProjectID,
				})
				require.NoError(t, err)
			default:
				toolset = createIssuerGatedMintToolset(t, ctx, ti, slug)
				require.NotEqual(t, wrapperIssuerID, toolset.UserSessionIssuerID.UUID)
			}

			wrapper, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
				ID:                    uuid.New(),
				ProjectID:             *authCtx.ProjectID,
				Name:                  pgtype.Text{String: slug + "-srv", Valid: true},
				Slug:                  pgtype.Text{String: slug + "-srv", Valid: true},
				EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				UserSessionIssuerID:   uuid.NullUUID{UUID: wrapperIssuerID, Valid: true},
				RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
				ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				Visibility:            mcpservers.VisibilityPrivate,
			})
			require.NoError(t, err)

			domain := seedMintCustomDomain(t, ctx, ti, true, false)
			seedMintEndpoint(t, ctx, ti, wrapper.ID, uuid.NullUUID{UUID: domain.ID, Valid: true})

			// The RBAC resource stays the wrapper, not the toolset.
			ctx = withExactAuthzGrants(t, ctx, ti.conn,
				authz.NewGrant(authz.ScopeMCPConnect, wrapper.ID.String()),
			)
			got, err := ti.service.MintUserSession(ctx, &sessionsgen.MintUserSessionPayload{
				ToolsetID:        conv.PtrEmpty(toolset.ID.String()),
				McpServerID:      nil,
				SessionToken:     nil,
				ProjectSlugInput: nil,
			})
			require.NoError(t, err)

			signer := usersessions.NewSigner("test-jwt-secret")
			claims, err := signer.Validate(got.AccessToken, urn.NewUserSessionIssuer(wrapperIssuerID).String())
			require.NoError(t, err)
			require.Equal(t, "http://0.0.0.0/mcp/"+wrapper.Slug.String, claims.Issuer)
			_, err = signer.Validate(got.AccessToken, urn.NewToolset(toolset.ID).String())
			require.Error(t, err, "the legacy toolset audience must not bind the token")
		})
	}
}
