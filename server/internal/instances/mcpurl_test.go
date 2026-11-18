package instances

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/instances"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type domainState struct {
	verified, activated, deleted bool
}

func seedInstanceDomain(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, state domainState) customdomainsrepo.CustomDomain {
	t.Helper()

	domains := customdomainsrepo.New(conn)
	d, err := domains.CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID:  orgID,
		Domain:          "playground-" + uuid.NewString()[:8] + ".example.com",
		IngressName:     pgtype.Text{String: "", Valid: false},
		CertSecretName:  pgtype.Text{String: "", Valid: false},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	d, err = domains.UpdateCustomDomain(ctx, customdomainsrepo.UpdateCustomDomainParams{
		Verified:        state.verified,
		Activated:       state.activated,
		IngressName:     pgtype.Text{String: "", Valid: false},
		CertSecretName:  pgtype.Text{String: "", Valid: false},
		ProvisionerKind: "ingress",
		ID:              d.ID,
	})
	require.NoError(t, err)
	if state.deleted {
		require.NoError(t, domains.DeleteCustomDomain(ctx, orgID))
	}
	return d
}

func seedInstanceToolset(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, projectID uuid.UUID) toolsetsrepo.Toolset {
	t.Helper()

	slug := "playground-" + uuid.NewString()[:8]
	toolset, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         orgID,
		ProjectID:              projectID,
		Name:                   slug,
		Slug:                   slug,
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, ctx, conn)
	_, err = hostedmcp.CreateCanonical(ctx, tx, audit.NewLogger(), hostedmcp.SystemActor("test"), hostedmcp.CreateCanonicalInput{
		Toolset: toolset, Slug: slug, Visibility: mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	return toolset
}

func instanceMCPURLs(t *testing.T, ctx context.Context, svc *Service, slug string) []string {
	t.Helper()

	got, err := svc.GetInstance(ctx, &gen.GetInstanceForm{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ChatSessionsToken: nil,
		ToolsetSlug:       types.Slug(slug),
	})
	require.NoError(t, err)
	urls := make([]string, 0, len(got.McpServers))
	for _, server := range got.McpServers {
		urls = append(urls, server.URL)
	}
	return urls
}

// A disabled wrapper advertises its address, which never names a custom domain the middleware would not serve.
func TestGetInstance_DisabledWrapperURLSkipsUnservedCustomDomain(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		state      domainState
		wantDomain bool
	}{
		"verified and activated":  {state: domainState{verified: true, activated: true}, wantDomain: true},
		"unverified":              {state: domainState{}},
		"verified, not activated": {state: domainState{verified: true}},
		"activated, not verified": {state: domainState{activated: true}},
		"deleted":                 {state: domainState{verified: true, activated: true, deleted: true}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, svc, conn := newTestInstanceService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, authCtx)

			toolset := seedInstanceToolset(t, ctx, conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
			domain := seedInstanceDomain(t, ctx, conn, authCtx.ActiveOrganizationID, tt.state)
			slug := "domain-" + uuid.NewString()[:8]
			testenv.SetHostedMCP(t, ctx, conn, toolset.ProjectID, toolset.ID, testenv.HostedMCP{
				Visibility:          mcpservers.VisibilityDisabled,
				UserSessionIssuerID: nil,
				Slug:                slug,
				CustomDomainID:      uuid.NullUUID{UUID: domain.ID, Valid: true},
			})

			want := []string{}
			if tt.wantDomain {
				want = []string{"https://" + domain.Domain + "/mcp/" + slug}
			}
			require.Equal(t, want, instanceMCPURLs(t, ctx, svc, toolset.Slug))
		})
	}
}

func TestGetInstance_WrapperURLSkipsUnservedCustomDomain(t *testing.T) {
	t.Parallel()

	for name, withPlatform := range map[string]bool{"platform endpoint": true, "no platform endpoint": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, svc, conn := newTestInstanceService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, authCtx)

			toolset := seedInstanceToolset(t, ctx, conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
			domain := seedInstanceDomain(t, ctx, conn, authCtx.ActiveOrganizationID, domainState{verified: true})
			testenv.SetHostedMCP(t, ctx, conn, toolset.ProjectID, toolset.ID, testenv.HostedMCP{
				Visibility:          "",
				UserSessionIssuerID: nil,
				Slug:                "unserved-" + uuid.NewString()[:8],
				CustomDomainID:      uuid.NullUUID{UUID: domain.ID, Valid: true},
			})

			want := []string{}
			if withPlatform {
				platformSlug := "platform-" + uuid.NewString()[:8]
				_, err := mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
					ProjectID:       *authCtx.ProjectID,
					CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
					McpServerID:     uuid.NullUUID{UUID: toolset.ID, Valid: true},
					MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
					Slug:            platformSlug,
				})
				require.NoError(t, err)
				want = []string{svc.serverURL.String() + "/mcp/" + platformSlug}
			}
			require.Equal(t, want, instanceMCPURLs(t, ctx, svc, toolset.Slug))
		})
	}
}
