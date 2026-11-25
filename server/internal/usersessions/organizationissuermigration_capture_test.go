package usersessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func seedHostedServerOnIssuer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, organizationID string, issuerID uuid.UUID, slug string) uuid.UUID {
	t.Helper()

	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: organizationID, ProjectID: projectID,
		Name: slug, Slug: slug, Description: pgtype.Text{}, DefaultEnvironmentSlug: pgtype.Text{},
	})
	require.NoError(t, err)
	serverID := uuid.New()
	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: serverID, ProjectID: projectID,
		Name: pgtype.Text{String: slug, Valid: true}, Slug: pgtype.Text{String: slug, Valid: true},
		EnvironmentID: uuid.NullUUID{}, UserSessionIssuerID: conv.ToNullUUID(issuerID), RemoteMcpServerID: uuid.NullUUID{},
		ToolsetID: conv.ToNullUUID(toolset.ID), ToolVariationsGroupID: uuid.NullUUID{}, Visibility: mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)
	return serverID
}
