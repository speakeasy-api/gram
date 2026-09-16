package okta

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func listApps(t *testing.T, tc testClient) []App {
	t.Helper()
	apps, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0})
	require.NoError(t, err)
	return apps
}

func TestClient_ListApps_FirstMintBurnsOneAssertionThenCaches(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(3))

	apps := listApps(t, tc)
	require.Len(t, apps, 3)
	require.Equal(t, "0oa000", apps[0].ID)
	require.Equal(t, "App 0", apps[0].Label)
	require.Equal(t, "OPENID_CONNECT", apps[0].SignOnMode)
	require.Equal(t, []string{"PUSH_NEW_USERS"}, apps[0].Features)

	require.Equal(t, 2, tc.signer.Calls())
	require.Equal(t, stubCounts{tokenRequests: 2, assertionsSeen: 2, issuedTokens: 1}, tc.stub.counts())

	listApps(t, tc)
	require.Equal(t, 2, tc.signer.Calls())
	require.Equal(t, 1, tc.stub.counts().issuedTokens)
}

func TestClient_Token_ConcurrentFirstCallersShareOneHandshake(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))

	const n = 16
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Go(func() {
			_, errs[i] = tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0})
		})
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 2, tc.signer.Calls())
	require.Equal(t, stubCounts{tokenRequests: 2, assertionsSeen: 2, issuedTokens: 1}, tc.stub.counts())
}

func TestClient_Token_NonceRetryUsesFreshAssertion(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))

	listApps(t, tc)

	require.Equal(t, 2, tc.signer.Calls())
	jtis := tc.signer.JTIs()
	require.Len(t, jtis, 2)
	require.NotEqual(t, jtis[0], jtis[1])
	require.Equal(t, 2, tc.stub.counts().tokenRequests)
	require.Equal(t, "nonce-1", tc.currentNonce())

	proofs := tc.stub.recordedProofs()
	require.GreaterOrEqual(t, len(proofs), 2)
	require.Empty(t, proofs[0].nonce)
	require.Equal(t, "nonce-1", proofs[1].nonce)
}

func TestClient_Token_SecondNonceChallengeFailsWithoutThirdAssertion(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	tc.stub.setRotateTokenNonce(true)

	_, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0})
	require.EqualError(t, err, "okta token endpoint rejected dpop nonce twice")
	require.Equal(t, 2, tc.signer.Calls())
	require.Equal(t, stubCounts{tokenRequests: 2, assertionsSeen: 2, issuedTokens: 0}, tc.stub.counts())
}

func TestClient_Token_SecondMintReusesCachedNonce(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))

	listApps(t, tc)
	require.Equal(t, 2, tc.signer.Calls())

	tc.clock.advance(time.Hour)
	listApps(t, tc)
	require.Equal(t, 3, tc.signer.Calls())
	require.Equal(t, stubCounts{tokenRequests: 3, assertionsSeen: 3, issuedTokens: 2}, tc.stub.counts())

	proofs := tc.stub.recordedProofs()
	last := proofs[len(proofs)-1]
	require.Equal(t, http.MethodGet, last.method)
	require.Equal(t, "nonce-1", last.nonce)
	tokenProofs := make([]proofRecord, 0)
	for _, p := range proofs {
		if p.method == http.MethodPost {
			tokenProofs = append(tokenProofs, p)
		}
	}
	require.Len(t, tokenProofs, 3)
	require.Equal(t, "nonce-1", tokenProofs[2].nonce)
}

func TestClient_Token_UsesNonceRotatedByResourceResponse(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))

	listApps(t, tc)
	require.Equal(t, "nonce-1", tc.currentNonce())

	tc.stub.setEmitResourceNonce("nonce-2")
	listApps(t, tc)
	require.Equal(t, "nonce-2", tc.currentNonce())

	tc.stub.setTokenNonce("nonce-2")
	tc.clock.advance(time.Hour)
	listApps(t, tc)
	require.Equal(t, 3, tc.signer.Calls())
	require.Equal(t, stubCounts{tokenRequests: 3, assertionsSeen: 3, issuedTokens: 2}, tc.stub.counts())
}

func TestClient_Token_StubRejectsReplayedAssertionJTI(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	tc.signer.setReplay(true)

	_, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0})
	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Equal(t, "invalid_client", apiErr.ErrorCode)
	require.Contains(t, apiErr.Summary, "already been used")
	require.Equal(t, 2, tc.signer.Calls())
}

func TestClient_Token_RefreshesAfterExpiry(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))

	listApps(t, tc)
	require.Equal(t, 2, tc.signer.Calls())

	tc.clock.advance(3600*time.Second - tokenExpirySafetyMargin + time.Second)
	listApps(t, tc)
	require.Equal(t, 3, tc.signer.Calls())
	require.Equal(t, 2, tc.stub.counts().issuedTokens)
}

func TestClient_Token_StubRejectsStaleProofIAT(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	tc.client.now = func() time.Time { return tc.clock.Now().Add(-2 * proofIATLeeway) }

	_, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0})
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, "invalid_dpop_proof", apiErr.ErrorCode)
	require.Contains(t, apiErr.Summary, "iat out of range")
}

func TestClient_ResourceProofBindsATHAndExactHTU(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(2))
	tc.stub.setPageSize(1)

	apps, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "ACTIVE", Limit: 1})
	require.NoError(t, err)
	require.Len(t, apps, 2)

	tokens := tc.stub.issuedTokens()
	require.Len(t, tokens, 1)
	sum := sha256.Sum256([]byte(tokens[0]))
	wantATH := base64.RawURLEncoding.EncodeToString(sum[:])

	var resourceProofs []proofRecord
	for _, p := range tc.stub.recordedProofs() {
		if p.method == http.MethodGet {
			resourceProofs = append(resourceProofs, p)
		}
	}
	require.Len(t, resourceProofs, 2)
	seen := map[string]bool{}
	for _, p := range resourceProofs {
		require.Equal(t, http.MethodGet, p.htm)
		require.Equal(t, tc.stub.srv.URL+"/api/v1/apps", p.htu)
		require.Equal(t, wantATH, p.ath)
		require.False(t, seen[p.jti])
		seen[p.jti] = true
	}
}

func TestClient_ListApps_StatusBuildsFilter(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))

	_, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: "gr", Status: "INACTIVE", Limit: 5})
	require.NoError(t, err)
	q := tc.stub.appsQuery()
	require.Equal(t, `status eq "INACTIVE"`, q.Get("filter"))
	require.Equal(t, "gr", q.Get("q"))
	require.Equal(t, "5", q.Get("limit"))

	listApps(t, tc)
	require.False(t, tc.stub.appsQuery().Has("filter"))
}

func TestClient_ResourceNonceChallengeRetriesOnce(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	tc.stub.setResourceNonce(true, "rs-nonce")

	app, err := tc.client.GetApp(t.Context(), "0oa000")
	require.NoError(t, err)
	require.Equal(t, "0oa000", app.ID)
	require.Equal(t, "rs-nonce", tc.currentNonce())
	require.Equal(t, 2, tc.signer.Calls())

	_, err = tc.client.GetApp(t.Context(), "0oa000")
	require.NoError(t, err)
	require.Equal(t, 2, tc.signer.Calls())
}

func TestClient_ResourceSecondNonceChallengeReturnsErrorWithoutRemint(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	listApps(t, tc)
	require.Equal(t, 2, tc.signer.Calls())

	var calls int
	var mu sync.Mutex
	tc.stub.setOverride("/api/v1/challenge", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		w.Header().Set("DPoP-Nonce", fmt.Sprintf("rs-%d", n))
		w.Header().Set("WWW-Authenticate", `DPoP error="use_dpop_nonce"`)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errorCode":"E0000011","errorSummary":"nonce required"}`))
	})

	_, _, err := getJSON[appJSON](t.Context(), tc.client, tc.client.apiURL("/api/v1/challenge", "", nil))
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
	mu.Lock()
	require.Equal(t, 2, calls)
	mu.Unlock()
	require.Equal(t, 2, tc.signer.Calls())
	require.Equal(t, 2, tc.stub.counts().tokenRequests)
}

func TestClient_RejectedTokenRefreshesOnce(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	listApps(t, tc)
	require.Equal(t, 2, tc.signer.Calls())

	var calls int
	var mu sync.Mutex
	tc.stub.setOverride("/api/v1/revoked", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.Header().Set("WWW-Authenticate", `DPoP error="invalid_token"`)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errorCode":"E0000011","errorSummary":"Invalid token provided"}`))
	})

	_, _, err := getJSON[appJSON](t.Context(), tc.client, tc.client.apiURL("/api/v1/revoked", "", nil))
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
	mu.Lock()
	require.Equal(t, 2, calls)
	mu.Unlock()
	require.Equal(t, 3, tc.signer.Calls())
	require.Equal(t, 2, tc.stub.counts().issuedTokens)
}

func TestClient_ListApps_PaginationBoundary(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.MaxPages = 3
	tc := newTestClient(t, testenv.NewTracerProvider(t), testenv.NewLogger(t), cfg)
	tc.stub.setApps(stubApps(5))
	tc.stub.setPageSize(2)

	apps, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 2})
	require.NoError(t, err)
	require.Len(t, apps, 5)
	require.Equal(t, "0oa004", apps[4].ID)

	tc.stub.setApps(stubApps(7))
	_, err = tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 2})
	require.ErrorIs(t, err, ErrTooManyPages)
}

func TestClient_RateLimited429BacksOffUntilReset(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	now := tc.clock.Now()
	tc.stub.setPending429(1)
	tc.stub.setRateLimit(100, 0, now.Add(7*time.Second).Unix())

	apps := listApps(t, tc)
	require.Len(t, apps, 1)
	require.Equal(t, []time.Duration{7 * time.Second}, tc.sleeper.Waits())
	require.Equal(t, 2, tc.signer.Calls())

	// The post-429 success carries remaining=0, so the next call slows down once more.
	tc.stub.setRateLimit(100, 90, now.Add(7*time.Second).Unix())
	listApps(t, tc)
	require.Equal(t, []time.Duration{7 * time.Second, 7 * time.Second}, tc.sleeper.Waits())
	listApps(t, tc)
	require.Len(t, tc.sleeper.Waits(), 2)
}

func TestClient_RateLimitExhaustedReturnsError(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	tc.stub.setPending429(maxRateLimitRetries + 1)
	tc.stub.setRateLimit(100, 90, tc.clock.Now().Add(time.Second).Unix())

	_, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0})
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
	require.Len(t, tc.sleeper.Waits(), maxRateLimitRetries)
}

func TestClient_SlowsDownBelowTwentyPercentRemaining(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	now := tc.clock.Now()
	tc.stub.setRateLimit(100, 19, now.Add(11*time.Second).Unix())

	listApps(t, tc)
	require.Empty(t, tc.sleeper.Waits())

	listApps(t, tc)
	require.Equal(t, []time.Duration{11 * time.Second}, tc.sleeper.Waits())

	tc.stub.setRateLimit(100, 20, now.Add(11*time.Second).Unix())
	tc.clearSlowdown()
	listApps(t, tc)
	listApps(t, tc)
	require.Len(t, tc.sleeper.Waits(), 1)
}

func TestClient_SlowdownCapsAtMaxWaitAndResets(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	now := tc.clock.Now()
	tc.stub.setRateLimit(100, 1, now.Add(time.Hour).Unix())

	listApps(t, tc)
	tc.client.mu.Lock()
	require.Equal(t, now.Add(maxRateLimitWait), tc.client.slowUntil)
	tc.client.mu.Unlock()

	tc.stub.setRateLimit(0, 0, 0)
	listApps(t, tc)
	require.Equal(t, []time.Duration{maxRateLimitWait}, tc.sleeper.Waits())
	tc.client.mu.Lock()
	require.True(t, tc.client.slowUntil.IsZero())
	tc.client.mu.Unlock()

	listApps(t, tc)
	require.Len(t, tc.sleeper.Waits(), 1)
}

func TestClient_ResponseBodyLimitTruncates(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setOverride("/api/v1/huge", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"huge","label":"`))
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxResponseBytes))
		_, _ = w.Write([]byte(`"}`))
	})

	_, _, err := getJSON[appJSON](t.Context(), tc.client, tc.client.apiURL("/api/v1/huge", "", nil))
	require.ErrorIs(t, err, ErrResponseTooLarge)

	tc.stub.setOverride("/api/v1/fits", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"fits","label":"`))
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxResponseBytes-64))
		_, _ = w.Write([]byte(`"}`))
	})
	app, _, err := getJSON[appJSON](t.Context(), tc.client, tc.client.apiURL("/api/v1/fits", "", nil))
	require.NoError(t, err)
	require.Equal(t, "fits", app.ID)
}

func TestClient_NeverLogsOrTracesCredentials(t *testing.T) {
	t.Parallel()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{AddSource: false, Level: slog.LevelDebug, ReplaceAttr: nil}))

	tc := newTestClient(t, provider, logger, testConfig())
	tc.stub.setApps(stubApps(1))
	tc.stub.setPending429(1)
	tc.stub.setRateLimit(100, 5, tc.clock.Now().Add(time.Second).Unix())

	listApps(t, tc)
	_, err := tc.client.GetApp(t.Context(), "missing")
	require.True(t, IsNotFound(err))

	spans := exporter.GetSpans()
	require.NotEmpty(t, spans)
	var dump strings.Builder
	for _, span := range spans {
		for _, kv := range span.Attributes {
			fmt.Fprintf(&dump, "%s=%s\n", kv.Key, kv.Value.Emit())
		}
		for _, ev := range span.Events {
			for _, kv := range ev.Attributes {
				fmt.Fprintf(&dump, "%s=%s\n", kv.Key, kv.Value.Emit())
			}
		}
	}
	dump.WriteString(logs.String())
	output := dump.String()
	require.NotEmpty(t, logs.String())

	for _, tok := range tc.stub.issuedTokens() {
		require.NotContains(t, output, tok)
	}
	require.NotContains(t, output, tc.signer.Last())
	require.NotContains(t, output, "nonce-1")
	require.NotContains(t, output, "authorization")
	require.NotContains(t, output, "Authorization")
	require.NotContains(t, output, "DPoP")
	require.NotContains(t, output, "dpop")
	require.NotContains(t, output, "access_token")
	require.NotContains(t, output, "client_assertion")
}

func TestPartitionByHashedHost_NeverExposesHostname(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://customer.okta.com/api/v1/apps", nil)
	require.NoError(t, err)

	segments := partitionByHashedHost()(req)
	require.Len(t, segments, 2)
	require.True(t, strings.HasPrefix(segments[0], "host-sha256-"))
	require.NotContains(t, strings.Join(segments, "/"), "customer")
	require.Equal(t, "443", segments[1])
	require.Equal(t, segments, partitionByHashedHost()(req))
}

func TestClient_VerifyScopes_ReportsTrimmedGrant(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)

	v, err := tc.client.VerifyScopes(t.Context(), []string{"okta.apps.read", "okta.users.read"})
	require.NoError(t, err)
	require.Equal(t, []string{"okta.apps.read"}, v.Granted)
	require.Equal(t, []string{"okta.users.read"}, v.Missing)
	require.True(t, v.DPoPBound)
	require.Equal(t, tc.clock.Now().Add(time.Hour), v.ExpiresAt)
	require.False(t, v.OK())

	v, err = tc.client.VerifyScopes(t.Context(), []string{"okta.groups.read"})
	require.NoError(t, err)
	require.True(t, v.OK())
	require.Equal(t, 3, tc.signer.Calls())
}

func TestClient_VerifyScopes_AllUngrantedIsMissingNotError(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)

	v, err := tc.client.VerifyScopes(t.Context(), []string{"okta.users.read"})
	require.NoError(t, err)
	require.Equal(t, []string{}, v.Granted)
	require.Equal(t, []string{"okta.users.read"}, v.Missing)
	require.False(t, v.DPoPBound)
	require.True(t, v.ExpiresAt.IsZero())
}

func TestClient_VerifyScopes_EvictsCachedTokenAndDoesNotCacheVerification(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	listApps(t, tc)
	require.Equal(t, 2, tc.signer.Calls())

	tc.stub.setGrantedScopes([]string{"okta.apps.read", "okta.users.read", "okta.groups.read"})
	v, err := tc.client.VerifyScopes(t.Context(), []string{"okta.users.read"})
	require.NoError(t, err)
	require.True(t, v.OK())
	require.Equal(t, 3, tc.signer.Calls())

	listApps(t, tc)
	require.Equal(t, 4, tc.signer.Calls())
}

func TestClient_AssignmentsAndGroups(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	ts := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	users := []appUserJSON{{ID: "00u1", Scope: "USER", Status: "PROVISIONED", Created: ts, LastUpdated: ts, Credentials: struct {
		UserName string `json:"userName"`
	}{UserName: "ada@example.com"}}}
	tc.stub.setAppUsers("0oa1", users)
	tc.stub.setAppGroups("0oa1", []appGroupJSON{{ID: "00g1", Priority: 2, LastUpdated: ts}})
	tc.stub.setGroups([]groupJSON{{ID: "00g1", Type: "OKTA_GROUP", Created: ts, LastUpdated: ts, Profile: struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}{Name: "Engineering", Description: "eng"}}})

	appUsers, err := tc.client.ListAppUsers(t.Context(), ListAppUsersRequest{AppID: "0oa1", Limit: 0})
	require.NoError(t, err)
	require.Equal(t, []AppUser{{ID: "00u1", Scope: "USER", Status: "PROVISIONED", UserName: "ada@example.com", Created: ts, LastUpdated: ts}}, appUsers)

	appGroups, err := tc.client.ListAppGroups(t.Context(), ListAppGroupsRequest{AppID: "0oa1", Limit: 0})
	require.NoError(t, err)
	require.Equal(t, []AppGroup{{ID: "00g1", Priority: 2, LastUpdated: ts}}, appGroups)

	groups, err := tc.client.ListGroups(t.Context(), ListGroupsRequest{Search: "", Limit: 0})
	require.NoError(t, err)
	require.Equal(t, []Group{{ID: "00g1", Type: "OKTA_GROUP", Name: "Engineering", Description: "eng", Created: ts, LastUpdated: ts}}, groups)

	_, err = tc.client.ListAppUsers(t.Context(), ListAppUsersRequest{AppID: "", Limit: 0})
	require.Error(t, err)
}

func TestNewClient_RequiresTokenEndpointAudienceFormat(t *testing.T) {
	t.Parallel()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	factory := NewClientFactory(testenv.NewLogger(t), policy, &stubSigner{clock: &fakeClock{mu: sync.Mutex{}, now: time.Now()}, mu: sync.Mutex{}, calls: 0, requests: nil, jtis: nil, replay: false, last: ""})

	cfg := testConfig()
	cfg.OrgURL = "https://example.okta.com"
	cfg.ClientID = stubClientID
	cfg.RemoteSessionClientID = uuid.New()
	for _, format := range []string{"", string(remotesessions.TokenEndpointAuthAudienceIssuer)} {
		cfg.AudienceFormat = format
		_, err = factory.Client(cfg)
		require.Error(t, err)
	}

	cfg.AudienceFormat = string(remotesessions.TokenEndpointAuthAudienceTokenEndpoint)
	client, err := factory.Client(cfg)
	require.NoError(t, err)
	impl, ok := client.(*httpClient)
	require.True(t, ok)
	require.Equal(t, "https://example.okta.com"+tokenEndpointPath, impl.audience)

	cfg.OrgURL = "not a url"
	cfg.RemoteSessionClientID = uuid.New()
	_, err = factory.Client(cfg)
	require.Error(t, err)
}

func TestNewClient_OrgURLRequiresHTTPSExceptLoopback(t *testing.T) {
	t.Parallel()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	factory := NewClientFactory(testenv.NewLogger(t), policy, &stubSigner{clock: &fakeClock{mu: sync.Mutex{}, now: time.Now()}, mu: sync.Mutex{}, calls: 0, requests: nil, jtis: nil, replay: false, last: ""})

	cfg := testConfig()
	cfg.ClientID = stubClientID
	cfg.AudienceFormat = string(remotesessions.TokenEndpointAuthAudienceTokenEndpoint)

	for _, raw := range []string{
		"https://example.okta.com",
		"https://example.okta.com/",
		"https://example.okta.com:8443",
		"http://127.0.0.1:9000",
		"http://localhost:9000",
		"http://[::1]:9000",
	} {
		cfg.OrgURL = raw
		cfg.RemoteSessionClientID = uuid.New()
		_, err = factory.Client(cfg)
		require.NoError(t, err, raw)
	}

	for _, raw := range []string{
		"http://example.okta.com",
		"http://127.0.0.1.example.com",
		"ftp://example.okta.com",
		"example.okta.com",
		"https://example.okta.com/oauth2",
		"https://example.okta.com/?x=1",
		"https://example.okta.com/#frag",
		"https://user:pw@example.okta.com",
	} {
		cfg.OrgURL = raw
		cfg.RemoteSessionClientID = uuid.New()
		_, err = factory.Client(cfg)
		require.Error(t, err, raw)
		require.Contains(t, err.Error(), "okta: ", raw)
	}

	cfg.OrgURL = "http://example.okta.com"
	cfg.RemoteSessionClientID = uuid.New()
	_, err = factory.Client(cfg)
	require.EqualError(t, err, `okta: org url "http://example.okta.com" must use https`)

	cfg.OrgURL = "https://example.okta.com/oauth2"
	cfg.RemoteSessionClientID = uuid.New()
	_, err = factory.Client(cfg)
	require.EqualError(t, err, `okta: org url "https://example.okta.com/oauth2" must be an origin without path, query, fragment, or userinfo`)
}

func TestClient_VerifyScopes_BearerTokenIsNotOK(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setTokenType("Bearer")

	v, err := tc.client.VerifyScopes(t.Context(), []string{"okta.apps.read", "okta.groups.read"})
	require.NoError(t, err)
	require.Empty(t, v.Missing)
	require.Equal(t, []string{"okta.apps.read", "okta.groups.read"}, v.Granted)
	require.False(t, v.DPoPBound)
	require.False(t, v.OK())

	_, err = tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0})
	require.EqualError(t, err, `okta token response type "Bearer" is not DPoP`)
}

func TestClient_ResourceRedirectIsNotFollowed(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	sink := newSinkServer(t)
	tc.stub.setOverride("/api/v1/elsewhere", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, sink.srv.URL+"/api/v1/apps", http.StatusTemporaryRedirect)
	})

	_, _, err := getJSON[appJSON](t.Context(), tc.client, tc.client.apiURL("/api/v1/elsewhere", "", nil))
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusTemporaryRedirect, apiErr.StatusCode)
	require.Empty(t, sink.Hits())
	require.Equal(t, 2, tc.signer.Calls())
}

func TestClient_TokenRedirectIsNotFollowed(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	sink := newSinkServer(t)
	tc.stub.setTokenRedirect(sink.srv.URL + tokenEndpointPath)

	_, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0})
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusTemporaryRedirect, apiErr.StatusCode)
	require.Equal(t, tokenEndpointPath, apiErr.Path)
	require.Empty(t, sink.Hits())
	require.Equal(t, 1, tc.signer.Calls())
	require.Equal(t, stubCounts{tokenRequests: 1, assertionsSeen: 0, issuedTokens: 0}, tc.stub.counts())
}

func TestClient_TokenEndpoint429BacksOffWithFreshAssertion(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	now := tc.clock.Now()
	tc.stub.setTokenPending429(2)
	tc.stub.setRateLimit(100, 0, now.Add(5*time.Second).Unix())

	apps := listApps(t, tc)
	require.Len(t, apps, 1)
	require.Equal(t, []time.Duration{5 * time.Second, 5 * time.Second}, tc.sleeper.Waits())

	// Two throttled attempts, one nonce challenge, one success: four assertions, all distinct.
	require.Equal(t, 4, tc.signer.Calls())
	jtis := tc.signer.JTIs()
	require.Len(t, jtis, 4)
	seen := map[string]bool{}
	for _, jti := range jtis {
		require.False(t, seen[jti])
		seen[jti] = true
	}
	require.Equal(t, stubCounts{tokenRequests: 4, assertionsSeen: 2, issuedTokens: 1}, tc.stub.counts())
}

func TestClient_TokenEndpoint429ExhaustedReturnsError(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	tc.stub.setTokenPending429(maxRateLimitRetries + 1)
	tc.stub.setRateLimit(100, 0, tc.clock.Now().Add(time.Hour).Unix())

	_, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0})
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
	require.Equal(t, tokenEndpointPath, apiErr.Path)
	require.Equal(t, maxRateLimitRetries+1, tc.signer.Calls())
	waits := tc.sleeper.Waits()
	require.Len(t, waits, maxRateLimitRetries)
	for _, w := range waits {
		require.Equal(t, maxRateLimitWait, w)
	}
	require.Equal(t, 0, tc.stub.counts().issuedTokens)
}

func TestClient_TokenRequestCarriesClientID(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	tc.client.cfg.ClientID = "0oa-other-client"

	_, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0})
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, "invalid_client", apiErr.ErrorCode)
	require.Contains(t, apiErr.Summary, "client_id mismatch")
}

func TestClient_WaitForSlowdownKeepsNewerDeadline(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	now := tc.clock.Now()
	first := now.Add(3 * time.Second)
	second := now.Add(9 * time.Second)

	tc.client.mu.Lock()
	tc.client.slowUntil = first
	tc.client.mu.Unlock()

	// A concurrent observer moves the deadline while this waiter sleeps.
	tc.client.sleep = func(ctx context.Context, d time.Duration) error {
		tc.client.mu.Lock()
		tc.client.slowUntil = second
		tc.client.mu.Unlock()
		return tc.sleeper.sleep(ctx, d)
	}
	require.NoError(t, tc.client.waitForSlowdown(t.Context()))
	require.Equal(t, []time.Duration{3 * time.Second}, tc.sleeper.Waits())
	tc.client.mu.Lock()
	require.Equal(t, second, tc.client.slowUntil)
	tc.client.mu.Unlock()

	tc.client.sleep = tc.sleeper.sleep
	require.NoError(t, tc.client.waitForSlowdown(t.Context()))
	require.Equal(t, []time.Duration{3 * time.Second, 9 * time.Second}, tc.sleeper.Waits())
	tc.client.mu.Lock()
	require.True(t, tc.client.slowUntil.IsZero())
	tc.client.mu.Unlock()
}

func TestFake_RequiresAppID(t *testing.T) {
	t.Parallel()
	fake := NewFake(Fixtures{Apps: nil, AppUsers: nil, AppGroups: nil, Groups: nil, GrantedScopes: nil})

	_, err := fake.GetApp(t.Context(), "")
	require.EqualError(t, err, "okta: app id is required")
	_, err = fake.ListAppUsers(t.Context(), ListAppUsersRequest{AppID: "", Limit: 0})
	require.EqualError(t, err, "okta: app id is required")
	_, err = fake.ListAppGroups(t.Context(), ListAppGroupsRequest{AppID: "", Limit: 0})
	require.EqualError(t, err, "okta: app id is required")
}

func TestFake_VerifyScopesMirrorsOkta(t *testing.T) {
	t.Parallel()
	fake := NewFake(Fixtures{Apps: nil, AppUsers: nil, AppGroups: nil, Groups: nil, GrantedScopes: []string{"okta.apps.read", "okta.groups.read"}})

	v, err := fake.VerifyScopes(t.Context(), []string{"okta.apps.read", "okta.users.read"})
	require.NoError(t, err)
	require.Equal(t, []string{"okta.apps.read"}, v.Granted)
	require.Equal(t, []string{"okta.users.read"}, v.Missing)
	require.True(t, v.DPoPBound)
	require.False(t, v.ExpiresAt.IsZero())
	require.False(t, v.OK())

	v, err = fake.VerifyScopes(t.Context(), []string{"okta.users.read"})
	require.NoError(t, err)
	require.Equal(t, []string{}, v.Granted)
	require.Equal(t, []string{"okta.users.read"}, v.Missing)
	require.False(t, v.DPoPBound)
	require.True(t, v.ExpiresAt.IsZero())
	require.False(t, v.OK())

	v, err = fake.VerifyScopes(t.Context(), []string{"okta.groups.read"})
	require.NoError(t, err)
	require.Equal(t, []string{"okta.groups.read"}, v.Granted)
	require.Empty(t, v.Missing)
	require.True(t, v.OK())

	v, err = fake.VerifyScopes(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, []string{"okta.apps.read", "okta.groups.read"}, v.Granted)
	require.True(t, v.OK())
}

func TestFake_ClonesAppFeatures(t *testing.T) {
	t.Parallel()
	fake := NewFake(Fixtures{
		Apps:          []App{{ID: "0oa1", Label: "Gram", Name: "oidc_client", SignOnMode: "OPENID_CONNECT", Status: "ACTIVE", Features: []string{"PUSH_NEW_USERS"}, Created: time.Time{}, LastUpdated: time.Time{}}},
		AppUsers:      nil,
		AppGroups:     nil,
		Groups:        nil,
		GrantedScopes: nil,
	})

	apps, err := fake.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0})
	require.NoError(t, err)
	apps[0].Features[0] = "mutated"

	app, err := fake.GetApp(t.Context(), "0oa1")
	require.NoError(t, err)
	require.Equal(t, []string{"PUSH_NEW_USERS"}, app.Features)
	app.Features[0] = "mutated"

	apps, err = fake.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0})
	require.NoError(t, err)
	require.Equal(t, []string{"PUSH_NEW_USERS"}, apps[0].Features)
}

func TestClientFactory_MemoizesAndForgets(t *testing.T) {
	t.Parallel()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	factory := NewClientFactory(testenv.NewLogger(t), policy, &stubSigner{clock: &fakeClock{mu: sync.Mutex{}, now: time.Now()}, mu: sync.Mutex{}, calls: 0, requests: nil, jtis: nil, replay: false, last: ""})

	cfg := testConfig()
	cfg.OrgURL = "https://example.okta.com"
	cfg.ClientID = stubClientID
	cfg.AudienceFormat = string(remotesessions.TokenEndpointAuthAudienceTokenEndpoint)
	cfg.RemoteSessionClientID = uuid.New()

	a, err := factory.Client(cfg)
	require.NoError(t, err)
	b, err := factory.Client(cfg)
	require.NoError(t, err)
	require.Same(t, a, b)

	other := cfg
	other.RemoteSessionClientID = uuid.New()
	c, err := factory.Client(other)
	require.NoError(t, err)
	require.NotSame(t, a, c)

	factory.Forget(cfg.RemoteSessionClientID)
	d, err := factory.Client(cfg)
	require.NoError(t, err)
	require.NotSame(t, a, d)
}

func TestNextLink_ParsesRelParams(t *testing.T) {
	t.Parallel()
	base := mustParseURL(t, "https://example.okta.com/api/v1/apps?limit=2")
	header := http.Header{}
	header.Add("Link", `<https://example.okta.com/api/v1/apps?limit=2&after=abc>; rel="next"`)
	next, err := nextLink(header, base)
	require.NoError(t, err)
	require.Equal(t, "https://example.okta.com/api/v1/apps?limit=2&after=abc", next.String())

	header = http.Header{}
	header.Add("Link", `<https://example.okta.com/api/v1/apps?after=x>; title="next"; rel=NEXT`)
	next, err = nextLink(header, base)
	require.NoError(t, err)
	require.Equal(t, "https://example.okta.com/api/v1/apps?after=x", next.String())

	header = http.Header{}
	header.Add("Link", `<https://example.okta.com/api/v1/apps?after=y>; rel="nextish"`)
	next, err = nextLink(header, base)
	require.NoError(t, err)
	require.Nil(t, next)

	header = http.Header{}
	header.Add("Link", `<https://example.okta.com/api/v1/apps?after=z>; title="rel=next"`)
	next, err = nextLink(header, base)
	require.NoError(t, err)
	require.Nil(t, next)

	header = http.Header{}
	header.Add("Link", `<https://evil.example.com/api/v1/apps>; rel="next"`)
	_, err = nextLink(header, base)
	require.Error(t, err)

	header = http.Header{}
	header.Add("Link", `<https://example.okta.com/api/v1/apps>; rel="self"`)
	next, err = nextLink(header, base)
	require.NoError(t, err)
	require.Nil(t, next)
}

func TestDPoPKey_ThumbprintDiffersPerInstance(t *testing.T) {
	t.Parallel()
	a, err := newDPoPKey()
	require.NoError(t, err)
	b, err := newDPoPKey()
	require.NoError(t, err)
	require.NotEqual(t, a.thumbprint, b.thumbprint)

	target := mustParseURL(t, "https://example.okta.com/api/v1/apps?limit=1#frag")
	require.Equal(t, "https://example.okta.com/api/v1/apps", dpopHTU(target))
}

func TestFake_Fixtures(t *testing.T) {
	t.Parallel()
	fake := NewFake(Fixtures{
		Apps:          []App{{ID: "0oa1", Label: "Gram", Name: "oidc_client", SignOnMode: "OPENID_CONNECT", Status: "ACTIVE", Features: nil, Created: time.Time{}, LastUpdated: time.Time{}}},
		AppUsers:      map[string][]AppUser{"0oa1": {{ID: "00u1", Scope: "USER", Status: "PROVISIONED", UserName: "", Created: time.Time{}, LastUpdated: time.Time{}}}},
		AppGroups:     nil,
		Groups:        []Group{{ID: "00g1", Type: "OKTA_GROUP", Name: "Eng", Description: "", Created: time.Time{}, LastUpdated: time.Time{}}},
		GrantedScopes: []string{"okta.apps.read"},
	})

	apps, err := fake.ListApps(t.Context(), ListAppsRequest{Query: "gr", Status: "", Limit: 0})
	require.NoError(t, err)
	require.Len(t, apps, 1)

	_, err = fake.GetApp(t.Context(), "nope")
	require.True(t, IsNotFound(err))

	users, err := fake.ListAppUsers(t.Context(), ListAppUsersRequest{AppID: "0oa1", Limit: 0})
	require.NoError(t, err)
	require.Len(t, users, 1)

	v, err := fake.VerifyScopes(t.Context(), []string{"okta.apps.read", "okta.groups.read"})
	require.NoError(t, err)
	require.Equal(t, []string{"okta.groups.read"}, v.Missing)

	groups, err := fake.ListGroups(t.Context(), ListGroupsRequest{Search: "en", Limit: 0})
	require.NoError(t, err)
	require.Len(t, groups, 1)

	fake.SetError(errors.New("boom"))
	_, err = fake.ListGroups(t.Context(), ListGroupsRequest{Search: "", Limit: 0})
	require.EqualError(t, err, "boom")
	require.Equal(t, []string{"ListApps", "GetApp", "ListAppUsers", "VerifyScopes", "ListGroups", "ListGroups"}, fake.Calls())
}

func TestFakeFactory_KeyedByOrgURL(t *testing.T) {
	t.Parallel()
	factory := NewFakeFactory(map[string]Fixtures{
		"https://a.okta.com": {Apps: []App{{ID: "0oaA", Label: "A", Name: "", SignOnMode: "", Status: "", Features: nil, Created: time.Time{}, LastUpdated: time.Time{}}}, AppUsers: nil, AppGroups: nil, Groups: nil, GrantedScopes: nil},
		"https://b.okta.com": {Apps: []App{{ID: "0oaB", Label: "B", Name: "", SignOnMode: "", Status: "", Features: nil, Created: time.Time{}, LastUpdated: time.Time{}}}, AppUsers: nil, AppGroups: nil, Groups: nil, GrantedScopes: nil},
	})

	cfg := testConfig()
	cfg.OrgURL = "https://a.okta.com"
	a, err := factory.Client(cfg)
	require.NoError(t, err)
	apps, err := a.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0})
	require.NoError(t, err)
	require.Equal(t, "0oaA", apps[0].ID)

	cfg.OrgURL = "https://b.okta.com"
	b, err := factory.Client(cfg)
	require.NoError(t, err)
	apps, err = b.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0})
	require.NoError(t, err)
	require.Equal(t, "0oaB", apps[0].ID)
	require.Equal(t, []string{"ListApps"}, factory.Fake("https://b.okta.com").Calls())

	cfg.OrgURL = "https://c.okta.com"
	_, err = factory.Client(cfg)
	require.Error(t, err)
}
