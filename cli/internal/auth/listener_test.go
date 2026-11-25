package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// noKeepAliveClient keeps the test from leaving idle connections behind, which
// would hold up the listener's graceful shutdown.
var noKeepAliveClient = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

const testState = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func newTestListener() *Listener {
	return &Listener{
		server:   nil,
		listener: nil,
		result:   make(chan CallbackResult, 1),
		errChan:  make(chan error, 1),
		state:    testState,
		consumed: atomic.Bool{},
	}
}

func callback(t *testing.T, l *Listener, query url.Values) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/callback?"+query.Encode(), nil)
	l.handleCallback(rec, req)
	return rec
}

func callbackQuery(state, code string) url.Values {
	q := url.Values{}
	if state != "" {
		q.Set("state", state)
	}
	if code != "" {
		q.Set("code", code)
	}
	return q
}

// requireNoResult asserts that no callback has completed the login attempt.
func requireNoResult(t *testing.T, l *Listener) {
	t.Helper()
	select {
	case r := <-l.result:
		t.Fatalf("login attempt completed with %+v", r)
	default:
	}
}

func TestHandleCallback_ServesSpeakeasyBrandedPage(t *testing.T) {
	t.Parallel()

	l := newTestListener()

	rec := callback(t, l, callbackQuery(testState, "code"))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, CallbackResult{Code: "code"}, <-l.result)

	body := rec.Body.String()
	require.Contains(t, body, `aria-label="Speakeasy"`)
	require.NotContains(t, body, `viewBox="0 0 37 45"`, "the success page still shows the old logo")
}

func TestHandleCallback_RejectsMissingStateWithoutConsumingAttempt(t *testing.T) {
	t.Parallel()

	l := newTestListener()

	rec := callback(t, l, callbackQuery("", "attacker-code"))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	requireNoResult(t, l)

	rec = callback(t, l, callbackQuery(testState, "victim-code"))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, CallbackResult{Code: "victim-code"}, <-l.result)
}

func TestHandleCallback_RejectsMismatchedStateWithoutConsumingAttempt(t *testing.T) {
	t.Parallel()

	l := newTestListener()

	for _, state := range []string{"wrong", testState[:len(testState)-1], testState + "0"} {
		rec := callback(t, l, callbackQuery(state, "attacker-code"))
		require.Equal(t, http.StatusBadRequest, rec.Code, "state %q", state)
	}
	requireNoResult(t, l)

	rec := callback(t, l, callbackQuery(testState, "victim-code"))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, CallbackResult{Code: "victim-code"}, <-l.result)
}

func TestHandleCallback_RejectsMissingCodeWithoutConsumingAttempt(t *testing.T) {
	t.Parallel()

	l := newTestListener()

	rec := callback(t, l, callbackQuery(testState, ""))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	requireNoResult(t, l)

	rec = callback(t, l, callbackQuery(testState, "code"))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, CallbackResult{Code: "code"}, <-l.result)
}

func TestHandleCallback_RejectsReplayedState(t *testing.T) {
	t.Parallel()

	l := newTestListener()

	rec := callback(t, l, callbackQuery(testState, "victim-code"))
	require.Equal(t, http.StatusOK, rec.Code)

	rec = callback(t, l, callbackQuery(testState, "attacker-code"))
	require.Equal(t, http.StatusConflict, rec.Code)

	require.Equal(t, CallbackResult{Code: "victim-code"}, <-l.result)
	requireNoResult(t, l)
}

func TestNewListener_URLCarriesUniqueState(t *testing.T) {
	t.Parallel()

	a, err := NewListener()
	require.NoError(t, err)
	t.Cleanup(func() { _ = a.listener.Close() })

	b, err := NewListener()
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.listener.Close() })

	aURL, err := url.Parse(a.URL())
	require.NoError(t, err)
	bURL, err := url.Parse(b.URL())
	require.NoError(t, err)

	require.Equal(t, "127.0.0.1", aURL.Hostname())
	require.Equal(t, "/callback", aURL.Path)
	require.Len(t, aURL.Query().Get("state"), 2*stateBytes)
	require.NotEqual(t, aURL.Query().Get("state"), bURL.Query().Get("state"))
}

// TestListener_ConcurrentAttemptsCannotExchangeCallbacks runs two login
// attempts at once and delivers each attempt's callback to the other's port:
// both must be refused, and each attempt must still complete with its own code.
func TestListener_ConcurrentAttemptsCannotExchangeCallbacks(t *testing.T) {
	t.Parallel()

	a, err := NewListener()
	require.NoError(t, err)
	a.Start()
	t.Cleanup(func() { _ = a.Stop(context.Background()) })

	b, err := NewListener()
	require.NoError(t, err)
	b.Start()
	t.Cleanup(func() { _ = b.Stop(context.Background()) })

	aURL, err := url.Parse(a.URL())
	require.NoError(t, err)
	bURL, err := url.Parse(b.URL())
	require.NoError(t, err)

	get := func(target url.URL, state, code string) int {
		target.RawQuery = callbackQuery(state, code).Encode()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target.String(), nil)
		require.NoError(t, err)
		resp, err := noKeepAliveClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	require.Equal(t, http.StatusBadRequest, get(*bURL, aURL.Query().Get("state"), "code-a"))
	require.Equal(t, http.StatusBadRequest, get(*aURL, bURL.Query().Get("state"), "code-b"))
	requireNoResult(t, a)
	requireNoResult(t, b)

	require.Equal(t, http.StatusOK, get(*aURL, aURL.Query().Get("state"), "code-a"))
	require.Equal(t, http.StatusOK, get(*bURL, bURL.Query().Get("state"), "code-b"))

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	ra, err := a.Wait(ctx)
	require.NoError(t, err)
	require.Equal(t, "code-a", ra.Code)

	rb, err := b.Wait(ctx)
	require.NoError(t, err)
	require.Equal(t, "code-b", rb.Code)
}
