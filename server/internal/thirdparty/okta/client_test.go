package okta

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/speakeasy-api/gram/server/internal/dpop"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// tokenProofs returns the proofs the stub verified on the token endpoint, in order.
func tokenProofs(tc testClient) []proofRecord {
	var out []proofRecord
	for _, p := range tc.stub.recordedProofs() {
		if p.method == http.MethodPost {
			out = append(out, p)
		}
	}
	return out
}

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
		tc.stub.issueNonce(w, fmt.Sprintf("rs-%d", n))
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
	require.Equal(t, []time.Duration{7*time.Second + maxRateLimitJitter/2}, tc.sleeper.Waits())
	require.Equal(t, 2, tc.signer.Calls())

	// The retry already reached reset, so the next call need not slow down.
	tc.stub.setRateLimit(100, 90, now.Add(7*time.Second).Unix())
	listApps(t, tc)
	require.Equal(t, []time.Duration{7*time.Second + maxRateLimitJitter/2}, tc.sleeper.Waits())
	listApps(t, tc)
	require.Len(t, tc.sleeper.Waits(), 1)
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

func TestClient_TokenResponseSlowdownDelaysFirstResourceCall(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	tc.stub.setTokenRateLimit(100, 19, tc.clock.Now().Add(11*time.Second).Unix())

	listApps(t, tc)
	require.Equal(t, []time.Duration{11 * time.Second}, tc.sleeper.Waits())
	require.Equal(t, 1, tc.stub.counts().issuedTokens)
}

func TestClient_VerifyScopes_HonorsSlowdown(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	tc.stub.setRateLimit(100, 19, tc.clock.Now().Add(11*time.Second).Unix())

	listApps(t, tc)
	require.Empty(t, tc.sleeper.Waits())

	v, err := tc.client.VerifyScopes(t.Context(), []string{"okta.apps.read"})
	require.NoError(t, err)
	require.True(t, v.OK())
	require.Equal(t, []time.Duration{11 * time.Second}, tc.sleeper.Waits())
}

func TestClient_APIErrorPathIsEscaped(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)

	_, err := tc.client.GetApp(t.Context(), "0oa 1?x")
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, "/api/v1/apps/0oa%201%3Fx", apiErr.Path)

	fake := NewFake(Fixtures{Apps: nil, AppUsers: nil, AppGroups: nil, Groups: nil, GrantedScopes: nil})
	_, err = fake.GetApp(t.Context(), "0oa 1?x")
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, "/api/v1/apps/0oa%201%3Fx", apiErr.Path)
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
	assertions := tc.signer.Assertions()
	require.Len(t, assertions, 2)
	for _, assertion := range assertions {
		require.NotContains(t, output, assertion)
	}
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

func TestClient_VerifyScopes_ConsentRequiredIsMissingNotError(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setScopeErrorCode("consent_required")

	v, err := tc.client.VerifyScopes(t.Context(), []string{"okta.users.read"})
	require.NoError(t, err)
	require.Equal(t, []string{}, v.Granted)
	require.Equal(t, []string{"okta.users.read"}, v.Missing)
	require.False(t, v.DPoPBound)
	require.True(t, v.ExpiresAt.IsZero())
	_, cached := tc.client.cachedToken()
	require.False(t, cached)
}

// A verification token whose grant covers the default scopes is kept, so a
// verify costs exactly one token request and the reads after it mint nothing.
func TestClient_VerifyScopes_ReusesVerificationTokenCoveringDefaults(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	listApps(t, tc)
	require.Equal(t, 2, tc.stub.counts().tokenRequests, "the first mint pays the nonce challenge")

	tc.stub.setGrantedScopes([]string{"okta.apps.read", "okta.users.read", "okta.groups.read"})
	v, err := tc.client.VerifyScopes(t.Context(), []string{"okta.apps.read", "okta.users.read", "okta.groups.read"})
	require.NoError(t, err)
	require.True(t, v.OK())
	require.Equal(t, 3, tc.stub.counts().tokenRequests)
	require.Equal(t, 3, tc.signer.Calls())

	listApps(t, tc)
	require.Equal(t, 3, tc.stub.counts().tokenRequests, "the reads after a verify reuse its token")
	require.Equal(t, 3, tc.signer.Calls())
}

// A verification token missing a default scope is never kept: the cached
// token is evicted and the next read mints with the default scopes.
func TestClient_VerifyScopes_EvictsWhenGrantLacksDefaults(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	listApps(t, tc)
	require.Equal(t, 2, tc.stub.counts().tokenRequests)

	tc.stub.setGrantedScopes([]string{"okta.users.read"})
	v, err := tc.client.VerifyScopes(t.Context(), []string{"okta.users.read"})
	require.NoError(t, err)
	require.True(t, v.OK())
	require.Equal(t, 3, tc.stub.counts().tokenRequests)

	listApps(t, tc)
	require.Equal(t, 4, tc.stub.counts().tokenRequests, "a partial grant must not serve the reads")
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
		"https://example.okta.com?///",
		"https://example.okta.com#///",
		"https://example.okta.com?",
		"https://example.okta.com#",
		"https://user:pw@example.okta.com",
		"https://:443",
		"https://bücher.example",
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
	require.Equal(t, []time.Duration{5*time.Second + maxRateLimitJitter/2, time.Second + maxRateLimitJitter/2}, tc.sleeper.Waits())

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

	// RFC 9449 §7.3, §11.1: every attempt, throttled ones included, signed a fresh proof.
	var proofJTIs []string
	for _, p := range tokenProofs(tc) {
		proofJTIs = append(proofJTIs, p.jti)
	}
	require.Len(t, proofJTIs, 4)
	require.Len(t, slices.Compact(slices.Sorted(slices.Values(proofJTIs))), 4)
}

// RFC 9449 §8.2: a nonce on a successful token response is used on the next
// token request with no challenge round trip and no extra assertion.
func TestClient_Token_NonceFromSuccessfulResponseSkipsChallenge(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	tc.stub.setEmitTokenNonce("nonce-2")

	listApps(t, tc)
	require.Equal(t, 2, tc.signer.Calls())
	require.Equal(t, "nonce-2", tc.currentNonce())

	tc.stub.setTokenNonce("nonce-2")
	tc.clock.advance(time.Hour)
	listApps(t, tc)
	require.Equal(t, 3, tc.signer.Calls())
	require.Equal(t, stubCounts{tokenRequests: 3, assertionsSeen: 3, issuedTokens: 2}, tc.stub.counts())

	proofs := tokenProofs(tc)
	require.Len(t, proofs, 3)
	require.Equal(t, []string{"", "nonce-1", "nonce-2"}, []string{proofs[0].nonce, proofs[1].nonce, proofs[2].nonce})
}

// RFC 9449 §4.3 step 1: a request with two DPoP header fields is rejected.
func TestStub_RejectsDuplicateDPoPHeader(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	listApps(t, tc)
	token := tc.stub.issuedTokens()[0]

	target := tc.client.apiURL("/api/v1/apps", "", nil)
	proof, err := tc.client.key.Proof(http.MethodGet, target, dpop.ProofOptions{AccessToken: token, Nonce: "", IssuedAt: tc.clock.Now()})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target.String(), nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "DPoP "+token)
	req.Header.Add("DPoP", proof)
	req.Header.Add("DPoP", proof)

	status, body := stubRoundTrip(t, req)
	require.Equal(t, http.StatusBadRequest, status)
	require.Contains(t, body, "expected one DPoP header, got 2")
}

// RFC 9449 §8: the stub refuses a nonce claim it never issued.
func TestStub_RejectsUnsolicitedNonce(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	listApps(t, tc)
	token := tc.stub.issuedTokens()[0]

	target := tc.client.apiURL("/api/v1/apps", "", nil)
	proof, err := tc.client.key.Proof(http.MethodGet, target, dpop.ProofOptions{AccessToken: token, Nonce: "made-up", IssuedAt: tc.clock.Now()})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target.String(), nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "DPoP "+token)
	req.Header.Set("DPoP", proof)

	status, body := stubRoundTrip(t, req)
	require.Equal(t, http.StatusBadRequest, status)
	require.Contains(t, body, "unsolicited nonce")
}

func stubRoundTrip(t *testing.T, req *http.Request) (int, string) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

// rewriteTransport records each wire URL and delivers the request to the
// stub listener instead of the host the client addressed.
type rewriteTransport struct {
	listener *url.URL

	mu   sync.Mutex
	urls []string
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.urls = append(rt.urls, req.URL.String())
	rt.mu.Unlock()
	clone := req.Clone(req.Context())
	clone.URL.Scheme, clone.URL.Host, clone.Host = rt.listener.Scheme, rt.listener.Host, ""
	resp, err := http.DefaultTransport.RoundTrip(clone)
	if err != nil {
		return nil, fmt.Errorf("rewrite transport: %w", err)
	}
	return resp, nil
}

func (rt *rewriteTransport) URLs() []string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return slices.Clone(rt.urls)
}

// RFC 9449 §4.2, §4.3: the org URL is canonicalized once so every wire URI
// is byte-identical to the htu the proof was signed over.
func TestNewClient_CanonicalOrgURLMatchesProofHTU(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	tc.stub.setOrigin("https://example.okta.com")

	cfg := tc.client.cfg
	cfg.OrgURL = "https://Example.okta.com:443"
	client, err := NewClient(testenv.NewLogger(t), tc.client.httpClient, tc.signer, cfg)
	require.NoError(t, err)
	impl, ok := client.(*httpClient)
	require.True(t, ok)
	require.Equal(t, "https://example.okta.com", impl.orgURL.String())
	require.Equal(t, "https://example.okta.com"+tokenEndpointPath, impl.audience)
	impl.now = tc.clock.Now
	transport := &rewriteTransport{listener: mustParseURL(t, tc.stub.srv.URL), mu: sync.Mutex{}, urls: nil}
	impl.httpClient.Transport = transport

	apps, err := impl.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "ACTIVE", Limit: 5})
	require.NoError(t, err)
	require.Len(t, apps, 1)

	wire := transport.URLs()
	proofs := tc.stub.recordedProofs()
	require.Len(t, wire, 3, "nonce challenge, token, resource")
	require.Len(t, proofs, 3)
	for i, raw := range wire {
		u := mustParseURL(t, raw)
		require.Equal(t, "example.okta.com", u.Host, raw)
		require.Equal(t, "https", u.Scheme, raw)
		htu, _, _ := strings.Cut(raw, "?")
		require.Equal(t, htu, proofs[i].htu, raw)
		require.Equal(t, dpop.HTU(u), proofs[i].htu, raw)
	}
	require.Contains(t, wire[2], "?")
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

	// Extend the deadline during the first sleep, then advance the fake clock
	// on each wake just as a real sleeper would.
	tc.client.sleep = func(ctx context.Context, d time.Duration) error {
		tc.client.mu.Lock()
		tc.client.slowUntil = second
		tc.client.mu.Unlock()
		tc.clock.advance(d)
		return tc.sleeper.sleep(ctx, d)
	}
	require.NoError(t, tc.client.waitForSlowdown(t.Context()))
	require.Equal(t, []time.Duration{3 * time.Second, 6 * time.Second}, tc.sleeper.Waits())
	require.Equal(t, second, tc.clock.Now())
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
	_, err = fake.GetApp(t.Context(), "..")
	require.EqualError(t, err, `okta: invalid app id ".."`)
}

func TestFake_ListAppsFiltersStatus(t *testing.T) {
	t.Parallel()
	fake := NewFake(Fixtures{
		Apps: []App{
			{ID: "0oa1", Label: "Gram", Name: "oidc_client", SignOnMode: "OPENID_CONNECT", Status: "ACTIVE", Features: nil, Created: time.Time{}, LastUpdated: time.Time{}},
			{ID: "0oa2", Label: "Old", Name: "oidc_client", SignOnMode: "OPENID_CONNECT", Status: "INACTIVE", Features: nil, Created: time.Time{}, LastUpdated: time.Time{}},
		},
		AppUsers: nil, AppGroups: nil, Groups: nil, GrantedScopes: nil,
	})

	apps, err := fake.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "INACTIVE", Limit: 0})
	require.NoError(t, err)
	require.Len(t, apps, 1)
	require.Equal(t, "0oa2", apps[0].ID)

	apps, err = fake.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0})
	require.NoError(t, err)
	require.Len(t, apps, 2)
}

func TestClient_AppIDRejectsPathTraversalWithoutRequest(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)

	for _, id := range []string{".", "..", "../users", "0oa1/users", `0oa1\users`} {
		_, err := tc.client.GetApp(t.Context(), id)
		require.EqualError(t, err, fmt.Sprintf("okta: invalid app id %q", id), id)
		_, err = tc.client.ListAppUsers(t.Context(), ListAppUsersRequest{AppID: id, Limit: 0})
		require.EqualError(t, err, fmt.Sprintf("okta: invalid app id %q", id), id)
		_, err = tc.client.ListAppGroups(t.Context(), ListAppGroupsRequest{AppID: id, Limit: 0})
		require.EqualError(t, err, fmt.Sprintf("okta: invalid app id %q", id), id)
	}
	require.Equal(t, stubCounts{tokenRequests: 0, assertionsSeen: 0, issuedTokens: 0}, tc.stub.counts())
}

func TestClient_AppIDIsEscapedAndProofMatchesWirePath(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	apps := stubApps(1)
	apps[0].ID = "0oa 1?x"
	tc.stub.setApps(apps)
	tc.stub.setAppUsers("0oa 1?x", []appUserJSON{{ID: "00u1", Scope: "USER", Status: "PROVISIONED", Created: time.Time{}, LastUpdated: time.Time{}, Credentials: struct {
		UserName string `json:"userName"`
	}{UserName: "ada@example.com"}}})

	app, err := tc.client.GetApp(t.Context(), "0oa 1?x")
	require.NoError(t, err)
	require.Equal(t, "0oa 1?x", app.ID)

	users, err := tc.client.ListAppUsers(t.Context(), ListAppUsersRequest{AppID: "0oa 1?x", Limit: 0})
	require.NoError(t, err)
	require.Len(t, users, 1)

	var htus []string
	for _, p := range tc.stub.recordedProofs() {
		if p.method == http.MethodGet {
			htus = append(htus, p.htu)
		}
	}
	require.Equal(t, []string{tc.stub.srv.URL + "/api/v1/apps/0oa%201%3Fx", tc.stub.srv.URL + "/api/v1/apps/0oa%201%3Fx/users"}, htus)
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

func TestClient_ListGroups_SearchPagination(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"Eng", `Team "A"\B`, ""} {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			tc := newDefaultTestClient(t)
			for i, name := range []string{prefix + " one", "Other", prefix + " two", prefix + " three"} {
				group := groupJSON{ID: fmt.Sprint(i)}
				group.Profile.Name = name
				tc.stub.groups = append(tc.stub.groups, group)
			}
			tc.stub.pageSize = 1
			groups, err := tc.client.ListGroups(t.Context(), ListGroupsRequest{Search: prefix, Limit: 1})
			require.NoError(t, err)
			wantIDs := []string{"0", "2", "3"}
			if prefix == "" {
				wantIDs = []string{"0", "1", "2", "3"}
			}
			ids := make([]string, 0, len(groups))
			for _, group := range groups {
				ids = append(ids, group.ID)
			}
			require.Equal(t, wantIDs, ids)
			tc.stub.mu.Lock()
			defer tc.stub.mu.Unlock()
			require.Len(t, tc.stub.groupsQueries, len(wantIDs))
			for i, query := range tc.stub.groupsQueries {
				require.False(t, query.Has("q"))
				wantSearch := ""
				switch prefix {
				case "Eng":
					wantSearch = `profile.name sw "Eng"`
				case `Team "A"\B`:
					wantSearch = `profile.name sw "Team \"A\"\\B"`
				}
				require.Equal(t, wantSearch, query.Get("search"))
				require.Equal(t, "1", query.Get("limit"))
				if i > 0 {
					require.Equal(t, fmt.Sprint(i), query.Get("after"))
				}
			}
		})
	}
}

func TestClient_MintAdmissionCancellation(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"token", "list", "verify"} {
		for _, cancellation := range []string{"canceled", "deadline", "already canceled"} {
			t.Run(operation+"/"+cancellation, func(t *testing.T) {
				t.Parallel()
				tc := newDefaultTestClient(t)
				entered, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				defer unblock()
				sleep := tc.client.sleep
				tc.client.sleep = func(ctx context.Context, d time.Duration) error {
					close(entered)
					select {
					case <-ctx.Done():
						return fmt.Errorf("test sleep: %w", ctx.Err())
					case <-release:
						return sleep(ctx, d)
					}
				}
				tc.stub.setTokenPending429(1)
				owner := make(chan error, 1)
				go func() {
					_, err := tc.client.token(t.Context())
					owner <- err
				}()
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("mint did not reach admission")
				}
				ctx, cancel := context.WithCancel(t.Context())
				wantErr := context.Canceled
				switch cancellation {
				case "deadline":
					cancel()
					ctx, cancel = context.WithTimeout(t.Context(), 20*time.Millisecond)
					wantErr = context.DeadlineExceeded
				case "already canceled":
					cancel()
				}
				defer cancel()
				started, result := make(chan struct{}), make(chan error, 1)
				go func() {
					close(started)
					var err error
					switch operation {
					case "token":
						_, err = tc.client.token(ctx)
					case "list":
						_, err = tc.client.ListApps(ctx, ListAppsRequest{})
					case "verify":
						_, err = tc.client.VerifyScopes(ctx, defaultScopes)
					}
					result <- err
				}()
				<-started
				if cancellation == "canceled" {
					cancel()
				}
				select {
				case err := <-result:
					require.ErrorIs(t, err, wantErr)
				case <-time.After(5 * time.Second):
					t.Fatal("waiter did not cancel while mint admission was occupied")
				}
				require.Equal(t, 1, tc.signer.Calls())
				unblock()
				require.NoError(t, <-owner)
				require.Equal(t, 1, tc.stub.counts().issuedTokens)

				// A canceled context must also lose to immediately available admission
				// (and to the token cache) without evicting or minting anything.
				canceled, stop := context.WithCancel(t.Context())
				stop()
				_, err := tc.client.token(canceled)
				require.ErrorIs(t, err, context.Canceled)
				_, err = tc.client.VerifyScopes(canceled, defaultScopes)
				require.ErrorIs(t, err, context.Canceled)
				_, cached := tc.client.cachedToken()
				require.True(t, cached)
				require.Equal(t, 1, tc.stub.counts().issuedTokens)
			})
		}
	}
}

func TestClient_WaitForSlowdownCancellation(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	first := tc.clock.Now().Add(3 * time.Second)
	second := tc.clock.Now().Add(9 * time.Second)
	tc.client.slowUntil = first
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	tc.client.sleep = func(ctx context.Context, d time.Duration) error {
		if tc.clock.Now().Before(first) {
			tc.client.mu.Lock()
			tc.client.slowUntil = second
			tc.client.mu.Unlock()
			tc.clock.advance(d)
			return nil
		}
		cancel()
		return sleepContext(ctx, d)
	}
	require.ErrorIs(t, tc.client.waitForSlowdown(ctx), context.Canceled)
	require.Equal(t, first, tc.clock.Now())
	require.Equal(t, second, tc.client.slowUntil)
}

func TestRateLimitJitterBounds(t *testing.T) {
	t.Parallel()
	for range 100 {
		jitter := rateLimitJitter()
		require.Positive(t, jitter)
		require.LessOrEqual(t, jitter, maxRateLimitJitter)
	}
}

func TestClient_RateLimitWaitJitterBounds(t *testing.T) {
	t.Parallel()
	now := time.Unix(1700000000, 0)
	for _, tt := range []struct {
		name  string
		reset string
		base  time.Duration
	}{
		{name: "future", reset: fmt.Sprint(now.Add(7 * time.Second).Unix()), base: 7 * time.Second},
		{name: "past", reset: fmt.Sprint(now.Add(-time.Second).Unix()), base: time.Second},
		{name: "now", reset: fmt.Sprint(now.Unix()), base: time.Second},
		{name: "missing", reset: "", base: time.Second},
		{name: "invalid", reset: "invalid", base: time.Second},
		{name: "near cap", reset: fmt.Sprint(now.Add(maxRateLimitWait).Unix()), base: maxRateLimitWait - time.Millisecond},
		{name: "beyond cap", reset: fmt.Sprint(now.Add(time.Hour).Unix()), base: maxRateLimitWait},
		{name: "saturated", reset: "253402300799", base: maxRateLimitWait},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, jitter := range []time.Duration{time.Nanosecond, maxRateLimitJitter} {
				clock := now
				if tt.name == "near cap" {
					clock = clock.Add(time.Millisecond)
				}
				client := &httpClient{
					now:    func() time.Time { return clock },
					jitter: func() time.Duration { return jitter },
				}
				header := http.Header{"X-Rate-Limit-Reset": []string{tt.reset}}
				wait := client.rateLimitWait(header)
				require.Equal(t, min(tt.base+jitter, maxRateLimitWait), wait)
				require.LessOrEqual(t, wait, maxRateLimitWait)
			}
		})
	}
}

func TestClient_RateLimitJitterWaitCancellation(t *testing.T) {
	t.Parallel()
	for _, token := range []bool{false, true} {
		t.Run(fmt.Sprintf("token=%t", token), func(t *testing.T) {
			t.Parallel()
			tc := newDefaultTestClient(t)
			tc.stub.setApps(stubApps(1))
			tc.stub.setRateLimit(100, 90, tc.clock.Now().Add(7*time.Second).Unix())
			if token {
				tc.stub.setTokenPending429(1)
			} else {
				tc.stub.setPending429(1)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var waits []time.Duration
			tc.client.sleep = func(ctx context.Context, d time.Duration) error {
				waits = append(waits, d)
				cancel()
				return sleepContext(ctx, d)
			}
			_, err := tc.client.ListApps(ctx, ListAppsRequest{Query: "", Status: "", Limit: 0})
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, []time.Duration{7*time.Second + maxRateLimitJitter/2}, waits)
		})
	}
}

// RFC 9449 §7.3: a resource retry after a transient 429 signs a fresh proof.
func TestClient_Resource429RetrySignsFreshProof(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setApps(stubApps(1))
	tc.stub.setPending429(1)

	listApps(t, tc)

	var resourceProofs []proofRecord
	for _, p := range tc.stub.recordedProofs() {
		if p.method == http.MethodGet {
			resourceProofs = append(resourceProofs, p)
		}
	}
	require.Len(t, resourceProofs, 2)
	require.NotEqual(t, resourceProofs[0].jti, resourceProofs[1].jti)
	require.Equal(t, resourceProofs[0].ath, resourceProofs[1].ath)
}
