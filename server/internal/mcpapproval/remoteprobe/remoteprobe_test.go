package remoteprobe

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newProbe(t *testing.T) *Probe {
	t.Helper()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)

	return New(testenv.NewLogger(t), policy)
}

// A server that cleanly 404s every well-known URL publishes no OAuth
// metadata: a real outcome, returned as nil declaration with nil error, never
// as a failure.
func TestDiscoverAuthority_CleanNotFoundIsNoDeclaration(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	declaration, err := newProbe(t).DiscoverAuthority(t.Context(), server.URL+"/mcp")
	require.NoError(t, err)
	require.Nil(t, declaration)
}

// Middle statuses — an auth refusal, a rate limit, a server error — leave
// publication unknown. Each must surface as a failed probe (an error the
// evidence records as a gap), never as "publishes no OAuth metadata".
func TestDiscoverAuthority_RefusalsAndErrorsAreFailedProbes(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			t.Cleanup(server.Close)

			declaration, err := newProbe(t).DiscoverAuthority(t.Context(), server.URL+"/mcp")
			require.Error(t, err, "a %d must not read as published-nothing", status)
			require.Nil(t, declaration)
		})
	}
}

// A 200 carrying unparseable JSON is invalid published metadata: discovery
// did not complete, so the probe fails rather than reporting no metadata.
func TestDiscoverAuthority_InvalidJSONOnOKIsAFailedProbe(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resource": not-json`))
	}))
	t.Cleanup(server.Close)

	declaration, err := newProbe(t).DiscoverAuthority(t.Context(), server.URL+"/mcp")
	require.Error(t, err)
	require.Nil(t, declaration)
}

// Published RFC 8414 metadata comes back as a declaration carrying the
// registration endpoint and scopes the server advertised.
func TestDiscoverAuthority_PublishedMetadataYieldsDeclaration(t *testing.T) {
	t.Parallel()

	// Filled in after the server starts; every request arrives later.
	var baseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/oauth-authorization-server/mcp" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
			"issuer": %[1]q,
			"authorization_endpoint": %[2]q,
			"token_endpoint": %[3]q,
			"registration_endpoint": %[4]q,
			"scopes_supported": ["read", "write"]
		}`, baseURL+"/mcp", baseURL+"/authorize", baseURL+"/token", baseURL+"/register")
	}))
	t.Cleanup(server.Close)
	baseURL = server.URL

	declaration, err := newProbe(t).DiscoverAuthority(t.Context(), server.URL+"/mcp")
	require.NoError(t, err)
	require.NotNil(t, declaration)
	require.True(t, declaration.RequiresOAuth)
	require.Equal(t, "2.1", declaration.OAuthVersion)
	require.Equal(t, server.URL+"/register", declaration.RegistrationEndpoint)
	require.Equal(t, []string{"read", "write"}, declaration.Scopes)
}

// The field bounds cap what one declaration can carry into a stored evidence
// document, and every cut is marked so a bounded value cannot pass as the
// server's own words.
func TestBoundField(t *testing.T) {
	t.Parallel()

	short := "an ordinary description"
	require.Equal(t, short, boundField(short))

	long := strings.Repeat("a", maxDeclarationFieldBytes+1)
	bounded := boundField(long)
	require.Len(t, bounded, maxDeclarationFieldBytes)
	require.True(t, strings.HasSuffix(bounded, truncationMarker))

	// A cut landing inside a multi-byte rune retreats to the rune's start:
	// the bounded value stays valid UTF-8 and within the cap, never carrying
	// a mangled half-rune into the stored document. The odd-length ASCII
	// prefix shifts the two-byte runes off even offsets so the cut lands on
	// a continuation byte — without it the cut falls on a rune start and the
	// retreat path never runs.
	multibyte := "a" + strings.Repeat("é", maxDeclarationFieldBytes)
	cut := maxDeclarationFieldBytes - len(truncationMarker)
	require.False(t, utf8.RuneStart(multibyte[cut]))
	boundedMultibyte := boundField(multibyte)
	require.LessOrEqual(t, len(boundedMultibyte), maxDeclarationFieldBytes)
	require.True(t, strings.HasSuffix(boundedMultibyte, truncationMarker))
	require.True(t, utf8.ValidString(boundedMultibyte))
}

// An oversized schema is dropped rather than clipped: truncated JSON would
// parse as nothing anyway, and a partial schema could surface only the
// capabilities in its head — silently reading as "declared less".
func TestBoundSchema(t *testing.T) {
	t.Parallel()

	small := `{"type": "object"}`
	require.Equal(t, small, boundSchema(small))

	oversized := `{"type": "object", "description": "` + strings.Repeat("a", maxDeclarationFieldBytes) + `"}`
	require.Empty(t, boundSchema(oversized))
}
