package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMaybeUploadSkillContentHandsOffExactTask(t *testing.T) {
	content := "# Exact skill\n\nKeep trailing newline.\n"
	rawSHA256 := sha256Hex([]byte(content))
	credential := creds{ServerURL: "https://example.com", APIKey: "secret-key", Project: "project", Email: "", Org: "", Source: credCache}
	capturedTasks := captureSkillUploadTasks(t)

	result := acceptedSkillUploadResult(rawSHA256, true)
	require.NoError(t, startSkillContentUpload(credential, result, &resolvedSkill{name: "skill", sourceLevel: "", sourcePath: "", rawSHA256: rawSHA256, content: content, captureReady: false}))
	require.Empty(t, capturedTasks())
	require.NoError(t, startSkillContentUpload(credential, result, &resolvedSkill{name: "skill", sourceLevel: "", sourcePath: "", rawSHA256: rawSHA256, content: content, captureReady: true}))
	require.Equal(t, []skillUploadTask{{
		Version:   skillUploadTaskVersion,
		ServerURL: credential.ServerURL,
		Project:   credential.Project,
		APIKey:    credential.APIKey,
		RawSHA256: rawSHA256,
		Content:   content,
	}}, capturedTasks())
}

func TestRunSkillUploadUploadsExactContent(t *testing.T) {
	content := "# Skill\n\nExact content.\n"
	rawSHA256 := sha256Hex([]byte(content))
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
		observed <- observedRequest{
			Method:   req.Method,
			Path:     req.URL.Path,
			GramKeys: req.Header.Values("Gram-Key"),
			Projects: req.Header.Values("Gram-Project"),
			Body:     body,
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	require.Equal(t, 0, RunSkillUpload(t.Context(), bytes.NewReader(marshalSkillUploadTask(t, skillUploadTask{
		Version:   skillUploadTaskVersion,
		ServerURL: server.URL,
		Project:   "project",
		APIKey:    "key",
		RawSHA256: rawSHA256,
		Content:   content,
	}))))
	request := <-observed
	require.Equal(t, http.MethodPost, request.Method)
	require.Equal(t, "/rpc/hooks.uploadSkillContent", request.Path)
	require.Equal(t, []string{"key"}, request.GramKeys)
	require.Equal(t, []string{"project"}, request.Projects)
	require.Equal(t, map[string]any{
		"content":        content,
		"raw_sha256":     rawSHA256,
		"schema_version": "hook.skill-content.v1",
	}, request.Body)
}

func TestRunSkillUploadRejectsInvalidInputWithoutNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	valid := marshalSkillUploadTask(t, skillUploadTask{
		Version:   skillUploadTaskVersion,
		ServerURL: server.URL,
		Project:   "project",
		APIKey:    "key",
		RawSHA256: sha256Hex([]byte("content")),
		Content:   "content",
	})

	require.NotZero(t, RunSkillUpload(t.Context(), strings.NewReader(`{"version":1,"server_url":"`+server.URL+`"`)))
	require.NotZero(t, RunSkillUpload(t.Context(), bytes.NewReader(valid[:len(valid)-1])))
	require.NotZero(t, RunSkillUpload(t.Context(), bytes.NewReader(append(valid, []byte(" garbage")...))))
	require.NotZero(t, RunSkillUpload(t.Context(), bytes.NewReader(append(valid, bytes.Repeat([]byte(" "), maxSkillUploadTaskBytes+1-len(valid))...))))
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
	content := "content"

	require.Equal(t, 0, RunSkillUpload(t.Context(), bytes.NewReader(marshalSkillUploadTask(t, skillUploadTask{
		Version:   skillUploadTaskVersion,
		ServerURL: server.URL,
		Project:   "",
		APIKey:    "key",
		RawSHA256: sha256Hex([]byte(content)),
		Content:   content,
	}))))
	require.Equal(t, []string{"key"}, keys)
	require.Empty(t, projects)
}

func TestRunSkillUploadDoesNotFollowRedirect(t *testing.T) {
	var targetRequests atomic.Int32
	var targetKey atomic.Value
	var sourceKey atomic.Value
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
	content := "content"

	code := RunSkillUpload(t.Context(), bytes.NewReader(marshalSkillUploadTask(t, skillUploadTask{
		Version:   skillUploadTaskVersion,
		ServerURL: redirect.URL,
		Project:   "project",
		APIKey:    "secret",
		RawSHA256: sha256Hex([]byte(content)),
		Content:   content,
	})))

	require.NotZero(t, code)
	require.Equal(t, "secret", sourceKey.Load())
	require.Zero(t, targetRequests.Load())
	require.Nil(t, targetKey.Load())
}

func TestNewSkillUploadCommandUsesOnlySubcommandArg(t *testing.T) {
	cmd, err := newSkillUploadCommand()
	require.NoError(t, err)
	require.Equal(t, []string{cmd.Args[0], "upload-skill"}, cmd.Args)
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
	content := "retry content"

	code := RunSkillUpload(t.Context(), bytes.NewReader(marshalSkillUploadTask(t, skillUploadTask{
		Version:   skillUploadTaskVersion,
		ServerURL: server.URL,
		Project:   "project",
		APIKey:    "key",
		RawSHA256: sha256Hex([]byte(content)),
		Content:   content,
	})))

	require.Zero(t, code)
	require.Equal(t, int32(2), requests.Load())
}

func TestRunSkillUploadRetriesDroppedConnection(t *testing.T) {
	var requests atomic.Int32
	var successfulKey, successfulProject string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if requests.Add(1) == 1 {
			hijacker, ok := w.(http.Hijacker)
			require.True(t, ok)
			conn, _, err := hijacker.Hijack()
			require.NoError(t, err)
			require.NoError(t, conn.Close())
			return
		}
		successfulKey = req.Header.Get("Gram-Key")
		successfulProject = req.Header.Get("Gram-Project")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	content := "retry-safe content"

	code := RunSkillUpload(t.Context(), bytes.NewReader(marshalSkillUploadTask(t, skillUploadTask{
		Version:   skillUploadTaskVersion,
		ServerURL: server.URL,
		Project:   "project",
		APIKey:    "key",
		RawSHA256: sha256Hex([]byte(content)),
		Content:   content,
	})))

	require.Zero(t, code)
	require.Equal(t, int32(2), requests.Load())
	require.Equal(t, "key", successfulKey)
	require.Equal(t, "project", successfulProject)
}

func TestRunSkillUploadDoesNotRetryDefinitiveClientError(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)
	content := "content"

	code := RunSkillUpload(t.Context(), bytes.NewReader(marshalSkillUploadTask(t, skillUploadTask{
		Version:   skillUploadTaskVersion,
		ServerURL: server.URL,
		Project:   "project",
		APIKey:    "key",
		RawSHA256: sha256Hex([]byte(content)),
		Content:   content,
	})))

	require.NotZero(t, code)
	require.Equal(t, int32(1), requests.Load())
}

func TestStartSkillUploadProcessRoundTripsLargeControlContent(t *testing.T) {
	originalCommand := newSkillUploadCommand
	t.Cleanup(func() { newSkillUploadCommand = originalCommand })
	newSkillUploadCommand = skillUploadHelperCommand("upload")

	content := strings.Repeat("\x00", maxSkillContentBytes)
	observed := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Content string `json:"content"`
		}
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		observed <- body.Content
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	task := marshalSkillUploadTask(t, skillUploadTask{
		Version:   skillUploadTaskVersion,
		ServerURL: server.URL,
		Project:   "project",
		APIKey:    "key",
		RawSHA256: sha256Hex([]byte(content)),
		Content:   content,
	})
	require.GreaterOrEqual(t, len(task), 64<<10)
	require.LessOrEqual(t, len(task), maxSkillUploadTaskBytes)

	require.NoError(t, startSkillUploadProcess(task))
	select {
	case got := <-observed:
		require.Equal(t, content, got)
	case <-time.After(5 * time.Second):
		t.Fatal("detached upload did not arrive")
	}
}

func TestStartSkillUploadProcessWatchdogStopsNonReader(t *testing.T) {
	originalCommand := newSkillUploadCommand
	originalTimeout := skillUploadPipeTimeout
	t.Cleanup(func() {
		newSkillUploadCommand = originalCommand
		skillUploadPipeTimeout = originalTimeout
	})
	newSkillUploadCommand = skillUploadHelperCommand("no-read")
	skillUploadPipeTimeout = 50 * time.Millisecond
	content := strings.Repeat("\x00", maxSkillContentBytes)
	task := marshalSkillUploadTask(t, skillUploadTask{
		Version:   skillUploadTaskVersion,
		ServerURL: "https://example.com",
		Project:   "project",
		APIKey:    "key",
		RawSHA256: sha256Hex([]byte(content)),
		Content:   content,
	})

	started := time.Now()
	err := startSkillUploadProcess(task)

	require.ErrorContains(t, err, "timed out")
	require.Less(t, time.Since(started), 2*time.Second)
}

func TestSkillUploadHelperProcess(t *testing.T) {
	switch os.Getenv("GO_SKILL_UPLOAD_HELPER") {
	case "upload":
		os.Exit(RunSkillUpload(context.Background(), os.Stdin))
	case "no-read":
		<-time.After(time.Hour)
	}
}

func skillUploadHelperCommand(mode string) func() (*exec.Cmd, error) {
	return func() (*exec.Cmd, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestSkillUploadHelperProcess$")
		cmd.Env = append(os.Environ(), "GO_SKILL_UPLOAD_HELPER="+mode)
		return cmd, nil
	}
}

func marshalSkillUploadTask(t *testing.T, task skillUploadTask) []byte {
	t.Helper()
	body, err := json.Marshal(task)
	require.NoError(t, err)
	return body
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
