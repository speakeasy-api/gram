package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

func TestResolveActivatedSkillPreservesExactRawContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "workspace")
	path := writeSkillManifest(t, filepath.Join(workspace, ".cursor", "skills", "raw-skill"), []byte("---\r\nname: raw-skill\r\n---\r\nbody\r\n"))
	event := cursorReadEvent(t, workspace, []string{workspace}, map[string]string{"file_path": path})

	resolved := resolveActivatedSkill(event, activatedSkillPayload("raw-skill"))

	require.NotNil(t, resolved)
	require.Equal(t, "raw-skill", resolved.name)
	require.Equal(t, "project", resolved.sourceLevel)
	require.Equal(t, path, resolved.sourcePath)
	require.Equal(t, "---\r\nname: raw-skill\r\n---\r\nbody\r\n", resolved.content)
	require.Equal(t, sha256Hex([]byte(resolved.content)), resolved.rawSHA256)
	require.True(t, resolved.captureReady)
}

func TestResolveActivatedSkillCapturesEmptyManifest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	path := writeSkillManifest(t, filepath.Join(workspace, ".cursor", "skills", "empty"), nil)
	event := cursorReadEvent(t, workspace, []string{workspace}, map[string]string{"file_path": path})

	resolved := resolveActivatedSkill(event, activatedSkillPayload("empty"))

	require.True(t, resolved.captureReady)
	require.Empty(t, resolved.content)
	require.Equal(t, sha256Hex(nil), resolved.rawSHA256)
}

func TestResolveActivatedSkillAcceptsMaximumManifestSize(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	content := []byte(strings.Repeat("a", maxSkillContentBytes))
	path := writeSkillManifest(t, filepath.Join(workspace, ".cursor", "skills", "maximum"), content)
	event := cursorReadEvent(t, workspace, []string{workspace}, map[string]string{"file_path": path})

	resolved := resolveActivatedSkill(event, activatedSkillPayload("maximum"))

	require.True(t, resolved.captureReady)
	require.Len(t, resolved.content, maxSkillContentBytes)
	require.Equal(t, sha256Hex(content), resolved.rawSHA256)
}

func TestResolveActivatedSkillHashesEntireOversizedManifestButDoesNotCaptureContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	content := []byte(strings.Repeat("a", maxSkillContentBytes+8192) + "full-file-tail")
	path := writeSkillManifest(t, filepath.Join(workspace, ".cursor", "skills", "oversized"), content)
	event := cursorReadEvent(t, workspace, []string{workspace}, map[string]string{"file_path": path})

	resolved := resolveActivatedSkill(event, activatedSkillPayload("oversized"))

	require.Equal(t, "project", resolved.sourceLevel)
	require.Equal(t, path, resolved.sourcePath)
	require.Equal(t, sha256Hex(content), resolved.rawSHA256)
	require.Empty(t, resolved.content)
	require.False(t, resolved.captureReady)
}

func TestResolveActivatedSkillHashesInvalidUTF8ButDoesNotCaptureContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	content := []byte{0xff, 0xfe, 0x00, 0x61}
	path := writeSkillManifest(t, filepath.Join(workspace, ".cursor", "skills", "invalid"), content)
	event := cursorReadEvent(t, workspace, []string{workspace}, map[string]string{"file_path": path})

	resolved := resolveActivatedSkill(event, activatedSkillPayload("invalid"))

	require.Equal(t, "project", resolved.sourceLevel)
	require.Equal(t, path, resolved.sourcePath)
	require.Equal(t, sha256Hex(content), resolved.rawSHA256)
	require.Empty(t, resolved.content)
	require.False(t, resolved.captureReady)
}

func TestResolveActivatedSkillClaudePersonalBeatsProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "nested")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))
	personalPath := writeSkillManifest(t, filepath.Join(home, ".claude", "skills", "shared"), []byte("personal"))
	writeSkillManifest(t, filepath.Join(repo, ".claude", "skills", "shared"), []byte("project"))

	resolved := resolveActivatedSkill(claudeSkillEvent(cwd, "shared"), activatedSkillPayload("shared"))

	require.Equal(t, "personal", resolved.sourceLevel)
	require.Equal(t, personalPath, resolved.sourcePath)
	require.Equal(t, "personal", resolved.content)
}

func TestResolveActivatedSkillClaudeManagedBeatsPersonalAndProject(t *testing.T) {
	managedRoot := filepath.Join(t.TempDir(), "managed", "skills")
	original := claudeManagedSkillsRoot
	claudeManagedSkillsRoot = func() string { return managedRoot }
	t.Cleanup(func() { claudeManagedSkillsRoot = original })
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "nested")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))
	managedPath := writeSkillManifest(t, filepath.Join(managedRoot, "shared"), []byte("managed"))
	writeSkillManifest(t, filepath.Join(home, ".claude", "skills", "shared"), []byte("personal"))
	writeSkillManifest(t, filepath.Join(repo, ".claude", "skills", "shared"), []byte("project"))

	resolved := resolveActivatedSkill(claudeSkillEvent(cwd, "shared"), activatedSkillPayload("shared"))

	require.Equal(t, "admin", resolved.sourceLevel)
	require.Equal(t, managedPath, resolved.sourcePath)
	require.Equal(t, "managed", resolved.content)
}

func TestResolveActivatedSkillClaudeManagedStatErrorIsNameOnly(t *testing.T) {
	managedRoot := filepath.Join(t.TempDir(), "managed", "skills")
	original := claudeManagedSkillsRoot
	claudeManagedSkillsRoot = func() string { return managedRoot }
	t.Cleanup(func() { claudeManagedSkillsRoot = original })
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	managedSkill := filepath.Join(managedRoot, "indeterminate")
	require.NoError(t, os.MkdirAll(managedSkill, 0o755))
	require.NoError(t, os.Symlink("SKILL.md", filepath.Join(managedSkill, "SKILL.md")))
	writeSkillManifest(t, filepath.Join(home, ".claude", "skills", "indeterminate"), []byte("personal"))

	resolved := resolveActivatedSkill(claudeSkillEvent(t.TempDir(), "indeterminate"), activatedSkillPayload("indeterminate"))

	require.Equal(t, "indeterminate", resolved.name)
	require.Empty(t, resolved.sourceLevel)
	require.Empty(t, resolved.sourcePath)
	require.False(t, resolved.captureReady)
}

func TestResolveActivatedSkillClaudeFindsProjectAtGitRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "nested", "deeper")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))
	path := writeSkillManifest(t, filepath.Join(repo, ".claude", "skills", "project-skill"), []byte("project"))

	resolved := resolveActivatedSkill(claudeSkillEvent(cwd, "project-skill"), activatedSkillPayload("project-skill"))

	require.Equal(t, "project", resolved.sourceLevel)
	require.Equal(t, path, resolved.sourcePath)
}

func TestResolveActivatedSkillClaudeUsesConfigDirectory(t *testing.T) {
	home := t.TempDir()
	configRoot := filepath.Join(t.TempDir(), "custom-claude")
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
	path := writeSkillManifest(t, filepath.Join(configRoot, "skills", "configured"), []byte("configured"))

	resolved := resolveActivatedSkill(claudeSkillEvent(t.TempDir(), "configured"), activatedSkillPayload("configured"))

	require.Equal(t, "personal", resolved.sourceLevel)
	require.Equal(t, path, resolved.sourcePath)
}

func TestResolveActivatedSkillClaudeUsesApplicablePluginRecord(t *testing.T) {
	home := t.TempDir()
	configRoot := filepath.Join(t.TempDir(), "claude")
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "nested")
	projectInstall := filepath.Join(t.TempDir(), "project-plugin")
	userInstall := filepath.Join(t.TempDir(), "user-plugin")
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
	path := writeSkillManifest(t, filepath.Join(projectInstall, "skills", "review"), []byte("project plugin"))
	writeSkillManifest(t, filepath.Join(userInstall, "skills", "review"), []byte("user plugin"))
	registry := map[string]any{
		"version": 2,
		"plugins": map[string]any{
			"quality@marketplace": []map[string]string{
				{"scope": "user", "installPath": userInstall},
				{"scope": "project", "projectPath": repo, "installPath": projectInstall},
			},
		},
	}
	writeJSONFile(t, filepath.Join(configRoot, "plugins", "installed_plugins.json"), registry)

	resolved := resolveActivatedSkill(claudeSkillEvent(cwd, "quality:review"), activatedSkillPayload("quality:review"))

	require.Equal(t, "quality:review", resolved.name)
	require.Equal(t, "plugin", resolved.sourceLevel)
	require.Equal(t, path, resolved.sourcePath)
	require.Equal(t, "project plugin", resolved.content)
}

func TestResolveActivatedSkillCodexAbsoluteReadPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	path := writeSkillManifest(t, filepath.Join(repo, ".agents", "skills", "absolute"), []byte("absolute"))
	event := codexToolEvent(t, repo, "Read", map[string]string{"file_path": path})

	resolved := resolveActivatedSkill(event, activatedSkillPayload("absolute"))

	require.Equal(t, "project", resolved.sourceLevel)
	require.Equal(t, path, resolved.sourcePath)
	require.Equal(t, "absolute", resolved.content)
}

func TestResolveActivatedSkillCodexRelativeShellPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	relative := filepath.Join(".agents", "skills", "relative", "SKILL.md")
	path := writeSkillManifest(t, filepath.Dir(filepath.Join(repo, relative)), []byte("relative"))
	event := codexToolEvent(t, repo, "Bash", map[string]string{"command": "cat " + relative})

	resolved := resolveActivatedSkill(event, activatedSkillPayload("relative"))

	require.Equal(t, "project", resolved.sourceLevel)
	require.Equal(t, path, resolved.sourcePath)
	require.Equal(t, "relative", resolved.content)
}

func TestResolveActivatedSkillCodexPromptReturnsWinningRootPath(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(t.TempDir(), "codex")
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", codexHome)
	path := writeSkillManifest(t, filepath.Join(codexHome, "skills", ".system", "bundled"), []byte("bundled"))
	event := &agenthooks.PromptEvent{
		Event:  agenthooks.Event{Provider: agenthooks.ProviderCodex, Kind: agenthooks.KindPromptSubmitted, Session: agenthooks.SessionInfo{CWD: t.TempDir()}},
		Prompt: "use $bundled",
	}

	resolved := resolveActivatedSkill(event, activatedSkillPayload("bundled"))

	require.Equal(t, "system", resolved.sourceLevel)
	require.Equal(t, path, resolved.sourcePath)
	require.Equal(t, "bundled", resolved.content)
}

func TestResolveActivatedSkillCursorProjectCurrentKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	path := writeSkillManifest(t, filepath.Join(workspace, ".cursor", "skills", "current"), []byte("current"))
	event := cursorReadEvent(t, workspace, []string{workspace}, map[string]string{"file_path": path})

	resolved := resolveActivatedSkill(event, activatedSkillPayload("current"))

	require.Equal(t, "project", resolved.sourceLevel)
	require.Equal(t, path, resolved.sourcePath)
}

func TestResolveActivatedSkillRejectsSymlinkedManifestParent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	externalDir := filepath.Join(t.TempDir(), "external")
	externalPath := writeSkillManifest(t, externalDir, []byte("private content"))
	root := filepath.Join(workspace, ".cursor", "skills")
	require.NoError(t, os.MkdirAll(root, 0o755))
	linkedDir := filepath.Join(root, "linked")
	require.NoError(t, os.Symlink(externalDir, linkedDir))
	event := cursorReadEvent(t, workspace, []string{workspace}, map[string]string{"file_path": filepath.Join(linkedDir, "SKILL.md")})

	resolved := resolveActivatedSkill(event, activatedSkillPayload("linked"))

	require.Equal(t, "linked", resolved.name)
	require.Empty(t, resolved.sourceLevel)
	require.Empty(t, resolved.sourcePath)
	require.Empty(t, resolved.content)
	require.False(t, resolved.captureReady)
	require.FileExists(t, externalPath)
}

func TestResolveActivatedSkillRejectsExternalManifestSymlink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	external := filepath.Join(t.TempDir(), "passwd")
	require.NoError(t, os.WriteFile(external, []byte("sensitive"), 0o600))
	path := filepath.Join(workspace, ".cursor", "skills", "linked", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.Symlink(external, path))
	event := cursorReadEvent(t, workspace, []string{workspace}, map[string]string{"file_path": path})

	resolved := resolveActivatedSkill(event, activatedSkillPayload("linked"))

	require.Equal(t, "linked", resolved.name)
	require.Empty(t, resolved.sourceLevel)
	require.Empty(t, resolved.sourcePath)
	require.Empty(t, resolved.rawSHA256)
	require.Empty(t, resolved.content)
	require.False(t, resolved.captureReady)
}

func TestResolveActivatedSkillCursorPluginLegacyKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	plugin := filepath.Join(t.TempDir(), "plugin")
	path := writeSkillManifest(t, filepath.Join(plugin, "skills", "legacy"), []byte("plugin"))
	require.NoError(t, os.MkdirAll(filepath.Join(plugin, ".cursor-plugin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(plugin, ".cursor-plugin", "plugin.json"), []byte(`{"name":"plugin"}`), 0o644))
	event := cursorReadEvent(t, workspace, []string{workspace}, map[string]string{"path": path})

	resolved := resolveActivatedSkill(event, activatedSkillPayload("legacy"))

	require.Equal(t, "plugin", resolved.sourceLevel)
	require.Equal(t, path, resolved.sourcePath)
}

func TestResolveActivatedSkillCursorPluginInsideWorkspace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	plugin := filepath.Join(workspace, "plugins", "quality")
	path := writeSkillManifest(t, filepath.Join(plugin, "skills", "review"), []byte("plugin"))
	writeJSONFile(t, filepath.Join(plugin, ".cursor-plugin", "plugin.json"), map[string]string{"name": "quality"})
	event := cursorReadEvent(t, workspace, []string{workspace}, map[string]string{"file_path": path})

	resolved := resolveActivatedSkill(event, activatedSkillPayload("review"))

	require.Equal(t, "plugin", resolved.sourceLevel)
	require.Equal(t, path, resolved.sourcePath)
}

func TestContainingWorkspaceRootChoosesLongestNestedRoot(t *testing.T) {
	outer := t.TempDir()
	inner := filepath.Join(outer, "nested", "workspace")
	path := filepath.Join(inner, ".cursor", "skills", "nested", "SKILL.md")

	require.Equal(t, inner, containingWorkspaceRoot(path, []string{outer, inner}))
}

func TestResolveActivatedSkillCursorDocsSkillsIsNameOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	path := writeSkillManifest(t, filepath.Join(workspace, "docs", "skills", "example"), []byte("documentation"))
	event := cursorReadEvent(t, workspace, []string{workspace}, map[string]string{"file_path": path})

	resolved := resolveActivatedSkill(event, activatedSkillPayload("example"))

	require.Equal(t, "example", resolved.name)
	require.Empty(t, resolved.sourceLevel)
	require.Empty(t, resolved.sourcePath)
	require.False(t, resolved.captureReady)
}

func TestResolveActivatedSkillCursorUnknownExternalPathIsNameOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	external := t.TempDir()
	path := writeSkillManifest(t, filepath.Join(external, "skills", "unknown"), []byte("external"))
	event := cursorReadEvent(t, workspace, []string{workspace}, map[string]string{"file_path": path})

	resolved := resolveActivatedSkill(event, activatedSkillPayload("unknown"))

	require.Equal(t, "unknown", resolved.name)
	require.Empty(t, resolved.sourceLevel)
	require.Empty(t, resolved.sourcePath)
	require.Empty(t, resolved.rawSHA256)
	require.False(t, resolved.captureReady)
}

func TestResolveActivatedSkillCodexUnknownExternalPathIsNameOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	repo := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))
	external := t.TempDir()
	path := writeSkillManifest(t, filepath.Join(external, "skills", "unknown"), []byte("external"))
	event := codexToolEvent(t, repo, "Read", map[string]string{"file_path": path})

	resolved := resolveActivatedSkill(event, activatedSkillPayload("unknown"))

	require.Equal(t, "unknown", resolved.name)
	require.Empty(t, resolved.sourceLevel)
	require.Empty(t, resolved.sourcePath)
	require.Empty(t, resolved.rawSHA256)
	require.False(t, resolved.captureReady)
}

func TestResolveActivatedSkillCodexPromptDuplicateCandidatesIsNameOnly(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(t.TempDir(), "codex")
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", codexHome)
	writeSkillManifest(t, filepath.Join(home, ".agents", "skills", "duplicate"), []byte("agents"))
	writeSkillManifest(t, filepath.Join(codexHome, "skills", "duplicate"), []byte("codex"))
	event := &agenthooks.PromptEvent{
		Event:  agenthooks.Event{Provider: agenthooks.ProviderCodex, Kind: agenthooks.KindPromptSubmitted, Session: agenthooks.SessionInfo{CWD: t.TempDir()}},
		Prompt: "use $duplicate",
	}
	payload := buildEnvelope(event, "host")

	resolved := resolveActivatedSkill(event, &payload)

	require.NotNil(t, payload.Data.Skill)
	require.Equal(t, "duplicate", payload.Data.Skill.Name)
	require.Equal(t, "duplicate", resolved.name)
	require.Empty(t, resolved.sourceLevel)
	require.Empty(t, resolved.sourcePath)
	require.False(t, resolved.captureReady)
}

func TestCodexProjectSearchStopsAfterGitRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	outer := t.TempDir()
	repo := filepath.Join(outer, "repo")
	cwd := filepath.Join(repo, "nested")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))
	writeSkillManifest(t, filepath.Join(outer, ".agents", "skills", "outside"), []byte("outside"))

	require.False(t, codexSkillExists("outside", cwd))
	require.Empty(t, codexPromptSkillName("use $outside", cwd))
}

func TestResolveActivatedSkillRejectsNonRegularManifest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	path := filepath.Join(workspace, ".cursor", "skills", "directory", "SKILL.md")
	require.NoError(t, os.MkdirAll(path, 0o755))
	event := cursorReadEvent(t, workspace, []string{workspace}, map[string]string{"file_path": path})

	resolved := resolveActivatedSkill(event, activatedSkillPayload("directory"))

	require.Equal(t, "directory", resolved.name)
	require.Empty(t, resolved.sourceLevel)
	require.Empty(t, resolved.sourcePath)
	require.False(t, resolved.captureReady)
}

func TestResolveActivatedSkillMissingManifestDegradesToNameOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	path := filepath.Join(workspace, ".cursor", "skills", "missing", "SKILL.md")
	event := cursorReadEvent(t, workspace, []string{workspace}, map[string]string{"file_path": path})

	resolved := resolveActivatedSkill(event, activatedSkillPayload("missing"))

	require.Equal(t, "missing", resolved.name)
	require.Empty(t, resolved.sourceLevel)
	require.Empty(t, resolved.sourcePath)
	require.Empty(t, resolved.rawSHA256)
	require.Empty(t, resolved.content)
	require.False(t, resolved.captureReady)
}

func TestResolveActivatedSkillWithoutSkillReturnsNil(t *testing.T) {
	require.Nil(t, resolveActivatedSkill(nil, nil))
	require.Nil(t, resolveActivatedSkill(nil, &components.IngestRequestBody{}))
	require.Nil(t, resolveActivatedSkill(nil, &components.IngestRequestBody{Data: &components.HookIngestData{}}))
}

func activatedSkillPayload(name string) *components.IngestRequestBody {
	return &components.IngestRequestBody{
		Data: &components.HookIngestData{
			Skill: &components.HookSkillData{Name: name},
		},
	}
}

func claudeSkillEvent(cwd, name string) *agenthooks.ToolPreEvent {
	input, _ := json.Marshal(map[string]string{"skill": name})
	return &agenthooks.ToolPreEvent{
		Event: agenthooks.Event{Provider: agenthooks.ProviderClaudeCode, Kind: agenthooks.KindToolPre, Session: agenthooks.SessionInfo{CWD: cwd}},
		Tool:  agenthooks.ToolCall{Name: "Skill", Input: input},
	}
}

func codexToolEvent(t *testing.T, cwd, name string, input any) *agenthooks.ToolPreEvent {
	t.Helper()
	encoded, err := json.Marshal(input)
	require.NoError(t, err)
	return &agenthooks.ToolPreEvent{
		Event: agenthooks.Event{Provider: agenthooks.ProviderCodex, Kind: agenthooks.KindToolPre, Session: agenthooks.SessionInfo{CWD: cwd}},
		Tool:  agenthooks.ToolCall{Name: name, Input: encoded},
	}
}

func cursorReadEvent(t *testing.T, cwd string, workspaceRoots []string, input any) *agenthooks.ToolPreEvent {
	t.Helper()
	encoded, err := json.Marshal(input)
	require.NoError(t, err)
	return &agenthooks.ToolPreEvent{
		Event: agenthooks.Event{Provider: agenthooks.ProviderCursor, Kind: agenthooks.KindToolPre, Session: agenthooks.SessionInfo{CWD: cwd, WorkspaceRoots: workspaceRoots}},
		Tool:  agenthooks.ToolCall{Name: "Read", Input: encoded},
	}
}

func writeSkillManifest(t *testing.T, dir string, content []byte) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "SKILL.md")
	require.NoError(t, os.WriteFile(path, content, 0o644))
	return path
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, data, 0o644))
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
