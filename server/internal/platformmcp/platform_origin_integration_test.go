package platformmcp

import (
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Access tokens are bound to the platform host they were minted for (RFC
// 8707): each host accepts its own tokens and refuses the other's, and
// requests without a platform origin fall back to the configured host.
func TestJWTAuthenticatorBindsTokensToPlatformHost(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_jwt_platform_host")
	require.NoError(t, err)

	organizationID := "org_" + uuid.NewString()
	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Platform MCP host binding test organization",
		Slug:        "org-" + uuid.NewString()[:8],
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	encryptionClient := testEncryption(t)
	codec, err := NewCredentialCodec(encryptionClient)
	require.NoError(t, err)
	signer := sessiontokens.NewSigner("test-key")
	now := time.Now().UTC()
	store := NewPostgresOAuthStore(conn)
	client := platformoauth.Client{ID: "client-" + uuid.NewString(), Name: "Platform MCP host binding test client", RedirectURIs: []string{"https://client.example.test/callback"}}
	require.NoError(t, store.RegisterClient(ctx, client))
	userID := "user_" + uuid.NewString()
	connection := platformoauth.Connection{ID: uuid.NewString(), ClientID: client.ID, Subject: userSubjectURN(userID), OrganizationID: organizationID, Generation: uuid.NewString(), AuthorizationExpiresAt: now.Add(platformoauth.AuthorizationLifetime)}
	require.NoError(t, store.RegisterConnection(ctx, connection))

	mint := func(baseURL string) string {
		t.Helper()
		resource := baseURL + "/platform-mcp"
		jti, err := codec.Issue(accessJTICredential, organizationID, resource)
		require.NoError(t, err)
		expiresAt := now.Add(time.Hour)
		token, jti, err := signer.Mint(sessiontokens.MintParams{Subject: urn.SessionSubject{Kind: urn.SessionSubjectKindUser, ID: userID}, Audience: resource, Issuer: resource, ExpiresAt: &expiresAt, ClientID: client.ID, JTI: jti})
		require.NoError(t, err)
		require.NoError(t, store.CreateSession(ctx, platformoauth.Session{ID: uuid.NewString(), ClientID: client.ID, Connection: connection, JTI: jti, RefreshHash: "refresh-" + uuid.NewString(), ExpiresAt: expiresAt, RefreshExpiresAt: expiresAt}))
		return token
	}
	canonicalToken := mint(testCanonicalBaseURL)
	extraToken := mint(testExtraHostBaseURL)

	base, err := url.Parse(testCanonicalBaseURL)
	require.NoError(t, err)
	authenticator, err := NewJWTAuthenticator(signer, conn, encryptionClient, base)
	require.NoError(t, err)
	for _, tc := range []struct {
		name    string
		surface requestorigin.Surface
		baseURL string
		token   string
		allowed bool
	}{
		{name: "canonical token on canonical host", surface: requestorigin.SurfacePlatform, baseURL: testCanonicalBaseURL, token: canonicalToken, allowed: true},
		{name: "canonical token without origin", surface: "", baseURL: "", token: canonicalToken, allowed: true},
		{name: "extra host token on extra host", surface: requestorigin.SurfacePlatform, baseURL: testExtraHostBaseURL, token: extraToken, allowed: true},
		{name: "extra host token on canonical host", surface: requestorigin.SurfacePlatform, baseURL: testCanonicalBaseURL, token: extraToken, allowed: false},
		{name: "canonical token on extra host", surface: requestorigin.SurfacePlatform, baseURL: testExtraHostBaseURL, token: canonicalToken, allowed: false},
		{name: "extra host token on custom domain", surface: requestorigin.SurfaceCustomDomain, baseURL: testExtraHostBaseURL, token: extraToken, allowed: false},
	} {
		requestCtx := ctx
		if tc.surface != "" {
			requestCtx = requestorigin.WithContext(ctx, requestorigin.Origin{Surface: tc.surface, BaseURL: tc.baseURL, OrganizationID: "", NetworkIngressID: uuid.Nil, NetworkIdentity: nil})
		}
		principal, err := authenticator.Authenticate(requestCtx, tc.token)
		if !tc.allowed {
			require.ErrorIs(t, err, ErrUnauthorized, tc.name)
			continue
		}
		require.NoError(t, err, tc.name)
		require.Equal(t, userID, principal.UserID, tc.name)
		require.Equal(t, organizationID, principal.OrganizationID, tc.name)
	}
}
