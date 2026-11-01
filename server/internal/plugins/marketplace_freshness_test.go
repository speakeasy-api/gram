package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"testing"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
)

// ttlRecordingCache misses every read and records the TTL of each write.
type ttlRecordingCache struct {
	cache.Cache
	ttls []time.Duration
}

func (c *ttlRecordingCache) Get(context.Context, string, any) error {
	return redisCache.ErrCacheMiss
}

func (c *ttlRecordingCache) Set(_ context.Context, _ string, _ any, ttl time.Duration) error {
	c.ttls = append(c.ttls, ttl)
	return nil
}

// repoFilesGitHub answers GetRepoFiles with files or err.
type repoFilesGitHub struct {
	GitHubPublisher
	files map[string][]byte
	err   error
}

func (g repoFilesGitHub) GetRepoFiles(_ context.Context, _ int64, _, _, _ string, include func(string) bool) (map[string][]byte, error) {
	if g.err != nil {
		return nil, g.err
	}
	out := make(map[string][]byte)
	for filePath, content := range g.files {
		if include == nil || include(filePath) {
			out[filePath] = content
		}
	}
	return out, nil
}

// heldPluginFiles are the published files heldPluginEntry needs for slug.
func heldPluginFiles(t *testing.T, slug string) map[string][]byte {
	t.Helper()
	claude, err := json.Marshal(map[string]string{"name": slug, "displayName": "Held", "description": ""})
	require.NoError(t, err)
	mcp := []byte(`{"mcpServers":{}}`)
	return map[string][]byte{
		path.Join(slug, ".claude-plugin/plugin.json"):                             claude,
		path.Join(slug, ".mcp.json"):                                              mcp,
		path.Join(cursorPluginRoot, slug+"-cursor", ".cursor-plugin/plugin.json"): claude,
		path.Join(cursorPluginRoot, slug+"-cursor", "mcp.json"):                   mcp,
		path.Join(slug+"-codex", ".codex-plugin/plugin.json"):                     claude,
		path.Join(slug+"-codex", ".mcp.json"):                                     mcp,
	}
}

// A freshness read caches held plugins' entries for as long as the published
// state lasts, but a failed read only briefly. A repo GitHub reports missing or
// empty is such a failure: the repo was published, so it is retried soon
// rather than compared against the live state for ten minutes.
func TestHoldPendingPluginEntriesCachesFailedReadsBriefly(t *testing.T) {
	t.Parallel()

	const slug = "held"
	info := PluginInfo{Name: "Held", Slug: slug, Description: "", Servers: nil, Skills: nil, AgentPluginsV1Issues: nil}
	for _, test := range []struct {
		name     string
		github   repoFilesGitHub
		wantTTL  time.Duration
		wantHeld bool
	}{
		{name: "readable files", github: repoFilesGitHub{files: heldPluginFiles(t, slug)}, wantTTL: heldEntriesCacheTTL, wantHeld: true},
		{name: "missing or empty repo", github: repoFilesGitHub{err: ghclient.ErrRepoNotFound}, wantTTL: heldEntriesFailureTTL, wantHeld: false},
		{name: "failed read", github: repoFilesGitHub{err: errors.New("rate limited")}, wantTTL: heldEntriesFailureTTL, wantHeld: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			recording := &ttlRecordingCache{}
			s := &Service{logger: testenv.NewLogger(t), cache: recording, github: &GitHubConfig{Client: test.github, Org: "org", InstallationID: 1}}
			plan := s.holdPendingPluginEntries(t.Context(), uuid.New(), repo.PluginGithubConnection{RepoOwner: "org", RepoName: "repo"},
				[]PluginInfo{info}, map[string]string{slug: "published"}, pausedPluginPlan{}, map[string]PluginInfo{slug: info})

			require.Equal(t, []time.Duration{test.wantTTL}, recording.ttls)
			if test.wantHeld {
				require.Len(t, plan.Held, 1)
				require.Empty(t, plan.Active)
			} else {
				require.Empty(t, plan.Held)
				require.Equal(t, []PluginInfo{info}, plan.Active, "an unreadable held plugin counts with its live state")
			}
		})
	}
}
