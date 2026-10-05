package remotesessions

import (
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	strandedTestIssuerURL = "https://idp.example.com"
	strandedTestResource  = "https://mcp.example.com/mcp"
)

// strandedFixture is a manager over a miniredis cache plus one client and one
// login request. No callback is ever driven unless a test consumes a state,
// which is what an issuer that renders its own error page looks like to Gram.
type strandedFixture struct {
	mgr    *ChallengeManager
	mr     *miniredis.Miniredis
	reader *sdkmetric.ManualReader
	client Client
	parent ParentChallenge
}

func newStrandedFixture(t *testing.T) strandedFixture {
	t.Helper()

	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, rc.Close()) })

	reader := sdkmetric.NewManualReader()
	serverURL, err := url.Parse("https://app.example.com")
	require.NoError(t, err)
	mgr := NewChallengeManager(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
		nil,
		nil,
		guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)),
		nil,
		cache.NewRedisCacheAdapter(rc),
		serverURL,
	)

	subject := urn.NewUserSubject("stranded-subject")
	return strandedFixture{
		mgr:    mgr,
		mr:     mr,
		reader: reader,
		client: Client{
			ID:                    uuid.New(),
			RemoteSessionIssuerID: uuid.New(),
			ExternalClientID:      "stranded-client",
			IssuerSlug:            "stranded-issuer",
			IssuerURL:             strandedTestIssuerURL,
			AuthorizationEndpoint: strandedTestIssuerURL + "/authorize",
			TokenEndpoint:         strandedTestIssuerURL + "/token",
		},
		parent: ParentChallenge{
			ID:                  uuid.NewString(),
			ProjectID:           uuid.New(),
			OrganizationID:      "org-example",
			UserSessionIssuerID: uuid.New(),
			Subject:             &subject,
			McpSlug:             "stranded-mcp",
			Resource:            strandedTestResource,
		},
	}
}

// start is one connect click; it returns the authorize URL's resource and the
// state stored for the leg.
func (f strandedFixture) start(t *testing.T) (string, RemoteLoginState) {
	t.Helper()
	authURL, err := f.mgr.BuildAuthorizationUrl(t.Context(), f.parent, f.client)
	require.NoError(t, err)
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	state, err := f.mgr.cache.Get(t.Context(), "remoteLogin:"+parsed.Query().Get("state"))
	require.NoError(t, err)
	return parsed.Query().Get("resource"), state
}

// answer consumes a leg's state the way the remote-login callback does.
func (f strandedFixture) answer(t *testing.T, state RemoteLoginState) {
	t.Helper()
	_, err := f.mgr.cache.GetAndDelete(t.Context(), "remoteLogin:"+state.ID)
	require.NoError(t, err)
}

// unanswered sums the unanswered-leg counter and asserts its only label is the issuer.
func (f strandedFixture) unanswered(t *testing.T) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, f.reader.Collect(t.Context(), &rm))
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "gram.remote_session.upstream_authorize.unanswered" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				require.Equal(t, 1, dp.Attributes.Len(), "the issuer is the only dimension")
				issuer, ok := dp.Attributes.Value(attr.OAuthIssuerKey)
				require.True(t, ok)
				require.Equal(t, strandedTestIssuerURL, issuer.AsString())
				total += dp.Value
			}
		}
	}
	return total
}

// An issuer that renders its own error page never calls back. The restart
// takes the resource-less leg once; later restarts send the resource again
// and never flip back within the window.
func TestBuildAuthorizationUrl_UnansweredResourceLegFallsBackOnce(t *testing.T) {
	t.Parallel()
	f := newStrandedFixture(t)

	resource, first := f.start(t)
	require.Equal(t, strandedTestResource, resource, "the first leg sends the resource")
	require.False(t, first.ResourceRetried)
	require.Equal(t, int64(0), f.unanswered(t))

	resource, second := f.start(t)
	require.Empty(t, resource, "the restart after an unanswered leg drops the resource")
	require.True(t, second.OmitResource)
	require.True(t, second.ResourceRetried, "the fallback is the login's one retry, so a later invalid_target fails it")
	require.Equal(t, strandedTestResource, second.Resource, "the resource is still recorded so the grant stays routable")
	require.Equal(t, int64(1), f.unanswered(t))

	resource, third := f.start(t)
	require.Equal(t, strandedTestResource, resource, "the window's fallback is spent; no second resource-less leg")
	require.False(t, third.ResourceRetried)
	require.Equal(t, int64(2), f.unanswered(t))

	resource, _ = f.start(t)
	require.Equal(t, strandedTestResource, resource, "no oscillation back to the resource-less leg")
	require.Equal(t, int64(3), f.unanswered(t), "each unanswered leg is counted once")
}

// The fallback leg is marked ResourceRetried, so an invalid_target on it is
// refused the same way a second invalid_target on the callback retry is.
func TestBuildAuthorizationUrl_FallbackLegRefusesAnotherRetry(t *testing.T) {
	t.Parallel()
	f := newStrandedFixture(t)

	f.start(t)
	_, fallback := f.start(t)
	require.True(t, fallback.ResourceRetried)

	cause := oautherr.RFC6749Error{Code: oautherr.CodeInvalidTarget, Description: "", URI: ""}
	result, err := f.mgr.retryWithoutResource(t.Context(), f.mgr.logger, fallback, cause)
	require.Error(t, err)
	require.Empty(t, result.RedirectURL, "no further leg is minted")
}

// A leg the issuer answered, successfully or not, is never treated as stranded.
func TestBuildAuthorizationUrl_AnsweredLegKeepsResource(t *testing.T) {
	t.Parallel()
	f := newStrandedFixture(t)

	_, first := f.start(t)
	f.answer(t, first)

	resource, second := f.start(t)
	require.Equal(t, strandedTestResource, resource)
	require.False(t, second.ResourceRetried)
	require.Equal(t, int64(0), f.unanswered(t))
}

// A leg that never sent a resource has nothing to drop, matching
// retryWithoutResource: no fallback and no marker.
func TestBuildAuthorizationUrl_NoFallbackWithoutResource(t *testing.T) {
	t.Parallel()

	t.Run("no resource", func(t *testing.T) {
		t.Parallel()
		f := newStrandedFixture(t)
		f.parent.Resource = ""
		f.start(t)
		resource, second := f.start(t)
		require.Empty(t, resource)
		require.False(t, second.ResourceRetried)
		for _, key := range f.mr.Keys() {
			require.NotContains(t, key, "remoteLoginPendingResource:", "no marker is recorded")
		}
	})

	t.Run("operator omits the resource", func(t *testing.T) {
		t.Parallel()
		f := newStrandedFixture(t)
		unsupported := false
		f.client.IssuerResourceIndicatorSupported = &unsupported
		f.start(t)
		_, second := f.start(t)
		require.True(t, second.OmitResource)
		require.False(t, second.ResourceRetried, "the operator setting is not a retry")
	})
}

// The marker is scoped to one resource: an unanswered leg for one MCP server
// does not drop the resource from a login for another.
func TestBuildAuthorizationUrl_FallbackIsScopedToTheResource(t *testing.T) {
	t.Parallel()
	f := newStrandedFixture(t)

	f.start(t)
	f.parent.Resource = "https://other.example.com/mcp"
	resource, state := f.start(t)
	require.Equal(t, "https://other.example.com/mcp", resource)
	require.False(t, state.ResourceRetried)
}

// The marker is scoped to one subject.
func TestBuildAuthorizationUrl_FallbackIsScopedToTheSubject(t *testing.T) {
	t.Parallel()
	f := newStrandedFixture(t)

	f.start(t)
	other := urn.NewUserSubject("another-subject")
	f.parent.Subject = &other
	resource, _ := f.start(t)
	require.Equal(t, strandedTestResource, resource)
}

// The marker is scoped to one project, so an organization-level client and
// issuer shared by two projects do not share one project's fallback.
func TestBuildAuthorizationUrl_FallbackIsScopedToTheProject(t *testing.T) {
	t.Parallel()
	f := newStrandedFixture(t)

	f.start(t)
	f.parent.ProjectID = uuid.New()
	resource, state := f.start(t)
	require.Equal(t, strandedTestResource, resource)
	require.False(t, state.ResourceRetried)
}

// Concurrent restarts that all see the unspent marker, such as two tabs or a
// double-clicked connect, mint one resource-less leg between them.
func TestBuildAuthorizationUrl_ConcurrentRestartsFallBackOnce(t *testing.T) {
	t.Parallel()
	f := newStrandedFixture(t)

	f.start(t)

	const restarts = 16
	var wg sync.WaitGroup
	resources := make(chan string, restarts)
	for range restarts {
		wg.Go(func() {
			authURL, err := f.mgr.BuildAuthorizationUrl(t.Context(), f.parent, f.client)
			if !assert.NoError(t, err) {
				return
			}
			parsed, err := url.Parse(authURL)
			if !assert.NoError(t, err) {
				return
			}
			resources <- parsed.Query().Get("resource")
		})
	}
	wg.Wait()
	close(resources)

	fallbacks := 0
	for resource := range resources {
		if resource == "" {
			fallbacks++
		}
	}
	require.Equal(t, 1, fallbacks, "exactly one restart takes the window's resource-less leg")
}

// Once the window expires a new one starts, with its own single fallback.
func TestBuildAuthorizationUrl_WindowExpiryStartsANewWindow(t *testing.T) {
	t.Parallel()
	f := newStrandedFixture(t)

	f.start(t)
	resource, _ := f.start(t)
	require.Empty(t, resource)

	f.mr.FastForward(strandedLegWindow + time.Second)

	resource, _ = f.start(t)
	require.Equal(t, strandedTestResource, resource, "an expired window forgets the spent fallback, and the expired leg proves nothing")
	resource, _ = f.start(t)
	require.Empty(t, resource)
}
