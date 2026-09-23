package mcpauthz

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func keyPEM(t *testing.T, bits int) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	require.NoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})), string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub}))
}

func issuerForTest(t *testing.T) (*Issuer, string) {
	t.Helper()
	private, public := keyPEM(t, 2048)
	issuer, err := New(private, public, "https://gram.example/", false)
	require.NoError(t, err)
	return issuer, public
}

func targetForTest() Target {
	return Target{OrganizationID: "org_test", ProjectID: uuid.New(), MCPServerID: uuid.NewString(), TunnelID: uuid.New(), ResourceIdentifier: ""}
}

func tenantContext(t *testing.T, target Target) context.Context {
	t.Helper()
	return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: target.OrganizationID, ProjectID: &target.ProjectID, UserID: "creator-must-not-appear", Email: new("creator@example.invalid")})
}

type notRevoked struct{}

func (notRevoked) IsTokenRevoked(context.Context, string) (bool, error) { return false, nil }

func sessionContext(t *testing.T, ctx context.Context, subject urn.SessionSubject, lifetime time.Duration) context.Context {
	t.Helper()
	signer := sessiontokens.NewSigner("test-only-session-secret")
	token, _, err := signer.Mint(sessiontokens.MintParams{Subject: subject, Audience: "test", Issuer: "test", Lifetime: lifetime})
	require.NoError(t, err)
	proof, err := signer.ValidateBearer(ctx, token, "test", notRevoked{})
	require.NoError(t, err)
	return mcpidentity.NewValidatorBoundary().StampValidatedSession(ctx, proof)
}

func verifiedClaims(t *testing.T, raw string, keys jose.JSONWebKeySet) (josejwt.Claims, map[string]any) {
	t.Helper()
	token, err := josejwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	require.NoError(t, err)
	require.Equal(t, "speakeasy-authz+jwt", token.Headers[0].ExtraHeaders[jose.HeaderType])
	require.NotEmpty(t, token.Headers[0].KeyID)
	require.Len(t, keys.Key(token.Headers[0].KeyID), 1)
	var standard josejwt.Claims
	var claims map[string]any
	require.NoError(t, token.Claims(keys, &standard, &claims))
	return standard, claims
}

func TestAssertionVerifiesWithPublicJWKSAndBindsDestination(t *testing.T) {
	t.Parallel()
	issuer, publicPEM := issuerForTest(t)
	target := targetForTest()
	ctx := sessionContext(t, tenantContext(t, target), urn.NewUserSubject("user_test"), time.Hour)
	raw, err := issuer.Mint(ctx, target)
	require.NoError(t, err)
	standard, claims := verifiedClaims(t, raw, servedKeys(t, publicPEM))
	expected := josejwt.Expected{Issuer: "https://gram.example", Subject: "user:user_test", AnyAudience: josejwt.Audience{urn.NewTunneledMcpServer(target.TunnelID).String()}, Time: time.Now()}
	require.NoError(t, standard.ValidateWithLeeway(expected, 0))
	require.Equal(t, target.OrganizationID, claims["organization_id"])
	require.Equal(t, target.ProjectID.String(), claims["project_id"])
	require.Equal(t, target.MCPServerID, claims["mcp_server_id"])
	require.Equal(t, target.TunnelID.String(), claims["tunneled_mcp_server_id"])
	require.Equal(t, "user", claims["principal_type"])
	require.Equal(t, "mcp_request", claims["purpose"])
	require.IsType(t, "", claims["aud"])
	require.Equal(t, time.Minute, standard.Expiry.Time().Sub(standard.IssuedAt.Time()))
	require.NotContains(t, claims, "email")
	require.NotContains(t, claims, "allowed_methods")
	expected.AnyAudience = josejwt.Audience{"wrong-resource"}
	require.Error(t, standard.ValidateWithLeeway(expected, 0))
	expected.AnyAudience = standard.Audience
	expected.Time = time.Now().Add(2 * time.Minute)
	require.Error(t, standard.ValidateWithLeeway(expected, 0))
	other := target
	other.OrganizationID = "another-org"
	_, err = issuer.Mint(ctx, other)
	require.Error(t, err)
	other = target
	other.ProjectID = uuid.New()
	_, err = issuer.Mint(ctx, other)
	require.Error(t, err)
}

func TestAssertionUsesExactConfiguredResourceAudience(t *testing.T) {
	t.Parallel()
	issuer, publicPEM := issuerForTest(t)
	target := targetForTest()
	target.ResourceIdentifier = "https://mcp.internal.example.com/a%2Fb/?tenant=example/"
	ctx := sessionContext(t, tenantContext(t, target), urn.NewUserSubject("user_test"), time.Hour)
	raw, err := issuer.Mint(ctx, target)
	require.NoError(t, err)
	standard, claims := verifiedClaims(t, raw, servedKeys(t, publicPEM))
	expected := josejwt.Expected{Issuer: "https://gram.example", Subject: "user:user_test", AnyAudience: josejwt.Audience{target.ResourceIdentifier}, Time: time.Now()}
	require.NoError(t, standard.ValidateWithLeeway(expected, 0))
	require.Equal(t, target.ResourceIdentifier, claims["aud"])
	require.Equal(t, target.TunnelID.String(), claims["tunneled_mcp_server_id"])
	require.Equal(t, target.OrganizationID, claims["organization_id"])
	require.Equal(t, target.ProjectID.String(), claims["project_id"])
	for _, wrongAudience := range []string{
		urn.NewTunneledMcpServer(target.TunnelID).String(),
		"https://mcp.internal.example.com/a%2Fb?tenant=example/",
		"https://mcp.internal.example.com/a/b/?tenant=example/",
	} {
		expected.AnyAudience = josejwt.Audience{wrongAudience}
		require.Error(t, standard.ValidateWithLeeway(expected, 0))
	}
}

func TestAPIKeyAssertionNeverPromotesCreator(t *testing.T) {
	t.Parallel()
	issuer, publicPEM := issuerForTest(t)
	target := targetForTest()
	keyID := uuid.NewString()
	ctx := mcpidentity.NewValidatorBoundary().StampAPIKey(tenantContext(t, target), keyID)
	raw, err := issuer.Mint(ctx, target)
	require.NoError(t, err)
	_, claims := verifiedClaims(t, raw, servedKeys(t, publicPEM))
	require.Equal(t, "api_key:"+keyID, claims["sub"])
	require.Equal(t, "api_key", claims["principal_type"])
	encoded, err := json.Marshal(claims)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "creator-must-not-appear")
	require.NotContains(t, claims, "user_id")
	require.NotContains(t, claims, "email")
}

func TestSessionAPIKeyAndAgentRetainVerifiedSubject(t *testing.T) {
	t.Parallel()
	issuer, publicPEM := issuerForTest(t)
	target := targetForTest()
	keys := servedKeys(t, publicPEM)
	for _, subject := range []urn.SessionSubject{urn.NewAPIKeySubject(uuid.New()), urn.NewAgentSubject(uuid.New())} {
		ctx := sessionContext(t, tenantContext(t, target), subject, 20*time.Second)
		raw, err := issuer.Mint(ctx, target)
		require.NoError(t, err)
		standard, claims := verifiedClaims(t, raw, keys)
		require.Contains(t, claims["sub"], subject.ID)
		require.NotContains(t, claims, "email")
		require.LessOrEqual(t, standard.Expiry.Time().Sub(standard.IssuedAt.Time()), 20*time.Second)
	}
}

func TestUnsupportedProvenanceOmitsAssertion(t *testing.T) {
	t.Parallel()
	issuer, _ := issuerForTest(t)
	target := targetForTest()
	ctx := tenantContext(t, target)
	b := mcpidentity.NewValidatorBoundary()
	contexts := []context.Context{ctx, b.StampAssistant(ctx), b.StampChatSession(ctx), b.StampAPIKey(ctx, ""), sessionContext(t, ctx, urn.NewAnonymousSubject(uuid.NewString()), time.Hour), sessionContext(t, ctx, urn.NewWorkloadSubject(uuid.New(), "synthetic-subject"), time.Hour)}
	for _, c := range contexts {
		raw, err := issuer.Mint(c, target)
		require.NoError(t, err)
		require.Empty(t, raw)
	}
}

func TestHumanAssertionIncludesOnlyMatchingUserEmail(t *testing.T) {
	t.Parallel()
	issuer, publicPEM := issuerForTest(t)
	target := targetForTest()
	keys := servedKeys(t, publicPEM)
	for _, discovery := range []bool{false, true} {
		for _, matchingUser := range []bool{false, true} {
			auth := &contextvalues.AuthContext{ActiveOrganizationID: target.OrganizationID, ProjectID: &target.ProjectID, UserID: "another_user", Email: new("user@example.invalid")}
			if matchingUser {
				auth.UserID = "user_test"
			}
			ctx := contextvalues.SetAuthContext(t.Context(), auth)
			if discovery {
				ctx = mcpidentity.NewValidatorBoundary().StampConsentDiscovery(ctx, "user_test", time.Now().Add(time.Minute))
			} else {
				ctx = sessionContext(t, ctx, urn.NewUserSubject("user_test"), time.Hour)
			}
			raw, err := issuer.Mint(ctx, target)
			require.NoError(t, err)
			_, claims := verifiedClaims(t, raw, keys)
			require.Equal(t, "user:user_test", claims["sub"])
			if matchingUser {
				require.Equal(t, "user@example.invalid", claims["email"])
			} else {
				require.NotContains(t, claims, "email")
			}
			require.NotContains(t, claims, "email_verified")
		}
	}
}

func TestDiscoveryAssertionIsScopedAndExpiresWithChallenge(t *testing.T) {
	t.Parallel()
	issuer, publicPEM := issuerForTest(t)
	target := targetForTest()
	deadline := time.Now().Add(15 * time.Second)
	ctx := mcpidentity.NewValidatorBoundary().StampConsentDiscovery(tenantContext(t, target), "user_test", deadline)
	raw, err := issuer.Mint(ctx, target)
	require.NoError(t, err)
	standard, claims := verifiedClaims(t, raw, servedKeys(t, publicPEM))
	require.Equal(t, "mcp_discovery", claims["purpose"])
	require.Equal(t, "user:user_test", claims["sub"])
	require.Equal(t, deadline.Unix(), standard.Expiry.Time().Unix())
	require.ElementsMatch(t, []any{"server/discover", "initialize", "notifications/initialized", "ping", "tools/list"}, claims["allowed_methods"])
	require.NotContains(t, claims["allowed_methods"], "tools/call")
}

func TestRotationPrepublishOverlapAndRetirement(t *testing.T) {
	t.Parallel()
	privateA, publicA := keyPEM(t, 2048)
	privateB, publicB := keyPEM(t, 2048)
	a, err := New(privateA, publicA+publicB, "https://gram.example", false)
	require.NoError(t, err)
	b, err := New(privateB, publicA+publicB, "https://gram.example", false)
	require.NoError(t, err)
	require.NotEqual(t, a.kid, b.kid)
	target := targetForTest()
	ctx := mcpidentity.NewValidatorBoundary().StampAPIKey(tenantContext(t, target), uuid.NewString())
	old, err := a.Mint(ctx, target)
	require.NoError(t, err)
	fresh, err := b.Mint(ctx, target)
	require.NoError(t, err)
	verifiedClaims(t, old, servedKeys(t, publicA+publicB))
	verifiedClaims(t, fresh, servedKeys(t, publicB+publicA))
	token, err := josejwt.ParseSigned(old, []jose.SignatureAlgorithm{jose.RS256})
	require.NoError(t, err)
	require.Error(t, token.Claims(servedKeys(t, publicB), &josejwt.Claims{}))
	// Key order, duplicates and whitespace leave the RFC 7638 kid unchanged.
	same, err := New(privateA, "\n"+publicB+publicA+publicA, "https://gram.example", false)
	require.NoError(t, err)
	require.Equal(t, a.kid, same.kid)
}

func TestStartupRejectsPartialOrUnsafeConfiguration(t *testing.T) {
	t.Parallel()
	private, public := keyPEM(t, 2048)
	smallPrivate, smallPublic := keyPEM(t, 1024)
	_, other := keyPEM(t, 2048)
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	ecBytes, err := x509.MarshalPKIXPublicKey(&ec.PublicKey)
	require.NoError(t, err)
	ecPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: ecBytes}))
	configs := [][3]string{{private, "", "https://gram.example"}, {"", public, "https://gram.example"}, {private, private + public, "https://gram.example"}, {private, other, "https://gram.example"}, {smallPrivate, smallPublic, "https://gram.example"}, {private, public + smallPublic, "https://gram.example"}, {private, public + ecPEM, "https://gram.example"}, {private + private, public, "https://gram.example"}, {private, public + "junk", "https://gram.example"}, {private, public, "http://gram.example"}, {private, public, "https://gram.example/path"}, {private, public, "https://user:pass@gram.example"}}
	for i, c := range configs {
		_, err := New(c[0], c[1], c[2], false)
		require.Error(t, err, "case %d", i)
	}
	disabled, err := New("", "", "", false)
	require.NoError(t, err)
	require.False(t, disabled.Enabled())
	require.Empty(t, servedKeys(t, "").Keys)
	raw, err := disabled.Mint(t.Context(), targetForTest())
	require.NoError(t, err)
	require.Empty(t, raw)
}

func TestStartupRequiresPKCS8PrivateAndSPKIPublicKeys(t *testing.T) {
	t.Parallel()
	issuer, _ := issuerForTest(t)
	private, public := keyPEM(t, 2048)
	legacyPrivate := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(issuer.key)}))
	legacyPublic := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&issuer.key.PublicKey)}))
	_, err := New(legacyPrivate, public, "https://tunnel.example", false)
	require.ErrorContains(t, err, "PKCS#8")
	_, err = New(private, legacyPublic, "https://tunnel.example", false)
	require.ErrorContains(t, err, "SubjectPublicKeyInfo")
	_, err = New(private, public, "", false)
	require.ErrorContains(t, err, "GRAM_AUTHZ_ISSUER_URL")
	_, err = New("", "", "https://tunnel.example", false)
	require.ErrorContains(t, err, "GRAM_AUTHZ_PRIVATE_KEY")
}

func TestInboundAssertionStripping(t *testing.T) {
	t.Parallel()
	var forwarded http.Header
	handler := StripMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, path := range []string{"/mcp/test", "/.well-known/jwks.json"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header = http.Header{"SPEAKEASY_AUTHZ": {"fake"}, "Speakeasy_authz": {"fake"}, "speakeasy-authz": {"fake"}, "Authorization": {"Bearer upstream"}}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		require.Equal(t, http.StatusNoContent, w.Code, "stripping middleware must not serve a public key route")
		require.Equal(t, http.Header{"Authorization": {"Bearer upstream"}}, forwarded)
	}
}
