package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMaybeUploadSkillContentHandsOffExactLocation(t *testing.T) {
	task := skillUploadTaskForTest(t, "https://example.com", "project", "secret-key", "# Exact skill\n")
	capturedTasks := captureSkillUploadTasks(t)
	skill := &resolvedSkill{
		name:         "skill",
		sourceLevel:  "project",
		sourcePath:   task.SourcePath,
		rawSHA256:    task.RawSHA256,
		content:      "# Exact skill\n",
		captureReady: true,
		root:         task.SourceRoot,
	}

	require.NoError(t, startSkillContentUpload(creds{ServerURL: task.ServerURL, APIKey: task.APIKey, Project: task.Project, Email: "", Org: "", Source: credCache}, acceptedSkillUploadResult(task.RawSHA256, true), skill))
	require.Equal(t, []skillUploadTask{task}, capturedTasks())

	skill.captureReady = false
	require.NoError(t, startSkillContentUpload(creds{ServerURL: task.ServerURL, APIKey: task.APIKey, Project: task.Project, Email: "", Org: "", Source: credCache}, acceptedSkillUploadResult(task.RawSHA256, true), skill))
	require.Len(t, capturedTasks(), 1)
}

func TestRunSkillUploadReopensAndUploadsExactContent(t *testing.T) {
	content := "# Skill\n\nExact content.\n"
	type observedRequest struct {
		Method   string
		Path     string
		GramKeys []string
		Projects []string
		Body     map[string]any
	}
	observed := make(chan observedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(req.Body).Decode(&body)
		observed <- observedRequest{Method: req.Method, Path: req.URL.Path, GramKeys: req.Header.Values("Gram-Key"), Projects: req.Header.Values("Gram-Project"), Body: body}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	task := skillUploadTaskForTest(t, server.URL, "project", "key", content)

	require.Equal(t, 0, runSkillUploadTask(t, task))
	request := <-observed
	require.Equal(t, http.MethodPost, request.Method)
	require.Equal(t, "/rpc/hooks.uploadSkillContent", request.Path)
	require.Equal(t, []string{"key"}, request.GramKeys)
	require.Equal(t, []string{"project"}, request.Projects)
	require.Equal(t, map[string]any{"content": content, "raw_sha256": task.RawSHA256, "schema_version": "hook.skill-content.v1"}, request.Body)
}

func TestRunSkillUploadRejectsChangedManifestWithoutNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	task := skillUploadTaskForTest(t, server.URL, "project", "key", "original")
	require.NoError(t, os.WriteFile(task.SourcePath, []byte("changed"), 0o600))

	require.NotZero(t, runSkillUploadTask(t, task))
	require.Zero(t, requests.Load())
}

func TestRunSkillUploadRejectsInvalidArgumentsWithoutNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	task := skillUploadTaskForTest(t, server.URL, "project", "key", "content")

	require.NotZero(t, RunSkillUpload(t.Context(), nil, strings.NewReader(task.APIKey)))
	require.NotZero(t, RunSkillUpload(t.Context(), append(skillUploadArgs(task), "extra"), strings.NewReader(task.APIKey)))
	badHash := task
	badHash.RawSHA256 = strings.Repeat("A", 64)
	require.NotZero(t, RunSkillUpload(t.Context(), skillUploadArgs(badHash), strings.NewReader(task.APIKey)))
	require.NotZero(t, RunSkillUpload(t.Context(), skillUploadArgs(task), strings.NewReader(strings.Repeat("x", maxSkillUploadAPIKeyBytes+1))))
	require.Zero(t, requests.Load())
}

func TestRunSkillUploadOmitsEmptyProjectHeader(t *testing.T) {
	var keys, projects []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		keys = req.Header.Values("Gram-Key")
		projects = req.Header.Values("Gram-Project")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	task := skillUploadTaskForTest(t, server.URL, "", "key", "content")

	require.Equal(t, 0, runSkillUploadTask(t, task))
	require.Equal(t, []string{"key"}, keys)
	require.Empty(t, projects)
}

func TestRunSkillUploadDoesNotFollowRedirect(t *testing.T) {
	var targetRequests atomic.Int32
	var targetKey, sourceKey atomic.Value
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		targetRequests.Add(1)
		targetKey.Store(req.Header.Get("Gram-Key"))
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		sourceKey.Store(req.Header.Get("Gram-Key"))
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(redirect.Close)
	task := skillUploadTaskForTest(t, redirect.URL, "project", "secret", "content")

	require.NotZero(t, runSkillUploadTask(t, task))
	require.Equal(t, "secret", sourceKey.Load())
	require.Zero(t, targetRequests.Load())
	require.Nil(t, targetKey.Load())
}

func TestNewSkillUploadCommandKeepsCredentialOutOfArgsAndEnvironment(t *testing.T) {
	task := skillUploadTaskForTest(t, "https://example.com", "project", "exact-secret-key-for-child-handoff", "private content")
	t.Setenv("GRAM_HOOKS_API_KEY", task.APIKey)
	t.Setenv("GRAM_HOOKS_ORG_KEY", task.APIKey)
	cmd, err := newSkillUploadCommand(task)
	require.NoError(t, err)

	args := strings.Join(cmd.Args, "\x00")
	require.NotContains(t, args, task.APIKey)
	require.NotContains(t, args, "private content")
	require.Contains(t, args, task.SourcePath)
	require.NotContains(t, strings.Join(cmd.Env, "\x00"), task.APIKey)
}

func TestRunSkillUploadRetriesTransientServerError(t *testing.T) {
	original := retryMaxElapsedMS
	retryMaxElapsedMS = 2_500
	t.Cleanup(func() { retryMaxElapsedMS = original })
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	require.Zero(t, runSkillUploadTask(t, skillUploadTaskForTest(t, server.URL, "project", "key", "retry content")))
	require.Equal(t, int32(2), requests.Load())
}

func TestRunSkillUploadRetriesDroppedConnection(t *testing.T) {
	var requests atomic.Int32
	var successfulKey, successfulProject atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if requests.Add(1) == 1 {
			hijacker, ok := w.(http.Hijacker)
			require.True(t, ok)
			conn, _, err := hijacker.Hijack()
			require.NoError(t, err)
			require.NoError(t, conn.Close())
			return
		}
		successfulKey.Store(req.Header.Get("Gram-Key"))
		successfulProject.Store(req.Header.Get("Gram-Project"))
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	require.Zero(t, runSkillUploadTask(t, skillUploadTaskForTest(t, server.URL, "project", "key", "retry-safe content")))
	require.Equal(t, int32(2), requests.Load())
	require.Equal(t, "key", successfulKey.Load())
	require.Equal(t, "project", successfulProject.Load())
}

func TestRunSkillUploadDoesNotRetryDefinitiveClientError(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)

	require.NotZero(t, runSkillUploadTask(t, skillUploadTaskForTest(t, server.URL, "project", "key", "content")))
	require.Equal(t, int32(1), requests.Load())
}

func TestStartSkillUploadProcessHandsOffExactCredentialAndMaximumContent(t *testing.T) {
	originalCommand := newSkillUploadCommand
	t.Cleanup(func() { newSkillUploadCommand = originalCommand })
	newSkillUploadCommand = skillUploadHelperCommand

	content := strings.Repeat("x", maxSkillContentBytes)
	type upload struct {
		key     string
		content string
	}
	observed := make(chan upload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Content string `json:"content"`
		}
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		observed <- upload{key: req.Header.Get("Gram-Key"), content: body.Content}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	task := skillUploadTaskForTest(t, server.URL, "project", "exact-pipe-key_123", content)

	require.NoError(t, startSkillUploadProcess(task))
	select {
	case got := <-observed:
		require.Equal(t, task.APIKey, got.key)
		require.Equal(t, content, got.content)
	case <-time.After(5 * time.Second):
		t.Fatal("detached upload did not arrive")
	}
}

func TestStartSkillUploadProcessReturnsWithoutWaitingForWorker(t *testing.T) {
	originalCommand := newSkillUploadCommand
	t.Cleanup(func() { newSkillUploadCommand = originalCommand })
	newSkillUploadCommand = func(task skillUploadTask) (*exec.Cmd, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestSkillUploadHelperProcess$")
		cmd.Env = append(envWithoutCredential(os.Environ(), task.APIKey), "GO_SKILL_UPLOAD_HELPER=delay")
		return cmd, nil
	}
	task := skillUploadTaskForTest(t, "https://example.com", "project", "key", "content")

	started := time.Now()
	require.NoError(t, startSkillUploadProcess(task))

	require.Less(t, time.Since(started), time.Second)
}

func TestSkillUploadHelperProcess(t *testing.T) {
	switch os.Getenv("GO_SKILL_UPLOAD_HELPER") {
	case "delay":
		<-time.After(2 * time.Second)
		os.Exit(0)
	case "1":
	default:
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		os.Exit(2)
	}
	os.Exit(RunSkillUpload(context.Background(), os.Args[separator+1:], os.Stdin))
}

func skillUploadHelperCommand(task skillUploadTask) (*exec.Cmd, error) {
	args := append([]string{"-test.run=^TestSkillUploadHelperProcess$", "--"}, skillUploadArgs(task)...)
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(envWithoutCredential(os.Environ(), task.APIKey), "GO_SKILL_UPLOAD_HELPER=1")
	return cmd, nil
}

func skillUploadTaskForTest(t *testing.T, serverURL, project, key, content string) skillUploadTask {
	t.Helper()
	root := filepath.Join(t.TempDir(), "skills")
	path := writeSkillManifest(t, filepath.Join(root, "test"), []byte(content))
	return skillUploadTask{ServerURL: serverURL, Project: project, APIKey: key, RawSHA256: sha256Hex([]byte(content)), SourcePath: path, SourceRoot: root}
}

func runSkillUploadTask(t *testing.T, task skillUploadTask) int {
	t.Helper()
	return RunSkillUpload(t.Context(), skillUploadArgs(task), strings.NewReader(task.APIKey))
}

func skillUploadArgs(task skillUploadTask) []string {
	return []string{
		"--server-url=" + task.ServerURL,
		"--project=" + task.Project,
		"--raw-sha256=" + task.RawSHA256,
		"--source-path=" + task.SourcePath,
		"--source-root=" + task.SourceRoot,
	}
}

func acceptedSkillUploadResult(rawSHA256 string, contentRequired bool) ingestResult {
	return ingestResult{
		statusCode:   http.StatusOK,
		decision:     decision{Decision: "allow", Reason: "", Message: ""},
		authRejected: false,
		failOpen:     nil,
		skillCapture: &skillCapture{rawSHA256: rawSHA256, contentRequired: contentRequired},
	}
}
