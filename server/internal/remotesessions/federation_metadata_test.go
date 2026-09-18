package remotesessions

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

type federatedHTTPDoerFunc func(*http.Request) (*http.Response, error)

func (f federatedHTTPDoerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func federatedPublicPolicy(t *testing.T) *guardian.Policy {
	t.Helper()
	return guardian.NewDefaultPolicy(testenv.NewTracerProvider(t), guardian.WithResolver(dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) { return []net.IP{net.ParseIP("1.2.3.4")}, nil },
	})))
}

func TestFederatedEndpointHostPolicy(t *testing.T) {
	p := federatedFixture(t)
	m := &ChallengeManager{policy: federatedPublicPolicy(t)}
	require.NoError(t, m.validateFederatedMetadataHosts(t.Context(), p.issuer, p.metadata))
	for _, name := range []string{"issuer", "authorization", "token", "jwks"} {
		t.Run(name, func(t *testing.T) {
			issuer, doc := p.issuer, p.metadata
			switch name {
			case "issuer":
				issuer.Issuer = "https://10.0.0.1"
				doc.Issuer = issuer.Issuer
			case "authorization":
				doc.AuthorizationEndpoint = "https://127.0.0.1/authorize"
			case "token":
				doc.TokenEndpoint = "https://169.254.169.254/token"
			case "jwks":
				doc.JwksURI = "https://[::1]/keys"
			}
			require.ErrorIs(t, m.validateFederatedMetadataHosts(t.Context(), issuer, doc), ErrFederatedConfiguration)
		})
	}
	p.issuer.TunneledMcpServerID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	p.metadata.TokenEndpoint = "https://10.0.0.1/token"
	p.metadata.JwksURI = "https://10.0.0.1/keys"
	require.ErrorIs(t, m.validateFederatedMetadataHosts(t.Context(), p.issuer, p.metadata), ErrFederatedConfiguration, "missing tunnel transport fails closed")
	m.tunnels = &tunnelrouting.HTTPClient{}
	require.NoError(t, m.validateFederatedMetadataHosts(t.Context(), p.issuer, p.metadata))
	p.metadata.AuthorizationEndpoint = "https://10.0.0.1/authorize"
	require.ErrorIs(t, m.validateFederatedMetadataHosts(t.Context(), p.issuer, p.metadata), ErrFederatedConfiguration, "browser redirect does not use tunnel")
}

func TestFederatedMetadataCache(t *testing.T) {
	mr := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })
	p := federatedFixture(t)
	m := &ChallengeManager{policy: federatedPublicPolicy(t), locks: cache.NewRedisCacheAdapter(redisClient)}
	calls := 0
	doer := federatedHTTPDoerFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, p.issuer.Issuer+"/.well-known/openid-configuration", r.URL.String())
		doc := p.metadata
		encoded, err := json.Marshal(doc)
		require.NoError(t, err)
		// Unrecognized raw provider extensions must never enter the metadata cache.
		var fields map[string]any
		require.NoError(t, json.Unmarshal(encoded, &fields))
		fields["client_secret"] = "not-cacheable"
		encoded, err = json.Marshal(fields)
		require.NoError(t, err)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(encoded)))}, nil
	})
	for range 2 {
		doc, err := m.loadFederatedMetadata(t.Context(), p.organizationID, p.issuer, doer)
		require.NoError(t, err)
		require.Equal(t, p.metadata.Issuer, doc.Issuer)
	}
	require.Equal(t, 1, calls)
	key := federatedMetadataCacheKey(p.organizationID, p.issuer)
	require.Equal(t, federatedMetadataTTL, mr.TTL(key))
	var entry federatedMetadataCacheEntry
	require.NoError(t, m.locks.Get(t.Context(), key, &entry))
	require.Empty(t, entry.Document.raw)
	encoded, err := json.Marshal(entry)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-cacheable")
	// A cache hit still rejects changed DNS answers, including browser endpoints.
	m.policy = guardian.NewDefaultPolicy(testenv.NewTracerProvider(t), guardian.WithResolver(dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) { return []net.IP{net.ParseIP("10.0.0.1")}, nil },
	})))
	_, err = m.loadFederatedMetadata(t.Context(), p.organizationID, p.issuer, doer)
	require.ErrorIs(t, err, ErrFederatedConfiguration)
	require.Equal(t, 1, calls)
	m.policy = federatedPublicPolicy(t)
	mr.FastForward(federatedMetadataTTL + time.Second)
	_, err = m.loadFederatedMetadata(t.Context(), p.organizationID, p.issuer, doer)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	// A live policy change creates a new key and forces discovery.
	p.issuer.Oidc = !p.issuer.Oidc
	_, err = m.loadFederatedMetadata(t.Context(), p.organizationID, p.issuer, doer)
	require.NoError(t, err)
	require.Equal(t, 3, calls)
	// Organization scope is part of the key, even for globally owned issuers.
	_, err = m.loadFederatedMetadata(t.Context(), "other-org", p.issuer, doer)
	require.NoError(t, err)
	require.Equal(t, 4, calls)
}

func TestFederatedIssuerVersion(t *testing.T) {
	p := federatedFixture(t)
	original := federatedIssuerVersion(p.issuer)
	changed := p.issuer
	changed.Jwks = []byte(`{"keys":[]}`)
	changed.UpdatedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	changed.JwksFetchedAt = changed.UpdatedAt
	changed.MetadataFetchedAt = changed.UpdatedAt
	require.Equal(t, original, federatedIssuerVersion(changed), "cache-only changes do not invalidate login challenges")
	changed.AuthorizationResponseIssParameterSupported = pgtype.Bool{Bool: true, Valid: true}
	require.NotEqual(t, original, federatedIssuerVersion(changed))
	next, err := newFederatedProvider(p.organizationID, changed, p.client, p.metadata)
	require.NoError(t, err)
	require.NotEqual(t, p.Fingerprint(), next.Fingerprint())
	for _, change := range []func(){
		func() { changed.OrganizationID = pgtype.Text{String: "another-org", Valid: true} },
		func() { changed.TunneledMcpServerID = uuid.NullUUID{UUID: uuid.New(), Valid: true} },
		func() { changed.IDTokenSigningAlgValuesSupported = []string{"ES256"} },
		func() { changed.ScopeOverride = []string{"different"} },
	} {
		changed = p.issuer
		change()
		require.NotEqual(t, original, federatedIssuerVersion(changed))
		require.NotEqual(t, federatedMetadataCacheKey(p.organizationID, p.issuer), federatedMetadataCacheKey(p.organizationID, changed))
	}
}

func TestFederatedAuthorizationReservedParameters(t *testing.T) {
	p := federatedFixture(t)
	p.metadata.AuthorizationEndpoint += "?prompt=consent&scope=offline_access&client_id=attacker&nonce=wrong&code_challenge=wrong&code_challenge_method=plain&response_mode=fragment&response_type=token&request=attacker&request_uri=https%3A%2F%2Fattacker.example.test&approval_prompt=force&include_granted_scopes=true&tenant_hint=allowed"
	u, err := p.BuildAuthorizationURL("https://gram.example.test/callback", "state", "nonce", strings.Repeat("a", 43))
	require.NoError(t, err)
	q := u.Query()
	for _, name := range []string{"prompt", "request", "request_uri", "approval_prompt", "include_granted_scopes"} {
		require.Empty(t, q.Get(name))
	}
	require.Equal(t, "upstream-client", q.Get("client_id"))
	require.Equal(t, "openid email profile", q.Get("scope"))
	require.Equal(t, "nonce", q.Get("nonce"))
	require.Equal(t, "S256", q.Get("code_challenge_method"))
	require.NotEqual(t, "wrong", q.Get("code_challenge"))
	require.Equal(t, "code", q.Get("response_type"))
	require.Equal(t, "query", q.Get("response_mode"))
	require.Equal(t, "allowed", q.Get("tenant_hint"))
}
