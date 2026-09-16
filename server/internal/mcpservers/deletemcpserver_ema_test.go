package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

func TestDeleteMcpServer_LastOwnerPreservesActiveEMABinding(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	backend := seedRemoteMcpServer(t, ctx, ti.conn, *auth.ProjectID).String()
	server, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{Name: "ema owner", RemoteMcpServerID: &backend, Visibility: types.McpServerVisibility("disabled")})
	require.NoError(t, err)
	require.NotNil(t, server.UserSessionIssuerID)
	userID := uuid.MustParse(*server.UserSessionIssuerID)
	q := remoterepo.New(ti.conn)
	issuer, err := q.CreateRemoteSessionIssuer(ctx, remoterepo.CreateRemoteSessionIssuerParams{ProjectID: conv.ToNullUUID(*auth.ProjectID), Slug: "ema-resource", Issuer: "https://authorization.example.com", ScopesSupported: []string{}, GrantTypesSupported: []string{}, ResponseTypesSupported: []string{}, TokenEndpointAuthMethodsSupported: []string{}})
	require.NoError(t, err)
	key := remoterepo.GetEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: userID, RemoteSessionIssuerID: issuer.ID, Resource: "https://resource.example.com/"}
	require.NoError(t, q.EnsureEMABinding(ctx, remoterepo.EnsureEMABindingParams(key)))
	err = ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{ID: server.ID})
	var shared *oops.ShareableError
	require.ErrorAs(t, err, &shared)
	require.Equal(t, oops.CodeConflict, shared.Code)
	_, err = mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: uuid.MustParse(server.ID), ProjectID: *auth.ProjectID})
	require.NoError(t, err, "the failed cascade rolls back server deletion")
	_, err = usersessionsrepo.New(ti.conn).GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{ID: userID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	binding, err := q.GetEMABinding(ctx, key)
	require.NoError(t, err)
	_, err = q.SetEMABinding(ctx, remoterepo.SetEMABindingParams{ID: binding.ID, ProjectID: binding.ProjectID, OrganizationID: binding.OrganizationID, Generation: binding.Generation + 1, ExpectedGeneration: binding.Generation, State: "unlinked", GrantSource: "unknown", RequestedScopes: []string{}})
	require.NoError(t, err)
	require.NoError(t, ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{ID: server.ID}))
	_, err = usersessionsrepo.New(ti.conn).GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{ID: userID, ProjectID: *auth.ProjectID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = q.GetEMABinding(ctx, key)
	require.ErrorIs(t, err, pgx.ErrNoRows, "successful orphan cleanup removes unlinked claims")
}
