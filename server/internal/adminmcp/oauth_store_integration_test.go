package adminmcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var staffMCPInfra *testenv.Environment

func TestMain(m *testing.M) {
	infra, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch staff MCP test database: %v", err)
	}
	staffMCPInfra = infra
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("clean up staff MCP test database: %v", err)
	}
	os.Exit(code)
}

func TestStaffOAuthStoreSingleUseGrantAndRefresh(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := staffMCPInfra.CloneTestDatabase(t, "admin_mcp_oauth")
	require.NoError(t, err)
	clients := postgresStaffClientStore{db: db}
	clientID := "client_" + uuid.NewString()
	require.NoError(t, clients.RegisterClient(ctx, staffOAuthClient{ID: clientID, Name: "Test editor", SecretHash: "", RedirectURIs: []string{"http://localhost:5555/callback"}, SecretExpiresAt: nil}))
	store := postgresStaffAuthorizationStore{db: db}
	grants := postgresStaffGrantStore{db: db}
	now := time.Now()
	verifier := "test-verifier-" + uuid.NewString() + uuid.NewString()
	challenge := staffChallengeForTest(verifier)
	code := "code-" + uuid.NewString()
	authorization := staffAuthorization{
		Subject: "user:staff-subject", ClientID: clientID, SessionEnc: "encrypted-session", ResourceURI: staffAudience,
		Scopes: []string{"admin:read"}, CodeHash: staffTokenHash(code), CodeChallenge: challenge,
		RedirectURI: "http://localhost:5555/callback", ExpiresAt: now.Add(2 * time.Hour), GrantExpires: now.Add(10 * time.Minute),
	}
	require.NoError(t, store.Authorize(ctx, authorization))
	connection, err := grants.ValidateGrant(ctx, authorization.CodeHash, clientID, authorization.RedirectURI, verifier, now)
	require.NoError(t, err)
	_, err = grants.ValidateGrant(ctx, authorization.CodeHash, clientID, "http://localhost:5555/other", verifier, now)
	require.ErrorIs(t, err, errStaffGrant)
	_, err = grants.ValidateGrant(ctx, authorization.CodeHash, clientID, authorization.RedirectURI, "bad-verifier", now)
	require.ErrorIs(t, err, errStaffGrant)

	first := staffIssuedSession{ID: uuid.New(), JTI: "jti-" + uuid.NewString(), RefreshHash: staffTokenHash("refresh-one"), ExpiresAt: now.Add(time.Hour), RefreshTil: now.Add(90 * time.Minute)}
	require.NoError(t, grants.ExchangeGrant(ctx, authorization.CodeHash, clientID, authorization.RedirectURI, verifier, first, now))
	require.ErrorIs(t, grants.ExchangeGrant(ctx, authorization.CodeHash, clientID, authorization.RedirectURI, verifier, first, now), errStaffGrant)
	loaded, err := grants.PrepareRefresh(ctx, first.RefreshHash, clientID, now)
	require.NoError(t, err)
	require.Equal(t, connection.ID, loaded.ID)

	second := staffIssuedSession{ID: uuid.New(), JTI: "jti-" + uuid.NewString(), RefreshHash: staffTokenHash("refresh-two"), ExpiresAt: now.Add(time.Hour), RefreshTil: now.Add(90 * time.Minute)}
	require.NoError(t, grants.RotateRefresh(ctx, first.RefreshHash, clientID, loaded, second, now))
	_, err = grants.PrepareRefresh(ctx, first.RefreshHash, clientID, now)
	require.ErrorIs(t, err, errStaffRefreshReuse)
	_, err = grants.PrepareRefresh(ctx, second.RefreshHash, clientID, now)
	require.ErrorIs(t, err, errStaffGrant)

	var reason string
	require.NoError(t, db.QueryRow(ctx, `SELECT reauthorization_reason FROM admin_mcp_connections WHERE id = $1`, connection.ID).Scan(&reason)) //nolint:glint // notestingrawsql: Assert the terminal database state of this transaction directly.
	require.Equal(t, "refresh_reuse", reason)
}

func TestStaffOAuthReauthorizationInvalidatesOldGeneration(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := staffMCPInfra.CloneTestDatabase(t, "admin_mcp_reauth")
	require.NoError(t, err)
	clientID := "client_" + uuid.NewString()
	require.NoError(t, (postgresStaffClientStore{db: db}).RegisterClient(ctx, staffOAuthClient{ID: clientID, Name: "Test editor", SecretHash: "", RedirectURIs: []string{"http://localhost:5555/callback"}, SecretExpiresAt: nil}))
	store := postgresStaffAuthorizationStore{db: db}
	grants := postgresStaffGrantStore{db: db}
	now := time.Now()
	verifier := "test-verifier-" + uuid.NewString() + uuid.NewString()
	challenge := staffChallengeForTest(verifier)
	first := staffAuthorization{Subject: "user:staff-subject", ClientID: clientID, SessionEnc: "encrypted-session", ResourceURI: staffAudience, Scopes: []string{"admin:read"}, CodeHash: staffTokenHash("code-one"), CodeChallenge: challenge, RedirectURI: "http://localhost:5555/callback", ExpiresAt: now.Add(2 * time.Hour), GrantExpires: now.Add(10 * time.Minute)}
	require.NoError(t, store.Authorize(ctx, first))
	previous, err := grants.ValidateGrant(ctx, first.CodeHash, clientID, first.RedirectURI, verifier, now)
	require.NoError(t, err)
	first.CodeHash = staffTokenHash("code-two")
	require.NoError(t, store.Authorize(ctx, first))
	_, err = grants.ValidateGrant(ctx, staffTokenHash("code-one"), clientID, first.RedirectURI, verifier, now)
	require.ErrorIs(t, err, errStaffGrant)
	current, err := grants.ValidateGrant(ctx, first.CodeHash, clientID, first.RedirectURI, verifier, now)
	require.NoError(t, err)
	require.Equal(t, previous.ID, current.ID)
	require.NotEqual(t, previous.Generation, current.Generation)
	_, err = grants.PrepareRefresh(ctx, "missing-token", clientID, now)
	require.ErrorIs(t, err, errStaffGrant)
}

func staffChallengeForTest(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
