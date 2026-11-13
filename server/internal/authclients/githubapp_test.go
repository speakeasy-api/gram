package authclients_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/stretchr/testify/require"
)

func TestGitHubAppConfiguration(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgAdmin)
	server := seedServer(t, ctx, ti.conn, ti.projectID, "private")
	enc, err := encryption.New(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))))
	require.NoError(t, err)
	calls := 0
	reject := false
	changeTarget := false
	authclients.SetGitHubAppTestVerifier(ti.service, enc, func(_ context.Context, instance string, app, installation int64, key string) error {
		calls++
		if calls == 1 {
			require.Equal(t, "TEST_PRIVATE_KEY_ONE", key)
		} else {
			require.Equal(t, "TEST_PRIVATE_KEY_TWO", key)
		}
		require.Equal(t, "https://git.example.com", instance)
		require.EqualValues(t, 123, app)
		require.EqualValues(t, 456, installation)
		if reject {
			return errors.New("TEST_SECRET_REJECTED")
		}
		if changeTarget {
			q := remoterepo.New(ti.conn)
			remote, err := q.GetServerByID(ctx, remoterepo.GetServerByIDParams{ID: server.remoteID, ProjectID: ti.projectID})
			require.NoError(t, err)
			_, err = q.UpdateServer(ctx, remoterepo.UpdateServerParams{ID: server.remoteID, ProjectID: ti.projectID, Name: remote.Name, Slug: remote.Slug, TransportType: remote.TransportType, Url: "https://changed.example.com/mcp"})
			require.NoError(t, err)
		}
		return nil
	})
	payload := &gen.ConfigureGitHubAppAuthClientPayload{McpServerID: server.id.String(), Name: strings.Repeat("界", 101), InstanceURL: "https://git.example.com", AppID: "123", InstallationID: "456", PrivateKey: "TEST_PRIVATE_KEY_ONE", UpstreamOrigin: "https://upstream.example.com"}
	_, err = ti.service.ConfigureGitHubAppAuthClient(ctx, payload)
	var invalid *oops.ShareableError
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, oops.CodeBadRequest, invalid.Code)
	require.Zero(t, calls, "overlong names are rejected before verification")
	payload.Name = strings.Repeat("界", 100)
	created, err := ti.service.ConfigureGitHubAppAuthClient(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, payload.Name, created.Name)
	require.Equal(t, "service_account", created.ClientType)
	require.Nil(t, created.RemoteSessionClientID)
	require.NotNil(t, created.GithubApp)
	require.Equal(t, "456", created.GithubApp.InstallationID)
	raw, err := json.Marshal(created) //nolint:musttag // Check the generated service result for secret leakage; HTTP transport owns its JSON tags.
	require.NoError(t, err)
	require.NotContains(t, string(raw), "TEST_PRIVATE_KEY")
	queries := repo.New(ti.conn)
	stored, err := queries.GetGitHubAppCredential(ctx, repo.GetGitHubAppCredentialParams{AuthClientID: uuid.MustParse(created.ID), OrganizationID: ti.organizationID, McpServerID: server.id})
	require.NoError(t, err)
	require.NotEqual(t, payload.PrivateKey, stored.PrivateKeyEncrypted)
	plain, err := enc.Decrypt(stored.PrivateKeyEncrypted)
	require.NoError(t, err)
	require.Equal(t, payload.PrivateKey, plain)
	version := stored.CredentialVersion
	payload.ID = &created.ID
	payload.PrivateKey = "TEST_PRIVATE_KEY_TWO"
	rotated, err := ti.service.ConfigureGitHubAppAuthClient(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, created.ID, rotated.ID)
	stored, err = queries.GetGitHubAppCredential(ctx, repo.GetGitHubAppCredentialParams{AuthClientID: uuid.MustParse(created.ID), OrganizationID: ti.organizationID, McpServerID: server.id})
	require.NoError(t, err)
	require.NotEqual(t, version, stored.CredentialVersion)
	version = stored.CredentialVersion
	reject = true
	_, err = ti.service.ConfigureGitHubAppAuthClient(ctx, payload)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "TEST_SECRET_REJECTED")
	stored, err = queries.GetGitHubAppCredential(ctx, repo.GetGitHubAppCredentialParams{AuthClientID: uuid.MustParse(created.ID), OrganizationID: ti.organizationID, McpServerID: server.id})
	require.NoError(t, err)
	require.Equal(t, version, stored.CredentialVersion)
	require.Equal(t, 3, calls)
	payload.McpServerID = uuid.NewString()
	_, err = ti.service.ConfigureGitHubAppAuthClient(ctx, payload)
	require.Error(t, err)
	require.Equal(t, 3, calls, "unknown target is rejected before verification")
	payload.McpServerID = server.id.String()
	denied := withOrgGrants(t, ctx, authz.ScopeOrgRead)
	_, err = ti.service.ConfigureGitHubAppAuthClient(denied, payload)
	require.Error(t, err)
	require.Equal(t, 3, calls, "permission is checked before verification")
	reject = false
	changeTarget = true
	_, err = ti.service.ConfigureGitHubAppAuthClient(ctx, payload)
	require.ErrorContains(t, err, "destination changed")
	changeTarget = false
	payload.UpstreamOrigin = "https://changed.example.com"
	_, err = ti.service.ConfigureGitHubAppAuthClient(ctx, payload)
	require.ErrorContains(t, err, "separate GitHub App")
	stored, err = queries.GetGitHubAppCredential(ctx, repo.GetGitHubAppCredentialParams{AuthClientID: uuid.MustParse(created.ID), OrganizationID: ti.organizationID, McpServerID: server.id})
	require.NoError(t, err)
	require.Equal(t, version, stored.CredentialVersion)
	require.Equal(t, "https://upstream.example.com", stored.UpstreamOrigin)

}

func TestGitHubAppConfigurationLookupErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                           string
		wantCode                       oops.Code
		missingTarget                  bool
		removeTargetDuringVerification bool
		missingClient                  bool
		closedPool                     bool
	}{
		{name: "missing target", missingTarget: true, wantCode: oops.CodeNotFound},
		{name: "target removed during verification", removeTargetDuringVerification: true, wantCode: oops.CodeNotFound},
		{name: "missing auth client", missingClient: true, wantCode: oops.CodeNotFound},
		{name: "destination read failure", closedPool: true, wantCode: oops.CodeUnexpected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			ctx = withOrgGrants(t, ctx, authz.ScopeOrgAdmin)
			server := seedServer(t, ctx, ti.conn, ti.projectID, "private")
			calls := 0
			authclients.SetGitHubAppTestVerifier(ti.service, ti.enc, func(context.Context, string, int64, int64, string) error {
				calls++
				if tc.removeTargetDuringVerification {
					_, err := remoterepo.New(ti.conn).DeleteServer(ctx, remoterepo.DeleteServerParams{ID: server.remoteID, ProjectID: ti.projectID})
					require.NoError(t, err)
				}
				return nil
			})
			payload := &gen.ConfigureGitHubAppAuthClientPayload{
				McpServerID: server.id.String(), Name: "Fixture bot", InstanceURL: "https://git.example.com",
				AppID: "123", InstallationID: "456", PrivateKey: "TEST_PRIVATE_KEY", UpstreamOrigin: "https://upstream.example.com",
			}
			if tc.missingTarget {
				payload.McpServerID = uuid.NewString()
			}
			if tc.missingClient {
				id := uuid.NewString()
				payload.ID = &id
			}
			if tc.closedPool {
				ti.conn.Close()
			}
			_, err := ti.service.ConfigureGitHubAppAuthClient(ctx, payload)
			var shared *oops.ShareableError
			require.ErrorAs(t, err, &shared)
			require.Equal(t, tc.wantCode, shared.Code)
			if tc.missingTarget || tc.closedPool {
				require.Zero(t, calls, "target lookup fails before verification")
			} else {
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestGitHubAppAttachmentUpsertKeepsHistory(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	server := seedServer(t, ctx, ti.conn, ti.projectID, "private")
	queries := repo.New(ti.conn)
	client, err := queries.CreateGitHubAppAuthClient(ctx, repo.CreateGitHubAppAuthClientParams{
		OrganizationID: ti.organizationID, ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true}, Name: "Fixture bot",
	})
	require.NoError(t, err)
	attachment := repo.AttachGitHubAppAuthClientParams{
		McpServerID: server.id, AuthClientID: client.ID, OrganizationID: ti.organizationID, ProjectID: ti.projectID,
	}
	require.NoError(t, queries.AttachGitHubAppAuthClient(ctx, attachment))
	require.NoError(t, queries.AttachGitHubAppAuthClient(ctx, attachment))
	var live, retired int
	counts := func() {
		err := ti.conn.QueryRow(ctx, //nolint:glint // notestingrawsql: attachment history counts are the invariant under test; application queries expose only live links.
			`SELECT count(*) FILTER (WHERE NOT deleted), count(*) FILTER (WHERE deleted)
			FROM mcp_server_auth_clients WHERE mcp_server_id = $1 AND auth_client_id = $2`, server.id, client.ID).Scan(&live, &retired)
		require.NoError(t, err)
	}
	counts()
	require.Equal(t, 1, live, "repeated attachment updates the live row")
	require.Zero(t, retired)
	n, err := queries.DetachAuthClientFromServer(ctx, repo.DetachAuthClientFromServerParams{
		McpServerID: server.id, AuthClientID: client.ID, OrganizationID: ti.organizationID,
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, n.ID)
	require.Equal(t, ti.projectID, n.ProjectID)
	require.NoError(t, queries.AttachGitHubAppAuthClient(ctx, attachment))
	counts()
	require.Equal(t, 1, live)
	require.Equal(t, 1, retired, "reattachment creates a new row instead of reviving history")

	locked, err := queries.LockGitHubAppServerURL(ctx, repo.LockGitHubAppServerURLParams{
		McpServerID: server.id, OrganizationID: ti.organizationID,
	})
	require.NoError(t, err)
	require.Equal(t, server.id, locked.ID)
	require.Equal(t, ti.projectID, locked.ProjectID)
	require.Equal(t, "https://upstream.example.com/"+server.remoteID.String(), locked.Url)
}
