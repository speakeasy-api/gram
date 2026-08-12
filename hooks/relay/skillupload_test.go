package relay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUploadSkillContentUsesNormalizedContent(t *testing.T) {
	content := "# Skill\n\nExact content.\n"
	type observedRequest struct {
		GramKey string
		Project string
		Body    map[string]any
	}
	observed := make(chan observedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		observed <- observedRequest{GramKey: req.Header.Get("Gram-Key"), Project: req.Header.Get("Gram-Project"), Body: body}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	task := skillUploadTaskForTest(server.URL, "project", "key", content)
	skill := &resolvedSkill{name: "skill", rawSHA256: task.RawSHA256, content: content, captureReady: true}

	require.NoError(t, uploadSkillContent(t.Context(), creds{ServerURL: task.ServerURL, APIKey: task.APIKey, Project: task.Project, Email: "", Org: "", Source: credCache}, acceptedSkillUploadResult(task.RawSHA256, true), skill))
	request := <-observed
	require.Equal(t, "key", request.GramKey)
	require.Equal(t, "project", request.Project)
	require.Equal(t, map[string]any{"content": content, "raw_sha256": task.RawSHA256, "schema_version": "hook.skill-content.v1"}, request.Body)
}

func TestUploadSkillContentRejectsMismatchedContentWithoutNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	task := skillUploadTaskForTest(server.URL, "project", "key", "original")
	skill := &resolvedSkill{name: "skill", rawSHA256: task.RawSHA256, content: "changed", captureReady: true}

	require.Error(t, uploadSkillContent(t.Context(), creds{ServerURL: task.ServerURL, APIKey: task.APIKey, Project: task.Project, Email: "", Org: "", Source: credCache}, acceptedSkillUploadResult(task.RawSHA256, true), skill))
	require.Zero(t, requests.Load())
}

func TestUploadSkillContentSkipsUnrequestedContent(t *testing.T) {
	task := skillUploadTaskForTest("https://example.com", "project", "key", "content")
	skill := &resolvedSkill{name: "skill", rawSHA256: task.RawSHA256, content: task.Content, captureReady: true}

	require.NoError(t, uploadSkillContent(t.Context(), creds{ServerURL: task.ServerURL, APIKey: task.APIKey, Project: task.Project, Email: "", Org: "", Source: credCache}, acceptedSkillUploadResult(task.RawSHA256, false), skill))
}

func skillUploadTaskForTest(serverURL, project, key, content string) skillUploadTask {
	return skillUploadTask{ServerURL: serverURL, Project: project, APIKey: key, RawSHA256: sha256Hex([]byte(content)), Content: content}
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

func TestValidSkillUploadTaskRejectsInvalidValues(t *testing.T) {
	task := skillUploadTaskForTest("https://example.com", "project", "key", "content")
	require.True(t, validSkillUploadTask(task))

	task.RawSHA256 = strings.Repeat("A", 64)
	require.False(t, validSkillUploadTask(task))
}
