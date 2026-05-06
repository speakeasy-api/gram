package marketplace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// rewriteManifest is the only place we encode the Claude Code marketplace
// schema, so its output shape is the load-bearing contract. The schema
// (https://www.schemastore.org/claude-code-marketplace.json) defines exactly
// four plugin source types: npm, url, github, and git-subdir. There is no
// plain "git" type — installs fail with "source type your version does not
// support" if you use one. These tests pin the discriminator and field names.
func TestRewriteManifest(t *testing.T) {
	t.Parallel()

	s := &Server{
		publicBaseURL: "https://gram.test",
		logger:        testenv.NewLogger(t),
	}

	t.Run("relative path source becomes git-subdir", func(t *testing.T) {
		t.Parallel()
		in := []byte(`{
			"name": "m",
			"owner": {"name": "Acme"},
			"plugins": [
				{"name": "foo", "source": "./foo", "description": ""}
			]
		}`)

		out, err := s.rewriteManifest(in, "TOK")
		require.NoError(t, err)

		var got struct {
			Plugins []struct {
				Source map[string]any `json:"source"`
			} `json:"plugins"`
		}
		require.NoError(t, json.Unmarshal(out, &got))
		require.Len(t, got.Plugins, 1)
		require.Equal(t, "git-subdir", got.Plugins[0].Source["source"])
		require.Equal(t, "https://gram.test/marketplace/p/TOK.git", got.Plugins[0].Source["url"])
		require.Equal(t, "foo", got.Plugins[0].Source["path"])
	})

	t.Run("leading slash on path is trimmed", func(t *testing.T) {
		t.Parallel()
		in := []byte(`{"plugins": [{"name": "x", "source": "/x"}]}`)
		out, err := s.rewriteManifest(in, "TOK")
		require.NoError(t, err)

		var got struct {
			Plugins []struct {
				Source map[string]any `json:"source"`
			} `json:"plugins"`
		}
		require.NoError(t, json.Unmarshal(out, &got))
		require.Equal(t, "x", got.Plugins[0].Source["path"])
	})

	t.Run("object source is preserved as-is", func(t *testing.T) {
		t.Parallel()
		// If the publish flow ever starts emitting object sources directly
		// (e.g. github source for a public dep), the proxy should leave them
		// alone — only string sources need rewriting.
		in := []byte(`{"plugins": [{"name": "y", "source": {"source": "github", "repo": "anthropics/example"}}]}`)
		out, err := s.rewriteManifest(in, "TOK")
		require.NoError(t, err)

		var got struct {
			Plugins []struct {
				Source map[string]any `json:"source"`
			} `json:"plugins"`
		}
		require.NoError(t, json.Unmarshal(out, &got))
		require.Equal(t, "github", got.Plugins[0].Source["source"])
		require.Equal(t, "anthropics/example", got.Plugins[0].Source["repo"])
	})

	t.Run("multiple plugins all rewritten", func(t *testing.T) {
		t.Parallel()
		in := []byte(`{"plugins": [
			{"name": "a", "source": "./a"},
			{"name": "b", "source": "./b"}
		]}`)
		out, err := s.rewriteManifest(in, "TOK")
		require.NoError(t, err)

		var got struct {
			Plugins []struct {
				Name   string         `json:"name"`
				Source map[string]any `json:"source"`
			} `json:"plugins"`
		}
		require.NoError(t, json.Unmarshal(out, &got))
		require.Len(t, got.Plugins, 2)
		require.Equal(t, "a", got.Plugins[0].Source["path"])
		require.Equal(t, "b", got.Plugins[1].Source["path"])
	})

	t.Run("missing plugins array errors", func(t *testing.T) {
		t.Parallel()
		_, err := s.rewriteManifest([]byte(`{"name": "no-plugins"}`), "TOK")
		require.Error(t, err)
	})

	t.Run("malformed JSON errors", func(t *testing.T) {
		t.Parallel()
		_, err := s.rewriteManifest([]byte(`{not json`), "TOK")
		require.Error(t, err)
	})
}

// spyResolver records whether Resolve was called and returns ErrNotFound.
// Used to assert that malformed tokens are rejected before the DB lookup.
type spyResolver struct {
	called bool
}

func (r *spyResolver) Resolve(_ context.Context, _ string) (Upstream, error) {
	r.called = true
	return Upstream{}, ErrNotFound
}

// Token-format check is a cheap pre-filter that keeps the resolver's DB
// lookup off the hot path for anyone hammering the proxy with random URLs.
// The 256-bit token entropy makes brute-force infeasible; this guards the
// DB from random-string flooding.
func TestMalformedTokensSkipResolver(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		req  *http.Request
	}{
		{"manifest: too short", httptest.NewRequest(http.MethodGet, "/marketplace/m/short/marketplace.json", nil)},
		{"manifest: bad chars", httptest.NewRequest(http.MethodGet, "/marketplace/m/bad!chars1234567890123456789012345678901234567/marketplace.json", nil)},
		{"manifest: too long", httptest.NewRequest(http.MethodGet, "/marketplace/m/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/marketplace.json", nil)},
		{"info/refs: no .git suffix", httptest.NewRequest(http.MethodGet, "/marketplace/p/short/info/refs?service=git-upload-pack", nil)},
		{"info/refs: bad chars before .git", httptest.NewRequest(http.MethodGet, "/marketplace/p/bad!chars1234567890123456789012345678901234567.git/info/refs?service=git-upload-pack", nil)},
		{"upload-pack: too short", httptest.NewRequest(http.MethodPost, "/marketplace/p/short.git/git-upload-pack", nil)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spy := &spyResolver{}
			s := &Server{
				resolver: spy,
				logger:   testenv.NewLogger(t),
			}
			rec := httptest.NewRecorder()
			s.Routes().ServeHTTP(rec, tc.req)

			require.Equal(t, http.StatusNotFound, rec.Code)
			require.False(t, spy.called, "resolver must not be called for malformed tokens")
		})
	}
}
