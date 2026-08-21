package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

func TestIngestParsesValidSkillCaptureEffect(t *testing.T) {
	hash := strings.Repeat("a", 64)
	capture := ingestSkillCaptureEffect(t, map[string]any{
		"raw_sha256":       hash,
		"content_required": true,
	})

	require.Equal(t, &skillCapture{rawSHA256: hash, contentRequired: true}, capture)
}

func TestIngestIgnoresMalformedSkillCaptureEffect(t *testing.T) {
	require.Nil(t, ingestSkillCaptureEffect(t, "not-a-map"))
	require.Nil(t, ingestSkillCaptureEffect(t, map[string]any{}))
	require.Nil(t, ingestSkillCaptureEffect(t, map[string]any{"raw_sha256": strings.Repeat("a", 63), "content_required": true}))
	require.Nil(t, ingestSkillCaptureEffect(t, map[string]any{"raw_sha256": strings.Repeat("g", 64), "content_required": true}))
}

func TestIngestIgnoresPartialSkillCaptureEffect(t *testing.T) {
	hash := strings.Repeat("b", 64)
	require.Nil(t, ingestSkillCaptureEffect(t, map[string]any{"raw_sha256": hash}))
	require.Nil(t, ingestSkillCaptureEffect(t, map[string]any{"content_required": true}))
}

func TestIngestIgnoresWrongTypedSkillCaptureEffect(t *testing.T) {
	hash := strings.Repeat("c", 64)
	require.Nil(t, ingestSkillCaptureEffect(t, map[string]any{"raw_sha256": []byte(hash), "content_required": true}))
	require.Nil(t, ingestSkillCaptureEffect(t, map[string]any{"raw_sha256": hash, "content_required": "true"}))
}

func TestIngestIgnoresUppercaseSkillCaptureHash(t *testing.T) {
	require.Nil(t, ingestSkillCaptureEffect(t, map[string]any{
		"raw_sha256":       strings.Repeat("A", 64),
		"content_required": true,
	}))
}

func TestRelayEnrichesSkillAndCreatesRequestedUploadTask(t *testing.T) {
	delivery := deliverSkillForTest(t, http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}, requestedSkillCaptureEffects(true), []byte("# Captured skill\n"))

	require.True(t, delivery.result.accepted())
	require.NotNil(t, delivery.payload.Data)
	require.NotNil(t, delivery.payload.Data.Skill)
	require.Nil(t, delivery.payload.Data.Skill.SourceLevel)
	require.Nil(t, delivery.payload.Data.Skill.SourcePath)
	require.Equal(t, delivery.rawSHA256, *delivery.payload.Data.Skill.RawSha256)
	require.Nil(t, delivery.payload.Data.Skill.Source)
	output, ok := delivery.payload.Data.ToolCall.Output.(string)
	require.True(t, ok)
	require.Equal(t, "# Captured skill\n", output)
	require.Equal(t, []skillUploadTask{{
		ServerURL: delivery.serverURL,
		Project:   "default",
		APIKey:    "test-hooks-key",
		RawSHA256: delivery.rawSHA256,
		Content:   "# Captured skill\n",
	}}, delivery.tasks)
}

func TestRelayKnownSkillDoesNotCreateUploadTask(t *testing.T) {
	delivery := deliverSkillForTest(t, http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}, requestedSkillCaptureEffects(false), []byte("known"))

	require.Empty(t, delivery.tasks)
}

func TestRelayMetadataOnlyResponseDoesNotCreateUploadTask(t *testing.T) {
	delivery := deliverSkillForTest(t, http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}, nil, []byte("metadata only"))

	require.Empty(t, delivery.tasks)
}

func TestRelayMismatchedSkillHashDoesNotCreateUploadTask(t *testing.T) {
	effects := func(components.IngestRequestBody) map[string]any {
		return map[string]any{"skill_capture": map[string]any{
			"raw_sha256":       strings.Repeat("0", 64),
			"content_required": true,
		}}
	}
	delivery := deliverSkillForTest(t, http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}, effects, []byte("mismatch"))

	require.Empty(t, delivery.tasks)
}

func TestRelayNon2xxSkillResponseDoesNotCreateUploadTask(t *testing.T) {
	delivery := deliverSkillForTest(t, http.StatusBadRequest, decision{Decision: "", Reason: "bad_request", Message: "bad request"}, requestedSkillCaptureEffects(true), []byte("rejected"))

	require.Empty(t, delivery.tasks)
}

func TestRelayDeniedAcceptedSkillCreatesRequestedUploadTask(t *testing.T) {
	delivery := deliverSkillForTest(t, http.StatusOK, decision{Decision: "deny", Reason: "policy_denied", Message: "blocked"}, requestedSkillCaptureEffects(true), []byte("denied but captured"))

	require.True(t, delivery.result.decision.denied())
	require.Len(t, delivery.tasks, 1)
}

func TestRelayCacheRejectionUsesFinalOrgCredentialsForUploadTask(t *testing.T) {
	capturedTasks := captureSkillUploadTasks(t)
	event, _ := relaySkillEvent(t, []byte("fallback"))
	var requests atomic.Int32
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		if requests.Add(1) == 1 {
			return http.StatusUnauthorized, decision{Decision: "", Reason: "", Message: "stale key"}
		}
		return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
	})
	fs.effects = requestedSkillCaptureEffects(true)
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	require.NoError(t, os.WriteFile(authFile, []byte("server_url="+fs.URL+"\napi_key=rejected-cache-key\nproject=cached-project\norg=org-1\n"), 0o600))
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	t.Setenv("GRAM_HOOKS_DISABLE_LOCAL_AUTH", "")
	cfg := Config{ServerURL: fs.URL, ProjectSlug: "org-project", OrgID: "org-1", HooksAPIKey: "org-key", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}

	result, state := NewRelay(cfg).deliver(t.Context(), event)

	require.True(t, result.accepted())
	require.Equal(t, stateReady, state)
	require.Len(t, fs.headers, 2)
	require.Equal(t, "rejected-cache-key", fs.headers[0].Get("Gram-Key"))
	require.Equal(t, "org-key", fs.headers[1].Get("Gram-Key"))
	tasks := capturedTasks()
	require.Len(t, tasks, 1)
	require.Equal(t, "org-key", tasks[0].APIKey)
	require.Equal(t, "org-project", tasks[0].Project)
	require.NotEqual(t, "rejected-cache-key", tasks[0].APIKey)
}

func TestRelaySpoolsEnrichedUnsentSkillPayload(t *testing.T) {
	setSpoolStateHome(t)
	event, rawSHA256 := relaySkillEvent(t, []byte("offline content"))
	cfg := authedConfig(t, closedPortURL(t))

	result, _ := NewRelay(cfg).deliver(t.Context(), event)

	require.True(t, result.unsent())
	names := spoolFiles(t)
	require.Len(t, names, 1)
	entry := readSpoolEntry(t, names[0])
	skill := entry.Envelope.Data.Skill
	require.NotNil(t, skill)
	require.Nil(t, skill.SourceLevel)
	require.Nil(t, skill.SourcePath)
	require.Equal(t, rawSHA256, *skill.RawSha256)
	require.Equal(t, "offline content", entry.Envelope.Data.ToolCall.Output)
}

func TestIngestRedirectDoesNotForwardKeyAndFailsClosedWithoutSpool(t *testing.T) {
	setSpoolStateHome(t)
	var targetRequests atomic.Int32
	var targetKey atomic.Value
	var sourceKey atomic.Value
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		targetRequests.Add(1)
		targetKey.Store(req.Header.Get("Gram-Key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"decision":"allow"}`))
	}))
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		sourceKey.Store(req.Header.Get("Gram-Key"))
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(redirect.Close)
	cfg := authedConfig(t, redirect.URL)
	event := codexToolEvent(t, t.TempDir(), "Read", map[string]string{"file_path": "ordinary.txt"})

	result, state := NewRelay(cfg).deliver(t.Context(), event)
	verdict := NewRelay(cfg).evaluate(t.Context(), event)

	require.Equal(t, http.StatusFound, result.statusCode)
	require.Equal(t, stateReady, state)
	require.False(t, result.accepted())
	require.False(t, result.unsent())
	require.True(t, verdict.block)
	require.Empty(t, spoolFiles(t))
	require.Equal(t, "test-hooks-key", sourceKey.Load())
	require.Zero(t, targetRequests.Load())
	require.Nil(t, targetKey.Load())
}

func ingestSkillCaptureEffect(t *testing.T, effect any) *skillCapture {
	t.Helper()
	fs := newFakeServer(t, nil)
	fs.effects = func(components.IngestRequestBody) map[string]any {
		return map[string]any{"skill_capture": effect}
	}
	result := newClient(fs.URL).send(t.Context(), creds{ServerURL: fs.URL, APIKey: "key", Project: "project", Email: "", Org: "", Source: credEnv}, components.IngestRequestBody{
		SchemaVersion: schemaVersion,
		Source:        components.HookIngestSource{Adapter: "claude", AdapterVersion: nil, RawEventName: nil, Hostname: nil, UserEmail: nil},
		Session:       nil,
		Event:         components.HookIngestEvent{Type: components.TypeSessionUpdated, OccurredAt: nil},
		Data:          nil,
		Raw:           nil,
	}, newIdempotencyToken())
	return result.skillCapture
}

type skillDelivery struct {
	serverURL string
	rawSHA256 string
	payload   components.IngestRequestBody
	result    ingestResult
	tasks     []skillUploadTask
}

func deliverSkillForTest(
	t *testing.T,
	status int,
	responseDecision decision,
	effects func(components.IngestRequestBody) map[string]any,
	content []byte,
) skillDelivery {
	t.Helper()
	capturedTasks := captureSkillUploadTasks(t)
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return status, responseDecision
	})
	fs.effects = effects
	cfg := authedConfig(t, fs.URL)
	event, rawSHA256 := relaySkillEvent(t, content)
	result, _ := NewRelay(cfg).deliver(t.Context(), event)
	return skillDelivery{
		serverURL: fs.URL,
		rawSHA256: rawSHA256,
		payload:   fs.last(),
		result:    result,
		tasks:     capturedTasks(),
	}
}

func requestedSkillCaptureEffects(contentRequired bool) func(components.IngestRequestBody) map[string]any {
	return func(payload components.IngestRequestBody) map[string]any {
		return map[string]any{"skill_capture": map[string]any{
			"raw_sha256":       *payload.Data.Skill.RawSha256,
			"content_required": contentRequired,
		}}
	}
}

func captureSkillUploadTasks(t *testing.T) func() []skillUploadTask {
	t.Helper()
	original := executeSkillUpload
	t.Cleanup(func() { executeSkillUpload = original })
	var tasks []skillUploadTask
	executeSkillUpload = func(_ context.Context, task skillUploadTask) error {
		tasks = append(tasks, task)
		return nil
	}
	return func() []skillUploadTask { return tasks }
}

func relaySkillEvent(t *testing.T, content []byte) (*agenthooks.ToolPostEvent, string) {
	t.Helper()
	cwd := t.TempDir()
	output, err := json.Marshal(string(content))
	require.NoError(t, err)
	input, err := json.Marshal(map[string]string{"skill": "dno-441-relay-capture"})
	require.NoError(t, err)
	return &agenthooks.ToolPostEvent{
		Event:  agenthooks.Event{Provider: agenthooks.ProviderClaudeCode, Kind: agenthooks.KindToolPost, Session: agenthooks.SessionInfo{CWD: cwd}},
		Tool:   agenthooks.ToolCall{Name: "Skill", Input: input},
		Output: output,
		Failed: false,
	}, sha256Hex(content)
}
