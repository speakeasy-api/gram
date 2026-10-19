package assistants

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/assistants/kitacp"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"

	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestKitProtocolStatusCapability(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, raw string
		want      bool
	}{
		{"advertised", `{"capabilities":{"_meta":{"kit/session/status":{}}}}`, true},
		{"absent", `{"capabilities":{}}`, false},
		{"null", `{"capabilities":{"_meta":{"kit/session/status":null}}}`, false},
		{"wrong type", `{"capabilities":{"_meta":{"kit/session/status":true}}}`, false},
		{"invalid", `{`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, kitStatusCapability(json.RawMessage(tc.raw)))
		})
	}
}

func TestKitProtocolSettlementFailsClosed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, raw        string
		settled, invalid bool
	}{
		{"settled", `{"sessionId":"s","settled":true,"foregroundActive":false,"backgroundActive":false,"pendingBackgroundTerminals":false,"activeSubagents":0}`, true, false},
		{"background despite idle", `{"sessionId":"s","settled":false,"foregroundActive":false,"backgroundActive":true,"pendingBackgroundTerminals":false,"activeSubagents":0}`, false, false},
		{"inconsistent", `{"sessionId":"s","settled":true,"foregroundActive":false,"backgroundActive":false,"pendingBackgroundTerminals":true,"activeSubagents":0}`, false, false},
		{"idle only", `{"sessionId":"s","state":"idle"}`, false, true},
		{"wrong session", `{"sessionId":"other","settled":true,"foregroundActive":false,"backgroundActive":false,"pendingBackgroundTerminals":false,"activeSubagents":0}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := kitSettled(json.RawMessage(tc.raw), "s")
			require.Equal(t, tc.settled, got)
			require.Equal(t, tc.invalid, err != nil)
		})
	}
}

func TestKitProtocolSwitchCannotAcquireDatabaseLock(t *testing.T) {
	t.Parallel()
	// A zero-value adapter deliberately has no database: switch admission must
	// refuse without attempting the helpers that reacquire the caller's lock.
	p := &KitRuntimeProtocol{}
	ready, err := p.PrepareRuntimeSwitch(context.Background(), assistantRecord{}, nil, 2)
	require.Error(t, err)
	require.False(t, ready.Settled)
	require.False(t, ready.DestinationReady)
}
func TestKitProtocolRejectsIncompleteConfig(t *testing.T) {
	t.Parallel()
	_, err := NewKitRuntimeProtocol(KitRuntimeProtocolConfig{})
	require.Error(t, err)
}

func TestKitProtocolDrainsQueuedProjection(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	notifications := make(chan kitacp.Notification, 3)
	for range 3 {
		notifications <- kitacp.Notification{}
	}
	flush := make(chan chan error)
	done := make(chan error, 1)
	seen := 0
	go func() {
		done <- kitConsumeNotifications(ctx, notifications, flush, func(kitacp.Notification) error { seen++; return nil })
	}()
	ack := make(chan error, 1)
	flush <- ack
	require.NoError(t, <-ack)
	require.Equal(t, 3, seen)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

// Exercise real HTTP/SSE with the assistant advisory lock held on another
// connection. Reacquiring that lock in settlement would time out this test.
func TestKitProtocolSwitchUnderAdmissionLock(t *testing.T) {
	t.Parallel()
	core, db, key, _ := kitSessionFixture(t)
	_, err := core.StoreKitRuntimeSession(t.Context(), key, "session-one")
	require.NoError(t, err)
	var mu sync.Mutex
	streams := map[string]chan string{}
	observed := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			w.WriteHeader(401)
			return
		}
		session := r.Header.Get("Acp-Session-Id")
		switch r.Method {
		case http.MethodPost:
			var msg struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if json.NewDecoder(r.Body).Decode(&msg) != nil {
				w.WriteHeader(400)
				return
			}
			if msg.Method == "initialize" {
				w.Header().Set("Acp-Connection-Id", "connection")
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{"_meta":{"kit/session/status":{}}}}}`, msg.ID)
				return
			}
			mu.Lock()
			ch := streams[session]
			mu.Unlock()
			if ch == nil || msg.Method != "kit/session/status" {
				w.WriteHeader(400)
				return
			}
			mu.Lock()
			observed++
			mu.Unlock()
			w.WriteHeader(202)
			ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"sessionId\":\"session-one\",\"settled\":true,\"foregroundActive\":false,\"backgroundActive\":false,\"pendingBackgroundTerminals\":false,\"activeSubagents\":0}}\n\n", msg.ID)
		case http.MethodGet:
			ch := make(chan string, 8)
			mu.Lock()
			streams[session] = ch
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			flushKitTestResponse(t, w)
			for {
				select {
				case data := <-ch:
					_, _ = fmt.Fprint(w, data)
					flushKitTestResponse(t, w)
				case <-r.Context().Done():
					return
				}
			}
		case http.MethodDelete:
			w.WriteHeader(202)
		}
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL + "/acp/v2")
	require.NoError(t, err)
	p, err := NewKitRuntimeProtocol(KitRuntimeProtocolConfig{Core: core, Endpoint: endpoint, Projector: &KitTranscriptProjector{}, CWD: "/workspace", GatewayToken: "fixture-token", HTTPClient: kitProtocolTestHTTPClient(t)})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	tx := testenv.BeginTx(t, ctx, db)
	defer func() { _ = tx.Rollback(context.Background()) }()
	require.NoError(t, assistantrepo.New(tx).AcquireAssistantAdvisoryLock(ctx, key.AssistantID.String()))
	r := assistantRuntimeRecord{ID: key.RuntimeID, AssistantID: key.AssistantID, ProjectID: key.ProjectID, RuntimeVersion: 3}
	ready, err := p.PrepareRuntimeSwitch(ctx, assistantRecord{ID: key.AssistantID, ProjectID: key.ProjectID}, []assistantRuntimeRecord{r}, 2)
	require.NoError(t, err)
	require.True(t, ready.Settled)
	require.NotNil(t, ready.Release)
	mu.Lock()
	require.Equal(t, 1, observed)
	mu.Unlock()
}

func TestKitProtocolRunTurnPersistsReplaysAndProjects(t *testing.T) {
	t.Parallel()
	core, db, key, chatID := kitSessionFixture(t)
	currentID, err := testrepo.New(db).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{ProjectID: uuid.NullUUID{UUID: key.ProjectID, Valid: true}, ChatID: chatID, Role: "user", Content: "hello"})
	require.NoError(t, err)
	err = assistantrepo.New(db).SetKitThreadSourceFixture(t.Context(), assistantrepo.SetKitThreadSourceFixtureParams{ProjectID: key.ProjectID, ThreadID: key.ThreadID, SourceKind: "dashboard"})
	require.NoError(t, err)
	source, _ := json.Marshal(map[string]string{"idempotency_key": currentID.String()})
	eventID, err := assistantrepo.New(db).InsertAssistantThreadEvent(t.Context(), assistantrepo.InsertAssistantThreadEventParams{AssistantThreadID: key.ThreadID, AssistantID: key.AssistantID, ProjectID: key.ProjectID, EventID: uuid.NewString(), CorrelationID: "kit-turn", Status: "processing", NormalizedPayloadJson: []byte(`{}`), SourcePayloadJson: source})
	require.NoError(t, err)
	ids, err := assistantrepo.New(db).ResolveKitCurrentUserMessage(t.Context(), assistantrepo.ResolveKitCurrentUserMessageParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID, EventID: eventID})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{currentID}, ids)

	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), db, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.Background()) })
	projector := NewKitTranscriptProjector(db, writer, nil, func(ctx context.Context, tx pgx.Tx, b KitTranscriptBinding) error {
		_, err := core.ValidateKitRuntimeSessionInTx(ctx, tx, key, b.SessionID)
		return err
	})
	var mu sync.Mutex
	streams := map[string]chan string{}
	methods := []string{}
	prompts := 0
	selectedModel := "speakeasy:gpt-5.4"
	catalog := func() string {
		return fmt.Sprintf(`{"configOptions":[{"configId":"model","type":"select","currentValue":%q,"options":[{"groupId":"speakeasy","name":"Speakeasy","options":[{"value":"speakeasy:openai/gpt-4.1-mini","name":"openai/gpt-4.1-mini"},{"value":"speakeasy:openai/gpt-4.1","name":"openai/gpt-4.1"}]}]}]}`, selectedModel)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			w.WriteHeader(401)
			return
		}
		session := r.Header.Get("Acp-Session-Id")
		switch r.Method {
		case http.MethodPost:
			var msg struct {
				ID     json.RawMessage            `json:"id"`
				Method string                     `json:"method"`
				Params map[string]json.RawMessage `json:"params"`
			}
			if json.NewDecoder(r.Body).Decode(&msg) != nil {
				w.WriteHeader(400)
				return
			}
			if msg.Method == "initialize" {
				w.Header().Set("Acp-Connection-Id", "connection")
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{"_meta":{"kit/session/status":{}}}}}`, msg.ID)
				return
			}
			mu.Lock()
			ch := streams[session]
			methods = append(methods, msg.Method)
			mu.Unlock()
			if ch == nil {
				t.Error("request before session attachment")
				w.WriteHeader(400)
				return
			}
			result := `{}`
			switch msg.Method {
			case "session/new", "session/resume":
				if _, supplied := msg.Params["mcpServers"]; supplied {
					t.Error("filesystem-owned MCP must not replace the session overlay")
				}
				var meta map[string]string
				_ = json.Unmarshal(msg.Params["_meta"], &meta)
				if meta["ai.speakeasy/gram-chat-id"] != chatID.String() {
					t.Error("missing canonical chat metadata")
				}
				if msg.Method == "session/new" {
					result = `{"sessionId":"session-one"}`
				} else if string(msg.Params["replayFrom"]) != `{"type":"start"}` {
					t.Error("missing replay")
				}
				if msg.Method == "session/resume" {
					result = catalog()
				}
				if msg.Method == "session/resume" && prompts > 0 {
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"session-one","update":{"sessionUpdate":"user_message","messageId":"authoritative-user-message-1","content":[{"type":"text","text":"hello"}]}}}` + "\n\n"
				}

			case "session/set_config_option":
				var configID, value, kind string
				_ = json.Unmarshal(msg.Params["configId"], &configID)
				_ = json.Unmarshal(msg.Params["value"], &value)
				_ = json.Unmarshal(msg.Params["type"], &kind)
				expected := "speakeasy:openai/gpt-4.1-mini"
				if prompts > 0 {
					expected = "speakeasy:openai/gpt-4.1"
				}
				if configID != "model" || kind != "id" || value != expected {
					t.Error("incorrect persisted model selection RPC")
					w.WriteHeader(400)
					return
				}
				selectedModel = value
				result = catalog()
			case "session/prompt":
				expected := "speakeasy:openai/gpt-4.1-mini"
				if prompts > 0 {
					expected = "speakeasy:openai/gpt-4.1"
				}
				if selectedModel != expected {
					t.Error("prompt before persisted model applied")
				}
				prompts++
				// User notification can win the race with acknowledgement delivery.
				ch <- fmt.Sprintf(`data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"session-one","update":{"sessionUpdate":"user_message","messageId":"authoritative-user-message-%d","content":[{"type":"text","text":"hello"}]}}}`, prompts) + "\n\n"
				saved, found, err := core.LoadKitRuntimeSession(r.Context(), key)
				if err != nil || !found || saved.SessionID != session {
					t.Error("prompt before persisted mapping")
				}
				result = fmt.Sprintf(`{"messageId":"authoritative-user-message-%d"}`, prompts)
			case "kit/session/status":
				// Deliberately queue the final full replacement immediately ahead of the
				// response. RunTurn may not return before its projector commits this row.
				ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"session-one","update":{"sessionUpdate":"agent_message","messageId":"reply","content":[{"type":"text","text":"projected final"}]}}}` + "\n\n"
				result = `{"sessionId":"session-one","settled":true,"foregroundActive":false,"backgroundActive":false,"pendingBackgroundTerminals":false,"activeSubagents":0}`
			case "session/cancel":
				w.WriteHeader(202)
				return
			default:
				t.Errorf("unexpected method %s", msg.Method)
			}
			w.WriteHeader(202)
			ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, result)
		case http.MethodGet:
			ch := make(chan string, 8)
			mu.Lock()
			streams[session] = ch
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			flushKitTestResponse(t, w)
			for {
				select {
				case data := <-ch:
					_, _ = fmt.Fprint(w, data)
					flushKitTestResponse(t, w)
				case <-r.Context().Done():
					return
				}
			}
		case http.MethodDelete:
			w.WriteHeader(202)
		}
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL + "/acp/v2")
	require.NoError(t, err)
	p, err := NewKitRuntimeProtocol(KitRuntimeProtocolConfig{Core: core, Endpoint: endpoint, Projector: projector, CWD: "/workspace", GatewayToken: "fixture-token", HTTPClient: kitProtocolTestHTTPClient(t)})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	r := assistantRuntimeRecord{ID: key.RuntimeID, ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeVersion: 3}
	require.NoError(t, p.RunTurn(ctx, r, runTurnRequest{Model: "openai/gpt-4.1-mini", ThreadID: key.ThreadID, Prompt: "hello", EventID: eventID, CanonicalUserMessageID: currentID}))
	// A new adapter reconstructs the durable correlation before replay.
	restarted, err := NewKitRuntimeProtocol(p.config)
	require.NoError(t, err)
	require.NoError(t, restarted.RunTurn(ctx, r, runTurnRequest{Model: "openai/gpt-4.1", ThreadID: key.ThreadID, Prompt: "hello", EventID: eventID, CanonicalUserMessageID: currentID}))
	messages, err := chatrepo.New(db).ListLatestGenerationChatMessages(ctx, chatrepo.ListLatestGenerationChatMessagesParams{ProjectID: key.ProjectID, ChatID: chatID})
	require.NoError(t, err)
	users, finals := 0, 0
	for _, message := range messages {
		if message.Role == "user" {
			users++
		}
		if message.Role == "assistant" && strings.Contains(message.Content, "projected final") {
			finals++
		}
	}
	require.Equal(t, 1, users)
	require.Equal(t, 1, finals)
	interrupted, err := p.InterruptTurn(ctx, r, key.ThreadID)
	require.NoError(t, err)
	require.True(t, interrupted)
	mu.Lock()
	require.Equal(t, []string{"session/new", "session/resume", "session/set_config_option", "session/prompt", "kit/session/status", "session/resume", "session/set_config_option", "session/prompt", "kit/session/status", "session/cancel"}, methods)
	mu.Unlock()
}

func TestKitProtocolPromptContent(t *testing.T) {
	t.Parallel()
	prompt, err := kitProtocolPrompt(runTurnRequest{Prompt: "hello", InputParts: []runtimeContentPart{{Type: "image_url", ImageURL: &runtimeImageURL{URL: "data:image/png;base64,aGVsbG8="}}}})
	require.NoError(t, err)
	require.Equal(t, "image", prompt[1]["type"])
	require.Equal(t, "image/png", prompt[1]["mimeType"])
	_, err = kitProtocolPrompt(runTurnRequest{InputParts: []runtimeContentPart{{Type: "image_url", ImageURL: &runtimeImageURL{URL: "http://example.invalid/image.png"}}}})
	require.Error(t, err)
}

func TestKitProtocolModelSelectionFailsClosed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, model, raw string
		valid            bool
	}{
		{"flat", "openai/gpt-4.1-mini", `{"configOptions":[{"configId":"model","type":"select","currentValue":"speakeasy:openai/gpt-4.1-mini","options":[{"value":"speakeasy:openai/gpt-4.1-mini"}]}]}`, true},
		{"empty persisted choice", "", `{"configOptions":[]}`, false},
		{"missing catalog", "openai/gpt-4.1-mini", `{}`, false},
		{"other provider not equivalent", "openai/gpt-4.1-mini", `{"configOptions":[{"configId":"model","type":"select","options":[{"value":"openrouter:openai/gpt-4.1-mini"}]}]}`, false},
		{"display name not identity", "openai/gpt-4.1-mini", `{"configOptions":[{"configId":"model","type":"select","options":[{"value":"speakeasy:gpt-5.4","name":"openai/gpt-4.1-mini"}]}]}`, false},
		{"malformed", "openai/gpt-4.1-mini", `{`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			selection, _, err := kitProtocolModelOption(json.RawMessage(tc.raw), tc.model)
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, "speakeasy:"+tc.model, selection)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func kitProtocolTestHTTPClient(t *testing.T) *guardian.HTTPClient {
	t.Helper()
	return guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)).Client(guardian.WithAllowedCIDRBlocks("127.0.0.1/32"))
}
func flushKitTestResponse(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	flusher, ok := w.(http.Flusher)
	if !ok {
		t.Error("test server requires HTTP flusher")
		return
	}
	flusher.Flush()
}
