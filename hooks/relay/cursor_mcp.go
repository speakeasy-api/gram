package relay

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/speakeasy-api/agenthooks"
)

// cursorServerMetadataFile is the file Cursor writes for every MCP server it
// exposes to a workspace, under
// ~/.cursor/projects/<workspace-slug>/mcps/<server-identifier>/.
const cursorServerMetadataFile = "SERVER_METADATA.json"

// maxCursorServerMetadataBytes bounds one SERVER_METADATA.json read. The file
// holds two short strings; 64 KiB leaves ample room while a corrupt or hostile
// file cannot make the hook buffer much.
const maxCursorServerMetadataBytes = 64 << 10 // 64 KiB

const (
	// cursorBuiltinIdentifierPrefix starts the identifier of servers Cursor
	// ships itself.
	cursorBuiltinIdentifierPrefix = "cursor-"

	// cursorPluginIdentifierPrefix starts the identifier of servers a Cursor
	// plugin provides.
	cursorPluginIdentifierPrefix = "plugin-"

	// cursorUserIdentifierPrefix starts the identifier of servers the user or
	// an editor extension configured.
	cursorUserIdentifierPrefix = "user-"
)

// cursorServerMetadata is the content of Cursor's SERVER_METADATA.json.
type cursorServerMetadata struct {
	// ServerIdentifier is Cursor's stable identifier for the server, prefixed
	// by where it comes from: "cursor-" for Cursor's own servers, "plugin-"
	// for plugin servers, "user-" for user and extension servers.
	ServerIdentifier string `json:"serverIdentifier"`

	// ServerName is the name Cursor reports for the server in its hooks.
	ServerName string `json:"serverName"`
}

// cursorProjectsDir returns Cursor's per-workspace state directory, or "" when
// the home directory cannot be resolved.
func cursorProjectsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".cursor", "projects")
}

// cursorWorkspaceSlugs returns the directory names Cursor may use under its
// projects directory for a workspace path. Cursor replaces every character
// other than an ASCII letter or digit with "-" ("/Users/a/my_app.v2" becomes
// "Users-a-my-app-v2"); the second candidate also folds runs of "-" in case a
// path holds adjacent separators.
func cursorWorkspaceSlugs(workspacePath string) []string {
	workspacePath = strings.TrimSpace(workspacePath)
	if workspacePath == "" {
		return nil
	}
	var b strings.Builder
	b.Grow(len(workspacePath))
	for _, r := range workspacePath {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	exact := strings.Trim(b.String(), "-")
	if exact == "" {
		return nil
	}
	folded := exact
	for strings.Contains(folded, "--") {
		folded = strings.ReplaceAll(folded, "--", "-")
	}
	if folded == exact {
		return []string{exact}
	}
	return []string{exact, folded}
}

// cursorServerIdentifier returns the Cursor server identifier of the MCP
// server a Cursor tool call reached, read from the SERVER_METADATA.json files
// Cursor keeps for the session's workspace under projectsDir. The server is
// matched by identifier, then by its reported name, then (when the hook
// reported no server name) by the tool it exposes. It returns "" when nothing
// matches or when the matches disagree on whether Cursor or the user provides
// the server, leaving the decision to the server's MCP inventory.
func cursorServerIdentifier(projectsDir string, session agenthooks.SessionInfo, serverName, toolName string) string {
	if strings.TrimSpace(projectsDir) == "" {
		return ""
	}
	serverName = strings.TrimSpace(serverName)
	toolName = strings.TrimSpace(toolName)
	if serverName == "" && !isPlainFileName(toolName) {
		return ""
	}

	for _, mcpsDir := range cursorWorkspaceMCPDirs(projectsDir, session) {
		if identifier, found := matchCursorServer(mcpsDir, serverName, toolName); found {
			return identifier
		}
	}
	return ""
}

// cursorWorkspaceMCPDirs lists the existing mcps directories of the session's
// workspace roots and working directory, without duplicates.
func cursorWorkspaceMCPDirs(projectsDir string, session agenthooks.SessionInfo) []string {
	paths := append([]string{}, session.WorkspaceRoots...)
	paths = append(paths, session.CWD)

	seen := map[string]struct{}{}
	var dirs []string
	for _, workspacePath := range paths {
		for _, slug := range cursorWorkspaceSlugs(workspacePath) {
			dir := filepath.Join(projectsDir, slug, "mcps")
			if _, ok := seen[dir]; ok {
				continue
			}
			seen[dir] = struct{}{}
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				dirs = append(dirs, dir)
			}
		}
	}
	return dirs
}

// matchCursorServer resolves the server in one workspace's mcps directory.
// found is true when the directory settles the question, including with an
// empty identifier for conflicting matches.
func matchCursorServer(mcpsDir, serverName, toolName string) (string, bool) {
	entries, err := os.ReadDir(mcpsDir)
	if err != nil {
		return "", false
	}

	var byName, byTool []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metadata, ok := readCursorServerMetadata(filepath.Join(mcpsDir, entry.Name(), cursorServerMetadataFile))
		if !ok {
			continue
		}
		if serverName != "" && metadata.ServerIdentifier == serverName {
			return metadata.ServerIdentifier, true
		}
		if serverName != "" && metadata.ServerName == serverName {
			byName = append(byName, metadata.ServerIdentifier)
		}
		if serverName == "" && cursorServerHasTool(filepath.Join(mcpsDir, entry.Name()), toolName) {
			byTool = append(byTool, metadata.ServerIdentifier)
		}
	}

	matches := byName
	if serverName == "" {
		matches = byTool
	}
	if len(matches) == 0 {
		return "", false
	}
	return agreeingCursorIdentifier(matches), true
}

func readCursorServerMetadata(path string) (cursorServerMetadata, bool) {
	var metadata cursorServerMetadata
	f, err := os.Open(path)
	if err != nil {
		return metadata, false
	}
	defer func() { _ = f.Close() }()

	raw, err := io.ReadAll(io.LimitReader(f, maxCursorServerMetadataBytes))
	if err != nil || json.Unmarshal(raw, &metadata) != nil {
		return metadata, false
	}
	metadata.ServerIdentifier = strings.TrimSpace(metadata.ServerIdentifier)
	metadata.ServerName = strings.TrimSpace(metadata.ServerName)
	return metadata, metadata.ServerIdentifier != ""
}

func cursorServerHasTool(serverDir, toolName string) bool {
	info, err := os.Stat(filepath.Join(serverDir, "tools", toolName+".json"))
	return err == nil && !info.IsDir()
}

// agreeingCursorIdentifier returns one of several matching identifiers when
// they agree on whether Cursor or the user provides the server, and "" when
// they do not.
func agreeingCursorIdentifier(identifiers []string) string {
	sort.Strings(identifiers)
	builtin := isCursorBuiltinIdentifier(identifiers[0])
	for _, identifier := range identifiers[1:] {
		if isCursorBuiltinIdentifier(identifier) != builtin {
			return ""
		}
	}
	return identifiers[0]
}

func isCursorBuiltinIdentifier(identifier string) bool {
	return strings.HasPrefix(identifier, cursorBuiltinIdentifierPrefix)
}

// isCursorServerIdentifier reports whether a value has the shape of a Cursor
// server identifier.
func isCursorServerIdentifier(identifier string) bool {
	return strings.HasPrefix(identifier, cursorBuiltinIdentifierPrefix) ||
		strings.HasPrefix(identifier, cursorPluginIdentifierPrefix) ||
		strings.HasPrefix(identifier, cursorUserIdentifierPrefix)
}

// isPlainFileName reports whether a tool name can name a file in a tools
// directory without escaping it.
func isPlainFileName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`)
}
