package mcpendpoints_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
)

func TestMcpEndpointDomainRootRequiresCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	endpoint, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:    uuid.NullUUID{UUID: serverID, Valid: true},
		Slug:           authCtx.OrganizationSlug + "-root-check",
	})
	require.NoError(t, err)

	_, err = mcpendpointsrepo.New(ti.conn).UpdateMCPEndpoint(ctx, mcpendpointsrepo.UpdateMCPEndpointParams{
		CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:    uuid.NullUUID{UUID: serverID, Valid: true},
		Slug:           endpoint.Slug,
		IsDomainRoot:   pgtype.Bool{Bool: true, Valid: true},
		ID:             endpoint.ID,
		ProjectID:      *authCtx.ProjectID,
	})
	requirePgErrorCode(t, err, pgerrcode.CheckViolation)
}

func TestMcpEndpointDomainRootUniquePerDomainButIndependentAcrossDomains(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	repository := customdomainsrepo.New(ti.conn)
	firstDomain, err := repository.CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "root-unique-first.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	secondDomain, err := repository.CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: "root-unique-second-organization",
		Domain:         "root-unique-second.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	first := seedSchemaEndpoint(t, ctx, ti, *authCtx.ProjectID, firstDomain.ID, "first")
	conflicting := seedSchemaEndpoint(t, ctx, ti, *authCtx.ProjectID, firstDomain.ID, "conflicting")
	independent := seedSchemaEndpoint(t, ctx, ti, *authCtx.ProjectID, secondDomain.ID, "independent")

	require.NoError(t, repository.SetRootMcpEndpoint(ctx, customdomainsrepo.SetRootMcpEndpointParams{
		McpEndpointID:  first,
		CustomDomainID: firstDomain.ID,
	}))
	err = repository.SetRootMcpEndpoint(ctx, customdomainsrepo.SetRootMcpEndpointParams{
		McpEndpointID:  conflicting,
		CustomDomainID: firstDomain.ID,
	})
	requirePgErrorCode(t, err, pgerrcode.UniqueViolation)
	require.NoError(t, repository.SetRootMcpEndpoint(ctx, customdomainsrepo.SetRootMcpEndpointParams{
		McpEndpointID:  independent,
		CustomDomainID: secondDomain.ID,
	}))
}

func TestMcpEndpointSoftDeletedRootFreesDomainSlot(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	repository := customdomainsrepo.New(ti.conn)
	domain, err := repository.CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "root-soft-delete.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	first := seedSchemaEndpoint(t, ctx, ti, *authCtx.ProjectID, domain.ID, "first")
	replacement := seedSchemaEndpoint(t, ctx, ti, *authCtx.ProjectID, domain.ID, "replacement")

	require.NoError(t, repository.SetRootMcpEndpoint(ctx, customdomainsrepo.SetRootMcpEndpointParams{
		McpEndpointID:  first,
		CustomDomainID: domain.ID,
	}))
	_, err = mcpendpointsrepo.New(ti.conn).DeleteMCPEndpoint(ctx, mcpendpointsrepo.DeleteMCPEndpointParams{
		ID:        first,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.NoError(t, repository.SetRootMcpEndpoint(ctx, customdomainsrepo.SetRootMcpEndpointParams{
		McpEndpointID:  replacement,
		CustomDomainID: domain.ID,
	}))
}

func seedSchemaEndpoint(t *testing.T, ctx context.Context, ti *testInstance, projectID, domainID uuid.UUID, slug string) uuid.UUID {
	t.Helper()

	endpoint, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      projectID,
		CustomDomainID: uuid.NullUUID{UUID: domainID, Valid: true},
		McpServerID:    uuid.NullUUID{UUID: seedMcpServer(t, ctx, ti.conn, projectID), Valid: true},
		Slug:           slug,
	})
	require.NoError(t, err)
	return endpoint.ID
}

func requirePgErrorCode(t *testing.T, err error, code string) {
	t.Helper()

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, code, pgErr.Code)
}
