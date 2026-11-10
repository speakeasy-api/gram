//go:build demoseed_safety

package demoseed

import (
	"crypto/rand"
	"encoding/base64"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/mcp-fixture/fixtureauth"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// The local seed points the OAuth, shared-token and generated servers at the
// MCP fixture and stores tokens the fixture accepts, on every reseed.
func TestLocalFixturesPointServersAtMCPFixture(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	blob := assetstest.NewTestBlobStore(t)

	key := make([]byte, 32)
	_, err = rand.Read(key)
	require.NoError(t, err)
	enc, err := encryption.New(base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)
	serverURL, err := url.Parse("https://speakeasy.example.test")
	require.NoError(t, err)
	const base = "https://fixture.example.test:4443"
	opts := LocalFixturesOptions{
		DeveloperEmail: testDeveloperEmail,
		Environment:    "local",
		MCPFixture:     LocalMCPFixtureOptions{BaseURL: base, ServerURL: serverURL, AccessTokenTTL: time.Hour, Encryption: enc},
	}

	spec := LocalSpec()
	s := &seeder{spec: spec}
	q := testrepo.New(db)
	for range 2 {
		seedLocalPostgres(ctx, t, db, spec)
		require.NoError(t, RunLocalFixtures(ctx, testenv.NewLogger(t), db, blob, nil, opts))

		oauth, err := q.GetRemoteMCPServerURLFixture(ctx, s.det("remotemcp-linear"))
		require.NoError(t, err)
		require.Equal(t, fixtureOAuthServerName, oauth.Name.String)
		require.Equal(t, fixtureauth.OAuthResource(base), oauth.Url)

		shared, err := q.GetRemoteMCPServerURLFixture(ctx, s.det("remotemcp-github"))
		require.NoError(t, err)
		require.Equal(t, base+fixtureauth.PathSharedMCP, shared.Url)
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
			access, err := enc.Decrypt(sess.AccessTokenEncrypted)
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
