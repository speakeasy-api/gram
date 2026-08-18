package marketplace

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type localResolver struct {
	token string
}

func (r localResolver) Resolve(_ context.Context, token string) (Upstream, error) {
	if token != r.token {
		return Upstream{}, ErrNotFound
	}
	return Upstream{Token: token, Owner: "local-fixture", Repo: "marketplace", AccessToken: ""}, nil
}

func TestLocalServerSupportsGitClone(t *testing.T) {
	t.Parallel()

	token := strings.Repeat("a", 43)
	server := NewLocalServer(
		localResolver{token: token},
		func(_ context.Context, owner, repo string) (map[string][]byte, error) {
			require.Equal(t, "local-fixture", owner)
			require.Equal(t, "marketplace", repo)
			return map[string][]byte{
				".claude-plugin/marketplace.json": []byte(`{"name":"local-marketplace"}`),
				"platform-mcp/.mcp.json":          []byte(`{"mcpServers":{}}`),
			}, nil
		},
		testenv.NewLogger(t),
	)
	httpServer := httptest.NewServer(server.Routes())
	t.Cleanup(httpServer.Close)

	cloneDir := filepath.Join(t.TempDir(), "clone")
	cmd := exec.CommandContext(t.Context(), "git", "clone", "--depth", "1", fmt.Sprintf("%s%s%s.git", httpServer.URL, RoutePrefix, token), cloneDir)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	require.FileExists(t, filepath.Join(cloneDir, ".git", "shallow"))

	marketplaceJSON, err := os.ReadFile(filepath.Join(cloneDir, ".claude-plugin", "marketplace.json"))
	require.NoError(t, err)
	require.JSONEq(t, `{"name":"local-marketplace"}`, string(marketplaceJSON))
	mcpJSON, err := os.ReadFile(filepath.Join(cloneDir, "platform-mcp", ".mcp.json"))
	require.NoError(t, err)
	require.JSONEq(t, `{"mcpServers":{}}`, string(mcpJSON))
}

func TestLocalServerRejectsUnknownToken(t *testing.T) {
	t.Parallel()

	token := strings.Repeat("a", 43)
	server := NewLocalServer(
		localResolver{token: token},
		func(context.Context, string, string) (map[string][]byte, error) {
			return map[string][]byte{}, nil
		},
		testenv.NewLogger(t),
	)

	request := httptest.NewRequest("GET", RoutePrefix+strings.Repeat("b", 43)+".git/info/refs", nil)
	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, request)
	require.Equal(t, 404, response.Code)
}
