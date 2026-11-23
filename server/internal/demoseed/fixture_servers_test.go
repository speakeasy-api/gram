//go:build demoseed_safety

package demoseed

import (
	"crypto/rand"
	"encoding/base64"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/demoseed/fixture/fixtureauth"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// Every organization the seed writes points the OAuth, shared-token and
// generated servers at the MCP fixture the Speakeasy server hosts, and stores
// tokens the fixture accepts, on every reseed.
func TestFixtureServersPointAtHostedFixture(t *testing.T) {
	t.Parallel()
	for _, spec := range []Spec{DefaultSpec(), LocalSpec()} {
		t.Run(spec.OrgSlug, func(t *testing.T) {
			t.Parallel()
			testFixtureServersPointAtHostedFixture(t, spec)
		})
	}
}

func testFixtureServersPointAtHostedFixture(t *testing.T, spec Spec) {
	t.Helper()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	key := make([]byte, 32)
	_, err = rand.Read(key)
	require.NoError(t, err)
	enc, err := encryption.New(base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)
	serverURL, err := url.Parse(testServerURL)
	require.NoError(t, err)
	base := fixtureauth.BaseURL(serverURL)
	require.Equal(t, testServerURL+"/rpc/demoseed", base)

	s := &seeder{spec: spec}
	q := testrepo.New(db)
	for range 2 {
		_, err := seedPostgres(ctx, testenv.NewLogger(t), db, assetstest.NewTestBlobStore(t), enc, testAssistantIssuerURL, testServerURL, spec)
		require.NoError(t, err)

		oauth, err := q.GetRemoteMCPServerURLFixture(ctx, s.det("remotemcp-linear"))
		require.NoError(t, err)
		require.Equal(t, "Example OAuth", oauth.Name.String)
		require.Equal(t, fixtureauth.OAuthResource(base), oauth.Url)

		serviceAccount, err := q.GetRemoteMCPServerURLFixture(ctx, s.det("remotemcp-slack"))
		require.NoError(t, err)
		require.Equal(t, "Example Shared Credential", serviceAccount.Name.String)
		require.Equal(t, base+fixtureauth.PathSharedMCP, serviceAccount.Url)
		static, err := q.GetRemoteMCPServerHeaderFixture(ctx, s.det("agent-identity-header-slack"))
		require.NoError(t, err)
		require.Equal(t, "Bearer "+fixtureauth.SharedToken, static.Value.String)
		require.False(t, static.IsSecret, "a secret header must hold ciphertext; this one holds the checked-in token")

		header, err := q.GetAuthClientHeaderFixture(ctx, s.det("auth-client-github-token-header"))
		require.NoError(t, err)
		require.Equal(t, "Bearer "+fixtureauth.SharedToken, header.Value.String)
		require.False(t, header.IsSecret, "a secret header must hold ciphertext; this one holds the checked-in token")

		entry, err := q.GetEnvironmentEntryFixture(ctx, testrepo.GetEnvironmentEntryFixtureParams{
			EnvironmentID: uuid.MustParse(spec.FixedUUID(fixtureEnvironmentFixedID)),
			Name:          fixtureAPIEnvVar,
		})
		require.NoError(t, err)
		require.Equal(t, base+fixtureauth.PathAPI, entry.Value)

		sessions, err := q.ListRemoteSessionTokensFixture(ctx, s.det("remote-identity-client-linear"))
		require.NoError(t, err)
		require.NotEmpty(t, sessions, "the seed must leave live sign-ins on the OAuth server")
		for _, sess := range sessions {
			require.True(t, sess.AutoRefresh)
			require.True(t, sess.AccessTokenEncrypted.Valid, "session %s must hold an access token", sess.ID)
			access, err := enc.Decrypt(sess.AccessTokenEncrypted.String)
			require.NoError(t, err)
			claims, err := fixtureauth.Parse(access, fixtureauth.KindAccess)
			require.NoError(t, err, "session %s must hold a fixture access token", sess.ID)
			require.Equal(t, base, claims.Issuer)
			require.Contains(t, []string(claims.Audience), fixtureauth.OAuthResource(base))
			refresh, err := enc.Decrypt(sess.RefreshTokenEncrypted)
			require.NoError(t, err)
			_, err = fixtureauth.Parse(refresh, fixtureauth.KindRefresh)
			require.NoError(t, err)
		}
	}
}
