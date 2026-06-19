package wellknown_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// newProbeTestPolicy returns a guardian.Policy permissive enough to dial
// httptest.NewServer on 127.0.0.1.
func newProbeTestPolicy(t *testing.T) *guardian.Policy {
	t.Helper()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return policy
}

// serveJSON returns an httptest server that serves the given metadata document
// at /.well-known/oauth-protected-resource and 404s elsewhere.
func serveJSON(t *testing.T, doc map[string]any) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/oauth-protected-resource" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestDiscoverProtectedResourceMetadata_HappyPath(t *testing.T) {
	t.Parallel()

	// Allocate the server first so we can use its URL in the metadata body.
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/oauth-protected-resource" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":                 serverURL,
			"authorization_servers":    []string{"https://auth.example.com"},
			"scopes_supported":         []string{"read", "write"},
			"bearer_methods_supported": []string{"header"},
			"resource_documentation":   "https://docs.example.com",
		})
	}))
	t.Cleanup(server.Close)
	serverURL = server.URL

	doc, warnings, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), newProbeTestPolicy(t), server.URL)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, server.URL, doc.Resource)
	require.Equal(t, []string{"https://auth.example.com"}, doc.AuthorizationServers)
	require.Equal(t, []string{"read", "write"}, doc.ScopesSupported)
	require.Equal(t, []string{"header"}, doc.BearerMethodsSupported)
	require.Equal(t, "https://docs.example.com", doc.ResourceDocumentation)
}

func TestDiscoverProtectedResourceMetadata_PathStyle(t *testing.T) {
	t.Parallel()

	// Upstream only advertises metadata under the path-style well-known URL
	// (e.g. /.well-known/oauth-protected-resource/mcp/foo). RFC 9728 §3.1
	// requires this form to be tried when the resource URL has a path.
	var probedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probedPaths = append(probedPaths, r.URL.Path)
		if r.URL.Path != "/.well-known/oauth-protected-resource/mcp/foo" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              "http://" + r.Host + "/mcp/foo",
			"authorization_servers": []string{"https://auth.example.com"},
		})
	}))
	t.Cleanup(server.Close)

	doc, warnings, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), newProbeTestPolicy(t), server.URL+"/mcp/foo")
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, server.URL+"/mcp/foo", doc.Resource)
	require.Equal(t, []string{"https://auth.example.com"}, doc.AuthorizationServers)
	// Probed exactly once — path-style hit on the first try, no fallback.
	require.Equal(t, []string{"/.well-known/oauth-protected-resource/mcp/foo"}, probedPaths)
}

func TestDiscoverProtectedResourceMetadata_PathStyleFallsBackToOriginOn404(t *testing.T) {
	t.Parallel()

	// Upstream only advertises metadata under the origin-style well-known URL.
	// The probe tries the path-style URL first, gets a 404, and falls back to
	// origin-style.
	var probedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probedPaths = append(probedPaths, r.URL.Path)
		if r.URL.Path != "/.well-known/oauth-protected-resource" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              "http://" + r.Host,
			"authorization_servers": []string{"https://auth.example.com"},
		})
	}))
	t.Cleanup(server.Close)

	doc, _, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), newProbeTestPolicy(t), server.URL+"/mcp/foo")
	require.NoError(t, err)
	require.Equal(t, []string{"https://auth.example.com"}, doc.AuthorizationServers)
	require.Equal(t, []string{
		"/.well-known/oauth-protected-resource/mcp/foo",
		"/.well-known/oauth-protected-resource",
	}, probedPaths, "path-style first, origin-style on 404")
}

func TestDiscoverProtectedResourceMetadata_PathStyleAndOriginBoth404(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	_, _, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), newProbeTestPolicy(t), server.URL+"/mcp/foo")

	var probeErr *wellknown.ProtectedResourceDiscoveryError
	require.ErrorAs(t, err, &probeErr)
	require.Equal(t, "not_found", probeErr.Code())
	require.Equal(t, http.StatusNotFound, probeErr.Status)
	// The error reflects the final attempt (origin-style), which is the more
	// useful signal for operators: "we couldn't find it at the canonical spot".
	require.Contains(t, probeErr.ProbeURL, "/.well-known/oauth-protected-resource")
	require.NotContains(t, probeErr.ProbeURL, "/mcp/foo")
}

func TestDiscoverProtectedResourceMetadata_PathStyle5xxFallsBackToOrigin(t *testing.T) {
	t.Parallel()

	// A 5xx at the speculative path-style probe falls back to the canonical
	// origin-style URL. Non-compliant SPA catch-alls answer the path-style
	// guess with a 500 (e.g. "Only HTML requests are supported here") rather
	// than a 404, so the path-style status must not block the origin-style
	// probe that actually serves the document.
	var probedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probedPaths = append(probedPaths, r.URL.Path)
		if r.URL.Path != "/.well-known/oauth-protected-resource" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              "http://" + r.Host,
			"authorization_servers": []string{"https://auth.example.com"},
		})
	}))
	t.Cleanup(server.Close)

	doc, _, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), newProbeTestPolicy(t), server.URL+"/mcp/foo")
	require.NoError(t, err)
	require.Equal(t, []string{"https://auth.example.com"}, doc.AuthorizationServers)
	require.Equal(t, []string{
		"/.well-known/oauth-protected-resource/mcp/foo",
		"/.well-known/oauth-protected-resource",
	}, probedPaths, "path-style 5xx falls through to origin-style")
}

func TestDiscoverProtectedResourceMetadata_PathStyle5xxAndOrigin5xxSurfacesOrigin(t *testing.T) {
	t.Parallel()

	// When both candidates fail, the surfaced error reflects the final,
	// canonical origin-style attempt — not the speculative path-style guess.
	var probedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probedPaths = append(probedPaths, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	_, _, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), newProbeTestPolicy(t), server.URL+"/mcp/foo")

	var probeErr *wellknown.ProtectedResourceDiscoveryError
	require.ErrorAs(t, err, &probeErr)
	require.Equal(t, "http_error", probeErr.Code())
	require.Equal(t, http.StatusInternalServerError, probeErr.Status)
	require.Len(t, probedPaths, 2, "path-style 5xx falls through, both probed")
	require.Contains(t, probeErr.ProbeURL, "/.well-known/oauth-protected-resource")
	require.NotContains(t, probeErr.ProbeURL, "/mcp/foo")
}

func TestDiscoverProtectedResourceMetadata_NoPathSkipsPathStyle(t *testing.T) {
	t.Parallel()

	var probedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probedPaths = append(probedPaths, r.URL.Path)
		if r.URL.Path != "/.well-known/oauth-protected-resource" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              "http://" + r.Host,
			"authorization_servers": []string{"https://auth.example.com"},
		})
	}))
	t.Cleanup(server.Close)

	// Resource URL has no path component — only origin-style is attempted.
	_, _, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), newProbeTestPolicy(t), server.URL)
	require.NoError(t, err)
	require.Equal(t, []string{"/.well-known/oauth-protected-resource"}, probedPaths)
}

func TestDiscoverProtectedResourceMetadata_TrailingSlashPathSkipsPathStyle(t *testing.T) {
	t.Parallel()

	// Resource URL is "<origin>/" — the trailing slash collapses to an empty
	// path, so path-style is skipped and only origin-style is probed.
	var probedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probedPaths = append(probedPaths, r.URL.Path)
		if r.URL.Path != "/.well-known/oauth-protected-resource" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              "http://" + r.Host,
			"authorization_servers": []string{"https://auth.example.com"},
		})
	}))
	t.Cleanup(server.Close)

	_, _, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), newProbeTestPolicy(t), server.URL+"/")
	require.NoError(t, err)
	require.Equal(t, []string{"/.well-known/oauth-protected-resource"}, probedPaths)
}

func TestDiscoverProtectedResourceMetadata_NotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	_, _, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), newProbeTestPolicy(t), server.URL)
	require.Error(t, err)

	var probeErr *wellknown.ProtectedResourceDiscoveryError
	require.ErrorAs(t, err, &probeErr)
	require.Equal(t, "not_found", probeErr.Code())
	require.Equal(t, http.StatusNotFound, probeErr.Status)
	require.Contains(t, probeErr.UserMessage(), "not advertised")
	require.Contains(t, probeErr.UserMessage(), server.URL)
}

func TestDiscoverProtectedResourceMetadata_ServerError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	_, _, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), newProbeTestPolicy(t), server.URL)

	var probeErr *wellknown.ProtectedResourceDiscoveryError
	require.ErrorAs(t, err, &probeErr)
	require.Equal(t, "http_error", probeErr.Code())
	require.Equal(t, http.StatusInternalServerError, probeErr.Status)
	require.Contains(t, probeErr.UserMessage(), "Unexpected HTTP 500")
}

func TestDiscoverProtectedResourceMetadata_MalformedJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not-json"))
	}))
	t.Cleanup(server.Close)

	_, _, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), newProbeTestPolicy(t), server.URL)

	var probeErr *wellknown.ProtectedResourceDiscoveryError
	require.ErrorAs(t, err, &probeErr)
	require.Equal(t, "malformed", probeErr.Code())
	require.Equal(t, http.StatusOK, probeErr.Status)
	require.Contains(t, probeErr.UserMessage(), "valid RFC 9728 document")
}

func TestDiscoverProtectedResourceMetadata_TransportError(t *testing.T) {
	t.Parallel()

	// Stand up a server, capture its URL, then close so subsequent dials fail.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	addr := server.URL
	server.Close()

	_, _, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), newProbeTestPolicy(t), addr)

	var probeErr *wellknown.ProtectedResourceDiscoveryError
	require.ErrorAs(t, err, &probeErr)
	require.Equal(t, "transport_error", probeErr.Code())
	require.Equal(t, 0, probeErr.Status)
	require.Contains(t, probeErr.UserMessage(), "Could not reach")
}

func TestDiscoverProtectedResourceMetadata_ReadBodyError(t *testing.T) {
	t.Parallel()

	// Hijack and write a 200 OK with a Content-Length larger than the bytes
	// actually written, then close the connection. The client sees an
	// unexpected EOF while reading the body — Status is 200 but no decode is
	// attempted, so the failure must classify as transport_error, not malformed.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !assert.True(t, ok, "ResponseWriter must support Hijacker") {
			return
		}
		conn, _, err := hj.Hijack()
		if !assert.NoError(t, err) {
			return
		}
		// Promise 1024 bytes; deliver a short prefix; close.
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 1024\r\n\r\n{\"resource\":"))
		_ = conn.Close()
	}))
	t.Cleanup(server.Close)

	_, _, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), newProbeTestPolicy(t), server.URL)

	var probeErr *wellknown.ProtectedResourceDiscoveryError
	require.ErrorAs(t, err, &probeErr)
	require.Equal(t, "transport_error", probeErr.Code())
	require.Equal(t, http.StatusOK, probeErr.Status)
	require.Contains(t, probeErr.UserMessage(), "Could not reach")
}

func TestDiscoverProtectedResourceMetadata_Timeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hold the request until the client's deadline expires.
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	_, _, err := wellknown.DiscoverProtectedResourceMetadata(ctx, newProbeTestPolicy(t), server.URL)

	var probeErr *wellknown.ProtectedResourceDiscoveryError
	require.ErrorAs(t, err, &probeErr)
	require.Equal(t, "timeout", probeErr.Code())
	require.Contains(t, probeErr.UserMessage(), "Timed out")
}

func TestDiscoverProtectedResourceMetadata_InvalidURL(t *testing.T) {
	t.Parallel()

	_, _, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), newProbeTestPolicy(t), "not-a-url")

	var probeErr *wellknown.ProtectedResourceDiscoveryError
	require.ErrorAs(t, err, &probeErr)
	require.Equal(t, "invalid_url", probeErr.Code())
	require.Empty(t, probeErr.ProbeURL)
	require.Contains(t, probeErr.UserMessage(), "Could not compute")
}

func TestDiscoverProtectedResourceMetadata_UnsupportedScheme(t *testing.T) {
	t.Parallel()

	_, _, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), newProbeTestPolicy(t), "ftp://example.com")

	var probeErr *wellknown.ProtectedResourceDiscoveryError
	require.ErrorAs(t, err, &probeErr)
	require.Equal(t, "invalid_url", probeErr.Code())
}

func TestDiscoverProtectedResourceMetadata_HostBlocked(t *testing.T) {
	t.Parallel()

	// Default policy blocks RFC 1918 / loopback ranges.
	defaultPolicy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))

	_, _, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), defaultPolicy, "http://127.0.0.1:1")

	var probeErr *wellknown.ProtectedResourceDiscoveryError
	require.ErrorAs(t, err, &probeErr)
	require.Equal(t, "host_blocked", probeErr.Code())
	require.Contains(t, probeErr.UserMessage(), "not allowed")
}

func TestDiscoverProtectedResourceMetadata_WarningsMissingFields(t *testing.T) {
	t.Parallel()

	server := serveJSON(t, map[string]any{
		"scopes_supported": []string{"read"},
	})

	doc, warnings, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), newProbeTestPolicy(t), server.URL)
	require.NoError(t, err)
	require.Empty(t, doc.Resource)
	require.Len(t, warnings, 2)
	requireContainsString(t, warnings, "resource field missing")
	requireContainsString(t, warnings, "authorization_servers missing")
}

func TestDiscoverProtectedResourceMetadata_WarningsMismatchedResource(t *testing.T) {
	t.Parallel()

	server := serveJSON(t, map[string]any{
		"resource":              "https://different.example.com",
		"authorization_servers": []string{"https://auth.example.com"},
	})

	_, warnings, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), newProbeTestPolicy(t), server.URL)
	require.NoError(t, err)
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "does not match requested")
}

func TestDiscoverProtectedResourceMetadata_TrailingSlashTolerated(t *testing.T) {
	t.Parallel()

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/oauth-protected-resource" {
			http.NotFound(w, r)
			return
		}
		// Caller requested "<origin>/mcp/"; upstream reports "<origin>/mcp"
		// (no trailing slash). The slash difference should not warn.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              serverURL + "/mcp",
			"authorization_servers": []string{"https://auth.example.com"},
		})
	}))
	t.Cleanup(server.Close)
	serverURL = server.URL

	_, warnings, err := wellknown.DiscoverProtectedResourceMetadata(t.Context(), newProbeTestPolicy(t), server.URL+"/mcp/")
	require.NoError(t, err)
	require.Empty(t, warnings, "trailing slash difference should not produce a warning")
}

func requireContainsString(t *testing.T, haystack []string, needle string) {
	t.Helper()
	for _, h := range haystack {
		if strings.Contains(h, needle) {
			return
		}
	}
	require.Failf(t, "missing warning", "expected one of %v to contain %q", haystack, needle)
}
