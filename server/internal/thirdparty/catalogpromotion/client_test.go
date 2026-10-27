package catalogpromotion

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func testClient(t *testing.T, handler http.HandlerFunc) *client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	c, ok := New(policy, Config{GitHubToken: "test-token", GitHubRepository: "example/guides"}).(*client)
	require.True(t, ok)
	c.githubBaseURL = server.URL
	return c
}

func TestEnsureGuideRecoversAfterAmbiguousCreate(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var created *githubIssue
	var labeled bool
	var creates, labels int
	var authorizations []string
	var requestedLabels []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/example/guides/issues":
			if created == nil {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			if err := json.NewEncoder(w).Encode([]githubIssue{*created}); err != nil {
				t.Error(err)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/repos/example/guides/issues":
			creates++
			var input createIssueRequest
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
				return
			}
			created = &githubIssue{Number: 42, Body: input.Body, State: "open"}
			// GitHub accepted the write, but a gateway lost the response.
			w.WriteHeader(http.StatusBadGateway)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/events"):
			if labeled {
				_, _ = w.Write([]byte(`[{"event":"labeled","label":{"name":"guide:draft"}}]`))
				return
			}
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/labels"):
			labels++
			var input labelsRequest
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
				return
			}
			requestedLabels = input.Labels
			labeled = true
			_, _ = w.Write([]byte(`[{"name":"guide:draft"}]`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	guide := Guide{Slug: "example", Name: "Example", Description: "Public service", RemoteURL: "https://mcp.example.com/mcp", DocumentationURL: "https://docs.example.com/mcp", AuthMode: "none"}
	_, err := c.EnsureGuide(t.Context(), guide)
	require.ErrorContains(t, err, "HTTP 502")
	issueURL, err := c.EnsureGuide(t.Context(), guide)
	require.NoError(t, err)
	require.Equal(t, "https://github.com/example/guides/issues/42", issueURL)
	// The label is gone after factory completion, but its event prevents reruns.
	_, err = c.EnsureGuide(t.Context(), guide)
	require.NoError(t, err)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, creates)
	require.Equal(t, 1, labels)
	require.Equal(t, []string{"guide:draft"}, requestedLabels)
	require.Contains(t, created.Body, `"auth_mode": "none"`)
	require.Contains(t, created.Body, "<!-- speakeasy-catalog-guide:example -->")
	for _, authorization := range authorizations {
		require.Equal(t, "Bearer test-token", authorization)
	}
}

func TestEnsureGuideRefusesConflictingIssueMarker(t *testing.T) {
	t.Parallel()
	var reads, writes atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			reads.Add(1)
		} else {
			writes.Add(1)
		}
		if err := json.NewEncoder(w).Encode([]githubIssue{{Number: 42, Body: "<!-- speakeasy-catalog-guide:example -->\nDifferent unreviewed instructions", State: "open"}}); err != nil {
			t.Error(err)
		}
	})
	_, err := c.EnsureGuide(t.Context(), Guide{Slug: "example", Name: "Example", RemoteURL: "https://mcp.example.com/mcp", DocumentationURL: "https://docs.example.com/mcp", AuthMode: "oauth"})
	require.ErrorContains(t, err, "conflicts with reviewed public metadata")
	kind, _ := Classify(err)
	require.Equal(t, ErrorPermanent, kind)
	require.EqualValues(t, 1, reads.Load())
	require.Zero(t, writes.Load(), "conflicting public issue must not create or trigger a guide")
}

func TestEnsureGuideRejectsPrivateInputsBeforeRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		alter func(*Guide)
	}{
		{name: "query credential", alter: func(g *Guide) { g.RemoteURL += "?token=secret" }},
		{name: "userinfo", alter: func(g *Guide) { g.RemoteURL = "https://secret@mcp.example.com" }},
		{name: "private host", alter: func(g *Guide) { g.RemoteURL = "https://service.internal/mcp" }},
		{name: "IP address", alter: func(g *Guide) { g.RemoteURL = "https://127.0.0.1/mcp" }},
		{name: "private documentation", alter: func(g *Guide) { g.DocumentationURL = "https://docs.local/setup" }},
		{name: "authentication", alter: func(g *Guide) { g.AuthMode = "bearer" }},
		{name: "invalid slug", alter: func(g *Guide) { g.Slug = "../../private" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid guide reached external service") })
			guide := Guide{Slug: "example", Name: "Example", RemoteURL: "https://mcp.example.com/mcp", DocumentationURL: "https://docs.example.com/mcp", AuthMode: "none"}
			tt.alter(&guide)
			_, err := c.EnsureGuide(t.Context(), guide)
			require.Error(t, err)
		})
	}
}

func TestEnsureGuideDoesNotCreateAfterLookupFailure(t *testing.T) {
	t.Parallel()
	methods := make(chan string, 1)
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		methods <- r.Method
		w.WriteHeader(http.StatusForbidden)
	})
	_, err := c.EnsureGuide(t.Context(), Guide{Slug: "example", Name: "Example", RemoteURL: "https://mcp.example.com/mcp", DocumentationURL: "https://docs.example.com/mcp", AuthMode: "none"})
	require.ErrorContains(t, err, "HTTP 403")
	require.Equal(t, http.MethodGet, <-methods)
}

func TestEnsureGuideRequiresFactoryTriggerAcknowledgement(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/repos/example/guides/issues" {
			_, _ = w.Write([]byte(`{"number":42,"state":"open"}`))
			return
		}
		// The issue exists, but a misconfigured repository did not add the trigger.
		_, _ = w.Write([]byte(`[]`))
	})
	_, err := c.EnsureGuide(t.Context(), Guide{Slug: "example", Name: "Example", RemoteURL: "https://mcp.example.com/mcp", DocumentationURL: "https://docs.example.com/mcp", AuthMode: "none"})
	require.ErrorContains(t, err, "trigger label was not applied")
}

func TestEnsureGuideDoesNotReportUntriggeredClosedIssueAsQueued(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/repos/example/guides/issues" {
			_, _ = w.Write([]byte(`{"number":42,"state":"closed"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/labels") {
			t.Error("closed issue must not be retriggered")
		}
		_, _ = w.Write([]byte(`[]`))
	})
	_, err := c.EnsureGuide(t.Context(), Guide{Slug: "example", Name: "Example", RemoteURL: "https://mcp.example.com/mcp", DocumentationURL: "https://docs.example.com/mcp", AuthMode: "none"})
	require.ErrorContains(t, err, "closed before the factory trigger")
	kind, _ := Classify(err)
	require.Equal(t, ErrorPermanent, kind)
}

func TestMissingConfigurationNeedsAttention(t *testing.T) {
	t.Parallel()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	c := New(policy, Config{})
	_, err = c.EnsureGuide(t.Context(), Guide{})
	require.ErrorIs(t, err, ErrNotConfigured)
}

func TestValidatePublicURLRejectsSpecialUseDomains(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"provider.test", "provider.invalid", "provider.example", "host.home.arpa", "provider.localhost", "provider.local", "provider.internal", "hidden.onion", "PROVIDER.TEST."} {
		require.Error(t, ValidatePublicURL("https://"+host+"/mcp"), host)
	}
	require.NoError(t, ValidatePublicURL("https://mcp.example.com/mcp"))
}
