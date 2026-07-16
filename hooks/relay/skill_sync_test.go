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

func TestRenameNoReplacePreservesEmptyDestination(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "staged")
	newPath := filepath.Join(root, "destination")
	require.NoError(t, os.Mkdir(oldPath, 0o755))
	require.NoError(t, os.Mkdir(newPath, 0o755))

	require.Error(t, renameNoReplace(oldPath, newPath))
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
	}}, PendingInstalls: []pendingSkillInstall{}, Exceptions: []managedSkillException{}}
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

func TestSkillSyncDropsMissingOtherDeploymentOwnership(t *testing.T) {
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("shared-name", "current-deployment", "")}}
	})
	relay, root := configuredSkillRelay(t, server.URL)
	require.NoError(t, os.MkdirAll(root, 0o755))
	other := skillDeployment{ServerURL: "https://other.example.test", Org: "other-org", Project: "other-project"}
	manifest := emptySkillManifest()
	manifest.Entries = []managedSkill{{
		Deployment:    other,
		Name:          "shared-name",
		Files:         []string{filepath.Join("shared-name", "SKILL.md")},
		RawSHA256:     rawSkillHash([]byte("missing")),
		PendingUpdate: nil,
		PendingRemove: nil,
	}}
	require.NoError(t, writeSkillManifest(filepath.Join(root, skillManifestFilename), manifest))

	contextNote := relay.syncSkills(t.Context(), false)
	require.Contains(t, contextNote, "shared-name")
	body, err := os.ReadFile(filepath.Join(root, "shared-name", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "current-deployment", string(body))
	stored, err := readSkillManifest(filepath.Join(root, skillManifestFilename))
	require.NoError(t, err)
	require.Len(t, stored.Entries, 1)
	require.Equal(t, "shared-name", stored.Entries[0].Name)
	require.Equal(t, skillDeployment{ServerURL: server.URL, Org: "org", Project: "project"}, stored.Entries[0].Deployment)
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

func TestSkillSyncUpdateRejectsSameHashReplacementBeforeBackup(t *testing.T) {
	phase := "install"
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		content := "old"
		if phase == "update" {
			content = "new"
		}
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("raced-update", content, "")}}
	})
	relay, root := configuredSkillRelay(t, server.URL)
	require.NotEmpty(t, relay.syncSkills(t.Context(), false))
	phase = "update"
	var replacementInfo os.FileInfo
	relay.skillSyncTransition = func(transition, name string) {
		if transition != "update-before-backup" || name != "raced-update" {
			return
		}
		manifest, err := readSkillManifest(filepath.Join(root, skillManifestFilename))
		require.NoError(t, err)
		require.Equal(t, skillUpdateStaged, manifest.Entries[0].PendingUpdate.Phase)
		path := filepath.Join(root, name, "SKILL.md")
		require.NoError(t, os.Remove(path))
		require.NoError(t, os.WriteFile(path, []byte("new"), 0o644))
		replacementInfo, err = os.Lstat(path)
		require.NoError(t, err)
	}

	require.Empty(t, relay.syncSkills(t.Context(), false))
	path := filepath.Join(root, "raced-update", "SKILL.md")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "new", string(body))
	currentInfo, err := os.Lstat(path)
	require.NoError(t, err)
	require.True(t, os.SameFile(replacementInfo, currentInfo))
	manifest, err := readSkillManifest(filepath.Join(root, skillManifestFilename))
	require.NoError(t, err)
	require.Empty(t, manifest.Entries)
	require.Len(t, manifest.Exceptions, 1)
	require.True(t, manifest.Exceptions[0].Permanent)
}

func TestSkillSyncUpdatePreservesReplacementBeforeInstall(t *testing.T) {
	phase := "install"
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		content := "old"
		if phase == "update" {
			content = "new"
		}
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("raced-gap", content, "")}}
	})
	relay, root := configuredSkillRelay(t, server.URL)
	require.NotEmpty(t, relay.syncSkills(t.Context(), false))
	phase = "update"
	relay.skillSyncTransition = func(transition, name string) {
		if transition == "update-before-install" && name == "raced-gap" {
			manifest, err := readSkillManifest(filepath.Join(root, skillManifestFilename))
			require.NoError(t, err)
			require.Equal(t, skillUpdateBackupMoved, manifest.Entries[0].PendingUpdate.Phase)
			require.NoError(t, os.WriteFile(filepath.Join(root, name, "SKILL.md"), []byte("user-replacement"), 0o644))
		}
	}

	require.Empty(t, relay.syncSkills(t.Context(), false))
	body, err := os.ReadFile(filepath.Join(root, "raced-gap", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "user-replacement", string(body))
	manifest, err := readSkillManifest(filepath.Join(root, skillManifestFilename))
	require.NoError(t, err)
	require.Empty(t, manifest.Entries)
	require.Len(t, manifest.Exceptions, 1)
	require.True(t, manifest.Exceptions[0].Permanent)
}

func TestSkillSyncUpdatePreservesReplacementBeforeFinalize(t *testing.T) {
	phase := "install"
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		content := "old"
		if phase == "update" {
			content = "new"
		}
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("raced-finalize", content, "")}}
	})
	relay, root := configuredSkillRelay(t, server.URL)
	require.NotEmpty(t, relay.syncSkills(t.Context(), false))
	phase = "update"
	var replacementInfo os.FileInfo
	relay.skillSyncTransition = func(transition, name string) {
		if transition != "update-before-finalize" || name != "raced-finalize" {
			return
		}
		path := filepath.Join(root, name, "SKILL.md")
		require.NoError(t, os.Remove(path))
		require.NoError(t, os.WriteFile(path, []byte("new"), 0o644))
		var err error
		replacementInfo, err = os.Lstat(path)
		require.NoError(t, err)
	}

	require.Empty(t, relay.syncSkills(t.Context(), false))
	path := filepath.Join(root, "raced-finalize", "SKILL.md")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "new", string(body))
	currentInfo, err := os.Lstat(path)
	require.NoError(t, err)
	require.True(t, os.SameFile(replacementInfo, currentInfo))
	manifest, err := readSkillManifest(filepath.Join(root, skillManifestFilename))
	require.NoError(t, err)
	require.Empty(t, manifest.Entries)
	require.Len(t, manifest.Exceptions, 1)
	require.True(t, manifest.Exceptions[0].Permanent)
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

func TestSkillManifestRejectsInvalidTransactionPhase(t *testing.T) {
	root := t.TempDir()
	name := "invalid-phase"
	require.NoError(t, os.Mkdir(filepath.Join(root, name), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, name, "SKILL.md"), []byte("old"), 0o644))
	manifest := emptySkillManifest()
	manifest.Entries = []managedSkill{{Deployment: skillDeployment{ServerURL: "https://gram.test", Org: "org", Project: "project"}, Name: name, Files: []string{filepath.Join(name, "SKILL.md")}, RawSHA256: rawSkillHash([]byte("old")), PendingUpdate: &pendingSkillUpdate{Phase: skillUpdatePhase("invalid"), NewSHA256: rawSkillHash([]byte("new")), Staged: skillUpdateStagedPrefix + "invalid", Backup: skillUpdateBackupPrefix + "invalid"}, PendingRemove: nil}}
	manifestPath := filepath.Join(root, skillManifestFilename)
	require.NoError(t, writeSkillManifest(manifestPath, manifest))

	_, err := readSkillManifest(manifestPath)
	require.Error(t, err)
}

func recoverPendingUpdateState(t *testing.T, phase skillUpdatePhase, finalBody, stagedBody, backupBody []byte) (string, skillManifest) {
	t.Helper()
	root := t.TempDir()
	deployment := skillDeployment{ServerURL: "https://gram.test", Org: "org", Project: "project"}
	oldBody := []byte("old")
	newBody := []byte("new")
	name := "recover-update"
	dir := filepath.Join(root, name)
	require.NoError(t, os.Mkdir(dir, 0o755))
	staged := skillUpdateStagedPrefix + "recovery"
	backup := skillUpdateBackupPrefix + "recovery"
	if finalBody != nil {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), finalBody, 0o644))
	}
	if stagedBody != nil {
		require.NoError(t, os.WriteFile(filepath.Join(dir, staged), stagedBody, 0o644))
	}
	if backupBody != nil {
		require.NoError(t, os.WriteFile(filepath.Join(dir, backup), backupBody, 0o644))
	}
	manifest := emptySkillManifest()
	manifest.Entries = []managedSkill{{Deployment: deployment, Name: name, Files: []string{filepath.Join(name, "SKILL.md")}, RawSHA256: rawSkillHash(oldBody), PendingUpdate: &pendingSkillUpdate{Phase: phase, NewSHA256: rawSkillHash(newBody), Staged: staged, Backup: backup}, PendingRemove: nil}}
	manifestPath := filepath.Join(root, skillManifestFilename)
	require.NoError(t, writeSkillManifest(manifestPath, manifest))

	changed, err := recoverSkillManifest(root, manifestPath, &manifest)
	require.NoError(t, err)
	require.True(t, changed)
	return root, manifest
}

func TestSkillManifestRecoversPlannedUpdate(t *testing.T) {
	oldBody := []byte("old")
	root, manifest := recoverPendingUpdateState(t, skillUpdatePlanned, oldBody, nil, nil)
	require.Len(t, manifest.Entries, 1)
	require.Nil(t, manifest.Entries[0].PendingUpdate)
	require.Equal(t, rawSkillHash(oldBody), manifest.Entries[0].RawSHA256)
	body, err := os.ReadFile(filepath.Join(root, "recover-update", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, oldBody, body)
}

func TestSkillManifestRecoversStagedUpdate(t *testing.T) {
	oldBody := []byte("old")
	root, manifest := recoverPendingUpdateState(t, skillUpdateStaged, oldBody, []byte("new"), nil)
	require.Len(t, manifest.Entries, 1)
	require.Nil(t, manifest.Entries[0].PendingUpdate)
	require.Equal(t, rawSkillHash(oldBody), manifest.Entries[0].RawSHA256)
	body, err := os.ReadFile(filepath.Join(root, "recover-update", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, oldBody, body)
	_, err = os.Lstat(filepath.Join(root, "recover-update", skillUpdateStagedPrefix+"recovery"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSkillManifestPlannedUpdateDoesNotAdoptByteIdenticalReplacement(t *testing.T) {
	newBody := []byte("new")
	root, manifest := recoverPendingUpdateState(t, skillUpdatePlanned, newBody, newBody, nil)
	require.Empty(t, manifest.Entries)
	require.Len(t, manifest.Exceptions, 1)
	body, err := os.ReadFile(filepath.Join(root, "recover-update", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, newBody, body)
}

func TestSkillManifestRecoversBackupMovedUpdate(t *testing.T) {
	newBody := []byte("new")
	root, manifest := recoverPendingUpdateState(t, skillUpdateBackupMoved, nil, newBody, []byte("old"))
	require.Len(t, manifest.Entries, 1)
	require.Nil(t, manifest.Entries[0].PendingUpdate)
	require.Equal(t, rawSkillHash(newBody), manifest.Entries[0].RawSHA256)
	body, err := os.ReadFile(filepath.Join(root, "recover-update", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, newBody, body)
}

func TestSkillManifestRecoversInstalledUpdate(t *testing.T) {
	newBody := []byte("new")
	root, manifest := recoverPendingUpdateState(t, skillUpdateInstalled, newBody, nil, []byte("old"))
	require.Len(t, manifest.Entries, 1)
	require.Nil(t, manifest.Entries[0].PendingUpdate)
	require.Equal(t, rawSkillHash(newBody), manifest.Entries[0].RawSHA256)
	_, err := os.Lstat(filepath.Join(root, "recover-update", skillUpdateBackupPrefix+"recovery"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSkillManifestRetainsStagedUpdateWhenConflictCleanupFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory mode bits do not reliably block removal on Windows")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "cleanup-update")
	require.NoError(t, os.Mkdir(dir, 0o755))
	oldBody := []byte("old")
	newBody := []byte("new")
	staged := skillUpdateStagedPrefix + "cleanup"
	backup := skillUpdateBackupPrefix + "cleanup"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), newBody, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, staged), newBody, 0o644))
	manifest := emptySkillManifest()
	manifest.Entries = []managedSkill{{Deployment: skillDeployment{ServerURL: "https://gram.test", Org: "org", Project: "project"}, Name: "cleanup-update", Files: []string{filepath.Join("cleanup-update", "SKILL.md")}, RawSHA256: rawSkillHash(oldBody), PendingUpdate: &pendingSkillUpdate{Phase: skillUpdateStaged, NewSHA256: rawSkillHash(newBody), Staged: staged, Backup: backup}, PendingRemove: nil}}
	manifestPath := filepath.Join(root, skillManifestFilename)
	require.NoError(t, writeSkillManifest(manifestPath, manifest))
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := recoverSkillManifest(root, manifestPath, &manifest)
	require.Error(t, err)
	stored, readErr := readSkillManifest(manifestPath)
	require.NoError(t, readErr)
	require.Len(t, stored.Entries, 1)
	require.Equal(t, skillUpdateStaged, stored.Entries[0].PendingUpdate.Phase)
}

func TestSkillManifestRetainsUpdateWithUnknownStateFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "unknown-update-state")
	staged := skillUpdateStagedPrefix + "unknown"
	backup := skillUpdateBackupPrefix + "unknown"
	oldBody := []byte("old")
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), oldBody, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, staged), []byte("user-state"), 0o644))
	manifest := emptySkillManifest()
	manifest.Entries = []managedSkill{{Deployment: skillDeployment{ServerURL: "https://gram.test", Org: "org", Project: "project"}, Name: "unknown-update-state", Files: []string{filepath.Join("unknown-update-state", "SKILL.md")}, RawSHA256: rawSkillHash(oldBody), PendingUpdate: &pendingSkillUpdate{Phase: skillUpdatePlanned, NewSHA256: rawSkillHash([]byte("new")), Staged: staged, Backup: backup}, PendingRemove: nil}}
	manifestPath := filepath.Join(root, skillManifestFilename)
	require.NoError(t, writeSkillManifest(manifestPath, manifest))

	_, err := recoverSkillManifest(root, manifestPath, &manifest)
	require.Error(t, err)
	stored, err := readSkillManifest(manifestPath)
	require.NoError(t, err)
	require.Len(t, stored.Entries, 1)
	require.Equal(t, skillUpdatePlanned, stored.Entries[0].PendingUpdate.Phase)
	body, err := os.ReadFile(filepath.Join(dir, staged))
	require.NoError(t, err)
	require.Equal(t, []byte("user-state"), body)
}

func recoverPendingRemovalState(t *testing.T, phase skillRemovalPhase, finalBody, backupBody []byte) (string, skillManifest) {
	t.Helper()
	root := t.TempDir()
	deployment := skillDeployment{ServerURL: "https://gram.test", Org: "org", Project: "project"}
	name := "recover-removal"
	dir := filepath.Join(root, name)
	backup := skillRemovalBackupPrefix + "recovery"
	require.NoError(t, os.Mkdir(dir, 0o755))
	if finalBody != nil {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), finalBody, 0o644))
	}
	if backupBody != nil {
		require.NoError(t, os.WriteFile(filepath.Join(dir, backup), backupBody, 0o644))
	}
	manifest := emptySkillManifest()
	manifest.Entries = []managedSkill{{Deployment: deployment, Name: name, Files: []string{filepath.Join(name, "SKILL.md")}, RawSHA256: rawSkillHash([]byte("managed")), PendingUpdate: nil, PendingRemove: &pendingSkillRemoval{Phase: phase, Backup: backup}}}
	manifestPath := filepath.Join(root, skillManifestFilename)
	require.NoError(t, writeSkillManifest(manifestPath, manifest))
	changed, err := recoverSkillManifest(root, manifestPath, &manifest)
	require.NoError(t, err)
	require.True(t, changed)
	return root, manifest
}

func TestSkillManifestRecoversPlannedRemoval(t *testing.T) {
	root, manifest := recoverPendingRemovalState(t, skillRemovalPlanned, []byte("managed"), nil)
	require.Len(t, manifest.Entries, 1)
	require.Nil(t, manifest.Entries[0].PendingRemove)
	body, err := os.ReadFile(filepath.Join(root, "recover-removal", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, []byte("managed"), body)
}

func TestSkillManifestRecoversMovedRemoval(t *testing.T) {
	root, manifest := recoverPendingRemovalState(t, skillRemovalMoved, nil, []byte("managed"))
	require.Empty(t, manifest.Entries)
	_, err := os.Lstat(filepath.Join(root, "recover-removal"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSkillManifestRetainsMovedRemovalWhenCleanupFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory mode bits do not reliably block removal on Windows")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "cleanup-removal")
	backup := skillRemovalBackupPrefix + "cleanup"
	body := []byte("managed")
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, backup), body, 0o644))
	manifest := emptySkillManifest()
	manifest.Entries = []managedSkill{{Deployment: skillDeployment{ServerURL: "https://gram.test", Org: "org", Project: "project"}, Name: "cleanup-removal", Files: []string{filepath.Join("cleanup-removal", "SKILL.md")}, RawSHA256: rawSkillHash(body), PendingUpdate: nil, PendingRemove: &pendingSkillRemoval{Phase: skillRemovalMoved, Backup: backup}}}
	manifestPath := filepath.Join(root, skillManifestFilename)
	require.NoError(t, writeSkillManifest(manifestPath, manifest))
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := recoverSkillManifest(root, manifestPath, &manifest)
	require.Error(t, err)
	stored, readErr := readSkillManifest(manifestPath)
	require.NoError(t, readErr)
	require.Len(t, stored.Entries, 1)
	require.Equal(t, skillRemovalMoved, stored.Entries[0].PendingRemove.Phase)
}

func TestSkillManifestRetainsRemovalWithUnknownBackup(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "unknown-removal-state")
	backup := skillRemovalBackupPrefix + "unknown"
	body := []byte("managed")
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), body, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, backup), []byte("user-backup"), 0o644))
	manifest := emptySkillManifest()
	manifest.Entries = []managedSkill{{Deployment: skillDeployment{ServerURL: "https://gram.test", Org: "org", Project: "project"}, Name: "unknown-removal-state", Files: []string{filepath.Join("unknown-removal-state", "SKILL.md")}, RawSHA256: rawSkillHash(body), PendingUpdate: nil, PendingRemove: &pendingSkillRemoval{Phase: skillRemovalPlanned, Backup: backup}}}
	manifestPath := filepath.Join(root, skillManifestFilename)
	require.NoError(t, writeSkillManifest(manifestPath, manifest))

	_, err := recoverSkillManifest(root, manifestPath, &manifest)
	require.Error(t, err)
	stored, err := readSkillManifest(manifestPath)
	require.NoError(t, err)
	require.Len(t, stored.Entries, 1)
	require.Equal(t, skillRemovalPlanned, stored.Entries[0].PendingRemove.Phase)
	backupBody, err := os.ReadFile(filepath.Join(dir, backup))
	require.NoError(t, err)
	require.Equal(t, []byte("user-backup"), backupBody)
}

func TestSkillManifestPlannedRemovalPreservesByteIdenticalReplacement(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "planned-replacement")
	body := []byte("managed")
	backup := skillRemovalBackupPrefix + "planned"
	require.NoError(t, os.Mkdir(dir, 0o755))
	finalPath := filepath.Join(dir, "SKILL.md")
	require.NoError(t, os.WriteFile(finalPath, body, 0o644))
	manifest := emptySkillManifest()
	manifest.Entries = []managedSkill{{Deployment: skillDeployment{ServerURL: "https://gram.test", Org: "org", Project: "project"}, Name: "planned-replacement", Files: []string{filepath.Join("planned-replacement", "SKILL.md")}, RawSHA256: rawSkillHash(body), PendingUpdate: nil, PendingRemove: &pendingSkillRemoval{Phase: skillRemovalPlanned, Backup: backup}}}
	manifestPath := filepath.Join(root, skillManifestFilename)
	require.NoError(t, writeSkillManifest(manifestPath, manifest))
	require.NoError(t, os.Remove(finalPath))
	require.NoError(t, os.WriteFile(finalPath, body, 0o644))
	replacementInfo, err := os.Lstat(finalPath)
	require.NoError(t, err)

	changed, err := recoverSkillManifest(root, manifestPath, &manifest)
	require.NoError(t, err)
	require.True(t, changed)
	require.Len(t, manifest.Entries, 1)
	require.Nil(t, manifest.Entries[0].PendingRemove)
	currentInfo, err := os.Lstat(finalPath)
	require.NoError(t, err)
	require.True(t, os.SameFile(replacementInfo, currentInfo))
}

func TestSkillManifestRecoversMissingDirectoryAfterManifestWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory mode bits do not reliably block manifest writes on Windows")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "missing-directory")
	require.NoError(t, os.Mkdir(dir, 0o755))
	manifest := emptySkillManifest()
	manifest.Entries = []managedSkill{{Deployment: skillDeployment{ServerURL: "https://gram.test", Org: "org", Project: "project"}, Name: "missing-directory", Files: []string{filepath.Join("missing-directory", "SKILL.md")}, RawSHA256: rawSkillHash([]byte("managed")), PendingUpdate: nil, PendingRemove: &pendingSkillRemoval{Phase: skillRemovalMoved, Backup: skillRemovalBackupPrefix + "missing"}}}
	manifestPath := filepath.Join(root, skillManifestFilename)
	require.NoError(t, writeSkillManifest(manifestPath, manifest))

	_, err := resumePendingRemoval(root, manifestPath, &manifest, 0, nil, func(transition, _ string) {
		if transition == "remove-after-directory" {
			require.NoError(t, os.Chmod(root, 0o555))
		}
	})
	require.Error(t, err)
	require.NoError(t, os.Chmod(root, 0o755))
	_, err = os.Lstat(dir)
	require.ErrorIs(t, err, os.ErrNotExist)
	stored, err := readSkillManifest(manifestPath)
	require.NoError(t, err)
	require.Len(t, stored.Entries, 1)
	require.Equal(t, skillRemovalMoved, stored.Entries[0].PendingRemove.Phase)

	changed, err := recoverSkillManifest(root, manifestPath, &stored)
	require.NoError(t, err)
	require.True(t, changed)
	require.Empty(t, stored.Entries)
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

func TestSkillSyncRemovalPreservesFileAddedAfterIntent(t *testing.T) {
	remove := false
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		if remove {
			return http.StatusOK, components.SyncSkillsResult{Removals: []string{"raced-remove"}, Updates: []components.SyncSkillUpdate{}}
		}
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("raced-remove", "managed", "")}}
	})
	relay, root := configuredSkillRelay(t, server.URL)
	require.NotEmpty(t, relay.syncSkills(t.Context(), false))
	remove = true
	relay.skillSyncTransition = func(transition, name string) {
		if transition == "remove-before-move" && name == "raced-remove" {
			manifest, err := readSkillManifest(filepath.Join(root, skillManifestFilename))
			require.NoError(t, err)
			require.Equal(t, skillRemovalPlanned, manifest.Entries[0].PendingRemove.Phase)
			require.NoError(t, os.WriteFile(filepath.Join(root, name, "notes.txt"), []byte("user-file"), 0o644))
		}
	}

	require.Empty(t, relay.syncSkills(t.Context(), false))
	userFile, err := os.ReadFile(filepath.Join(root, "raced-remove", "notes.txt"))
	require.NoError(t, err)
	require.Equal(t, "user-file", string(userFile))
	manifest, err := readSkillManifest(filepath.Join(root, skillManifestFilename))
	require.NoError(t, err)
	require.Empty(t, manifest.Entries)
	require.Empty(t, manifest.Exceptions)
	_, err = os.Lstat(filepath.Join(root, "raced-remove", "SKILL.md"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSkillSyncRemovalPreservesSkillAddedAfterMove(t *testing.T) {
	remove := false
	server := newSkillSyncServer(t, func(_ int, _ components.SyncSkillsRequestBody) (int, components.SyncSkillsResult) {
		if remove {
			return http.StatusOK, components.SyncSkillsResult{Removals: []string{"raced-tombstone"}, Updates: []components.SyncSkillUpdate{}}
		}
		return http.StatusOK, components.SyncSkillsResult{Removals: []string{}, Updates: []components.SyncSkillUpdate{skillUpdate("raced-tombstone", "managed", "")}}
	})
	relay, root := configuredSkillRelay(t, server.URL)
	require.NotEmpty(t, relay.syncSkills(t.Context(), false))
	remove = true
	relay.skillSyncTransition = func(transition, name string) {
		if transition != "remove-after-move" || name != "raced-tombstone" {
			return
		}
		manifest, err := readSkillManifest(filepath.Join(root, skillManifestFilename))
		require.NoError(t, err)
		require.Equal(t, skillRemovalMoved, manifest.Entries[0].PendingRemove.Phase)
		require.NoError(t, os.WriteFile(filepath.Join(root, name, "SKILL.md"), []byte("user-skill"), 0o644))
	}

	require.Empty(t, relay.syncSkills(t.Context(), false))
	managed, err := os.ReadFile(filepath.Join(root, "raced-tombstone", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "user-skill", string(managed))
	manifest, err := readSkillManifest(filepath.Join(root, skillManifestFilename))
	require.NoError(t, err)
	require.Empty(t, manifest.Entries)
	require.Len(t, manifest.Exceptions, 1)
	require.True(t, manifest.Exceptions[0].Permanent)
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
