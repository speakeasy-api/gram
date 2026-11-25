package relay

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

const treeTestManifest = "---\nname: tree-skill\ndescription: uses scripts\n---\n\nRun scripts/run.sh.\n"

func writeTreeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
}

// isolateSkillRoots points every skill root the relay checks at empty temp
// directories so tests never read the developer's own skills.
func isolateSkillRoots(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	original := claudeManagedSkillsRoot
	t.Cleanup(func() { claudeManagedSkillsRoot = original })
	claudeManagedSkillsRoot = func() string { return "" }
}

func claudeSkillEvent(t *testing.T, cwd, name, content string) *agenthooks.ToolPostEvent {
	t.Helper()
	output, err := json.Marshal(content)
	require.NoError(t, err)
	input, err := json.Marshal(map[string]string{"skill": name})
	require.NoError(t, err)
	return &agenthooks.ToolPostEvent{
		Event:  agenthooks.Event{Provider: agenthooks.ProviderClaudeCode, Kind: agenthooks.KindToolPost, Session: agenthooks.SessionInfo{CWD: cwd}},
		Tool:   agenthooks.ToolCall{Name: "Skill", Input: input},
		Output: output,
		Failed: false,
	}
}

func treePaths(tree *skillTree) []string {
	paths := make([]string, 0, len(tree.files))
	for _, file := range tree.files {
		paths = append(paths, file.Path)
	}
	return paths
}

func TestReadSkillTreeCollectsSupportingFilesAndSkipsTheRest(t *testing.T) {
	dir := t.TempDir()
	writeTreeFile(t, dir, "SKILL.md", treeTestManifest)
	writeTreeFile(t, dir, "scripts/run.sh", "echo run\n")
	writeTreeFile(t, dir, "references/REFERENCE.md", "# Reference\n")
	writeTreeFile(t, dir, ".env", "TOKEN=secret\n")
	writeTreeFile(t, dir, ".git/config", "[core]\n")
	writeTreeFile(t, dir, "node_modules/dep/index.js", "module.exports = 1\n")
	writeTreeFile(t, dir, "__pycache__/run.cpython-312.pyc", "bytecode")
	big := filepath.Join(dir, "assets", "huge.bin")
	require.NoError(t, os.MkdirAll(filepath.Dir(big), 0o755))
	require.NoError(t, os.WriteFile(big, nil, 0o600))
	require.NoError(t, os.Truncate(big, maxSkillTreeFileBytes+1))

	outside := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "escape.txt")))
	require.NoError(t, os.Symlink("scripts/run.sh", filepath.Join(dir, "run-link.sh")))
	require.NoError(t, os.Symlink(filepath.Join(dir, "scripts"), filepath.Join(dir, "scripts-link")))

	tree, ok := readSkillTree(dir, sha256Hex([]byte(treeTestManifest)))
	require.True(t, ok)
	require.Equal(t, []string{"references/REFERENCE.md", "run-link.sh", "scripts/run.sh"}, treePaths(tree))
	require.Equal(t, "echo run\n", string(tree.files[1].Content), "a symlink inside the root uploads its target's bytes")
	require.True(t, validSkillTreeFiles(tree.files))
}

func TestReadSkillTreeRequiresManifestMatchingActivation(t *testing.T) {
	dir := t.TempDir()
	writeTreeFile(t, dir, "SKILL.md", treeTestManifest)

	_, ok := readSkillTree(dir, sha256Hex([]byte(treeTestManifest+"edited")))
	require.False(t, ok)
}

func TestReadSkillTreeStopsAtFileCap(t *testing.T) {
	dir := t.TempDir()
	writeTreeFile(t, dir, "SKILL.md", treeTestManifest)
	for i := range maxSkillTreeFiles + 5 {
		writeTreeFile(t, dir, filepath.Join("refs", string(rune('a'+i/26))+string(rune('a'+i%26))+".md"), "x\n")
	}

	tree, ok := readSkillTree(dir, sha256Hex([]byte(treeTestManifest)))
	require.True(t, ok)
	require.Len(t, tree.files, maxSkillTreeFiles)
}

// TestSkillTreeDigestVector pins the digest to the server's skilltree
// digest; TestDeviceTreeDigest_MatchesRelayVector asserts the same values.
func TestSkillTreeDigestVector(t *testing.T) {
	manifest := sha256.Sum256([]byte("---\nname: vector\ndescription: v\n---\n"))
	files := []skillTreeFile{}
	for path, content := range map[string]string{
		"scripts/run.sh":          "echo run\n",
		"references/REFERENCE.md": "# Reference\n",
		"scripts-extra/a.txt":     "a\n",
	} {
		files = append(files, skillTreeFile{Path: path, Content: []byte(content), SHA256: sha256Hex([]byte(content))})
	}

	require.Equal(t, "4a9dad5cfb52ac40d1aa641d9db4d0bbc2b9a3ede59bfdd91cc45a3b44836b7c", skillTreeDigest(manifest, files))
	require.Equal(t, "6a33ebf8ebb26e85ee3593d5cdf432355a3d036c052a9c5f4313471f98deb5b9", skillTreeDigest(manifest, nil))
}

func TestLocateSkillTreeFindsProjectSkill(t *testing.T) {
	isolateSkillRoots(t)
	project := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(project, ".git"), 0o755))
	skillDir := filepath.Join(project, ".claude", "skills", "tree-skill")
	writeTreeFile(t, skillDir, "SKILL.md", treeTestManifest)
	writeTreeFile(t, skillDir, "scripts/run.sh", "echo run\n")
	cwd := filepath.Join(project, "src", "pkg")
	require.NoError(t, os.MkdirAll(cwd, 0o755))

	tree := locateSkillTree(claudeSkillEvent(t, cwd, "tree-skill", treeTestManifest), "tree-skill", sha256Hex([]byte(treeTestManifest)))
	require.NotNil(t, tree)
	require.Equal(t, []string{"scripts/run.sh"}, treePaths(tree))
}

func TestLocateSkillTreeSkipsSameNamedSkillWithDifferentManifest(t *testing.T) {
	isolateSkillRoots(t)
	project := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(project, ".git"), 0o755))
	writeTreeFile(t, filepath.Join(project, ".claude", "skills", "tree-skill"), "SKILL.md", "---\nname: tree-skill\n---\nother\n")
	personal := filepath.Join(os.Getenv("HOME"), ".agents", "skills", "tree-skill")
	writeTreeFile(t, personal, "SKILL.md", treeTestManifest)
	writeTreeFile(t, personal, "references/a.md", "a\n")

	tree := locateSkillTree(claudeSkillEvent(t, project, "tree-skill", treeTestManifest), "tree-skill", sha256Hex([]byte(treeTestManifest)))
	require.NotNil(t, tree)
	require.Equal(t, []string{"references/a.md"}, treePaths(tree))
}

func TestLocateSkillTreeUsesReadToolManifestPath(t *testing.T) {
	isolateSkillRoots(t)
	skillDir := filepath.Join(t.TempDir(), "anywhere", "skills", "tree-skill")
	writeTreeFile(t, skillDir, "SKILL.md", treeTestManifest)
	writeTreeFile(t, skillDir, "scripts/run.sh", "echo run\n")
	input, err := json.Marshal(map[string]string{"command": "cat " + filepath.Join(skillDir, "SKILL.md")})
	require.NoError(t, err)
	event := &agenthooks.ToolPostEvent{
		Event:  agenthooks.Event{Provider: agenthooks.ProviderCodex, Kind: agenthooks.KindToolPost, Session: agenthooks.SessionInfo{CWD: t.TempDir()}},
		Tool:   agenthooks.ToolCall{Name: "exec", Canonical: agenthooks.ToolShell, Input: input},
		Output: nil,
		Failed: false,
	}

	tree := locateSkillTree(event, "tree-skill", sha256Hex([]byte(treeTestManifest)))
	require.NotNil(t, tree)
	require.Equal(t, []string{"scripts/run.sh"}, treePaths(tree))
}

func TestRelaySendsTreeDigestAndRequestsTreeUpload(t *testing.T) {
	isolateSkillRoots(t)
	project := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(project, ".git"), 0o755))
	skillDir := filepath.Join(project, ".claude", "skills", "tree-skill")
	writeTreeFile(t, skillDir, "SKILL.md", treeTestManifest)
	writeTreeFile(t, skillDir, "scripts/run.sh", "echo run\n")

	capturedTasks := captureSkillUploadTasks(t)
	fs := newFakeServer(t, func(_ components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
	})
	fs.effects = requestedSkillCaptureEffects(true)
	_, _ = NewRelay(authedConfig(t, fs.URL)).deliver(t.Context(), claudeSkillEvent(t, project, "tree-skill", treeTestManifest))

	tree, ok := readSkillTree(skillDir, sha256Hex([]byte(treeTestManifest)))
	require.True(t, ok)
	payload := fs.last()
	require.NotNil(t, payload.Data.Skill.TreeSha256)
	require.Equal(t, tree.digest, *payload.Data.Skill.TreeSha256)
	tasks := capturedTasks()
	require.Len(t, tasks, 1)
	require.True(t, tasks[0].SendTree)
	require.Equal(t, tree.files, tasks[0].Files)
}

type recordedUpload struct {
	schemaVersion string
	files         []skillContentV2File
	encoding      string
}

func skillUploadServer(t *testing.T, v2Status int) (*httptest.Server, func() []recordedUpload) {
	t.Helper()
	return skillUploadServerSequence(t, []int{v2Status})
}

// skillUploadServerSequence answers successive v2 uploads with v2Statuses in
// order, repeating the last status once the sequence is spent.
func skillUploadServerSequence(t *testing.T, v2Statuses []int) (*httptest.Server, func() []recordedUpload) {
	t.Helper()
	var v2Calls int
	var mu sync.Mutex
	var uploads []recordedUpload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, uploadSkillContentPath, r.URL.Path)
		require.Equal(t, "test-hooks-key", r.Header.Get("Gram-Key"))
		var reader io.Reader = r.Body
		if r.Header.Get("Content-Encoding") == "gzip" {
			gz, err := gzip.NewReader(r.Body)
			require.NoError(t, err)
			reader = gz
		}
		var body skillContentV2Body
		require.NoError(t, json.NewDecoder(reader).Decode(&body))
		mu.Lock()
		uploads = append(uploads, recordedUpload{schemaVersion: body.SchemaVersion, files: body.Files, encoding: r.Header.Get("Content-Encoding")})
		v2Status := v2Statuses[min(v2Calls, len(v2Statuses)-1)]
		if body.SchemaVersion == skillContentSchemaV2 {
			v2Calls++
		}
		mu.Unlock()
		if body.SchemaVersion == skillContentSchemaV2 {
			w.WriteHeader(v2Status)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	return server, func() []recordedUpload {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedUpload(nil), uploads...)
	}
}

func treeUploadTask(serverURL string) skillUploadTask {
	return skillUploadTask{
		ServerURL: serverURL, Project: "default", APIKey: "test-hooks-key",
		RawSHA256: sha256Hex([]byte(treeTestManifest)), Content: treeTestManifest, SendTree: true,
		Files: []skillTreeFile{{Path: "scripts/run.sh", Content: []byte("echo run\n"), SHA256: sha256Hex([]byte("echo run\n"))}},
	}
}

func TestExecuteSkillUploadSendsTreeWithV2(t *testing.T) {
	server, uploads := skillUploadServer(t, http.StatusNoContent)

	require.NoError(t, executeSkillUpload(t.Context(), treeUploadTask(server.URL)))
	got := uploads()
	require.Len(t, got, 1)
	require.Equal(t, skillContentSchemaV2, got[0].schemaVersion)
	require.Equal(t, "gzip", got[0].encoding)
	require.Equal(t, []skillContentV2File{{Path: "scripts/run.sh", Content: []byte("echo run\n"), SHA256: sha256Hex([]byte("echo run\n"))}}, got[0].files)
}

func TestExecuteSkillUploadFallsBackToV1WhenServerRejectsV2(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusRequestEntityTooLarge} {
		server, uploads := skillUploadServer(t, status)

		require.NoError(t, executeSkillUpload(t.Context(), treeUploadTask(server.URL)))
		got := uploads()
		require.Len(t, got, 2, "status %d", status)
		require.Equal(t, skillContentSchemaV2, got[0].schemaVersion)
		require.Equal(t, "hook.skill-content.v1", got[1].schemaVersion)
		require.Empty(t, got[1].files)
	}
}

func TestExecuteSkillUploadDoesNotFallBackOnServerError(t *testing.T) {
	server, uploads := skillUploadServer(t, http.StatusForbidden)

	require.Error(t, executeSkillUpload(t.Context(), treeUploadTask(server.URL)))
	require.Len(t, uploads(), 1)
}

// A transient 429 or 5xx on the v2 upload is retried, as the SDK retries the
// v1 upload, instead of losing the capture. (cubic)
func TestExecuteSkillUploadRetriesTransientV2Status(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		server, uploads := skillUploadServerSequence(t, []int{status, http.StatusNoContent})

		require.NoError(t, executeSkillUpload(t.Context(), treeUploadTask(server.URL)), "status %d", status)
		got := uploads()
		require.Len(t, got, 2, "status %d", status)
		require.Equal(t, skillContentSchemaV2, got[0].schemaVersion)
		require.Equal(t, skillContentSchemaV2, got[1].schemaVersion)
	}
}

func TestExecuteSkillUploadStopsRetryingPersistentServerError(t *testing.T) {
	server, uploads := skillUploadServer(t, http.StatusInternalServerError)

	require.Error(t, executeSkillUpload(t.Context(), treeUploadTask(server.URL)))
	got := uploads()
	require.Len(t, got, sendAttempts)
	for _, upload := range got {
		require.Equal(t, skillContentSchemaV2, upload.schemaVersion)
	}
}
