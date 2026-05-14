package customdomains_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// pgTextValid is a terse wrapper around conv.ToPGText for test fixtures.
func pgTextValid(s string) pgtype.Text {
	return conv.ToPGText(s)
}

// seedProject creates a second project in the given organization so the
// cascade tests can exercise cross-project soft-delete behaviour.
func seedProject(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	slug := "cross-" + uuid.New().String()[:8]
	row, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	return row.ID
}

// seedMcpServer inserts a remote_mcp_server + mcp_server pair so the
// endpoint fixtures have a valid mcp_server_id FK to point at.
func seedMcpServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	remote := remotemcptest.SeedServer(t, ctx, conn, remotemcprepo.CreateServerParams{
		ProjectID:     projectID,
		TransportType: "streamable-http",
		Url:           "https://test.example.com/mcp/" + uuid.NewString(),
	})

	row, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ProjectID:             projectID,
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExternalOauthServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OauthProxyServerID:    uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     uuid.NullUUID{UUID: remote.ID, Valid: true},
		ToolsetID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            "disabled",
	})
	require.NoError(t, err)
	return row.ID
}

// seedMcpEndpoint inserts an mcp_endpoint pointing at a freshly created
// mcp_server inside the given project and bound to the given custom domain.
// Returns the endpoint id.
func seedMcpEndpoint(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, customDomainID uuid.UUID, slug string) uuid.UUID {
	t.Helper()

	serverID := seedMcpServer(t, ctx, conn, projectID)
	row, err := mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      projectID,
		CustomDomainID: uuid.NullUUID{UUID: customDomainID, Valid: true},
		McpServerID:    serverID,
		Slug:           slug,
	})
	require.NoError(t, err)
	return row.ID
}
