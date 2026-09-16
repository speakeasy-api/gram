package gram

import (
	"context"
	"errors"
	"flag"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func privateIngressTemporalTestContext(values map[string]string) *cli.Context {
	set := flag.NewFlagSet("private-ingress", flag.ContinueOnError)
	for name, value := range values {
		set.String(name, value, "")
	}
	return cli.NewContext(nil, set, nil)
}

func TestPrivateIngressRejectsMissingTemporalBeforeDependencies(t *testing.T) {
	t.Parallel()
	for _, missing := range []string{"temporal-address", "temporal-namespace", "temporal-task-queue"} {
		values := map[string]string{"environment": "local", "temporal-address": "localhost:7233", "temporal-namespace": "default", "temporal-task-queue": "main"}
		delete(values, missing)
		runtime, err := newPrivateIngressRuntime(t.Context(), privateIngressTemporalTestContext(values), testenv.NewLogger(t))
		require.Nil(t, runtime)
		require.EqualError(t, err, "private ingress Temporal address, namespace, and task queue are required", missing)
	}
}

func TestPrivateIngressRejectsNonLocalTemporalWithoutMTLS(t *testing.T) {
	t.Parallel()
	values := map[string]string{"environment": "dev", "temporal-address": "temporal.example.com:7233", "temporal-namespace": "default", "temporal-task-queue": "main"}
	runtime, err := newPrivateIngressRuntime(t.Context(), privateIngressTemporalTestContext(values), testenv.NewLogger(t))
	require.Nil(t, runtime)
	require.EqualError(t, err, "private ingress Temporal mTLS is required outside local development")
	values["temporal-client-cert"] = "certificate"
	require.EqualError(t, validatePrivateIngressTemporalConfig(privateIngressTemporalTestContext(values)), "private ingress Temporal client certificate and key must be configured together")
	values["temporal-client-key"] = "key"
	require.NoError(t, validatePrivateIngressTemporalConfig(privateIngressTemporalTestContext(values)))
}

func TestPrivateIngressAllowsLocalTemporalWithoutMTLS(t *testing.T) {
	t.Parallel()
	values := map[string]string{"environment": "local", "temporal-address": "localhost:7233", "temporal-namespace": "default", "temporal-task-queue": "main"}
	require.NoError(t, validatePrivateIngressTemporalConfig(privateIngressTemporalTestContext(values)))
}

func TestParseSiteURL(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "/dashboard", "//example.com", "ftp://example.com", "https:///dashboard", "https://:443", ":invalid"} {
		parsed, err := parseSiteURL(raw)
		require.Error(t, err, raw)
		require.Nil(t, parsed, raw)
	}
	for _, raw := range []string{"https://example.com", "http://localhost:3000", "https://example.com/dashboard"} {
		parsed, err := parseSiteURL(raw)
		require.NoError(t, err, raw)
		require.Equal(t, raw, parsed.String())
	}
}

func TestPrivateIngressShutdownCancelsRequestsAndClosesConnections(t *testing.T) {
	t.Parallel()
	ctx, cancelRequests := context.WithCancel(t.Context())
	defer cancelRequests()
	started, finished := make(chan struct{}), make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(finished)
	}))
	server.Config.BaseContext = func(net.Listener) context.Context { return ctx }
	server.Start()
	defer server.Close()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		response, err := server.Client().Get(server.URL)
		if err == nil {
			_ = response.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not start")
	}
	shutdownPrivateIngress(t.Context(), testenv.NewLogger(t), server.Config, cancelRequests, 0)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("handler was not cancelled")
	}
	select {
	case <-clientDone:
	case <-time.After(5 * time.Second):
		t.Fatal("connection was not closed")
	}
}

type privateIngressTokenReviewerMock struct {
	mock.Mock
}

func (m *privateIngressTokenReviewerMock) Create(ctx context.Context, review *authenticationv1.TokenReview, opts metav1.CreateOptions) (*authenticationv1.TokenReview, error) {
	args := m.Called(ctx, review, opts)
	result, _ := args.Get(0).(*authenticationv1.TokenReview)
	return result, args.Error(1)
}

func TestPrivateIngressReadinessFailsAndRecoversWithoutLeakingToken(t *testing.T) {
	t.Parallel()
	synctest.Test(t, testPrivateIngressReadinessRecovery)
}

func testPrivateIngressReadinessRecovery(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenFile, []byte("test-service-account-token"), 0600))
	reviewer := &privateIngressTokenReviewerMock{}
	boundedContext := mock.MatchedBy(func(ctx context.Context) bool {
		deadline, ok := ctx.Deadline()
		return ok && time.Until(deadline) <= 2*time.Second
	})
	request := mock.MatchedBy(func(review *authenticationv1.TokenReview) bool {
		return review.Spec.Token == "test-service-account-token" && len(review.Spec.Audiences) == 0
	})
	reviewer.On("Create", boundedContext, request, metav1.CreateOptions{}).Return(nil, errors.New("test-service-account-token upstream failure")).Once()
	dependenciesCalled := 0
	handler := privateIngressReadinessHandler(reviewer, tokenFile, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dependenciesCalled++
		w.WriteHeader(http.StatusOK)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.NotContains(t, response.Body.String(), "test-service-account-token")
	require.Zero(t, dependenciesCalled)

	time.Sleep(time.Second) // Advance the synctest clock to replenish the readiness limiter.
	require.NoError(t, os.WriteFile(tokenFile, []byte("rotated-test-token"), 0600))
	reviewer.On("Create", boundedContext, mock.MatchedBy(func(review *authenticationv1.TokenReview) bool {
		return review.Spec.Token == "rotated-test-token" && len(review.Spec.Audiences) == 0
	}), metav1.CreateOptions{}).Return(&authenticationv1.TokenReview{Status: authenticationv1.TokenReviewStatus{Authenticated: true}}, nil).Once()
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, dependenciesCalled)
	reviewer.AssertExpectations(t)
}

func TestPrivateIngressReadinessRateLimitsConcurrentProbes(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		tokenFile := filepath.Join(t.TempDir(), "token")
		require.NoError(t, os.WriteFile(tokenFile, []byte("test-service-account-token"), 0600))
		reviewer := &privateIngressTokenReviewerMock{}
		reviewer.On("Create", mock.Anything, mock.Anything, metav1.CreateOptions{}).
			Return(&authenticationv1.TokenReview{Status: authenticationv1.TokenReviewStatus{Authenticated: true}}, nil).Once()
		handler := privateIngressReadinessHandler(reviewer, tokenFile, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		const probes = 20
		results := make(chan int, probes)
		for range probes {
			go func() {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
				results <- response.Code
			}()
		}
		synctest.Wait()
		accepted := 0
		for range probes {
			code := <-results
			if code == http.StatusOK {
				accepted++
			} else {
				require.Equal(t, http.StatusServiceUnavailable, code)
			}
		}
		require.Equal(t, 1, accepted)
		reviewer.AssertExpectations(t)
	})
}

func TestPrivateIngressReadinessRejectsMissingTokenAndLeavesLivenessAlone(t *testing.T) {
	t.Parallel()
	reviewer := &privateIngressTokenReviewerMock{}
	handler := privateIngressReadinessHandler(reviewer, filepath.Join(t.TempDir(), "missing"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/livez", nil))
	require.Equal(t, http.StatusOK, response.Code)
	reviewer.AssertNotCalled(t, "Create", mock.Anything, mock.Anything, mock.Anything)
}
