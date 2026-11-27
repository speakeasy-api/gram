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

	"github.com/stretchr/testify/assert"
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

func TestKitProtocolPromptDeliveryCapability(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, raw, mode string
		want            bool
	}{
		{"queue and steer", `{"capabilities":{"session":{"prompt":{"delivery":{"steer":{},"queue":{}}}}}}`, kitPromptDeliverySteer, true},
		{"steer only", `{"capabilities":{"session":{"prompt":{"delivery":{"steer":{}}}}}}`, kitPromptDeliverySteer, true},
		{"queue only", `{"capabilities":{"session":{"prompt":{"delivery":{"queue":{}}}}}}`, kitPromptDeliverySteer, false},
		{"absent delivery", `{"capabilities":{"_meta":{"kit/session/status":{}}}}`, kitPromptDeliverySteer, false},
		{"null mode", `{"capabilities":{"session":{"prompt":{"delivery":{"steer":null}}}}}`, kitPromptDeliverySteer, false},
		{"wrong type", `{"capabilities":{"session":{"prompt":{"delivery":{"steer":true}}}}}`, kitPromptDeliverySteer, false},
		{"invalid", `{`, kitPromptDeliverySteer, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, kitPromptDeliveryCapability(json.RawMessage(tc.raw), tc.mode))
		})
	}
}

func TestKitProtocolPromptIdempotencyKey(t *testing.T) {
	t.Parallel()
	eventID := uuid.New()
	for _, tc := range []struct {
		name    string
		req     runTurnRequest
		want    string
		wantErr bool
	}{
		{"explicit key", runTurnRequest{IdempotencyKey: "turn-123", EventID: eventID, EventAttempt: 1}, "turn-123", false},
		{"retry keeps same key", runTurnRequest{IdempotencyKey: "turn-123", EventID: eventID, EventAttempt: 3}, "turn-123", false},
		{"falls back to event ID", runTurnRequest{EventID: eventID, EventAttempt: 2}, eventID.String(), false},
		{"empty when no identity", runTurnRequest{}, "", false},
		{"max length allowed", runTurnRequest{IdempotencyKey: strings.Repeat("k", kitPromptIdempotencyKeyMaxRunes)}, strings.Repeat("k", kitPromptIdempotencyKeyMaxRunes), false},
		{"exceeds max length", runTurnRequest{IdempotencyKey: strings.Repeat("k", kitPromptIdempotencyKeyMaxRunes+1)}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := kitPromptIdempotencyKey(tc.req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
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
	for _, busy := range []bool{false, true} {
		t.Run(fmt.Sprintf("background_busy_%t", busy), func(t *testing.T) {
			t.Parallel()
			core, db, key, _ := kitSessionFixture(t)
			_, err := core.StoreKitRuntimeSession(t.Context(), key, "session-one")
			require.NoError(t, err)
			_, err = assistantrepo.New(db).CancelPendingAssistantThreadEvents(t.Context(), assistantrepo.CancelPendingAssistantThreadEventsParams{ProjectID: key.ProjectID, ThreadID: key.ThreadID, PendingStatus: eventStatusPending, CancelledStatus: eventStatusCancelled})
			require.NoError(t, err)
			var mu sync.Mutex
			streams := map[string]chan string{}
			observed := 0
			resumed := false
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
					mu.Unlock()
					if ch != nil && msg.Method == "session/resume" {
						assert.NotContains(t, msg.Params, "replayFrom")
						assert.NotContains(t, msg.Params, "mcpServers")
						assert.NotContains(t, msg.Params, "_meta")
						resumed = true
						w.WriteHeader(202)
						ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{}}\n\n", msg.ID)
						return
					}
					if ch == nil || msg.Method != "kit/session/status" || !resumed {
						w.WriteHeader(400)
						return
					}
					mu.Lock()
					observed++
					mu.Unlock()
					w.WriteHeader(202)
					ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"sessionId\":\"session-one\",\"settled\":%t,\"foregroundActive\":false,\"backgroundActive\":%t,\"pendingBackgroundTerminals\":false,\"activeSubagents\":0}}\n\n", msg.ID, !busy, busy)
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
			admitted, err := assistantrepo.New(tx).AssistantHasUnsettledEvents(ctx, assistantrepo.AssistantHasUnsettledEventsParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID})
			require.NoError(t, err)
			require.True(t, admitted.Valid)
			require.False(t, admitted.Bool)
			r := assistantRuntimeRecord{ID: key.RuntimeID, AssistantID: key.AssistantID, ProjectID: key.ProjectID, RuntimeVersion: 3}
			ready, err := p.PrepareRuntimeSwitch(ctx, assistantRecord{ID: key.AssistantID, ProjectID: key.ProjectID}, []assistantRuntimeRecord{r}, 2)
			require.NoError(t, err)
			require.Equal(t, !busy, ready.Settled)
			require.NotNil(t, ready.Release)
			mu.Lock()
			require.Equal(t, 1, observed)
			mu.Unlock()
		})
	}
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
	eventID2, err := assistantrepo.New(db).InsertAssistantThreadEvent(t.Context(), assistantrepo.InsertAssistantThreadEventParams{AssistantThreadID: key.ThreadID, AssistantID: key.AssistantID, ProjectID: key.ProjectID, EventID: uuid.NewString(), CorrelationID: "kit-turn", Status: "processing", NormalizedPayloadJson: []byte(`{}`), SourcePayloadJson: source})
	require.NoError(t, err)
	ids, err := assistantrepo.New(db).ResolveKitCurrentUserMessage(t.Context(), assistantrepo.ResolveKitCurrentUserMessageParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID, EventID: eventID})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{currentID}, ids)

	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), db, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.Background()) })
	projector := NewKitTranscriptProjector(db, writer, nil, func(ctx context.Context, tx pgx.Tx, b KitTranscriptBinding) error {
		// Exercise transport backpressure with a deliberately slow real consumer.
		select {
		case <-time.After(time.Millisecond):
		case <-ctx.Done():
			return fmt.Errorf("waiting for slow consumer: %w", ctx.Err())
		}
		_, err := core.ValidateKitRuntimeSessionInTx(ctx, tx, key, b.SessionID)
		return err
	})
	var mu sync.Mutex
	streams := map[string]chan string{}
	methods := []string{}
	connections := 0
	controls := map[string]bool{}
	prompts := 0
	selectedModel := "speakeasy:gpt-5.4"
	selectedEffort := "medium"
	catalog := func() string {
		return fmt.Sprintf(`{"configOptions":[{"configId":"model","type":"select","currentValue":%q,"options":[{"groupId":"speakeasy","name":"Speakeasy","options":[{"value":"speakeasy:openai/gpt-4.1-mini","name":"openai/gpt-4.1-mini"},{"value":"speakeasy:openai/gpt-4.1","name":"openai/gpt-4.1"}]}]},{"configId":"reasoning_effort","type":"select","currentValue":%q,"options":[{"value":"low"},{"value":"medium"},{"value":"high"}]}]}`, selectedModel, selectedEffort)
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
				mu.Lock()
				connections++
				connection := fmt.Sprintf("connection-%d", connections)
				mu.Unlock()
				w.Header().Set("Acp-Connection-Id", connection)
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{"_meta":{"kit/session/status":{}},"session":{"prompt":{"delivery":{"steer":{},"queue":{}}}}}}}`, msg.ID)
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
			// Session SSE attachment is not gateway control. Uncontrolled
			// notifications are admitted with 202 but never execute.
			mu.Lock()
			controlled := controls[r.Header.Get("Acp-Connection-Id")]
			mu.Unlock()
			if msg.Method != "session/new" && msg.Method != "session/resume" && !controlled {
				w.WriteHeader(http.StatusAccepted)
				if len(msg.ID) > 0 {
					ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"error\":{\"code\":-32000,\"message\":\"resume session before controlling it\"}}\n\n", msg.ID)
				}
				return
			}
			result := `{}`
			switch msg.Method {
			case "session/new", "session/resume":
				// Match the pinned gateway: creation already grants connection control.
				connection := r.Header.Get("Acp-Connection-Id")
				mu.Lock()
				alreadyControls := controls[connection]
				controls[connection] = true
				mu.Unlock()
				if msg.Method == "session/resume" && alreadyControls {
					w.WriteHeader(http.StatusAccepted)
					ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"error\":{\"code\":-32000,\"message\":\"connection already controls session\"}}\n\n", msg.ID)
					return
				}
				if _, supplied := msg.Params["mcpServers"]; supplied {
					t.Error("filesystem-owned MCP must not replace the session overlay")
				}
				assert.NotContains(t, msg.Params, "_meta", "chat identity belongs to the control plane")
				if msg.Method == "session/new" {
					result = strings.TrimSuffix(catalog(), "}") + `,"sessionId":"session-one"}`
				} else if string(msg.Params["replayFrom"]) != `{"type":"start"}` {
					t.Error("missing replay")
				}
				if msg.Method == "session/resume" {
					result = catalog()
				}
				if msg.Method == "session/resume" && prompts > 0 {
					// An unknown historical user must not wait for the new prompt's
					// acknowledgement. Replay exceeds the transport queue, and the
					// response is deliberately emitted only after the entire replay.
					for range 300 {
						select {
						case ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"session-one","update":{"sessionUpdate":"user_message","messageId":"historical-user","content":[{"type":"text","text":"historical"}]}}}` + "\n\n":
						case <-r.Context().Done():
							return
						}
					}
					// Resident gateway replay is the original ordered journal,
					// including chunks with their original live message ID.
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"session-one","update":{"sessionUpdate":"agent_message_chunk","messageId":"reply","content":{"type":"text","text":"projected "}}}}` + "\n\n"
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"session-one","update":{"sessionUpdate":"agent_message_chunk","messageId":"reply","content":{"type":"text","text":"final"}}}}` + "\n\n"
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
				if configID == "reasoning_effort" {
					expectedEffort := "low"
					if prompts > 0 {
						expectedEffort = "high"
					}
					if kind != "id" || value != expectedEffort {
						t.Error("incorrect persisted reasoning effort selection RPC")
					}
					selectedEffort = value
					result = catalog()
					break
				}
				if configID != "model" || kind != "id" || value != expected {
					t.Error("incorrect persisted model selection RPC")
					w.WriteHeader(400)
					return
				}
				selectedModel = value
				result = catalog()
			case "session/prompt":
				var delivery, idempotencyKey string
				_ = json.Unmarshal(msg.Params["delivery"], &delivery)
				_ = json.Unmarshal(msg.Params["idempotencyKey"], &idempotencyKey)
				if delivery != kitPromptDeliverySteer {
					t.Errorf("expected steer delivery mode, got %q", delivery)
				}
				expectedKey := eventID.String()
				if prompts > 0 {
					expectedKey = eventID2.String()
				}
				if idempotencyKey != expectedKey {
					t.Errorf("expected idempotencyKey %q, got %q", expectedKey, idempotencyKey)
				}
				if strings.Contains(string(msg.Params["prompt"]), kitHistoryDroppedNotice) {
					t.Error("history notice sent for a conversation without earlier assistant turns")
				}
				expected := "speakeasy:openai/gpt-4.1-mini"
				if prompts > 0 {
					expected = "speakeasy:openai/gpt-4.1"
				}
				expectedEffort := "low"
				if prompts > 0 {
					expectedEffort = "high"
				}
				if selectedEffort != expectedEffort {
					t.Error("prompt before persisted reasoning effort applied")
				}
				if selectedModel != expected {
					t.Error("prompt before persisted model applied")
				}
				prompts++
				// User notification can win the race with acknowledgement delivery.
				ch <- fmt.Sprintf(`data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"session-one","update":{"sessionUpdate":"user_message","messageId":"authoritative-user-message-%d","content":[{"type":"text","text":"hello"}]}}}`, prompts) + "\n\n"
				// The live echo also must not block the same-stream response.
				// Agent updates can arrive while acknowledgement is delayed.
				for range 300 {
					select {
					case ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"session-one","update":{"sessionUpdate":"agent_message_chunk","messageId":"reply","content":{"type":"text","text":""}}}}` + "\n\n":
					case <-r.Context().Done():
						return
					}
				}
				saved, found, err := core.LoadKitRuntimeSession(r.Context(), key)
				if err != nil || !found || saved.SessionID != session {
					t.Error("prompt before persisted mapping")
				}
				result = fmt.Sprintf(`{"messageId":"authoritative-user-message-%d"}`, prompts)
			case "kit/session/status":
				// Pinned Kit normal live completion emits chunks only, not a
				// final full replacement. Settlement must await their writes.
				if prompts == 1 {
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"session-one","update":{"sessionUpdate":"agent_message_chunk","messageId":"reply","content":{"type":"text","text":"projected "}}}}` + "\n\n"
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"session-one","update":{"sessionUpdate":"agent_message_chunk","messageId":"reply","content":{"type":"text","text":"final"}}}}` + "\n\n"
				}
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
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	r := assistantRuntimeRecord{ID: key.RuntimeID, ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeVersion: 3}
	interrupted, err := p.InterruptTurn(ctx, r, key.ThreadID)
	require.NoError(t, err)
	require.False(t, interrupted)
	mu.Lock()
	require.Zero(t, connections, "missing session must not dial")
	mu.Unlock()
	require.NoError(t, p.RunTurn(ctx, r, runTurnRequest{Model: "openai/gpt-4.1-mini", ReasoningEffort: "low", ThreadID: key.ThreadID, Prompt: "hello", EventID: eventID, IdempotencyKey: eventID.String(), CanonicalUserMessageID: currentID}))
	// A new adapter reconstructs the durable correlation before replay.
	restarted, err := NewKitRuntimeProtocol(p.config)
	require.NoError(t, err)
	require.NoError(t, restarted.RunTurn(ctx, r, runTurnRequest{Model: "openai/gpt-4.1", ReasoningEffort: "high", ThreadID: key.ThreadID, Prompt: "hello", EventID: eventID2, IdempotencyKey: eventID2.String(), CanonicalUserMessageID: currentID}))
	messages, err := chatrepo.New(db).ListLatestGenerationChatMessages(ctx, chatrepo.ListLatestGenerationChatMessagesParams{ProjectID: key.ProjectID, ChatID: chatID})
	require.NoError(t, err)
	users, finals := 0, 0
	for _, message := range messages {
		if message.Role == "user" {
			users++
		}
		if message.Role == "assistant" && message.Content == "projected final" {
			finals++
		}
	}
	require.Equal(t, 2, users, "historical user plus canonical dashboard user; live echoes must not duplicate")
	require.Equal(t, 1, finals)
	// Desired-set removal cannot be expressed by an additive MCP upsert.
	// Reject before opening another transport or submitting another prompt.
	for _, desired := range [][]runtimeMCPServer{{}, {{ID: "client", URL: "https://mcp.example/mcp"}}} {
		err := p.RunTurn(ctx, r, runTurnRequest{Model: "openai/gpt-4.1", ReasoningEffort: "high", ThreadID: key.ThreadID, Prompt: "must not send", MCPServers: desired})
		require.ErrorIs(t, err, ErrKitMCPDesiredSetUnsupported)
	}
	interrupted, err = p.InterruptTurn(ctx, r, key.ThreadID)
	require.NoError(t, err)
	require.False(t, interrupted)
	mu.Lock()
	require.Equal(t, []string{"session/new", "session/set_config_option", "session/set_config_option", "session/prompt", "kit/session/status", "session/resume", "session/set_config_option", "session/set_config_option", "session/prompt", "kit/session/status"}, methods)
	require.Equal(t, 2, connections, "interrupt must not dial or replace projector control")
	mu.Unlock()
}

func TestKitProtocolPromptContent(t *testing.T) {
	t.Parallel()
	prompt, err := kitProtocolPrompt(runTurnRequest{Prompt: "hello", InputParts: []runtimeContentPart{{Type: "image_url", ImageURL: &runtimeImageURL{URL: "data:image/png;base64,aGVsbG8="}}}}, false)
	require.NoError(t, err)
	require.Equal(t, "image", prompt[1]["type"])
	require.Equal(t, "image/png", prompt[1]["mimeType"])
	_, err = kitProtocolPrompt(runTurnRequest{InputParts: []runtimeContentPart{{Type: "image_url", ImageURL: &runtimeImageURL{URL: "http://example.invalid/image.png"}}}}, false)
	require.Error(t, err)
}

func TestKitProtocolPromptNoticesDroppedHistory(t *testing.T) {
	t.Parallel()
	prompt, err := kitProtocolPrompt(runTurnRequest{SourceInstructions: "source", Prompt: "hello"}, true)
	require.NoError(t, err)
	require.Len(t, prompt, 3)
	require.Equal(t, "source", prompt[0]["text"])
	require.Equal(t, kitHistoryDroppedNotice, prompt[1]["text"])
	require.Equal(t, "hello", prompt[2]["text"])
	prompt, err = kitProtocolPrompt(runTurnRequest{Prompt: "hello"}, false)
	require.NoError(t, err)
	require.Len(t, prompt, 1)
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

func TestKitProtocolSourceContextIsPerTurn(t *testing.T) {
	t.Parallel()
	contexts := []string{}
	for _, channel := range []string{"CHANNEL_ONE", "CHANNEL_TWO"} {
		source, err := composeInstructions("", assistantThreadRecord{SourceKind: sourceKindSlack, SourceRefJSON: []byte(`{"channel_id":"` + channel + `","thread_id":"123.456"}`)}, nil, "")
		require.NoError(t, err)
		prompt, err := kitProtocolPrompt(runTurnRequest{SourceInstructions: source, Prompt: "Please reply"}, false)
		require.NoError(t, err)
		require.Len(t, prompt, 2)
		require.Equal(t, "Please reply", prompt[1]["text"])
		instructions, ok := prompt[0]["text"].(string)
		require.True(t, ok)
		require.Contains(t, instructions, "Text responses are not delivered to the user")
		require.Contains(t, instructions, "call a Slack post tool")
		require.Contains(t, instructions, "Surface AuthURL to owner verbatim")
		require.Contains(t, instructions, "Never expose AuthURL to non-owners")
		require.Contains(t, instructions, "ChannelID: "+channel)
		require.Contains(t, instructions, "ThreadID: 123.456")
		contexts = append(contexts, instructions)
	}
	require.NotContains(t, contexts[0], "CHANNEL_TWO")
	require.NotContains(t, contexts[1], "CHANNEL_ONE")
	prompt, err := kitProtocolPrompt(runTurnRequest{Prompt: "No Slack context"}, false)
	require.NoError(t, err)
	require.Len(t, prompt, 1)
	require.Equal(t, "No Slack context", prompt[0]["text"])
}

func TestKitProtocolPreservesUpstreamSignInAction(t *testing.T) {
	t.Parallel()
	source, err := composeInstructions("", assistantThreadRecord{SourceKind: sourceKindSlack, SourceRefJSON: []byte(`{"channel_id":"CHANNEL","thread_id":"123.456"}`)}, nil, "")
	require.NoError(t, err)
	action := "<message-context>\nEventType: assistant_mcp_auth_required\nMCPSlug: tools\nAuthURL: https://auth.example/authorize?state=opaque-state&code_challenge=opaque-challenge\n</message-context>"
	prompt, err := kitProtocolPrompt(runTurnRequest{SourceInstructions: source, Prompt: action}, false)
	require.NoError(t, err)
	require.Equal(t, action, prompt[1]["text"], "an upstream sign-in action must reach the model verbatim")
	require.Contains(t, prompt[0]["text"], "deliver it to the OWNER")
	require.Contains(t, prompt[0]["text"], "single primary button")
	require.Contains(t, prompt[0]["text"], "Never ask users to paste passwords or tokens")
	require.Contains(t, prompt[0]["text"], "never disclose workload credentials")
}

func TestKitProtocolPromptDeliveryRetryDeduplicates(t *testing.T) {
	t.Parallel()
	core, db, key, chatID := kitSessionFixture(t)
	currentID, err := testrepo.New(db).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ProjectID: uuid.NullUUID{UUID: key.ProjectID, Valid: true}, ChatID: chatID, Role: "user", Content: "retry me",
	})
	require.NoError(t, err)
	require.NoError(t, assistantrepo.New(db).SetKitThreadSourceFixture(t.Context(), assistantrepo.SetKitThreadSourceFixtureParams{
		ProjectID: key.ProjectID, ThreadID: key.ThreadID, SourceKind: "dashboard",
	}))
	source, _ := json.Marshal(map[string]string{"idempotency_key": currentID.String()})
	eventID, err := assistantrepo.New(db).InsertAssistantThreadEvent(t.Context(), assistantrepo.InsertAssistantThreadEventParams{
		AssistantThreadID: key.ThreadID, AssistantID: key.AssistantID, ProjectID: key.ProjectID,
		EventID: uuid.NewString(), CorrelationID: "kit-retry", Status: "processing",
		NormalizedPayloadJson: []byte(`{}`), SourcePayloadJson: source,
	})
	require.NoError(t, err)

	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), db, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.Background()) })
	projector := NewKitTranscriptProjector(db, writer, nil, func(ctx context.Context, tx pgx.Tx, b KitTranscriptBinding) error {
		_, err := core.ValidateKitRuntimeSessionInTx(ctx, tx, key, b.SessionID)
		return err
	})

	var mu sync.Mutex
	streams := map[string]chan string{}
	promptKeys := map[string]string{}
	insertions := 0
	promptCalls := 0
	setConfigCalls := 0
	selectedModel := "speakeasy:openai/gpt-4.1-mini"
	selectedEffort := "medium"
	catalog := func() string {
		return fmt.Sprintf(`{"configOptions":[{"configId":"model","type":"select","currentValue":%q,"options":[{"value":"speakeasy:openai/gpt-4.1-mini"}]},{"configId":"reasoning_effort","type":"select","currentValue":%q,"options":[{"value":"low"},{"value":"medium"},{"value":"high"}]}]}`, selectedModel, selectedEffort)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			w.WriteHeader(http.StatusUnauthorized)
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
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if msg.Method == "initialize" {
				w.Header().Set("Acp-Connection-Id", "conn")
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{"_meta":{"kit/session/status":{}},"session":{"prompt":{"delivery":{"steer":{},"queue":{}}}}}}}`, msg.ID)
				return
			}
			mu.Lock()
			ch := streams[session]
			mu.Unlock()
			if ch == nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			switch msg.Method {
			case "session/new":
				w.WriteHeader(http.StatusAccepted)
				result := strings.TrimSuffix(catalog(), "}") + `,"sessionId":"session-retry"}`
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, result)
			case "session/resume":
				w.WriteHeader(http.StatusAccepted)
				// Replay the prompt inserted during attempt 1 before its response was lost.
				ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"session-retry","update":{"sessionUpdate":"user_message","messageId":"user-msg-1","content":[{"type":"text","text":"retry me"}]}}}` + "\n\n"
				ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"session-retry","update":{"sessionUpdate":"agent_message_chunk","messageId":"reply-1","content":{"type":"text","text":"single answer"}}}}` + "\n\n"
				ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"session-retry","update":{"sessionUpdate":"state_update","state":"idle","stopReason":"end_turn"}}}` + "\n\n"
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, catalog())
			case "session/set_config_option":
				mu.Lock()
				setConfigCalls++
				mu.Unlock()
				w.WriteHeader(http.StatusAccepted)
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, catalog())
			case "session/prompt":
				var delivery, idempotencyKey string
				_ = json.Unmarshal(msg.Params["delivery"], &delivery)
				_ = json.Unmarshal(msg.Params["idempotencyKey"], &idempotencyKey)
				if delivery != kitPromptDeliverySteer || idempotencyKey != eventID.String() {
					t.Errorf("unexpected prompt delivery=%q key=%q", delivery, idempotencyKey)
				}
				mu.Lock()
				promptCalls++
				existing, duplicate := promptKeys[idempotencyKey]
				if !duplicate {
					insertions++
					existing = fmt.Sprintf("user-msg-%d", insertions)
					promptKeys[idempotencyKey] = existing
				}
				attempt := promptCalls
				mu.Unlock()
				w.WriteHeader(http.StatusAccepted)
				if attempt == 1 {
					// Simulate transport loss after Kit inserted the prompt and
					// before the server received PromptResponse or stored the echo.
					close(ch)
					return
				}
				// Deduplicated retry returns the original messageId without
				// inserting a second user message or emitting another echo.
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"messageId\":%q}}\n\n", msg.ID, existing)
			case "kit/session/status":
				w.WriteHeader(http.StatusAccepted)
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"sessionId\":\"session-retry\",\"settled\":true,\"foregroundActive\":false,\"backgroundActive\":false,\"pendingBackgroundTerminals\":false,\"activeSubagents\":0}}\n\n", msg.ID)
			default:
				t.Errorf("unexpected method %s", msg.Method)
				w.WriteHeader(http.StatusBadRequest)
			}
		case http.MethodGet:
			ch := make(chan string, 16)
			mu.Lock()
			streams[session] = ch
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flushKitTestResponse(t, w)
			for {
				select {
				case data, ok := <-ch:
					if !ok {
						return
					}
					_, _ = fmt.Fprint(w, data)
					flushKitTestResponse(t, w)
				case <-r.Context().Done():
					return
				}
			}
		case http.MethodDelete:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer server.Close()

	endpoint, err := url.Parse(server.URL + "/acp/v2")
	require.NoError(t, err)
	p, err := NewKitRuntimeProtocol(KitRuntimeProtocolConfig{
		Core: core, Endpoint: endpoint, Projector: projector, CWD: "/workspace",
		GatewayToken: "fixture-token", HTTPClient: kitProtocolTestHTTPClient(t),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	r := assistantRuntimeRecord{ID: key.RuntimeID, ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeVersion: 3}
	turn := runTurnRequest{
		Model:                  "openai/gpt-4.1-mini",
		ReasoningEffort:        "medium",
		ThreadID:               key.ThreadID,
		Prompt:                 "retry me",
		EventID:                eventID,
		EventAttempt:           1,
		IdempotencyKey:         eventID.String(),
		CanonicalUserMessageID: currentID,
	}

	require.Error(t, p.RunTurn(ctx, r, turn), "attempt 1 must fail when SSE closes before prompt ack")

	turn.EventAttempt = 2
	require.NoError(t, p.RunTurn(ctx, r, turn), "attempt 2 must recover cleanly via idempotencyKey")

	mu.Lock()
	require.Equal(t, 1, insertions, "Kit must insert the retried prompt only once")
	require.Equal(t, 2, promptCalls)
	require.Zero(t, setConfigCalls, "matching model and effort must not issue redundant set_config_option RPCs")
	mu.Unlock()

	messages, err := chatrepo.New(db).ListLatestGenerationChatMessages(ctx, chatrepo.ListLatestGenerationChatMessagesParams{
		ProjectID: key.ProjectID, ChatID: chatID,
	})
	require.NoError(t, err)
	users, assistantsCount := 0, 0
	for _, m := range messages {
		if m.Role == "user" {
			users++
		}
		if m.Role == "assistant" && m.Content == "single answer" {
			assistantsCount++
		}
	}
	require.Equal(t, 1, users, "replayed user echo must correlate with canonical user row without duplicating")
	require.Equal(t, 1, assistantsCount)
}

func TestKitProtocolOmitsUnadvertisedDelivery(t *testing.T) {
	t.Parallel()
	core, db, key, chatID := kitSessionFixture(t)
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), db, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.Background()) })
	projector := NewKitTranscriptProjector(db, writer, nil, func(ctx context.Context, tx pgx.Tx, b KitTranscriptBinding) error {
		_, err := core.ValidateKitRuntimeSessionInTx(ctx, tx, key, b.SessionID)
		return err
	})

	var mu sync.Mutex
	streams := map[string]chan string{}
	sawDelivery := false
	sawIdempotencyKey := ""
	const catalog = `{"configOptions":[{"configId":"model","type":"select","currentValue":"speakeasy:openai/gpt-4.1-mini","options":[{"value":"speakeasy:openai/gpt-4.1-mini"}]},{"configId":"reasoning_effort","type":"select","currentValue":"medium","options":[{"value":"medium"}]}]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session := r.Header.Get("Acp-Session-Id")
		switch r.Method {
		case http.MethodPost:
			var msg struct {
				ID     json.RawMessage            `json:"id"`
				Method string                     `json:"method"`
				Params map[string]json.RawMessage `json:"params"`
			}
			if json.NewDecoder(r.Body).Decode(&msg) != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if msg.Method == "initialize" {
				w.Header().Set("Acp-Connection-Id", "no-delivery-conn")
				// Harness advertises kit/session/status without prompt delivery capabilities.
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{"_meta":{"kit/session/status":{}}}}}`, msg.ID)
				return
			}
			mu.Lock()
			ch := streams[session]
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			switch msg.Method {
			case "session/new":
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, strings.TrimSuffix(catalog, "}")+`,"sessionId":"no-delivery-session"}`)
			case "session/prompt":
				mu.Lock()
				_, sawDelivery = msg.Params["delivery"]
				_ = json.Unmarshal(msg.Params["idempotencyKey"], &sawIdempotencyKey)
				mu.Unlock()
				ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"no-delivery-session","update":{"sessionUpdate":"user_message","messageId":"u1","content":[{"type":"text","text":"hi"}]}}}` + "\n\n"
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"messageId\":\"u1\"}}\n\n", msg.ID)
			case "kit/session/status":
				ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"no-delivery-session","update":{"sessionUpdate":"agent_message_chunk","messageId":"a1","content":{"type":"text","text":"ok"}}}}` + "\n\n"
				ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"no-delivery-session","update":{"sessionUpdate":"state_update","state":"idle","stopReason":"end_turn"}}}` + "\n\n"
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"sessionId\":\"no-delivery-session\",\"settled\":true,\"foregroundActive\":false,\"backgroundActive\":false,\"pendingBackgroundTerminals\":false,\"activeSubagents\":0}}\n\n", msg.ID)
			}
		case http.MethodGet:
			ch := make(chan string, 16)
			mu.Lock()
			streams[session] = ch
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
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
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer server.Close()

	endpoint, err := url.Parse(server.URL + "/acp/v2")
	require.NoError(t, err)
	p, err := NewKitRuntimeProtocol(KitRuntimeProtocolConfig{
		Core: core, Endpoint: endpoint, Projector: projector, CWD: "/workspace",
		GatewayToken: "fixture-token", HTTPClient: kitProtocolTestHTTPClient(t),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	eventID := uuid.New()
	r := assistantRuntimeRecord{ID: key.RuntimeID, ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeVersion: 3}
	require.NoError(t, p.RunTurn(ctx, r, runTurnRequest{
		Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", ThreadID: key.ThreadID,
		Prompt: "hi", EventID: eventID, IdempotencyKey: eventID.String(),
	}))
	mu.Lock()
	require.False(t, sawDelivery, "must omit delivery when the harness does not advertise it")
	require.Equal(t, eventID.String(), sawIdempotencyKey)
	mu.Unlock()

	messages, err := chatrepo.New(db).ListLatestGenerationChatMessages(ctx, chatrepo.ListLatestGenerationChatMessagesParams{
		ProjectID: key.ProjectID, ChatID: chatID,
	})
	require.NoError(t, err)
	foundAssistant := false
	for _, m := range messages {
		if m.Role == "assistant" && m.Content == "ok" && m.FinishReason.String == "end_turn" {
			foundAssistant = true
		}
	}
	require.True(t, foundAssistant, "assistant reply must be projected when delivery is omitted")
}

func TestKitProtocolInterruptRunningTurnAndSettle(t *testing.T) {
	t.Parallel()
	core, db, key, chatID := kitSessionFixture(t)
	event, err := assistantrepo.New(db).ClaimNextPendingEvent(t.Context(), assistantrepo.ClaimNextPendingEventParams{
		ProjectID: key.ProjectID, ThreadID: key.ThreadID, PendingStatus: eventStatusPending, ProcessingStatus: eventStatusProcessing,
	})
	require.NoError(t, err)

	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), db, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.Background()) })
	projector := NewKitTranscriptProjector(db, writer, nil, func(ctx context.Context, tx pgx.Tx, b KitTranscriptBinding) error {
		_, err := core.ValidateKitRuntimeSessionInTx(ctx, tx, key, b.SessionID)
		return err
	})

	var mu sync.Mutex
	streams := map[string]chan string{}
	promptInserted := make(chan struct{})
	cancelled := false
	cancelCalls := 0
	const catalog = `{"configOptions":[{"configId":"model","type":"select","currentValue":"speakeasy:openai/gpt-4.1-mini","options":[{"value":"speakeasy:openai/gpt-4.1-mini"}]},{"configId":"reasoning_effort","type":"select","currentValue":"medium","options":[{"value":"medium"}]}]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session := r.Header.Get("Acp-Session-Id")
		switch r.Method {
		case http.MethodPost:
			var msg struct {
				ID     json.RawMessage            `json:"id"`
				Method string                     `json:"method"`
				Params map[string]json.RawMessage `json:"params"`
			}
			if json.NewDecoder(r.Body).Decode(&msg) != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if msg.Method == "initialize" {
				w.Header().Set("Acp-Connection-Id", "cancel-conn")
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{"_meta":{"kit/session/status":{}},"session":{"prompt":{"delivery":{"steer":{},"queue":{}}}}}}}`, msg.ID)
				return
			}
			mu.Lock()
			ch := streams[session]
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			switch msg.Method {
			case "session/new":
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, strings.TrimSuffix(catalog, "}")+`,"sessionId":"cancel-session"}`)
			case "session/prompt":
				var delivery string
				_ = json.Unmarshal(msg.Params["delivery"], &delivery)
				if delivery != kitPromptDeliverySteer {
					t.Errorf("expected steer delivery mode, got %q", delivery)
				}
				ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"cancel-session","update":{"sessionUpdate":"user_message","messageId":"u1","content":[{"type":"text","text":"work"}]}}}` + "\n\n"
				ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"cancel-session","update":{"sessionUpdate":"state_update","state":"running"}}}` + "\n\n"
				ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"cancel-session","update":{"sessionUpdate":"agent_message_chunk","messageId":"a1","content":{"type":"text","text":"partial"}}}}` + "\n\n"
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"messageId\":\"u1\"}}\n\n", msg.ID)
				close(promptInserted)
			case "session/cancel":
				mu.Lock()
				cancelCalls++
				cancelled = true
				mu.Unlock()
				ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"cancel-session","update":{"sessionUpdate":"state_update","state":"idle","stopReason":"cancelled"}}}` + "\n\n"
			case "kit/session/status":
				mu.Lock()
				done := cancelled
				mu.Unlock()
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"sessionId\":\"cancel-session\",\"settled\":%t,\"foregroundActive\":%t,\"backgroundActive\":false,\"pendingBackgroundTerminals\":false,\"activeSubagents\":0}}\n\n", msg.ID, done, !done)
			}
		case http.MethodGet:
			ch := make(chan string, 16)
			mu.Lock()
			streams[session] = ch
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
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
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer server.Close()

	endpoint, err := url.Parse(server.URL + "/acp/v2")
	require.NoError(t, err)
	worker, err := NewKitRuntimeProtocol(KitRuntimeProtocolConfig{
		Core: core, Endpoint: endpoint, Projector: projector, CWD: "/workspace",
		GatewayToken: "fixture-token", HTTPClient: kitProtocolTestHTTPClient(t),
	})
	require.NoError(t, err)
	api := &KitRuntimeProtocol{config: KitRuntimeProtocolConfig{Core: &ServiceCore{db: db}}}

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	r := assistantRuntimeRecord{ID: key.RuntimeID, ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeVersion: 3}
	turnDone := make(chan error, 1)
	go func() {
		turnDone <- worker.RunTurn(ctx, r, runTurnRequest{
			Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", ThreadID: key.ThreadID,
			Prompt: "work", EventID: event.ID, EventAttempt: int(event.Attempts), IdempotencyKey: event.ID.String(),
		})
	}()

	select {
	case <-promptInserted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	interrupted, err := api.InterruptTurn(ctx, r, key.ThreadID)
	require.NoError(t, err)
	require.True(t, interrupted)
	require.NoError(t, <-turnDone)

	mu.Lock()
	require.Equal(t, 1, cancelCalls)
	mu.Unlock()

	messages, err := chatrepo.New(db).ListLatestGenerationChatMessages(ctx, chatrepo.ListLatestGenerationChatMessagesParams{
		ProjectID: key.ProjectID, ChatID: chatID,
	})
	require.NoError(t, err)
	foundCancelled := false
	for _, m := range messages {
		if m.Role == "assistant" && m.Content == "partial" && m.FinishReason.String == "cancelled" {
			foundCancelled = true
		}
	}
	require.True(t, foundCancelled, "cancelled turn must persist partial output and cancelled finish reason")
}
