package relay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"
)

// writeCursorServer lays out one server the way Cursor does under
// <projectsDir>/<slug>/mcps/<identifier>/.
func writeCursorServer(t *testing.T, projectsDir, slug, identifier, serverName string, tools ...string) {
	t.Helper()
	serverDir := filepath.Join(projectsDir, slug, "mcps", identifier)
	require.NoError(t, os.MkdirAll(filepath.Join(serverDir, "tools"), 0o700))
	raw, err := json.Marshal(cursorServerMetadata{ServerIdentifier: identifier, ServerName: serverName})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(serverDir, cursorServerMetadataFile), raw, 0o600))
	for _, tool := range tools {
		require.NoError(t, os.WriteFile(filepath.Join(serverDir, "tools", tool+".json"), []byte(`{}`), 0o600))
	}
}

func TestCursorWorkspaceSlugs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want []string
	}{
		{name: "plain path", path: "/Users/dev/code/app", want: []string{"Users-dev-code-app"}},
		{name: "dots and underscores", path: "/Users/dev/code/my_app.v2", want: []string{"Users-dev-code-my-app-v2"}},
		{name: "spaces", path: "/Users/dev/Library/Application Support/Cursor/workspace.json", want: []string{"Users-dev-Library-Application-Support-Cursor-workspace-json"}},
		{name: "adjacent separators", path: "/tmp/a--b", want: []string{"tmp-a--b", "tmp-a-b"}},
		{name: "empty", path: "  ", want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, cursorWorkspaceSlugs(tc.path))
		})
	}
}

func TestCursorServerIdentifier(t *testing.T) {
	t.Parallel()

	projectsDir := t.TempDir()
	const slug = "Users-dev-code-app"
	writeCursorServer(t, projectsDir, slug, "cursor-ide-browser", "cursor-ide-browser", "browser_click")
	writeCursorServer(t, projectsDir, slug, "plugin-acme-Linear", "Linear", "list_issues")
	writeCursorServer(t, projectsDir, slug, "user-vendor.ext-Tracker", "extension-Tracker", "track", "shared_tool")
	writeCursorServer(t, projectsDir, slug, "cursor-app-control", "cursor-app-control", "shared_tool")
	session := agenthooks.SessionInfo{CWD: "/Users/dev/code/app", WorkspaceRoots: []string{"/Users/dev/code/app"}}

	tests := []struct {
		name       string
		session    agenthooks.SessionInfo
		serverName string
		toolName   string
		want       string
	}{
		{name: "cursor built-in by identifier", session: session, serverName: "cursor-ide-browser", want: "cursor-ide-browser"},
		{name: "plugin server by reported name", session: session, serverName: "Linear", want: "plugin-acme-Linear"},
		{name: "extension server by reported name", session: session, serverName: "extension-Tracker", want: "user-vendor.ext-Tracker"},
		{name: "unknown server", session: session, serverName: "missing", want: ""},
		{name: "no server name falls back to the tool", session: session, toolName: "browser_click", want: "cursor-ide-browser"},
		{name: "tool shared by a built-in and a user server is left undecided", session: session, toolName: "shared_tool", want: ""},
		{name: "tool name that escapes the tools directory", session: session, toolName: "../SERVER_METADATA", want: ""},
		{
			name:       "workspace root only",
			session:    agenthooks.SessionInfo{CWD: "", WorkspaceRoots: []string{"/Users/dev/code/app"}},
			serverName: "Linear",
			want:       "plugin-acme-Linear",
		},
		{
			name:       "another workspace's servers are not consulted",
			session:    agenthooks.SessionInfo{CWD: "/Users/dev/code/other", WorkspaceRoots: []string{"/Users/dev/code/other"}},
			serverName: "Linear",
			want:       "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, cursorServerIdentifier(projectsDir, tc.session, tc.serverName, tc.toolName))
		})
	}
}

func TestCursorServerIdentifierSameNameAcrossPlugins(t *testing.T) {
	t.Parallel()

	projectsDir := t.TempDir()
	const slug = "Users-dev-code-app"
	writeCursorServer(t, projectsDir, slug, "plugin-two-Linear", "Linear")
	writeCursorServer(t, projectsDir, slug, "plugin-one-Linear", "Linear")
	session := agenthooks.SessionInfo{CWD: "/Users/dev/code/app", WorkspaceRoots: nil}

	require.Equal(t, "plugin-one-Linear", cursorServerIdentifier(projectsDir, session, "Linear", ""))
}

func TestCursorServerIdentifierWithoutProjectsDir(t *testing.T) {
	t.Parallel()

	session := agenthooks.SessionInfo{CWD: "/Users/dev/code/app", WorkspaceRoots: nil}
	require.Empty(t, cursorServerIdentifier("", session, "cursor-ide-browser", ""))
	require.Empty(t, cursorServerIdentifier(filepath.Join(t.TempDir(), "missing"), session, "cursor-ide-browser", ""))
}

func TestCursorServerIdentifierIgnoresUnreadableMetadata(t *testing.T) {
	t.Parallel()

	projectsDir := t.TempDir()
	serverDir := filepath.Join(projectsDir, "Users-dev-code-app", "mcps", "cursor-ide-browser")
	require.NoError(t, os.MkdirAll(serverDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(serverDir, cursorServerMetadataFile), []byte(`not json`), 0o600))
	session := agenthooks.SessionInfo{CWD: "/Users/dev/code/app", WorkspaceRoots: nil}

	require.Empty(t, cursorServerIdentifier(projectsDir, session, "cursor-ide-browser", ""))
}
