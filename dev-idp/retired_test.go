package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func newCapturingLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		AddSource:   false,
		Level:       slog.LevelDebug,
		ReplaceAttr: nil,
	})), &buf
}

// The retired prefixes are exactly the paths a checkout predating the rename
// still calls, so each has to explain itself rather than 404.
func TestRetiredPrefixesExplainThemselves(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		path        string
		wantEnvVar  string
		wantCurrent string
	}{
		{path: "/oauth2/authorize?response_type=code", wantEnvVar: "GRAM_IDP_BASE_URL", wantCurrent: "/oauth2-1"},
		{path: "/mock-workos/user_management/authenticate", wantEnvVar: "WORKOS_API_URL", wantCurrent: "/workos"},
	} {
		logger, logs := newCapturingLogger()
		mux := http.NewServeMux()
		mountRetiredPrefixes(mux, logger)

		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))

		require.Equal(t, http.StatusGone, rec.Code, "path %s", tc.path)

		var body map[string]string
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Equal(t, tc.wantEnvVar, body["env_var"])
		require.Equal(t, tc.wantCurrent, body["current_prefix"])
		require.Contains(t, body["fix"], "mise.local.toml")

		// The log is the surface that matters: whatever called this is about
		// to report something far less specific.
		require.Contains(t, logs.String(), tc.wantEnvVar, "the fix must be visible in the logs")
		require.Contains(t, logs.String(), "mise.local.toml")
		require.Contains(t, logs.String(), "level=ERROR")
	}
}

// A retired prefix must not shadow the live one that replaced it —
// "/oauth2/" and "/oauth2-1/" differ only after a segment boundary.
func TestRetiredPrefixDoesNotShadowLivePrefix(t *testing.T) {
	t.Parallel()

	logger, _ := newCapturingLogger()
	mux := http.NewServeMux()
	mountRetiredPrefixes(mux, logger)
	mux.Handle("/oauth2-1/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("live"))
	}))
	mux.Handle("/workos/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("live"))
	}))

	for _, path := range []string{"/oauth2-1/authorize", "/workos/user_management/users"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, rec.Code, "live path %s must not be captured", path)
		require.Equal(t, "live", rec.Body.String(), "live path %s must not be captured", path)
	}
}

// Every retired prefix must name a replacement that is actually mounted, or
// the message sends people somewhere that does not exist.
func TestRetiredPrefixesPointAtRealPrefixes(t *testing.T) {
	t.Parallel()

	live := map[string]bool{"/oauth2-1": true, "/workos": true}
	for _, p := range retiredPrefixes {
		require.True(t, live[p.current],
			"retired %s points at %s, which is not a mounted prefix", p.old, p.current)
		require.False(t, strings.HasPrefix(p.current, p.old+"/"),
			"retired %s would shadow its own replacement %s", p.old, p.current)
	}
}
