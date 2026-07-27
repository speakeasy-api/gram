package customdomains_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	cdrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

func TestListActivatedCustomDomainResourcesReportsEligibleRootMapping(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		state    string
		expected bool
	}{
		{
			name:     "live root",
			state:    "live",
			expected: true,
		},
		{
			name:     "disabled parent",
			state:    "disabled",
			expected: false,
		},
		{
			name:     "deleted endpoint",
			state:    "deleted",
			expected: false,
		},
		{
			name:     "cleared root",
			state:    "cleared",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestCustomDomainsService(t)
			authCtx := testAuthContext(t, ctx)
			domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
				OrganizationID:  authCtx.ActiveOrganizationID,
				Domain:          "resource-" + uuid.NewString() + ".example.com",
				IngressName:     pgtype.Text{String: "", Valid: false},
				CertSecretName:  pgtype.Text{String: "", Valid: false},
				ProvisionerKind: "ingress",
				IpAllowlist:     []string{},
			})
			require.NoError(t, err)
			_, err = ti.repo.UpdateCustomDomain(ctx, cdrepo.UpdateCustomDomainParams{
				Verified:        true,
				Activated:       true,
				IngressName:     pgtype.Text{String: "resource-" + uuid.NewString(), Valid: true},
				CertSecretName:  pgtype.Text{String: "", Valid: false},
				ProvisionerKind: "ingress",
				ID:              domain.ID,
			})
			require.NoError(t, err)

			serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
			endpoint, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
				ProjectID:      *authCtx.ProjectID,
				CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true},
				McpServerID:    serverID,
				Slug:           "root-" + uuid.NewString(),
			})
			require.NoError(t, err)
			require.NoError(t, ti.repo.SetRootMcpEndpoint(ctx, cdrepo.SetRootMcpEndpointParams{
				McpEndpointID:  endpoint.ID,
				CustomDomainID: domain.ID,
			}))

			switch tc.state {
			case "live":
			case "disabled":
				server, getErr := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
					ID:        serverID,
					ProjectID: *authCtx.ProjectID,
				})
				require.NoError(t, getErr)
				_, err = mcpserversrepo.New(ti.conn).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{
					Name:                  server.Name,
					Slug:                  server.Slug,
					EnvironmentID:         server.EnvironmentID,
					UserSessionIssuerID:   server.UserSessionIssuerID,
					RemoteMcpServerID:     server.RemoteMcpServerID,
					TunneledMcpServerID:   server.TunneledMcpServerID,
					ToolsetID:             server.ToolsetID,
					ToolVariationsGroupID: server.ToolVariationsGroupID,
					Visibility:            "disabled",
					ID:                    server.ID,
					ProjectID:             server.ProjectID,
				})
				require.NoError(t, err)
			case "deleted":
				_, err = mcpendpointsrepo.New(ti.conn).DeleteMCPEndpoint(ctx, mcpendpointsrepo.DeleteMCPEndpointParams{
					ID:        endpoint.ID,
					ProjectID: *authCtx.ProjectID,
				})
				require.NoError(t, err)
			case "cleared":
				require.NoError(t, ti.repo.ClearRootMcpEndpoint(ctx, domain.ID))
			default:
				require.Failf(t, "unknown eligibility state", "state: %s", tc.state)
			}

			resources, err := ti.repo.ListActivatedCustomDomainResources(ctx)
			require.NoError(t, err)
			for _, resource := range resources {
				if resource.ID == domain.ID {
					require.Equal(t, tc.expected, resource.HasRootMapping)
					return
				}
			}
			require.Fail(t, "activated custom domain resource not found")
		})
	}
}

func TestListActivatedCustomDomainResourcesIgnoresSoftDeletedDomains(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "deleted-resource-" + uuid.NewString() + ".example.com",
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	_, err = ti.repo.UpdateCustomDomain(ctx, cdrepo.UpdateCustomDomainParams{
		Verified:        true,
		Activated:       true,
		IngressName:     pgtype.Text{String: "deleted-resource-" + uuid.NewString(), Valid: true},
		ProvisionerKind: "ingress",
		ID:              domain.ID,
	})
	require.NoError(t, err)
	require.NoError(t, ti.repo.DeleteCustomDomain(ctx, authCtx.ActiveOrganizationID))

	resources, err := ti.repo.ListActivatedCustomDomainResources(ctx)
	require.NoError(t, err)
	for _, resource := range resources {
		require.NotEqual(t, domain.ID, resource.ID)
	}
}
