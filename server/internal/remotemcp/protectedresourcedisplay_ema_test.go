package remotemcp_test

import (
	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestUpdateServer_EMABindingBlocksResourceIdentityChange(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))
	auth, _ := contextvalues.GetAuthContext(ctx)
	upstream, _ := launchResourceMetadata(t, "Example MCP", "https://docs.example.test/mcp", "")
	server := seedRemoteMcpServerWithURL(t, ctx, ti, "https://old.example.test")
	attached := seedAttachedResource(t, ctx, ti, server, server.Url)
	q := remoterepo.New(ti.conn)
	key := remoterepo.GetEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: attached.userSessionIssuerID, RemoteSessionIssuerID: attached.issuer.ID, Resource: server.Url}
	require.NoError(t, q.EnsureEMABinding(ctx, remoterepo.EnsureEMABindingParams(key)))
	b, err := q.GetEMABinding(ctx, key)
	require.NoError(t, err)
	b, err = q.SetEMABinding(ctx, remoterepo.SetEMABindingParams{ID: b.ID, ProjectID: b.ProjectID, OrganizationID: b.OrganizationID, ExpectedGeneration: b.Generation, Generation: b.Generation, RemoteSessionClientID: conv.ToNullUUID(attached.client.ID), State: conv.ToPGText("unknown_grants"), GrantSource: conv.ToPGText("unknown"), RequestedScopes: []string{}})
	require.NoError(t, err)
	_, err = ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{ID: server.ID.String(), URL: conv.PtrEmpty(upstream.URL)})
	var shared *oops.ShareableError
	require.ErrorAs(t, err, &shared)
	require.Equal(t, oops.CodeConflict, shared.Code)
	stored, err := repo.New(ti.conn).GetServerByID(ctx, repo.GetServerByIDParams{ID: server.ID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	require.Equal(t, server.Url, stored.Url)
	require.Equal(t, server.Url, loadClient(t, ctx, ti, attached.client).ResourceIdentifier.String)
	_, err = q.SetEMABinding(ctx, remoterepo.SetEMABindingParams{ID: b.ID, ProjectID: b.ProjectID, OrganizationID: b.OrganizationID, ExpectedGeneration: b.Generation, Generation: b.Generation + 1, State: conv.ToPGText("unlinked"), GrantSource: conv.ToPGText("unknown"), RequestedScopes: []string{}})
	require.NoError(t, err)
	updateServerURL(t, ctx, ti, server, upstream.URL)
	require.Equal(t, upstream.URL, loadClient(t, ctx, ti, attached.client).ResourceIdentifier.String)
}
