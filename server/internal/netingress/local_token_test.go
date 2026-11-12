package netingress

import (
	"bytes"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var localTestKey = bytes.Repeat([]byte("k"), localTokenKeyBytes)

func localGatewayClaims() LocalTokenClaims {
	return LocalTokenClaims{Namespace: "gram-local", ServiceAccount: "gram-tailnet-gateway", Audience: DefaultTokenAudience, TTL: time.Hour}
}

func reviewLocal(t *testing.T, reviewer *LocalTokenReviewer, token string, audiences ...string) authenticationv1.TokenReviewStatus {
	t.Helper()
	review, err := reviewer.Create(t.Context(), &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{Token: token, Audiences: audiences},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	return review.Status
}

func TestLocalTokenReviewerAcceptsMintedToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)
	token, err := MintLocalToken(localTestKey, localGatewayClaims(), now)
	require.NoError(t, err)
	reviewer, err := NewLocalTokenReviewer(localTestKey)
	require.NoError(t, err)
	reviewer.now = func() time.Time { return now.Add(time.Minute) }

	status := reviewLocal(t, reviewer, token, DefaultTokenAudience, "other")
	require.True(t, status.Authenticated)
	require.Empty(t, status.Error)
	require.Equal(t, "system:serviceaccount:gram-local:gram-tailnet-gateway", status.User.Username)
	require.Equal(t, []string{DefaultTokenAudience}, status.Audiences, "only requested audiences are reported")

	status = reviewLocal(t, reviewer, token)
	require.True(t, status.Authenticated, "a review without audiences, as the readiness probe sends, is accepted")
	require.Equal(t, []string{DefaultTokenAudience}, status.Audiences)
}

func TestLocalTokenReviewerRejectsInvalidTokens(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)
	valid, err := MintLocalToken(localTestKey, localGatewayClaims(), now)
	require.NoError(t, err)
	otherKey, err := MintLocalToken(bytes.Repeat([]byte("x"), localTokenKeyBytes), localGatewayClaims(), now)
	require.NoError(t, err)
	expired, err := MintLocalToken(localTestKey, localGatewayClaims(), now.Add(-2*time.Hour))
	require.NoError(t, err)
	wrongIssuer, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer: "someone-else", Subject: "system:serviceaccount:gram-local:gram-tailnet-gateway",
		Audience: jwt.ClaimStrings{DefaultTokenAudience}, ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}).SignedString(localTestKey)
	require.NoError(t, err)
	notServiceAccount, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer: LocalTokenIssuer, Subject: "alice",
		Audience: jwt.ClaimStrings{DefaultTokenAudience}, ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}).SignedString(localTestKey)
	require.NoError(t, err)
	noExpiry, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer: LocalTokenIssuer, Subject: "system:serviceaccount:gram-local:gram-tailnet-gateway",
		Audience: jwt.ClaimStrings{DefaultTokenAudience},
	}).SignedString(localTestKey)
	require.NoError(t, err)
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{
		Issuer: LocalTokenIssuer, Subject: "system:serviceaccount:gram-local:gram-tailnet-gateway",
		Audience: jwt.ClaimStrings{DefaultTokenAudience}, ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	reviewer, err := NewLocalTokenReviewer(localTestKey)
	require.NoError(t, err)
	reviewer.now = func() time.Time { return now }

	for _, tc := range []struct {
		name      string
		token     string
		audiences []string
	}{
		{name: "signed with another key", token: otherKey},
		{name: "expired", token: expired},
		{name: "another issuer", token: wrongIssuer},
		{name: "subject is not a service account", token: notServiceAccount},
		{name: "no expiry", token: noExpiry},
		{name: "unsigned", token: unsigned},
		{name: "audience not requested", token: valid, audiences: []string{"another-audience"}},
		{name: "not a token", token: "not-a-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			status := reviewLocal(t, reviewer, tc.token, tc.audiences...)
			require.False(t, status.Authenticated)
			require.NotEmpty(t, status.Error)
			require.Empty(t, status.User.Username)
		})
	}
}

func TestLocalTokenReviewerRequiresAFullLengthKey(t *testing.T) {
	t.Parallel()

	_, err := NewLocalTokenReviewer([]byte("short"))
	require.Error(t, err)
	_, err = MintLocalToken([]byte("short"), localGatewayClaims(), time.Now())
	require.Error(t, err)
}

func TestMintLocalTokenRejectsInvalidClaims(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		mutate func(*LocalTokenClaims)
	}{
		{name: "no namespace", mutate: func(c *LocalTokenClaims) { c.Namespace = "" }},
		{name: "no service account", mutate: func(c *LocalTokenClaims) { c.ServiceAccount = "" }},
		{name: "no audience", mutate: func(c *LocalTokenClaims) { c.Audience = "" }},
		{name: "no lifetime", mutate: func(c *LocalTokenClaims) { c.TTL = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			claims := localGatewayClaims()
			tc.mutate(&claims)
			_, err := MintLocalToken(localTestKey, claims, time.Now())
			require.Error(t, err)
		})
	}
}

func TestLocalTokenAttestsTheGatewayThroughTheVerifier(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)
	token, err := MintLocalToken(localTestKey, localGatewayClaims(), now)
	require.NoError(t, err)
	reviewer, err := NewLocalTokenReviewer(localTestKey)
	require.NoError(t, err)
	reviewer.now = func() time.Time { return now }
	ingress := Ingress{ID: uuid.New(), OrganizationID: "org_123", Provider: ProviderTailscale, DNSName: "speakeasy.example.ts.net", AttestorNamespace: "", AttestorServiceAccount: ""}
	lookup := &fakeAttestorLookup{ingress: ingress}
	verifier := NewAttestationVerifier(reviewer, lookup, DefaultTokenAudience, 30*time.Second)
	verifier.now = func() time.Time { return now }
	require.NoError(t, verifier.UseGatewayPrincipal("gram-local", "gram-tailnet-gateway"))

	got, err := verifier.Verify(t.Context(), token, "127.0.0.1:1234", "speakeasy.example.ts.net")
	require.NoError(t, err)
	require.Equal(t, ingress, got)
	require.Equal(t, 1, lookup.byDNSNameCalls, "the gateway resolves its ingress from the host")

	other, err := MintLocalToken(localTestKey, LocalTokenClaims{Namespace: "gram-local", ServiceAccount: "someone-else", Audience: DefaultTokenAudience, TTL: time.Hour}, now)
	require.NoError(t, err)
	lookup.byAttestorErr = ErrAttestationRejected
	_, err = verifier.Verify(t.Context(), other, "127.0.0.1:1234", "speakeasy.example.ts.net")
	require.ErrorIs(t, err, ErrAttestationRejected, "another service account is not the gateway")
}

func TestLoadOrCreateLocalTokenKeyAgreesOnOneKey(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "netingress", "token.key")
	const callers = 8
	keys := make([][]byte, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			key, err := LoadOrCreateLocalTokenKey(path)
			require.NoError(t, err)
			keys[i] = key
		})
	}
	wg.Wait()

	for _, key := range keys {
		require.Len(t, key, localTokenKeyBytes)
		require.Equal(t, keys[0], key)
	}
	again, err := LoadOrCreateLocalTokenKey(path)
	require.NoError(t, err)
	require.Equal(t, keys[0], again, "an existing key is reused")
}

func TestWriteLocalTokenReplacesTheFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "netingress", "token")
	require.NoError(t, WriteLocalToken(path, "first"))
	require.NoError(t, WriteLocalToken(path, "second"))

	token, err := readProjectedToken(path)
	require.NoError(t, err)
	require.Equal(t, "second", token)
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".token-*"))
	require.NoError(t, err)
	require.Empty(t, matches, "no temporary files are left behind")
}
