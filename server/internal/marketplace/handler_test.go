package marketplace

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
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
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
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
		require.Equal(t, "https://gram.test/p/TOK.git", got.Plugins[0].Source["url"])
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
