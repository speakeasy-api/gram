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
	authclients.SetGitHubAppTestVerifier(ti.service, enc, func(_ context.Context, instance string, app, installation int64, key string) error {
		calls++
		require.Equal(t, "https://git.example.com", instance)
		require.EqualValues(t, 123, app)
		require.EqualValues(t, 456, installation)
		if reject {
			return errors.New("TEST_SECRET_REJECTED")
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
	raw, err := json.Marshal(created)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "TEST_PRIVATE_KEY")
	queries := repo.New(ti.conn)
	stored, err := queries.GetGitHubAppCredential(ctx, repo.GetGitHubAppCredentialParams{AuthClientID: uuid.MustParse(created.ID), OrganizationID: ti.organizationID})
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
	stored, err = queries.GetGitHubAppCredential(ctx, repo.GetGitHubAppCredentialParams{AuthClientID: uuid.MustParse(created.ID), OrganizationID: ti.organizationID})
	require.NoError(t, err)
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
}
