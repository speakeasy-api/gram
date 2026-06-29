package tunnelledmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/tunnelled_mcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/tunnelledmcp/repo"
)

func TestListAndGetServersAllowProjectScopedMCPReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunnelledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	_, err := ti.service.ListServers(authztest.WithExactGrants(t, ctx), &gen.ListServersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	readCtx := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPRead, *authCtx.ProjectID))
	listResult, err := ti.service.ListServers(readCtx, &gen.ListServersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, listResult.TunnelledMcpServers, 1)
	require.Equal(t, server.ID.String(), listResult.TunnelledMcpServers[0].ID)

	getResult, err := ti.service.GetServer(readCtx, &gen.GetServerPayload{
		ID:               server.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, server.ID.String(), getResult.ID)
}

func TestCreateServerAllowsProjectScopedMCPWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)

	_, err := ti.service.CreateServer(authztest.WithExactGrants(t, ctx), &gen.CreateServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             "created",
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	writeCtx := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPWrite, *authCtx.ProjectID))
	result, err := ti.service.CreateServer(writeCtx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             "created",
	})
	require.NoError(t, err)
	require.NotNil(t, result.Server)
	require.NotEmpty(t, result.TunnelKey)
	require.Equal(t, "created", result.Server.Name)
}

func TestMutatingServerMethodsAllowProjectScopedMCPWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunnelledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	_, err := ti.service.UpdateServer(authztest.WithExactGrants(t, ctx), &gen.UpdateServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               server.ID.String(),
		Name:             "renamed",
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	writeCtx := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPWrite, *authCtx.ProjectID))
	updated, err := ti.service.UpdateServer(writeCtx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               server.ID.String(),
		Name:             "renamed",
	})
	require.NoError(t, err)
	require.Equal(t, "renamed", updated.Name)

	rotated, err := ti.service.RotateServerKey(writeCtx, &gen.RotateServerKeyPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               server.ID.String(),
	})
	require.NoError(t, err)
	require.NotNil(t, rotated.Server)
	require.NotEmpty(t, rotated.TunnelKey)

	require.NoError(t, ti.service.DeleteServer(writeCtx, &gen.DeleteServerPayload{
		ID:               server.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))
}

func seedTunnelledMcpServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) repo.TunnelledMcpServer {
	t.Helper()

	server, err := repo.New(conn).CreateServer(ctx, repo.CreateServerParams{
		ID:        uuid.New(),
		ProjectID: projectID,
		Name:      "tunnel-" + uuid.NewString(),
		KeyHash:   "hash-" + uuid.NewString(),
		KeyPrefix: "gram_test_" + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	return server
}
