package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest,tparallel // Access subtests share the lifecycle fixture's mutable auth context and must remain sequential.
func TestListServerClientAssociationsGitHubAppMetadata(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, auth.ProjectID)
	projectID, orgID := *auth.ProjectID, auth.ActiveOrganizationID

	// This arbitrary HTTPS upstream has no GitHub-specific hostname or path.
	fixtures := testrepo.New(ti.conn)
	remoteID, serverID := uuid.New(), uuid.New()
	require.NoError(t, fixtures.CreateRemoteMCPBackendFixture(ctx, testrepo.CreateRemoteMCPBackendFixtureParams{
		ID: remoteID, ProjectID: projectID, Url: "https://tools.example.net/arbitrary-api",
	}))
	require.NoError(t, fixtures.CreateRemoteBackedMCPServerFixture(ctx, testrepo.CreateRemoteBackedMCPServerFixtureParams{
		ID: serverID, ProjectID: projectID, Slug: conv.ToPGText("github-app-metadata"),
		RemoteMcpServerID: conv.ToNullUUID(remoteID), Visibility: "private",
	}))
	queries := authclientsrepo.New(ti.conn)
	client, err := queries.CreateGitHubAppAuthClient(ctx, authclientsrepo.CreateGitHubAppAuthClientParams{
		OrganizationID: orgID, ProjectID: conv.ToNullUUID(projectID), Name: "Metadata fixture",
	})
	require.NoError(t, err)
	require.NoError(t, queries.SetGitHubAppCredential(ctx, authclientsrepo.SetGitHubAppCredentialParams{
		AuthClientID: client.ID, OrganizationID: orgID, InstanceUrl: "https://git.example.net",
		AppID: 123, InstallationID: 456, PrivateKeyEncrypted: "TEST_ENCRYPTED_PRIVATE_KEY",
		UpstreamOrigin: "https://tools.example.net",
	}))
	require.NoError(t, queries.AttachGitHubAppAuthClient(ctx, authclientsrepo.AttachGitHubAppAuthClientParams{
		ProjectID: projectID, McpServerID: serverID, AuthClientID: client.ID, OrganizationID: orgID,
	}))

	// The metadata belongs to the server read surface, not org:read.
	reader := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, serverID.String()))
	assertMetadata := func(bound, enabled, required bool) {
		t.Helper()
		result, err := ti.service.ListServerClientAssociations(reader, &gen.ListServerClientAssociationsPayload{McpServerID: serverID.String()})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, bound, result.GithubAppBound, "bound")
		require.Equal(t, enabled, result.GithubAppEnabled, "enabled")
		require.Equal(t, required, result.GithubAppRequired, "required")
	}
	assertMetadata(true, true, true)
	_, err = queries.UpdateAuthClient(ctx, authclientsrepo.UpdateAuthClientParams{
		ID: client.ID, OrganizationID: orgID, Name: client.Name, Enabled: false,
	})
	require.NoError(t, err)
	assertMetadata(true, false, true)
	_, err = queries.DetachAuthClientFromServer(ctx, authclientsrepo.DetachAuthClientFromServerParams{
		AuthClientID: client.ID, McpServerID: serverID, OrganizationID: orgID,
	})
	require.NoError(t, err)
	assertMetadata(false, false, true)

	issuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "github-metadata-replacement")
	replacementID, _ := seedActiveClient(t, ctx, ti.conn, projectID, issuerID, orgID, "github-metadata-oauth")
	require.Equal(t, int64(1), associate(t, ctx, ti.conn, orgID, projectID, serverID, replacementID))
	recordTypedClients(t, ctx, ti.conn, orgID, serverID)
	assertMetadata(false, false, false)

	t.Run("no MCP read", func(t *testing.T) {
		denied := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgRead, orgID))
		result, err := ti.service.ListServerClientAssociations(denied, &gen.ListServerClientAssociationsPayload{McpServerID: serverID.String()})
		requireOopsCode(t, err, oops.CodeForbidden)
		require.Nil(t, result, "denied callers receive no binding metadata")
	})
	t.Run("different server grant", func(t *testing.T) {
		denied := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, uuid.NewString()))
		result, err := ti.service.ListServerClientAssociations(denied, &gen.ListServerClientAssociationsPayload{McpServerID: serverID.String()})
		requireOopsCode(t, err, oops.CodeForbidden)
		require.Nil(t, result)
	})
	t.Run("missing server", func(t *testing.T) {
		missing := uuid.New()
		missingReader := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, missing.String()))
		result, err := ti.service.ListServerClientAssociations(missingReader, &gen.ListServerClientAssociationsPayload{McpServerID: missing.String()})
		requireOopsCode(t, err, oops.CodeNotFound)
		require.Nil(t, result, "missing servers receive no binding metadata")
	})
	t.Run("foreign server", func(t *testing.T) {
		foreignOrg := createOrganization(t, ctx, ti.conn, "github-metadata-foreign")
		foreignServer := seedMCPServerInOrg(t, ctx, ti.conn, foreignOrg, "github-metadata-foreign")
		foreignReader := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, foreignServer.String()))
		result, err := ti.service.ListServerClientAssociations(foreignReader, &gen.ListServerClientAssociationsPayload{McpServerID: foreignServer.String()})
		requireOopsCode(t, err, oops.CodeNotFound)
		require.Nil(t, result, "foreign servers receive no binding metadata")
	})
}
