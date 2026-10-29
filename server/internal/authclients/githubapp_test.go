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
	payload := &gen.ConfigureGitHubAppAuthClientPayload{McpServerID: server.id.String(), Name: "Fixture bot", InstanceURL: "https://git.example.com", AppID: "123", InstallationID: "456", PrivateKey: "TEST_PRIVATE_KEY_ONE", UpstreamOrigin: "https://upstream.example.com"}
	created, err := ti.service.ConfigureGitHubAppAuthClient(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "service_account", created.ClientType)
	require.Nil(t, created.RemoteSessionClientID)
	require.NotNil(t, created.GithubApp)
	require.Equal(t, "456", created.GithubApp.InstallationID)
	raw, err := json.Marshal(created) //nolint:musttag // Check the generated service result for secret leakage; HTTP transport owns its JSON tags.
	require.NoError(t, err)
	require.NotContains(t, string(raw), "TEST_PRIVATE_KEY")
	queries := repo.New(ti.conn)
	stored, err := queries.GetGitHubAppCredential(ctx, repo.GetGitHubAppCredentialParams{AuthClientID: uuid.MustParse(created.ID), OrganizationID: ti.organizationID})
	require.NoError(t, err)
	require.NotEqual(t, payload.PrivateKey, stored.PrivateKeyEncrypted)
	plain, err := enc.Decrypt(stored.PrivateKeyEncrypted)
	require.NoError(t, err)
	require.Equal(t, payload.PrivateKey, plain)
	createdAt, updatedAt := stored.CreatedAt, stored.UpdatedAt
	require.Equal(t, "service_account", stored.ClientType)
	version := stored.CredentialVersion
	payload.ID = &created.ID
	payload.PrivateKey = "TEST_PRIVATE_KEY_TWO"
	rotated, err := ti.service.ConfigureGitHubAppAuthClient(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, created.ID, rotated.ID)
	stored, err = queries.GetGitHubAppCredential(ctx, repo.GetGitHubAppCredentialParams{AuthClientID: uuid.MustParse(created.ID), OrganizationID: ti.organizationID})
	require.NoError(t, err)
	require.Equal(t, createdAt, stored.CreatedAt)
	require.True(t, stored.UpdatedAt.Time.After(updatedAt.Time))
	require.NotEqual(t, version, stored.CredentialVersion)
	version = stored.CredentialVersion
	reject = true
	_, err = ti.service.ConfigureGitHubAppAuthClient(ctx, payload)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "TEST_SECRET_REJECTED")
	stored, err = queries.GetGitHubAppCredential(ctx, repo.GetGitHubAppCredentialParams{AuthClientID: uuid.MustParse(created.ID), OrganizationID: ti.organizationID})
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
	stored, err = queries.GetGitHubAppCredential(ctx, repo.GetGitHubAppCredentialParams{AuthClientID: uuid.MustParse(created.ID), OrganizationID: ti.organizationID})
	require.NoError(t, err)
	require.Equal(t, version, stored.CredentialVersion)
	require.Equal(t, "https://upstream.example.com", stored.UpstreamOrigin)

}
