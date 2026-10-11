package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type proxyTierBillingRepo struct {
	billing.Repository
	tier billing.Tier
}

func (r *proxyTierBillingRepo) GetCustomerTier(context.Context, string) (*billing.Tier, bool, error) {
	return &r.tier, true, nil
}

func TestPrepareProxyBackendContextUsesCurrentBillingTier(t *testing.T) {
	t.Parallel()

	db, err := TestInfra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	ctx := t.Context()
	orgs := orgrepo.New(db)
	org, err := orgs.UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: uuid.NewString(), Name: "Test Organization", Slug: "test-organization",
	})
	require.NoError(t, err)
	require.NoError(t, orgs.SetAccountType(ctx, orgrepo.SetAccountTypeParams{
		ID: org.ID, GramAccountType: string(billing.TierBase),
	}))
	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{
		OrganizationID: org.ID, Name: "Test Project", Slug: "test-project",
	})
	require.NoError(t, err)

	repo := &proxyTierBillingRepo{tier: billing.TierPro}
	s := &Service{db: db, billingRepository: repo}
	endpoint := &mcpendpointsrepo.McpEndpoint{ProjectID: project.ID}
	server := &mcpserversrepo.McpServer{Visibility: mcpservers.VisibilityPublic}
	request := httptest.NewRequest(http.MethodPost, "/mcp/test", nil)
	for _, tier := range []billing.Tier{billing.TierPro, billing.TierBase} {
		repo.tier = tier
		got, orgID, err := s.prepareProxyBackendContext(ctx, httptest.NewRecorder(), request, testenv.NewLogger(t), endpoint, server)
		require.NoError(t, err)
		require.Equal(t, org.ID, orgID)
		serverCtx, ok := remotemcp.GetServerContext(got)
		require.True(t, ok)
		require.Equal(t, remotemcp.ServerContext{
			OrganizationID: org.ID, OrganizationSlug: org.Slug,
			ProjectID: project.ID, ProjectSlug: project.Slug, AccountType: string(tier),
		}, serverCtx)
	}

	persisted, err := orgs.GetOrganizationMetadata(ctx, org.ID)
	require.NoError(t, err)
	require.Equal(t, string(billing.TierBase), persisted.GramAccountType, "proxy tier resolution must not reconcile metadata")
}
