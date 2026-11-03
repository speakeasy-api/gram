package oops_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestIsInfrastructureFailure(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "server shutting down", err: fmt.Errorf("query: %w", &pgconn.PgError{Code: "57P01"}), want: true},
		{name: "crash shutdown", err: &pgconn.PgError{Code: "57P02"}, want: true},
		{name: "starting up", err: &pgconn.PgError{Code: "57P03"}, want: true},
		{name: "connection exception", err: &pgconn.PgError{Code: "08006"}, want: true},
		{name: "too many connections", err: &pgconn.PgError{Code: "53300"}, want: true},
		{name: "serialization failure", err: &pgconn.PgError{Code: "40001"}, want: true},
		{name: "deadlock", err: &pgconn.PgError{Code: "40P01"}, want: true},
		{name: "pool wait ran out", err: fmt.Errorf("acquire: %w", context.DeadlineExceeded), want: true},
		{name: "redis pool exhausted", err: fmt.Errorf("get: %w", redis.ErrPoolTimeout), want: true},
		{name: "syntax error", err: &pgconn.PgError{Code: "42601"}, want: false},
		{name: "missing table", err: &pgconn.PgError{Code: "42P01"}, want: false},
		{name: "missing column", err: &pgconn.PgError{Code: "42703"}, want: false},
		{name: "insufficient privilege", err: &pgconn.PgError{Code: "42501"}, want: false},
		{name: "unique violation", err: &pgconn.PgError{Code: "23505"}, want: false},
		{name: "bad data", err: &pgconn.PgError{Code: "22P02"}, want: false},
		{name: "no rows", err: fmt.Errorf("load: %w", pgx.ErrNoRows), want: false},
		{name: "other", err: errors.New("boom"), want: false},
		{name: "nil", err: nil, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, oops.IsInfrastructureFailure(tc.err))
		})
	}
}

// Real failures, not sentinels: what a dropped store and a refused upstream actually return.
func TestIsInfrastructureFailure_RealNetworkErrors(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedAddr := listener.Addr().String()
	require.NoError(t, listener.Close())

	_, refused := net.Dial("tcp", closedAddr)
	require.True(t, oops.IsInfrastructureFailure(refused), "dial refused: %v", refused)

	_, pgDown := pgx.Connect(ctx, "postgres://u@"+closedAddr+"/d?connect_timeout=1")
	require.True(t, oops.IsInfrastructureFailure(pgDown), "postgres down: %v", pgDown)

	redisErr := redis.NewClient(&redis.Options{Addr: closedAddr, MaxRetries: -1}).Ping(ctx).Err()
	require.True(t, oops.IsInfrastructureFailure(redisErr), "redis down: %v", redisErr)

	require.True(t, oops.IsInfrastructureFailure(droppedConnectionError(t)), "connection reset")

	timeoutConn, timeoutPeer := net.Pipe()
	t.Cleanup(func() { _ = timeoutPeer.Close(); _ = timeoutConn.Close() })
	require.NoError(t, timeoutConn.SetReadDeadline(time.Now().Add(-time.Second)))
	_, timedOut := timeoutConn.Read(make([]byte, 1))
	require.True(t, oops.IsInfrastructureFailure(timedOut), "read timeout: %v", timedOut)

	// Someone else's service failing isn't our outage, however it failed.
	outboundRefused := httpGetError(http.DefaultClient, "http://"+closedAddr)
	require.False(t, oops.IsInfrastructureFailure(outboundRefused), "outbound refused: %v", outboundRefused)
	release := make(chan struct{})
	hanging := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(hanging.Close)
	t.Cleanup(func() { close(release) })
	outboundTimeout := httpGetError(&http.Client{Timeout: 20 * time.Millisecond}, hanging.URL)
	require.Error(t, outboundTimeout)
	require.False(t, oops.IsInfrastructureFailure(outboundTimeout), "outbound timeout: %v", outboundTimeout)

	// A bad certificate is a real answer: it needs a fix, not a retry.
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)
	x509Err := httpGetError(http.DefaultClient, server.URL)
	var unknownAuthority x509.UnknownAuthorityError
	require.ErrorAs(t, x509Err, &unknownAuthority)
	require.False(t, oops.IsInfrastructureFailure(x509Err), "x509: %v", x509Err)

	// A dial the policy refused never reached the network.
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{"127.0.0.0/8"})
	require.NoError(t, err)
	_, denied := policy.Dialer().DialContext(ctx, "tcp", "127.0.0.1:443")
	require.ErrorIs(t, denied, guardian.ErrBlockedIP)
	require.False(t, oops.IsInfrastructureFailure(denied), "policy denied: %v", denied)

	for name, cause := range map[string]error{"x509": x509Err, "policy denied": denied, "outbound refused": outboundRefused, "outbound timeout": outboundTimeout} {
		w := httptest.NewRecorder()
		oops.ErrHandle(testenv.NewLogger(t), func(http.ResponseWriter, *http.Request) error {
			return oops.E(oops.CodeUnexpected, cause, "call upstream")
		}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		require.Equal(t, http.StatusInternalServerError, w.Code, name)
		w = httptest.NewRecorder()
		oops.MCPErrHandle(testenv.NewLogger(t), func(http.ResponseWriter, *http.Request) error {
			return oops.E(oops.CodeUnexpected, cause, "call upstream")
		}).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/", nil))
		require.Equal(t, http.StatusInternalServerError, w.Code, name)
		require.Empty(t, w.Header().Get("Retry-After"), name)
	}
}

func httpGetError(client *http.Client, url string) error {
	resp, err := client.Get(url)
	if err == nil {
		_ = resp.Body.Close()
	}
	return err //nolint:wrapcheck // the test inspects the client's own error
}

// droppedConnectionError reads from a connection its peer reset.
func droppedConnectionError(t *testing.T) error {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		conn, aerr := listener.Accept()
		if aerr != nil {
			return
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}
		_ = conn.Close()
	}()
	conn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	<-accepted
	_, err = conn.Read(make([]byte, 1))
	require.ErrorIs(t, err, syscall.ECONNRESET)
	return fmt.Errorf("read: %w", err)
}

func TestIsStoreFailure(t *testing.T) {
	t.Parallel()

	require.True(t, oops.IsStoreFailure(fmt.Errorf("load: %w", &pgconn.PgError{Code: "42P01"})), "a broken deploy")
	require.True(t, oops.IsStoreFailure(&pgconn.PgError{Code: "57P01"}), "an outage")
	require.False(t, oops.IsStoreFailure(fmt.Errorf("load: %w", pgx.ErrNoRows)), "a missing row is an answer")
	require.False(t, oops.IsStoreFailure(oops.C(oops.CodeUnauthorized)), "a refusal")
}

func TestErrHandlersAnswerInfrastructureFailureAsOutage(t *testing.T) {
	t.Parallel()

	down := oops.E(oops.CodeUnexpected, &pgconn.PgError{Code: "57P01"}, "load mcp endpoint")
	failing := func(http.ResponseWriter, *http.Request) error { return down }
	for name, handler := range map[string]http.Handler{
		"resource": oops.MCPErrHandle(testenv.NewLogger(t), failing),
		"token":    oops.OAuthErrHandle(testenv.NewLogger(t), failing),
		"machine":  oops.AuthErrHandle(testenv.NewLogger(t), failing),
		"browser":  oops.BrowserErrHandle(testenv.NewLogger(t), failing),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/", nil))
			require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
			require.Equal(t, "30", w.Header().Get("Retry-After"))
			require.Empty(t, w.Header().Get("WWW-Authenticate"))
			require.Empty(t, w.Header().Get(constants.HeaderProxiedResponse), "our own failure keeps alerting")
		})
	}

	token := httptest.NewRecorder()
	oops.OAuthErrHandle(testenv.NewLogger(t), failing).ServeHTTP(token, httptest.NewRequest(http.MethodPost, "/", nil))
	var body oautherr.RFC6749Error
	require.NoError(t, json.Unmarshal(token.Body.Bytes(), &body))
	require.Equal(t, oautherr.CodeTemporarilyUnavailable, body.Code)
}

// Chat, webhooks, instance execution and admin keep their own retry contract.
func TestErrHandleLeavesStoreFailuresAlone(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	oops.ErrHandle(testenv.NewLogger(t), func(http.ResponseWriter, *http.Request) error {
		return oops.E(oops.CodeUnexpected, &pgconn.PgError{Code: "57P01"}, "load")
	}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Empty(t, w.Header().Get("Retry-After"))
}

func TestErrHandlersKeepAnswersThatAreNotOutages(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{name: "not found", err: oops.E(oops.CodeNotFound, pgx.ErrNoRows, "mcp endpoint not found"), want: http.StatusNotFound},
		{name: "unauthorized despite a store cause", err: oops.E(oops.CodeUnauthorized, &pgconn.PgError{Code: "57P01"}, "no"), want: http.StatusUnauthorized},
		{name: "unexpected missing row", err: oops.E(oops.CodeUnexpected, pgx.ErrNoRows, "gone"), want: http.StatusInternalServerError},
		{name: "unexpected bug", err: oops.E(oops.CodeUnexpected, errors.New("boom"), "bug"), want: http.StatusInternalServerError},
		{name: "broken query", err: oops.E(oops.CodeUnexpected, &pgconn.PgError{Code: "42601"}, "bug"), want: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			oops.AuthErrHandle(testenv.NewLogger(t), func(http.ResponseWriter, *http.Request) error { return tc.err }).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
			require.Equal(t, tc.want, w.Code)
			require.Empty(t, w.Header().Get("Retry-After"))
		})
	}
}
