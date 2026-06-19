package oauthtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
)

// ProtectedResourceServerOpts configures [LaunchProtectedResourceServer].
type ProtectedResourceServerOpts struct {
	// Metadata is the RFC 9728 OAuth Protected Resource Metadata document the
	// fake will serve at /.well-known/oauth-protected-resource. When nil, the
	// server responds with 404 at every path (useful for "no OAuth advertised"
	// scenarios).
	Metadata *wellknown.OAuthProtectedResourceMetadata

	// StatusCode lets callers force a specific HTTP status at the well-known
	// path (e.g. 500, 401). When zero, the server returns 200 with Metadata
	// when set or 404 when Metadata is nil.
	StatusCode int

	// Body, when non-empty, is served verbatim at the well-known path instead
	// of the Metadata struct. Use to simulate malformed JSON responses.
	Body []byte
}

// LaunchProtectedResourceServer starts an httptest.Server that serves a
// configurable RFC 9728 protected-resource-metadata document at the standard
// well-known path. The server is closed automatically on test cleanup.
//
// Callers receive the server (whose .URL is the resource origin to point
// remote_mcp_server rows at). When opts.Metadata is non-nil, the handler
// re-encodes its current value on every request — mutate it before probing
// to advertise the upstream's own URL once it is known.
func LaunchProtectedResourceServer(t *testing.T, opts ProtectedResourceServerOpts) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wellknown.OAuthProtectedResourcePath {
			http.NotFound(w, r)
			return
		}

		if opts.StatusCode != 0 && opts.StatusCode != http.StatusOK {
			w.WriteHeader(opts.StatusCode)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		if len(opts.Body) > 0 {
			_, _ = w.Write(opts.Body)
			return
		}

		if opts.Metadata == nil {
			http.NotFound(w, r)
			return
		}

		// Marshal per-request so callers can mutate opts.Metadata after the
		// server is up (e.g. to advertise the upstream's own URL). Encode
		// errors are written through to the client as a 500 so the linter
		// stays happy — none of the dashboard-facing fields can actually
		// fail to marshal, so this is effectively unreachable.
		body, err := json.Marshal(opts.Metadata)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server
}
