package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

type skillSyncServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []components.SyncSkillsRequestBody
	headers  []http.Header
	respond  func(int, components.SyncSkillsRequestBody) (int, components.SyncSkillsResult)
}

func newSkillSyncServer(t *testing.T, respond func(int, components.SyncSkillsRequestBody) (int, components.SyncSkillsResult)) *skillSyncServer {
	t.Helper()
	server := &skillSyncServer{Server: nil, mu: sync.Mutex{}, requests: nil, headers: nil, respond: respond}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body components.SyncSkillsRequestBody
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		server.mu.Lock()
		index := len(server.requests)
		server.requests = append(server.requests, body)
		server.headers = append(server.headers, req.Header.Clone())
		server.mu.Unlock()
		status, result := http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{}}
		if respond != nil {
			status, result = respond(index, body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusOK {
			require.NoError(t, json.NewEncoder(w).Encode(result))
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"fault": false, "id": "test", "message": "rejected", "name": "rejected", "temporary": false, "timeout": false,
		}))
	}))
	t.Cleanup(server.Close)
	return server
}

func (s *skillSyncServer) captured() ([]components.SyncSkillsRequestBody, []http.Header) {
	s.mu.Lock()
	defer s.mu.Unlock()
	requests := append([]components.SyncSkillsRequestBody(nil), s.requests...)
	headers := append([]http.Header(nil), s.headers...)
	return requests, headers
}

func skillUpdate(name, content, description string) components.SyncSkillUpdate {
	return components.SyncSkillUpdate{Name: name, Content: content, Description: new(description), RawSha256: rawSkillHash([]byte(content))}
}

func configuredSkillRelay(t *testing.T, serverURL string) (*Relay, string) {
	t.Helper()
	configDir := filepath.Join(t.TempDir(), "claude")
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "personal-key")
	cfg := Config{ServerURL: serverURL, ProjectSlug: "project", OrgID: "org", HooksAPIKey: "baked-org-key", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}
	return NewRelay(cfg), filepath.Join(configDir, "skills")
}

func TestSkillSyncLifecycleAndReceiptFollowup(t *testing.T) {
	phase := "install"
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		switch phase {
		case "install":
			return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("release-notes", "first", "Draft release notes")}}
		case "update":
			return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("release-notes", "second", "Draft release notes")}}
		default:
			return http.StatusOK, components.SyncSkillsResult{Removals: []string{"release-notes"}, Updates: []components.SyncSkillUpdate{}}
		}
	})
	relay, root := configuredSkillRelay(t, server.URL)

	contextNote := relay.syncSkills(t.Context(), false)
	require.Contains(t, contextNote, "release-notes")
	require.Contains(t, contextNote, "Read its SKILL.md")
	require.Contains(t, contextNote, filepath.Join(root, "release-notes", "SKILL.md"))
	body, err := os.ReadFile(filepath.Join(root, "release-notes", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "first", string(body))

	phase = "update"
	require.Empty(t, relay.syncSkills(t.Context(), false))
	body, err = os.ReadFile(filepath.Join(root, "release-notes", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "second", string(body))

	phase = "remove"
	require.Empty(t, relay.syncSkills(t.Context(), false))
	_, err = os.Lstat(filepath.Join(root, "release-notes"))
	require.ErrorIs(t, err, os.ErrNotExist)

	requests, headers := server.captured()
	require.Len(t, requests, 6)
	require.Empty(t, requests[0].Installed)
	require.Equal(t, rawSkillHash([]byte("first")), requests[1].Installed[0].RawSha256)
	require.Equal(t, rawSkillHash([]byte("first")), requests[2].Installed[0].RawSha256)
	require.Equal(t, rawSkillHash([]byte("second")), requests[3].Installed[0].RawSha256)
	require.Equal(t, rawSkillHash([]byte("second")), requests[4].Installed[0].RawSha256)
	require.Empty(t, requests[5].Installed)
	require.Equal(t, "personal-key", headers[0].Get("Gram-Key"))
	require.Equal(t, "project", headers[0].Get("Gram-Project"))
	require.Len(t, headers[0].Values("Gram-Key"), 1)
	require.Len(t, headers[0].Values("Gram-Project"), 1)
	require.NotEmpty(t, headers[0].Get("X-Gram-Hook-Hostname"))
	require.NotEmpty(t, headers[0].Get("Idempotency-Key"))
}

func TestSkillSyncPreservesUnownedConflict(t *testing.T) {
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("local-skill", "managed", "")}}
	})
	relay, root := configuredSkillRelay(t, server.URL)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "local-skill"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "local-skill", "SKILL.md"), []byte("user-owned"), 0o644))

	require.Empty(t, relay.syncSkills(t.Context(), false))
	body, err := os.ReadFile(filepath.Join(root, "local-skill", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "user-owned", string(body))
	requests, _ := server.captured()
	require.Len(t, requests, 2)
	require.Equal(t, components.StatusConflictSkipped, requests[1].Exceptions[0].Status)
}

func TestSkillSyncDestinationAppearingDuringRequestIsConflict(t *testing.T) {
	var root string
	server := newSkillSyncServer(t, func(index int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		if index == 0 {
			require.NoError(t, os.Mkdir(filepath.Join(root, "appeared"), 0o755))
		}
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("appeared", "managed", "")}}
	})
	relay, configuredRoot := configuredSkillRelay(t, server.URL)
	root = configuredRoot

	require.Empty(t, relay.syncSkills(t.Context(), false))
	info, err := os.Lstat(filepath.Join(root, "appeared"))
	require.NoError(t, err)
	require.True(t, info.IsDir())
	entries, err := os.ReadDir(filepath.Join(root, "appeared"))
	require.NoError(t, err)
	require.Empty(t, entries)
	requests, _ := server.captured()
	require.Len(t, requests, 2)
	require.Equal(t, components.StatusConflictSkipped, requests[1].Exceptions[0].Status)
}

func TestRenameDirNoReplacePreservesEmptyDestination(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "staged")
	newPath := filepath.Join(root, "destination")
	require.NoError(t, os.Mkdir(oldPath, 0o755))
	require.NoError(t, os.Mkdir(newPath, 0o755))

	require.Error(t, renameDirNoReplace(oldPath, newPath))
	oldInfo, err := os.Lstat(oldPath)
	require.NoError(t, err)
	require.True(t, oldInfo.IsDir())
	newInfo, err := os.Lstat(newPath)
	require.NoError(t, err)
	require.True(t, newInfo.IsDir())
}

func TestSkillSyncPreservesOtherDeploymentOwnership(t *testing.T) {
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("shared-name", "second-deployment", "")}}
	})
	relay, root := configuredSkillRelay(t, server.URL)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "shared-name"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "shared-name", "SKILL.md"), []byte("first-deployment"), 0o644))
	other := skillDeployment{ServerURL: "https://other.example.test", Org: "other-org", Project: "other-project"}
	manifest := skillManifest{Version: skillManifestVersion, Entries: []managedSkill{{
		Deployment:    other,
		Name:          "shared-name",
		Files:         []string{filepath.Join("shared-name", "SKILL.md")},
		RawSHA256:     rawSkillHash([]byte("first-deployment")),
		PendingUpdate: nil,
		PendingRemove: nil,
	}}, PendingInstalls: []pendingSkillInstall{}, Tombstones: []skillRemovalTombstone{}, Exceptions: []managedSkillException{}}
	require.NoError(t, writeSkillManifest(filepath.Join(root, skillManifestFilename), manifest))

	require.Empty(t, relay.syncSkills(t.Context(), false))
	body, err := os.ReadFile(filepath.Join(root, "shared-name", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "first-deployment", string(body))
	stored, err := readSkillManifest(filepath.Join(root, skillManifestFilename))
	require.NoError(t, err)
	require.Len(t, stored.Entries, 1)
	require.Equal(t, other, stored.Entries[0].Deployment)
	requests, _ := server.captured()
	require.Equal(t, components.StatusConflictSkipped, requests[1].Exceptions[0].Status)
}

func TestSkillSyncModifiedManagedSkillBecomesPermanentConflict(t *testing.T) {
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("managed", "server-copy", "")}}
	})
	relay, root := configuredSkillRelay(t, server.URL)
	require.NotEmpty(t, relay.syncSkills(t.Context(), false))
	path := filepath.Join(root, "managed", "SKILL.md")
	require.NoError(t, os.WriteFile(path, []byte("user-edit"), 0o644))

	require.Empty(t, relay.syncSkills(t.Context(), false))
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "user-edit", string(body))
	manifest, err := readSkillManifest(filepath.Join(root, skillManifestFilename))
	require.NoError(t, err)
	require.Empty(t, manifest.Entries)
	require.True(t, manifest.Exceptions[0].Permanent)
	requests, _ := server.captured()
	require.Len(t, requests, 3)
	require.NoError(t, os.RemoveAll(filepath.Join(root, "managed")))

	contextNote := relay.syncSkills(t.Context(), false)
	require.Contains(t, contextNote, filepath.Join(root, "managed", "SKILL.md"))
	body, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "server-copy", string(body))
}

func TestSkillSyncOwnedPathChangingDuringRequestIsConflict(t *testing.T) {
	phase := "install"
	var root string
	changedPath := false
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		content := "old"
		if phase == "race" {
			content = "new"
			if !changedPath {
				changedPath = true
				path := filepath.Join(root, "raced-update", "SKILL.md")
				require.NoError(t, os.Remove(path))
				require.NoError(t, os.Mkdir(path, 0o755))
			}
		}
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("raced-update", content, "")}}
	})
	relay, configuredRoot := configuredSkillRelay(t, server.URL)
	root = configuredRoot
	require.NotEmpty(t, relay.syncSkills(t.Context(), false))
	phase = "race"

	require.Empty(t, relay.syncSkills(t.Context(), false))
	info, err := os.Lstat(filepath.Join(root, "raced-update", "SKILL.md"))
	require.NoError(t, err)
	require.True(t, info.IsDir())
	requests, _ := server.captured()
	require.Len(t, requests, 4)
	require.Equal(t, components.StatusConflictSkipped, requests[3].Exceptions[0].Status)
}

func TestSkillSyncRejectsSymlinkAndUnsafeNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires elevated privileges on Windows")
	}
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{
			skillUpdate("linked", "managed", ""), skillUpdate("../escape", "unsafe", ""),
		}}
	})
	relay, root := configuredSkillRelay(t, server.URL)
	require.NoError(t, os.MkdirAll(root, 0o755))
	target := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("outside"), 0o644))
	require.NoError(t, os.Symlink(target, filepath.Join(root, "linked")))

	require.Empty(t, relay.syncSkills(t.Context(), false))
	body, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "outside", string(body))
	_, err = os.Lstat(filepath.Join(filepath.Dir(root), "escape"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSkillSyncCorruptManifestOwnsNothing(t *testing.T) {
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{
			skillUpdate("ignored", "managed", ""),
			skillUpdate("existing", "server-body", ""),
		}}
	})
	relay, root := configuredSkillRelay(t, server.URL)
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(root, "existing"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "existing", "SKILL.md"), []byte("user-body"), 0o644))
	manifestPath := filepath.Join(root, skillManifestFilename)
	require.NoError(t, os.WriteFile(manifestPath, []byte("not-json"), 0o600))

	contextNote := relay.syncSkills(t.Context(), false)
	require.Contains(t, contextNote, "read-only fallback")
	body, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	require.Equal(t, "not-json", string(body))
	existing, err := os.ReadFile(filepath.Join(root, "existing", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "user-body", string(existing))
	requests, _ := server.captured()
	require.Len(t, requests, 2)
	statuses := map[string]components.Status{}
	for _, exception := range requests[1].Exceptions {
		statuses[exception.Name] = exception.Status
	}
	require.Equal(t, components.StatusFsReadonly, statuses["ignored"])
	require.Equal(t, components.StatusConflictSkipped, statuses["existing"])
}

func TestSkillManifestRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), skillManifestFilename)
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("x", skillManifestMax+1)), 0o600))

	_, err := readSkillManifest(path)
	require.Error(t, err)
}

func TestSkillManifestRejectsTrailingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), skillManifestFilename)
	body, err := json.Marshal(emptySkillManifest())
	require.NoError(t, err)
	body = append(body, []byte(" trailing")...)
	require.NoError(t, os.WriteFile(path, body, 0o600))

	_, err = readSkillManifest(path)
	require.Error(t, err)
}

func TestSkillManifestRecoversPendingUpdateHashes(t *testing.T) {
	root := t.TempDir()
	deployment := skillDeployment{ServerURL: "https://gram.test", Org: "org", Project: "project"}
	oldBody := []byte("old")
	newBody := []byte("new")
	for _, name := range []string{"disk-old", "disk-new"} {
		require.NoError(t, os.Mkdir(filepath.Join(root, name), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "disk-old", "SKILL.md"), oldBody, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "disk-new", "SKILL.md"), newBody, 0o644))
	manifest := emptySkillManifest()
	manifest.Entries = []managedSkill{
		{Deployment: deployment, Name: "disk-old", Files: []string{filepath.Join("disk-old", "SKILL.md")}, RawSHA256: rawSkillHash(oldBody), PendingUpdate: &pendingSkillUpdate{RawSHA256: rawSkillHash(newBody)}, PendingRemove: nil},
		{Deployment: deployment, Name: "disk-new", Files: []string{filepath.Join("disk-new", "SKILL.md")}, RawSHA256: rawSkillHash(oldBody), PendingUpdate: &pendingSkillUpdate{RawSHA256: rawSkillHash(newBody)}, PendingRemove: nil},
	}
	manifestPath := filepath.Join(root, skillManifestFilename)
	require.NoError(t, writeSkillManifest(manifestPath, manifest))

	changed, err := recoverSkillManifest(root, manifestPath, &manifest)
	require.NoError(t, err)
	require.True(t, changed)
	entries := map[string]managedSkill{}
	for _, entry := range manifest.Entries {
		entries[entry.Name] = entry
	}
	require.Nil(t, entries["disk-old"].PendingUpdate)
	require.Equal(t, rawSkillHash(oldBody), entries["disk-old"].RawSHA256)
	require.Nil(t, entries["disk-new"].PendingUpdate)
	require.Equal(t, rawSkillHash(newBody), entries["disk-new"].RawSHA256)
}

func TestSkillManifestRecoversPendingRemovalStates(t *testing.T) {
	root := t.TempDir()
	deployment := skillDeployment{ServerURL: "https://gram.test", Org: "org", Project: "project"}
	body := []byte("managed")
	finalName := "before-rename"
	tombName := skillRemovalPrefix + "after-rename"
	require.NoError(t, os.Mkdir(filepath.Join(root, finalName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, finalName, "SKILL.md"), body, 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(root, tombName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, tombName, "SKILL.md"), body, 0o644))
	manifest := emptySkillManifest()
	manifest.Entries = []managedSkill{
		{Deployment: deployment, Name: finalName, Files: []string{filepath.Join(finalName, "SKILL.md")}, RawSHA256: rawSkillHash(body), PendingUpdate: nil, PendingRemove: &pendingSkillRemoval{Tombstone: skillRemovalPrefix + "before-rename"}},
		{Deployment: deployment, Name: "after-rename", Files: []string{filepath.Join("after-rename", "SKILL.md")}, RawSHA256: rawSkillHash(body), PendingUpdate: nil, PendingRemove: &pendingSkillRemoval{Tombstone: tombName}},
	}
	manifestPath := filepath.Join(root, skillManifestFilename)
	require.NoError(t, writeSkillManifest(manifestPath, manifest))

	changed, err := recoverSkillManifest(root, manifestPath, &manifest)
	require.NoError(t, err)
	require.True(t, changed)
	require.Empty(t, manifest.Entries)
	require.Empty(t, manifest.Tombstones)
	_, err = os.Lstat(filepath.Join(root, finalName))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Lstat(filepath.Join(root, tombName))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSkillManifestRecoversEmptyRemovalTombstone(t *testing.T) {
	root := t.TempDir()
	deployment := skillDeployment{ServerURL: "https://gram.test", Org: "org", Project: "project"}
	tombstoneName := skillRemovalPrefix + "empty"
	require.NoError(t, os.Mkdir(filepath.Join(root, tombstoneName), 0o755))
	manifest := emptySkillManifest()
	manifest.Tombstones = []skillRemovalTombstone{{Deployment: deployment, Name: "removed", Files: []string{filepath.Join("removed", "SKILL.md")}, RawSHA256: rawSkillHash([]byte("managed")), Tombstone: tombstoneName}}
	manifestPath := filepath.Join(root, skillManifestFilename)
	require.NoError(t, writeSkillManifest(manifestPath, manifest))

	changed, err := recoverSkillManifest(root, manifestPath, &manifest)
	require.NoError(t, err)
	require.True(t, changed)
	require.Empty(t, manifest.Tombstones)
	_, err = os.Lstat(filepath.Join(root, tombstoneName))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSkillManifestRecoversStagedInstall(t *testing.T) {
	root := t.TempDir()
	deployment := skillDeployment{ServerURL: "https://gram.test", Org: "org", Project: "project"}
	body := []byte("staged")
	tempName := skillInstallPrefix + "recovery"
	require.NoError(t, os.Mkdir(filepath.Join(root, tempName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, tempName, "SKILL.md"), body, 0o644))
	manifest := emptySkillManifest()
	manifest.PendingInstalls = []pendingSkillInstall{{Deployment: deployment, Name: "recovered", Files: []string{filepath.Join("recovered", "SKILL.md")}, RawSHA256: rawSkillHash(body), Temporary: tempName}}
	manifestPath := filepath.Join(root, skillManifestFilename)
	require.NoError(t, writeSkillManifest(manifestPath, manifest))

	changed, err := recoverSkillManifest(root, manifestPath, &manifest)
	require.NoError(t, err)
	require.True(t, changed)
	require.Empty(t, manifest.PendingInstalls)
	require.Len(t, manifest.Entries, 1)
	installed, err := os.ReadFile(filepath.Join(root, "recovered", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, body, installed)
}

func TestSkillSyncRemovalPreservesDirectoryWithUnownedFiles(t *testing.T) {
	remove := false
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		if remove {
			return http.StatusOK, components.SyncSkillsResult{Removals: []string{"mixed"}, Updates: []components.SyncSkillUpdate{}}
		}
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("mixed", "managed", "")}}
	})
	relay, root := configuredSkillRelay(t, server.URL)
	require.NotEmpty(t, relay.syncSkills(t.Context(), false))
	require.NoError(t, os.WriteFile(filepath.Join(root, "mixed", "notes.txt"), []byte("user-file"), 0o644))
	remove = true

	require.Empty(t, relay.syncSkills(t.Context(), false))
	managed, err := os.ReadFile(filepath.Join(root, "mixed", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "managed", string(managed))
	userFile, err := os.ReadFile(filepath.Join(root, "mixed", "notes.txt"))
	require.NoError(t, err)
	require.Equal(t, "user-file", string(userFile))
}

func TestSkillSyncUsesClaudeConfigDir(t *testing.T) {
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("configured", "body", "")}}
	})
	relay, root := configuredSkillRelay(t, server.URL)
	require.NotEmpty(t, relay.syncSkills(t.Context(), false))
	_, err := os.Stat(filepath.Join(root, "configured", "SKILL.md"))
	require.NoError(t, err)
}

func TestSkillSyncUsesOnlyPersonalCredentials(t *testing.T) {
	server := newSkillSyncServer(t, nil)
	configDir := filepath.Join(t.TempDir(), "claude")
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	authPath := filepath.Join(t.TempDir(), "hooks-auth.env")
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authPath)
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	cfg := Config{ServerURL: server.URL, ProjectSlug: "project", OrgID: "org", HooksAPIKey: "baked-org-key", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}
	relay := NewRelay(cfg)

	require.Empty(t, relay.syncSkills(t.Context(), false))
	requests, _ := server.captured()
	require.Empty(t, requests)
	t.Setenv("GRAM_HOOKS_API_KEY", "baked-org-key")
	require.Empty(t, relay.syncSkills(t.Context(), false))
	requests, _ = server.captured()
	require.Empty(t, requests)
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	require.NoError(t, writeAuth(creds{ServerURL: server.URL, APIKey: "cached-personal", Project: "other", Email: "user@example.com", Org: "org", Source: credCache}))
	require.Empty(t, relay.syncSkills(t.Context(), false))
	requests, headers := server.captured()
	require.Len(t, requests, 1)
	require.Equal(t, "cached-personal", headers[0].Get("Gram-Key"))
	require.NotEqual(t, "baked-org-key", headers[0].Get("Gram-Key"))
}

func TestSkillSyncUnauthorizedRemovesOnlyIntactOwnedSkill(t *testing.T) {
	reject := false
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		if reject {
			return http.StatusForbidden, components.SyncSkillsResult{}
		}
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("private", "managed", "")}}
	})
	relay, root := configuredSkillRelay(t, server.URL)
	require.NotEmpty(t, relay.syncSkills(t.Context(), false))
	reject = true

	require.Empty(t, relay.syncSkills(t.Context(), false))
	_, err := os.Lstat(filepath.Join(root, "private"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSkillSyncUnauthorizedPreservesOtherDeployment(t *testing.T) {
	reject := false
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		if reject {
			return http.StatusUnauthorized, components.SyncSkillsResult{}
		}
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("current", "current-body", "")}}
	})
	relay, root := configuredSkillRelay(t, server.URL)
	require.NotEmpty(t, relay.syncSkills(t.Context(), false))
	other := skillDeployment{ServerURL: "https://other.example.test", Org: "other-org", Project: "other-project"}
	require.NoError(t, os.Mkdir(filepath.Join(root, "other"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "other", "SKILL.md"), []byte("other-body"), 0o644))
	manifestPath := filepath.Join(root, skillManifestFilename)
	manifest, err := readSkillManifest(manifestPath)
	require.NoError(t, err)
	manifest.Entries = append(manifest.Entries, managedSkill{Deployment: other, Name: "other", Files: []string{filepath.Join("other", "SKILL.md")}, RawSHA256: rawSkillHash([]byte("other-body")), PendingUpdate: nil, PendingRemove: nil})
	require.NoError(t, writeSkillManifest(manifestPath, manifest))
	reject = true

	require.Empty(t, relay.syncSkills(t.Context(), false))
	_, err = os.Lstat(filepath.Join(root, "current"))
	require.ErrorIs(t, err, os.ErrNotExist)
	otherBody, err := os.ReadFile(filepath.Join(root, "other", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "other-body", string(otherBody))
	manifest, err = readSkillManifest(manifestPath)
	require.NoError(t, err)
	require.Len(t, manifest.Entries, 1)
	require.Equal(t, other, manifest.Entries[0].Deployment)
}

func TestSkillSyncPersistenceFailureDoesNotFollowUp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires elevated privileges on Windows")
	}
	var root string
	server := newSkillSyncServer(t, func(index int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		if index == 0 {
			manifestPath := filepath.Join(root, skillManifestFilename)
			require.NoError(t, os.Symlink(filepath.Join(root, "missing-target"), manifestPath))
		}
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("not-durable", "body", "")}}
	})
	relay, configuredRoot := configuredSkillRelay(t, server.URL)
	root = configuredRoot

	require.Empty(t, relay.syncSkills(t.Context(), false))
	requests, _ := server.captured()
	require.Len(t, requests, 1)
	_, err := os.Lstat(filepath.Join(root, "not-durable"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSkillSyncTransientFailurePreservesManagedSkill(t *testing.T) {
	failing := false
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		if failing {
			return http.StatusTooManyRequests, components.SyncSkillsResult{}
		}
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("retained", "last-good", "")}}
	})
	relay, root := configuredSkillRelay(t, server.URL)
	require.NotEmpty(t, relay.syncSkills(t.Context(), false))
	failing = true
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	require.Empty(t, relay.syncSkills(ctx, false))
	body, err := os.ReadFile(filepath.Join(root, "retained", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "last-good", string(body))
	requests, _ := server.captured()
	require.Len(t, requests, 3)
}

func TestSkillSyncReadOnlyFallbackAndReceipt(t *testing.T) {
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("fallback", strings.Repeat("content ", 100), "Use when disk is unavailable")}}
	})
	configPath := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(configPath, []byte("file"), 0o600))
	t.Setenv("CLAUDE_CONFIG_DIR", configPath)
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "auth"))
	t.Setenv("GRAM_HOOKS_API_KEY", "personal")
	relay := NewRelay(Config{ServerURL: server.URL, ProjectSlug: "project", OrgID: "org", HooksAPIKey: "baked", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""})

	contextNote := relay.syncSkills(t.Context(), false)
	require.Contains(t, contextNote, "read-only fallback")
	require.Contains(t, contextNote, "<skill-md>")
	requests, _ := server.captured()
	require.Len(t, requests, 2)
	require.Equal(t, components.StatusFsReadonly, requests[1].Exceptions[0].Status)
}

func TestSkillSyncContextBounds(t *testing.T) {
	installed := make([]installedSkillNotice, 25)
	readonly := make([]readonlySkillNotice, 400)
	for i := range installed {
		installed[i] = installedSkillNotice{Name: fmt.Sprintf("installed-%d", i), Description: strings.Repeat("description", 40), Path: filepath.Join("/absolute/skills", fmt.Sprintf("installed-%d", i), "SKILL.md")}
	}
	for i := range readonly {
		readonly[i] = readonlySkillNotice{Name: fmt.Sprintf("readonly-%d", i), Description: strings.Repeat("description", 40), Content: strings.Repeat("z", 10<<10)}
	}

	contextNote := skillSyncContext(skillSyncOutcome{InstalledNew: installed, Readonly: readonly, DurableChanged: true, PersistenceError: false})
	parts := strings.SplitN(contextNote, "\n\nSpeakeasy could not write", 2)
	require.Len(t, parts, 2)
	require.LessOrEqual(t, len(parts[0]), skillContextMax)
	require.LessOrEqual(t, len("Speakeasy could not write"+parts[1]), skillReadonlyContextMax)
	require.Contains(t, contextNote, "[SKILL.md truncated]")
	require.Contains(t, contextNote, "additional skills omitted: 24 KiB fallback limit reached")
	require.LessOrEqual(t, strings.Count(contextNote, "- installed-"), 20)
}

func TestSkillSyncSessionEndSkipsContendedLock(t *testing.T) {
	server := newSkillSyncServer(t, nil)
	relay, root := configuredSkillRelay(t, server.URL)
	lock, contended, err := acquireSkillSyncLock(t.Context(), root, false)
	require.NoError(t, err)
	require.False(t, contended)
	defer func() {
		unlockFile(lock)
		require.NoError(t, lock.Close())
	}()

	require.Empty(t, relay.syncSkills(t.Context(), true))
	requests, _ := server.captured()
	require.Empty(t, requests)
}

func TestSkillSyncLifecycleIgnoresNonClaudeProviders(t *testing.T) {
	var mu sync.Mutex
	syncRequests := 0
	telemetryRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		if req.URL.Path == "/rpc/skills.sync" {
			syncRequests++
		} else {
			telemetryRequests++
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path == "/rpc/skills.sync" {
			require.NoError(t, json.NewEncoder(w).Encode(components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{}}))
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"decision": "allow", "effects": map[string]any{}}))
	}))
	t.Cleanup(server.Close)
	relay, _ := configuredSkillRelay(t, server.URL)
	event := agenthooks.Event{Provider: agenthooks.ProviderCursor, Variant: agenthooks.VariantCLI, NativeName: "sessionStart", Kind: agenthooks.KindSessionStart, Time: time.Now(), Session: agenthooks.SessionInfo{ID: "session", TurnID: "", CWD: t.TempDir(), WorkspaceRoots: nil, TranscriptPath: "", Model: "", PermissionMode: "", UserEmail: ""}, Agent: nil, DetectionConfidence: agenthooks.DetectionConfig, Backfilled: false, Raw: json.RawMessage(`{}`)}

	_, err := relay.onSessionStart(t.Context(), &agenthooks.SessionStartEvent{Event: event, Source: "startup"})
	require.NoError(t, err)
	event.Kind = agenthooks.KindSessionEnd
	event.NativeName = "sessionEnd"
	require.NoError(t, relay.onSessionEnd(t.Context(), &agenthooks.SessionEndEvent{Event: event, Reason: "complete"}))
	mu.Lock()
	defer mu.Unlock()
	require.Zero(t, syncRequests)
	require.Equal(t, 2, telemetryRequests)
}
