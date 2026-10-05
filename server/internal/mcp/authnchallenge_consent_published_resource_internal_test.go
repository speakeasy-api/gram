package mcp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func publishing(resource string) protectedResourceFetcher {
	return func(context.Context, string) (wellknown.OAuthProtectedResourceMetadata, error) {
		return wellknown.OAuthProtectedResourceMetadata{Resource: resource}, nil
	}
}

func TestPublishedResource(t *testing.T) {
	t.Parallel()

	failing := func(context.Context, string) (wellknown.OAuthProtectedResourceMetadata, error) {
		return wellknown.OAuthProtectedResourceMetadata{}, errors.New("unreachable")
	}
	cases := []struct {
		name       string
		registered string
		fetch      protectedResourceFetcher
		want       string
	}{
		{name: "published trailing slash added", registered: "https://host.example.com", fetch: publishing("https://host.example.com/"), want: "https://host.example.com/"},
		{name: "published trailing slash dropped", registered: "https://host.example.com/mcp/", fetch: publishing("https://host.example.com/mcp"), want: "https://host.example.com/mcp"},
		{name: "same spelling", registered: "https://host.example.com/mcp", fetch: publishing("https://host.example.com/mcp"), want: "https://host.example.com/mcp"},
		{name: "another host ignored", registered: "https://host.example.com", fetch: publishing("https://elsewhere.example.com/"), want: "https://host.example.com"},
		{name: "another path ignored", registered: "https://host.example.com/mcp", fetch: publishing("https://host.example.com/"), want: "https://host.example.com/mcp"},
		{name: "no published resource", registered: "https://host.example.com", fetch: publishing(""), want: "https://host.example.com"},
		{name: "unreadable metadata", registered: "https://host.example.com", fetch: failing, want: "https://host.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &Service{protectedResourceFetcher: tc.fetch}
			require.Equal(t, tc.want, s.publishedResource(t.Context(), testenv.NewLogger(t), tc.registered))
		})
	}
}

// A Connect click waits on the metadata read for at most
// publishedResourceTimeout, then sends the registered URL.
func TestPublishedResource_TimeoutFallsBackToRegistered(t *testing.T) {
	t.Parallel()

	s := &Service{protectedResourceFetcher: func(ctx context.Context, _ string) (wellknown.OAuthProtectedResourceMetadata, error) {
		deadline, ok := ctx.Deadline()
		require.True(t, ok, "the read is bounded")
		require.LessOrEqual(t, time.Until(deadline), publishedResourceTimeout)
		return wellknown.OAuthProtectedResourceMetadata{Resource: "https://host.example.com/"}, context.DeadlineExceeded
	}}
	require.Equal(t, "https://host.example.com", s.publishedResource(t.Context(), testenv.NewLogger(t), "https://host.example.com"))
}

// Plain-http, non-loopback upstreams are never read.
func TestPublishedResource_SkipsNonHTTPSUpstreams(t *testing.T) {
	t.Parallel()

	s := &Service{protectedResourceFetcher: func(context.Context, string) (wellknown.OAuthProtectedResourceMetadata, error) {
		t.Fatal("metadata must not be read")
		return wellknown.OAuthProtectedResourceMetadata{}, nil
	}}
	require.Equal(t, "http://host.example.com", s.publishedResource(t.Context(), testenv.NewLogger(t), "http://host.example.com"))
}

// The default fetcher reads the well-known document through the guardian
// policy.
func TestPublishedResource_ReadsWellKnownDocument(t *testing.T) {
	t.Parallel()

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wellknown.OAuthProtectedResourcePath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resource":"` + srv.URL + `/","authorization_servers":["` + srv.URL + `"]}`))
	}))
	t.Cleanup(srv.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	s := &Service{guardianPolicy: policy}
	require.Equal(t, srv.URL+"/", s.publishedResource(t.Context(), testenv.NewLogger(t), srv.URL))
}

// A failed read logs the upstream's host, never the registered URL: its query
// may carry credentials.
func TestPublishedResource_FailureLogOmitsRegisteredQuery(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	s := &Service{protectedResourceFetcher: func(context.Context, string) (wellknown.OAuthProtectedResourceMetadata, error) {
		return wellknown.OAuthProtectedResourceMetadata{}, errors.New("unreachable")
	}}
	registered := "https://host.example.com/mcp?api_key=registered-secret"
	require.Equal(t, registered, s.publishedResource(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil)), registered))
	require.Contains(t, logs.String(), "host.example.com")
	require.NotContains(t, logs.String(), "registered-secret")
}
