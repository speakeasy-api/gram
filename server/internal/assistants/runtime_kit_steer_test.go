package assistants

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/assistants/kitacp"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

type kitSteerBackendStub struct {
	protocol runtimeProtocol
}

func (b *kitSteerBackendStub) ProtocolForRuntime(assistantRuntimeRecord) (runtimeProtocol, error) {
	return b.protocol, nil
}

func (b *kitSteerBackendStub) RunTurn(ctx context.Context, r assistantRuntimeRecord, req runTurnRequest) error {
	if err := b.protocol.RunTurn(ctx, r, req); err != nil {
		return fmt.Errorf("stub run turn: %w", err)
	}
	return nil
}

func (b *kitSteerBackendStub) InterruptTurn(ctx context.Context, r assistantRuntimeRecord, threadID uuid.UUID) (bool, error) {
	interrupted, err := b.protocol.InterruptTurn(ctx, r, threadID)
	if err != nil {
		return false, fmt.Errorf("stub interrupt turn: %w", err)
	}
	return interrupted, nil
}

func (b *kitSteerBackendStub) Status(ctx context.Context, r assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	status, err := b.protocol.Status(ctx, r)
	if err != nil {
		return RuntimeBackendStatus{}, fmt.Errorf("stub status: %w", err)
	}
	return status, nil
}

func (b *kitSteerBackendStub) Backend() string                  { return runtimeBackendLocal }
func (b *kitSteerBackendStub) SupportsBackend(name string) bool { return name == runtimeBackendLocal }
func (b *kitSteerBackendStub) ServerURL() *url.URL              { return nil }
func (b *kitSteerBackendStub) ImageRef() string                 { return "kit-test:v0.3.1" }
func (b *kitSteerBackendStub) Ensure(context.Context, assistantRuntimeRecord) (RuntimeBackendEnsureResult, error) {
	return RuntimeBackendEnsureResult{}, nil
}
func (b *kitSteerBackendStub) RecycleImage(context.Context, assistantRuntimeRecord) (RuntimeBackendRecycleResult, error) {
	return RuntimeBackendRecycleResult{}, nil
}
func (b *kitSteerBackendStub) Stop(context.Context, assistantRuntimeRecord) error { return nil }
func (b *kitSteerBackendStub) Reap(context.Context, assistantRuntimeRecord) error { return nil }
func (b *kitSteerBackendStub) ReapStoppedMachine(context.Context, assistantRuntimeRecord) error {
	return nil
}

func insertDashboardEventFixture(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	key kitRuntimeSessionKey,
	chatID uuid.UUID,
	text string,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	msgID, err := testrepo.New(db).InsertChatMessage(ctx, testrepo.InsertChatMessageParams{
		ProjectID: uuid.NullUUID{UUID: key.ProjectID, Valid: true},
		ChatID:    chatID,
		Role:      "user",
		Content:   text,
	})
	require.NoError(t, err)
	norm, err := json.Marshal(dashboardEventPayload{Text: text, UserID: "user-1"})
	require.NoError(t, err)
	source, err := json.Marshal(map[string]string{"idempotency_key": msgID.String()})
	require.NoError(t, err)
	eventID, err := assistantrepo.New(db).InsertAssistantThreadEvent(ctx, assistantrepo.InsertAssistantThreadEventParams{
		AssistantThreadID:     key.ThreadID,
		AssistantID:           key.AssistantID,
		ProjectID:             key.ProjectID,
		EventID:               uuid.NewString(),
		CorrelationID:         chatID.String(),
		Status:                eventStatusPending,
		NormalizedPayloadJson: norm,
		SourcePayloadJson:     source,
	})
	require.NoError(t, err)
	return eventID, msgID
}

func setupKitSteerOrchestration(t *testing.T, serverURL string) (*ServiceCore, *pgxpool.Pool, kitRuntimeSessionKey, uuid.UUID, *KitRuntimeProtocol) {
	t.Helper()
	ctx := t.Context()
	seedCore, db, key, chatID := kitSessionFixture(t)
	require.NoError(t, assistantrepo.New(db).SetKitThreadSourceFixture(ctx, assistantrepo.SetKitThreadSourceFixtureParams{
		ProjectID:  key.ProjectID,
		ThreadID:   key.ThreadID,
		SourceKind: sourceKindDashboard,
	}))
	// Cancel the default fixture event so each test controls exact events.
	_, err := assistantrepo.New(db).CancelPendingAssistantThreadEvents(ctx, assistantrepo.CancelPendingAssistantThreadEventsParams{
		ProjectID:       key.ProjectID,
		ThreadID:        key.ThreadID,
		PendingStatus:   eventStatusPending,
		CancelledStatus: eventStatusCancelled,
	})
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	backend := &kitSteerBackendStub{}
	core := NewServiceCore(
		logger,
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		db,
		nil,
		nil,
		backend,
		nil,
		assistanttokens.New("test-jwt-secret", db, nil, nil, nil),
		mustParseURLForServiceTest(t, "https://api.example"),
		telemetry.NewStub(logger),
		nil,
		newTestAuditLogger(),
		testIdentityService,
		newTestAuthzEngine(t, db),
	)
	core.encryptionClient = seedCore.encryptionClient

	writer, shutdown := chat.NewChatMessageWriter(logger, db, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.Background()) })
	projector := NewKitTranscriptProjector(db, writer, nil, func(ctx context.Context, tx pgx.Tx, b KitTranscriptBinding) error {
		_, err := core.ValidateKitRuntimeSessionInTx(ctx, tx, key, b.SessionID)
		return err
	})
	endpoint, err := url.Parse(serverURL + "/acp/v2")
	require.NoError(t, err)
	protocol, err := NewKitRuntimeProtocol(KitRuntimeProtocolConfig{
		Core:         core,
		Endpoint:     endpoint,
		HTTPClient:   kitProtocolTestHTTPClient(t),
		Projector:    projector,
		CWD:          "/workspace",
		GatewayToken: "fixture-token",
	})
	require.NoError(t, err)
	backend.protocol = protocol
	return core, db, key, chatID, protocol
}

func TestKitSteerMidTurnAbsorbsAndSettlesAllEvents(t *testing.T) {
	t.Parallel()
	const catalog = `{"configOptions":[{"configId":"model","type":"select","currentValue":"speakeasy:openai/gpt-4o-mini","options":[{"value":"speakeasy:openai/gpt-4o-mini"}]},{"configId":"reasoning_effort","type":"select","currentValue":"medium","options":[{"value":"low"},{"value":"medium"},{"value":"high"}]}]}`

	var mu sync.Mutex
	streams := map[string]chan string{}
	connections := 0
	newSessions := 0
	resumes := 0
	settled := false
	insertedKeys := []string{}
	insertedDeliveries := []string{}
	firstPromptRunning := make(chan struct{})
	firstPromptSignaled := false

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
				mu.Lock()
				connections++
				connID := fmt.Sprintf("conn-%d", connections)
				mu.Unlock()
				w.Header().Set("Acp-Connection-Id", connID)
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{"_meta":{"kit/session/status":{}},"session":{"prompt":{"delivery":{"steer":{},"queue":{}}}}}}}`, msg.ID)
				return
			}
			mu.Lock()
			ch := streams[session]
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			switch msg.Method {
			case "session/new":
				mu.Lock()
				newSessions++
				mu.Unlock()
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, strings.TrimSuffix(catalog, "}")+`,"sessionId":"steer-session"}`)
			case "session/resume":
				mu.Lock()
				resumes++
				mu.Unlock()
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, catalog)
			case "session/prompt":
				var delivery, key string
				_ = json.Unmarshal(msg.Params["delivery"], &delivery)
				_ = json.Unmarshal(msg.Params["idempotencyKey"], &key)
				mu.Lock()
				insertedDeliveries = append(insertedDeliveries, delivery)
				insertedKeys = append(insertedKeys, key)
				n := len(insertedKeys)
				msgID := fmt.Sprintf("u%d", n)
				shouldCloseFirst := !firstPromptSignaled
				if shouldCloseFirst {
					firstPromptSignaled = true
				}
				if n == 3 {
					settled = true
				}
				mu.Unlock()
				ch <- fmt.Sprintf(`data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"steer-session","update":{"sessionUpdate":"user_message","messageId":%q,"content":[{"type":"text","text":"msg-%d"}]}}}`+"\n\n", msgID, n)
				if n == 1 {
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"steer-session","update":{"sessionUpdate":"state_update","state":"running"}}}` + "\n\n"
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"steer-session","update":{"sessionUpdate":"agent_message_chunk","messageId":"a1","content":{"type":"text","text":"working..."}}}}` + "\n\n"
				}
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"messageId\":%q}}\n\n", msg.ID, msgID)
				if shouldCloseFirst {
					close(firstPromptRunning)
				}
				if n == 3 {
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"steer-session","update":{"sessionUpdate":"agent_message_chunk","messageId":"a2","content":{"type":"text","text":"handled all three"}}}}` + "\n\n"
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"steer-session","update":{"sessionUpdate":"state_update","state":"idle","stopReason":"end_turn"}}}` + "\n\n"
				}
			case "kit/session/status":
				mu.Lock()
				done := settled
				mu.Unlock()
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"sessionId\":\"steer-session\",\"settled\":%t,\"foregroundActive\":%t,\"backgroundActive\":false,\"pendingBackgroundTerminals\":false,\"activeSubagents\":0}}\n\n", msg.ID, done, !done)
			}
		case http.MethodGet:
			ch := make(chan string, 32)
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

	core, db, key, chatID, _ := setupKitSteerOrchestration(t, server.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	event1, _ := insertDashboardEventFixture(t, ctx, db, key, chatID, "start task")
	var event2, event3 uuid.UUID
	done := make(chan error, 1)
	go func() {
		_, err := core.ProcessThreadEvents(ctx, key.ProjectID, key.ThreadID)
		done <- err
	}()

	select {
	case <-firstPromptRunning:
	case <-ctx.Done():
		t.Fatal("timed out waiting for first prompt to start")
	}

	// Arrive mid-turn while event1's Kit turn is still active.
	event2, _ = insertDashboardEventFixture(t, ctx, db, key, chatID, "steer one")
	event3, _ = insertDashboardEventFixture(t, ctx, db, key, chatID, "steer two")

	require.NoError(t, <-done)

	mu.Lock()
	require.Equal(t, 1, connections, "all three events must share the single running turn connection")
	require.Equal(t, 1, newSessions)
	require.Equal(t, 0, resumes)
	require.Equal(t, []string{kitPromptDeliverySteer, kitPromptDeliverySteer, kitPromptDeliverySteer}, insertedDeliveries)
	require.Equal(t, []string{event1.String(), event2.String(), event3.String()}, insertedKeys)
	mu.Unlock()

	// All three absorbed events must be settled as completed.
	busy, err := assistantrepo.New(db).AssistantHasUnsettledEvents(ctx, assistantrepo.AssistantHasUnsettledEventsParams{
		ProjectID: key.ProjectID, AssistantID: key.AssistantID,
	})
	require.NoError(t, err)
	require.True(t, busy.Valid)
	require.False(t, busy.Bool, "all absorbed events must be settled")

	// Each dashboard user message must exist once (no duplicate echo rows) and
	// assistant output must be persisted with finish reason.
	messages, err := chatrepo.New(db).ListLatestGenerationChatMessages(ctx, chatrepo.ListLatestGenerationChatMessagesParams{
		ProjectID: key.ProjectID, ChatID: chatID,
	})
	require.NoError(t, err)
	var userTexts []string
	foundFinalAssistant := false
	for _, m := range messages {
		if m.Role == "user" {
			userTexts = append(userTexts, m.Content)
		}
		if m.Role == "assistant" && m.Content == "handled all three" && m.FinishReason.String == "end_turn" {
			foundFinalAssistant = true
		}
	}
	require.Equal(t, []string{"start task", "steer one", "steer two"}, userTexts)
	require.True(t, foundFinalAssistant)
}

func TestKitSteerIdempotentRetryAfterSettlement(t *testing.T) {
	t.Parallel()
	const catalog = `{"configOptions":[{"configId":"model","type":"select","currentValue":"speakeasy:openai/gpt-4o-mini","options":[{"value":"speakeasy:openai/gpt-4o-mini"}]},{"configId":"reasoning_effort","type":"select","currentValue":"medium","options":[{"value":"low"},{"value":"medium"},{"value":"high"}]}]}`

	var mu sync.Mutex
	streams := map[string]chan string{}
	promptKeys := map[string]string{}
	insertions := 0
	promptCalls := 0
	firstPromptRunning := make(chan struct{})
	firstSignaled := false
	crashedOnce := false

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
				w.Header().Set("Acp-Connection-Id", "retry-conn")
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{"_meta":{"kit/session/status":{}},"session":{"prompt":{"delivery":{"steer":{},"queue":{}}}}}}}`, msg.ID)
				return
			}
			mu.Lock()
			ch := streams[session]
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			switch msg.Method {
			case "session/new":
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, strings.TrimSuffix(catalog, "}")+`,"sessionId":"retry-steer-session"}`)
			case "session/resume":
				// Replay the already-settled turn containing both u1 and u2 plus final reply.
				ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"retry-steer-session","update":{"sessionUpdate":"user_message","messageId":"u1","content":[{"type":"text","text":"first"}]}}}` + "\n\n"
				ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"retry-steer-session","update":{"sessionUpdate":"user_message","messageId":"u2","content":[{"type":"text","text":"second"}]}}}` + "\n\n"
				ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"retry-steer-session","update":{"sessionUpdate":"agent_message_chunk","messageId":"a1","content":{"type":"text","text":"settled reply"}}}}` + "\n\n"
				ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"retry-steer-session","update":{"sessionUpdate":"state_update","state":"idle","stopReason":"end_turn"}}}` + "\n\n"
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, catalog)
			case "session/prompt":
				var key string
				_ = json.Unmarshal(msg.Params["idempotencyKey"], &key)
				mu.Lock()
				promptCalls++
				existing, dup := promptKeys[key]
				if !dup {
					insertions++
					existing = fmt.Sprintf("u%d", insertions)
					promptKeys[key] = existing
				}
				curInsertions := insertions
				shouldSignalFirst := !firstSignaled && curInsertions == 1
				if shouldSignalFirst {
					firstSignaled = true
				}
				shouldCrash := !crashedOnce && curInsertions == 2 && !dup
				if shouldCrash {
					crashedOnce = true
				}
				mu.Unlock()
				if shouldCrash {
					// Simulate transport loss AFTER Kit inserted steered prompt u2
					// and BEFORE the server received PromptResponse or stored u2's echo.
					close(ch)
					return
				}
				if !dup {
					ch <- fmt.Sprintf(`data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"retry-steer-session","update":{"sessionUpdate":"user_message","messageId":%q,"content":[{"type":"text","text":"first"}]}}}`+"\n\n", existing)
				}
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"messageId\":%q}}\n\n", msg.ID, existing)
				if shouldSignalFirst {
					close(firstPromptRunning)
				}
			case "kit/session/status":
				mu.Lock()
				done := crashedOnce
				mu.Unlock()
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"sessionId\":\"retry-steer-session\",\"settled\":%t,\"foregroundActive\":%t,\"backgroundActive\":false,\"pendingBackgroundTerminals\":false,\"activeSubagents\":0}}\n\n", msg.ID, done, !done)
			}
		case http.MethodGet:
			ch := make(chan string, 32)
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

	core, db, key, chatID, _ := setupKitSteerOrchestration(t, server.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	insertDashboardEventFixture(t, ctx, db, key, chatID, "first")

	type processOutcome struct {
		res ProcessThreadEventsResult
		err error
	}
	attempt1Done := make(chan processOutcome, 1)
	go func() {
		res, err := core.ProcessThreadEvents(ctx, key.ProjectID, key.ThreadID)
		attempt1Done <- processOutcome{res: res, err: err}
	}()

	select {
	case <-firstPromptRunning:
	case <-ctx.Done():
		t.Fatal("timed out waiting for first prompt")
	}

	insertDashboardEventFixture(t, ctx, db, key, chatID, "second")

	out1 := <-attempt1Done
	require.NoError(t, out1.err)
	require.True(t, out1.res.RetryAdmission, "transport drop mid-turn must requeue absorbed events for retry")

	// Attempt 2 runs AFTER the turn that absorbed event1 + event2 has already settled in Kit.
	res2, err := core.ProcessThreadEvents(ctx, key.ProjectID, key.ThreadID)
	require.NoError(t, err)
	require.True(t, res2.ProcessedAnyEvent)
	require.False(t, res2.RetryAdmission)

	mu.Lock()
	require.Equal(t, 2, insertions, "retried delivery after settlement must never insert either event twice")
	require.GreaterOrEqual(t, promptCalls, 4, "both events were retried with their idempotency keys")
	mu.Unlock()

	busy, err := assistantrepo.New(db).AssistantHasUnsettledEvents(ctx, assistantrepo.AssistantHasUnsettledEventsParams{
		ProjectID: key.ProjectID, AssistantID: key.AssistantID,
	})
	require.NoError(t, err)
	require.True(t, busy.Valid)
	require.False(t, busy.Bool)

	messages, err := chatrepo.New(db).ListLatestGenerationChatMessages(ctx, chatrepo.ListLatestGenerationChatMessagesParams{
		ProjectID: key.ProjectID, ChatID: chatID,
	})
	require.NoError(t, err)
	var userTexts []string
	foundReply := false
	for _, m := range messages {
		if m.Role == "user" {
			userTexts = append(userTexts, m.Content)
		}
		if m.Role == "assistant" && m.Content == "settled reply" && m.FinishReason.String == "end_turn" {
			foundReply = true
		}
	}
	require.Equal(t, []string{"first", "second"}, userTexts, "replayed steered echo must correlate without creating duplicate user rows")
	require.True(t, foundReply)
}

func TestKitSteerInterruptWithdrawsHeldSteerAndSettlesAllEvents(t *testing.T) {
	t.Parallel()
	const catalog = `{"configOptions":[{"configId":"model","type":"select","currentValue":"speakeasy:openai/gpt-4o-mini","options":[{"value":"speakeasy:openai/gpt-4o-mini"}]},{"configId":"reasoning_effort","type":"select","currentValue":"medium","options":[{"value":"low"},{"value":"medium"},{"value":"high"}]}]}`

	var mu sync.Mutex
	streams := map[string]chan string{}
	promptsSeen := 0
	var heldPromptID uint64
	var withdrawnID uint64
	cancelOrder := []string{}
	cancelled := false
	firstPromptRunning := make(chan struct{})
	secondPromptHeld := make(chan struct{})

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
				w.Header().Set("Acp-Connection-Id", "cancel-steer-conn")
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{"_meta":{"kit/session/status":{}},"session":{"prompt":{"delivery":{"steer":{},"queue":{}}}}}}}`, msg.ID)
				return
			}
			mu.Lock()
			ch := streams[session]
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			switch msg.Method {
			case "session/new":
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, strings.TrimSuffix(catalog, "}")+`,"sessionId":"cancel-steer-session"}`)
			case "session/prompt":
				mu.Lock()
				promptsSeen++
				n := promptsSeen
				mu.Unlock()
				if n == 1 {
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"cancel-steer-session","update":{"sessionUpdate":"user_message","messageId":"u1","content":[{"type":"text","text":"work"}]}}}` + "\n\n"
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"cancel-steer-session","update":{"sessionUpdate":"state_update","state":"running"}}}` + "\n\n"
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"cancel-steer-session","update":{"sessionUpdate":"agent_message_chunk","messageId":"a1","content":{"type":"text","text":"partial"}}}}` + "\n\n"
					ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"messageId\":\"u1\"}}\n\n", msg.ID)
					close(firstPromptRunning)
					return
				}
				// Hold the steered prompt uninserted (simulating Kit waiting for a safe point).
				var id uint64
				_ = json.Unmarshal(msg.ID, &id)
				mu.Lock()
				heldPromptID = id
				mu.Unlock()
				close(secondPromptHeld)
			case "$/cancel_request":
				var reqID uint64
				_ = json.Unmarshal(msg.Params["requestId"], &reqID)
				mu.Lock()
				withdrawnID = reqID
				cancelOrder = append(cancelOrder, "$/cancel_request")
				mu.Unlock()
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":{\"code\":%d,\"message\":\"withdrawn\"}}\n\n", reqID, kitacp.RequestCancelledCode)
			case "session/cancel":
				mu.Lock()
				cancelOrder = append(cancelOrder, "session/cancel")
				cancelled = true
				mu.Unlock()
				ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"cancel-steer-session","update":{"sessionUpdate":"state_update","state":"idle","stopReason":"cancelled"}}}` + "\n\n"
			case "kit/session/status":
				mu.Lock()
				done := cancelled && withdrawnID != 0
				mu.Unlock()
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"sessionId\":\"cancel-steer-session\",\"settled\":%t,\"foregroundActive\":%t,\"backgroundActive\":false,\"pendingBackgroundTerminals\":false,\"activeSubagents\":0}}\n\n", msg.ID, done, !done)
			}
		case http.MethodGet:
			ch := make(chan string, 32)
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

	core, db, key, chatID, _ := setupKitSteerOrchestration(t, server.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	insertDashboardEventFixture(t, ctx, db, key, chatID, "work")
	done := make(chan error, 1)
	go func() {
		_, err := core.ProcessThreadEvents(ctx, key.ProjectID, key.ThreadID)
		done <- err
	}()

	select {
	case <-firstPromptRunning:
	case <-ctx.Done():
		t.Fatal("timed out waiting for first prompt")
	}

	insertDashboardEventFixture(t, ctx, db, key, chatID, "held steer")
	select {
	case <-secondPromptHeld:
	case <-ctx.Done():
		t.Fatal("timed out waiting for held steer prompt")
	}

	api := &KitRuntimeProtocol{config: KitRuntimeProtocolConfig{Core: &ServiceCore{db: db}}}
	r := assistantRuntimeRecord{ID: key.RuntimeID, ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeVersion: 3}
	interrupted, err := api.InterruptTurn(ctx, r, key.ThreadID)
	require.NoError(t, err)
	require.True(t, interrupted)
	require.NoError(t, <-done)

	mu.Lock()
	require.NotZero(t, heldPromptID)
	require.Equal(t, heldPromptID, withdrawnID, "held steer must be withdrawn via $/cancel_request")
	require.Equal(t, []string{"$/cancel_request", "session/cancel"}, cancelOrder, "held steer must be withdrawn before session/cancel")
	mu.Unlock()

	busy, err := assistantrepo.New(db).AssistantHasUnsettledEvents(ctx, assistantrepo.AssistantHasUnsettledEventsParams{
		ProjectID: key.ProjectID, AssistantID: key.AssistantID,
	})
	require.NoError(t, err)
	require.True(t, busy.Valid)
	require.False(t, busy.Bool, "both running and withdrawn steered events must be settled")
}

func TestKitSteerInterruptAfterInsertionSettlesAllEvents(t *testing.T) {
	t.Parallel()
	const catalog = `{"configOptions":[{"configId":"model","type":"select","currentValue":"speakeasy:openai/gpt-4o-mini","options":[{"value":"speakeasy:openai/gpt-4o-mini"}]},{"configId":"reasoning_effort","type":"select","currentValue":"medium","options":[{"value":"low"},{"value":"medium"},{"value":"high"}]}]}`

	var mu sync.Mutex
	streams := map[string]chan string{}
	promptsSeen := 0
	cancelled := false
	firstPromptRunning := make(chan struct{})
	secondPromptInserted := make(chan struct{})

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
				w.Header().Set("Acp-Connection-Id", "cancel-inserted-conn")
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{"_meta":{"kit/session/status":{}},"session":{"prompt":{"delivery":{"steer":{},"queue":{}}}}}}}`, msg.ID)
				return
			}
			mu.Lock()
			ch := streams[session]
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			switch msg.Method {
			case "session/new":
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, strings.TrimSuffix(catalog, "}")+`,"sessionId":"cancel-inserted-session"}`)
			case "session/prompt":
				mu.Lock()
				promptsSeen++
				n := promptsSeen
				mu.Unlock()
				msgID := fmt.Sprintf("u%d", n)
				ch <- fmt.Sprintf(`data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"cancel-inserted-session","update":{"sessionUpdate":"user_message","messageId":%q,"content":[{"type":"text","text":"m%d"}]}}}`+"\n\n", msgID, n)
				if n == 1 {
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"cancel-inserted-session","update":{"sessionUpdate":"state_update","state":"running"}}}` + "\n\n"
				}
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"messageId\":%q}}\n\n", msg.ID, msgID)
				switch n {
				case 1:
					close(firstPromptRunning)
				case 2:
					close(secondPromptInserted)
				}
			case "session/cancel":
				mu.Lock()
				cancelled = true
				mu.Unlock()
				ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"cancel-inserted-session","update":{"sessionUpdate":"agent_message_chunk","messageId":"a1","content":{"type":"text","text":"stopped"}}}}` + "\n\n"
				ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"cancel-inserted-session","update":{"sessionUpdate":"state_update","state":"idle","stopReason":"cancelled"}}}` + "\n\n"
			case "kit/session/status":
				mu.Lock()
				done := cancelled
				mu.Unlock()
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"sessionId\":\"cancel-inserted-session\",\"settled\":%t,\"foregroundActive\":%t,\"backgroundActive\":false,\"pendingBackgroundTerminals\":false,\"activeSubagents\":0}}\n\n", msg.ID, done, !done)
			}
		case http.MethodGet:
			ch := make(chan string, 32)
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

	core, db, key, chatID, _ := setupKitSteerOrchestration(t, server.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	insertDashboardEventFixture(t, ctx, db, key, chatID, "m1")
	done := make(chan error, 1)
	go func() {
		_, err := core.ProcessThreadEvents(ctx, key.ProjectID, key.ThreadID)
		done <- err
	}()

	select {
	case <-firstPromptRunning:
	case <-ctx.Done():
		t.Fatal("timed out waiting for first prompt")
	}

	insertDashboardEventFixture(t, ctx, db, key, chatID, "m2")
	select {
	case <-secondPromptInserted:
	case <-ctx.Done():
		t.Fatal("timed out waiting for steered prompt insertion")
	}

	api := &KitRuntimeProtocol{config: KitRuntimeProtocolConfig{Core: &ServiceCore{db: db}}}
	r := assistantRuntimeRecord{ID: key.RuntimeID, ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeVersion: 3}
	interrupted, err := api.InterruptTurn(ctx, r, key.ThreadID)
	require.NoError(t, err)
	require.True(t, interrupted)
	require.NoError(t, <-done)

	busy, err := assistantrepo.New(db).AssistantHasUnsettledEvents(ctx, assistantrepo.AssistantHasUnsettledEventsParams{
		ProjectID: key.ProjectID, AssistantID: key.AssistantID,
	})
	require.NoError(t, err)
	require.True(t, busy.Valid)
	require.False(t, busy.Bool, "both inserted events must be settled after turn cancel")
}

func TestKitLegacyGatewayKeepsOneTurnPerEvent(t *testing.T) {
	t.Parallel()
	const catalog = `{"configOptions":[{"configId":"model","type":"select","currentValue":"speakeasy:openai/gpt-4o-mini","options":[{"value":"speakeasy:openai/gpt-4o-mini"}]},{"configId":"reasoning_effort","type":"select","currentValue":"medium","options":[{"value":"low"},{"value":"medium"},{"value":"high"}]}]}`

	var mu sync.Mutex
	streams := map[string]chan string{}
	connections := 0
	turn1Active := false
	turn1Released := make(chan struct{})
	firstPromptRunning := make(chan struct{})
	sawDelivery := false
	promptCount := 0
	promptWhileActive := false

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
				mu.Lock()
				connections++
				connID := fmt.Sprintf("legacy-conn-%d", connections)
				mu.Unlock()
				w.Header().Set("Acp-Connection-Id", connID)
				// Kit 0.2.x: kit/session/status advertised, no prompt delivery capability.
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{"_meta":{"kit/session/status":{}}}}}`, msg.ID)
				return
			}
			mu.Lock()
			ch := streams[session]
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			switch msg.Method {
			case "session/new":
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, strings.TrimSuffix(catalog, "}")+`,"sessionId":"legacy-fifo-session"}`)
			case "session/resume":
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, catalog)
			case "session/prompt":
				mu.Lock()
				if _, ok := msg.Params["delivery"]; ok {
					sawDelivery = true
				}
				if turn1Active {
					promptWhileActive = true
				}
				promptCount++
				n := promptCount
				if n == 1 {
					turn1Active = true
				}
				mu.Unlock()
				msgID := fmt.Sprintf("u%d", n)
				ch <- fmt.Sprintf(`data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"legacy-fifo-session","update":{"sessionUpdate":"user_message","messageId":%q,"content":[{"type":"text","text":"m%d"}]}}}`+"\n\n", msgID, n)
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"messageId\":%q}}\n\n", msg.ID, msgID)
				if n == 1 {
					close(firstPromptRunning)
				}
			case "kit/session/status":
				mu.Lock()
				active := turn1Active
				mu.Unlock()
				if active {
					select {
					case <-turn1Released:
						mu.Lock()
						turn1Active = false
						mu.Unlock()
						active = false
						ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"legacy-fifo-session","update":{"sessionUpdate":"agent_message_chunk","messageId":"a1","content":{"type":"text","text":"reply1"}}}}` + "\n\n"
						ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"legacy-fifo-session","update":{"sessionUpdate":"state_update","state":"idle","stopReason":"end_turn"}}}` + "\n\n"
					default:
					}
				} else {
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"legacy-fifo-session","update":{"sessionUpdate":"agent_message_chunk","messageId":"a2","content":{"type":"text","text":"reply2"}}}}` + "\n\n"
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"legacy-fifo-session","update":{"sessionUpdate":"state_update","state":"idle","stopReason":"end_turn"}}}` + "\n\n"
				}
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"sessionId\":\"legacy-fifo-session\",\"settled\":%t,\"foregroundActive\":%t,\"backgroundActive\":false,\"pendingBackgroundTerminals\":false,\"activeSubagents\":0}}\n\n", msg.ID, !active, active)
			}
		case http.MethodGet:
			ch := make(chan string, 32)
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

	core, db, key, chatID, _ := setupKitSteerOrchestration(t, server.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	insertDashboardEventFixture(t, ctx, db, key, chatID, "first")
	done := make(chan error, 1)
	go func() {
		_, err := core.ProcessThreadEvents(ctx, key.ProjectID, key.ThreadID)
		done <- err
	}()

	select {
	case <-firstPromptRunning:
	case <-ctx.Done():
		t.Fatal("timed out waiting for first prompt")
	}

	insertDashboardEventFixture(t, ctx, db, key, chatID, "second")
	// Let several status ticks pass while turn 1 is still active; legacy gateway
	// must not receive event 2 mid-turn.
	require.Never(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return promptCount > 1
	}, 400*time.Millisecond, 25*time.Millisecond, "legacy gateway must not deliver event 2 while turn 1 is active")
	close(turn1Released)

	require.NoError(t, <-done)

	mu.Lock()
	require.False(t, sawDelivery, "legacy gateway must not receive delivery field")
	require.False(t, promptWhileActive, "legacy gateway must keep one-turn-per-event behavior")
	require.Equal(t, 2, connections, "each event runs its own turn on a legacy gateway")
	require.Equal(t, 2, promptCount)
	mu.Unlock()
}

func TestKitSteerSlackThreadPersistsOrderedUserMessagesAndSettlesAll(t *testing.T) {
	t.Parallel()
	const catalog = `{"configOptions":[{"configId":"model","type":"select","currentValue":"speakeasy:openai/gpt-4o-mini","options":[{"value":"speakeasy:openai/gpt-4o-mini"}]},{"configId":"reasoning_effort","type":"select","currentValue":"medium","options":[{"value":"low"},{"value":"medium"},{"value":"high"}]}]}`

	var mu sync.Mutex
	streams := map[string]chan string{}
	settled := false
	receivedPrompts := []string{}
	firstPromptRunning := make(chan struct{})

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
				w.Header().Set("Acp-Connection-Id", "slack-steer-conn")
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{"_meta":{"kit/session/status":{}},"session":{"prompt":{"delivery":{"steer":{},"queue":{}}}}}}}`, msg.ID)
				return
			}
			mu.Lock()
			ch := streams[session]
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			switch msg.Method {
			case "session/new":
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, strings.TrimSuffix(catalog, "}")+`,"sessionId":"slack-steer-session"}`)
			case "session/prompt":
				var delivery string
				_ = json.Unmarshal(msg.Params["delivery"], &delivery)
				if delivery != kitPromptDeliverySteer {
					t.Errorf("expected steer delivery, got %q", delivery)
				}
				promptJSON := string(msg.Params["prompt"])
				mu.Lock()
				receivedPrompts = append(receivedPrompts, promptJSON)
				n := len(receivedPrompts)
				if n == 2 {
					settled = true
				}
				mu.Unlock()
				msgID := fmt.Sprintf("slack-u%d", n)
				userText := fmt.Sprintf("slack-msg-%d", n)
				ch <- fmt.Sprintf(`data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"slack-steer-session","update":{"sessionUpdate":"user_message","messageId":%q,"content":[{"type":"text","text":%q}]}}}`+"\n\n", msgID, userText)
				if n == 1 {
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"slack-steer-session","update":{"sessionUpdate":"state_update","state":"running"}}}` + "\n\n"
				}
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"messageId\":%q}}\n\n", msg.ID, msgID)
				if n == 1 {
					close(firstPromptRunning)
				}
				if n == 2 {
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"slack-steer-session","update":{"sessionUpdate":"agent_message_chunk","messageId":"slack-a1","content":{"type":"text","text":"posted to slack"}}}}` + "\n\n"
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"slack-steer-session","update":{"sessionUpdate":"state_update","state":"idle","stopReason":"end_turn"}}}` + "\n\n"
				}
			case "kit/session/status":
				mu.Lock()
				done := settled
				mu.Unlock()
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"sessionId\":\"slack-steer-session\",\"settled\":%t,\"foregroundActive\":%t,\"backgroundActive\":false,\"pendingBackgroundTerminals\":false,\"activeSubagents\":0}}\n\n", msg.ID, done, !done)
			}
		case http.MethodGet:
			ch := make(chan string, 32)
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

	core, db, key, chatID, _ := setupKitSteerOrchestration(t, server.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	require.NoError(t, assistantrepo.New(db).SetKitThreadSourceFixture(ctx, assistantrepo.SetKitThreadSourceFixtureParams{
		ProjectID:  key.ProjectID,
		ThreadID:   key.ThreadID,
		SourceKind: sourceKindSlack,
	}))

	insertSlackEvent := func(userID, text, ts string) {
		norm, err := json.Marshal(slackEventPayload{
			TeamID:    "T1",
			ChannelID: "C1",
			ThreadID:  "1700000000.000100",
			UserID:    userID,
			Text:      text,
			Timestamp: ts,
			Addressed: true,
		})
		require.NoError(t, err)
		_, err = assistantrepo.New(db).InsertAssistantThreadEvent(ctx, assistantrepo.InsertAssistantThreadEventParams{
			AssistantThreadID:     key.ThreadID,
			AssistantID:           key.AssistantID,
			ProjectID:             key.ProjectID,
			EventID:               uuid.NewString(),
			CorrelationID:         "T1:C1:1700000000.000100",
			Status:                eventStatusPending,
			NormalizedPayloadJson: norm,
			SourcePayloadJson:     []byte(`{}`),
		})
		require.NoError(t, err)
	}

	// Event 1 from Alice starts the turn.
	insertSlackEvent("U_ALICE", "check deployment status", "1700000000.000100")

	done := make(chan error, 1)
	go func() {
		_, err := core.ProcessThreadEvents(ctx, key.ProjectID, key.ThreadID)
		done <- err
	}()

	select {
	case <-firstPromptRunning:
	case <-ctx.Done():
		t.Fatal("timed out waiting for first slack prompt")
	}

	// Event 2 from Bob arrives mid-turn; must steer immediately without waiting
	// or comparing sender identity.
	insertSlackEvent("U_BOB", "also check staging logs", "1700000001.000200")

	require.NoError(t, <-done)

	mu.Lock()
	require.Len(t, receivedPrompts, 2)
	require.Contains(t, receivedPrompts[0], "U_ALICE")
	require.Contains(t, receivedPrompts[0], "check deployment status")
	require.Contains(t, receivedPrompts[1], "U_BOB")
	require.Contains(t, receivedPrompts[1], "also check staging logs")
	require.Contains(t, receivedPrompts[1], "platform_slack_send_message", "source instructions travel with steered Slack events")
	mu.Unlock()

	busy, err := assistantrepo.New(db).AssistantHasUnsettledEvents(ctx, assistantrepo.AssistantHasUnsettledEventsParams{
		ProjectID: key.ProjectID, AssistantID: key.AssistantID,
	})
	require.NoError(t, err)
	require.True(t, busy.Valid)
	require.False(t, busy.Bool, "both Slack events must settle")

	messages, err := chatrepo.New(db).ListLatestGenerationChatMessages(ctx, chatrepo.ListLatestGenerationChatMessagesParams{
		ProjectID: key.ProjectID, ChatID: chatID,
	})
	require.NoError(t, err)
	var userTexts []string
	foundAssistant := false
	for _, m := range messages {
		if m.Role == "user" {
			userTexts = append(userTexts, m.Content)
		}
		if m.Role == "assistant" && m.Content == "posted to slack" && m.FinishReason.String == "end_turn" {
			foundAssistant = true
		}
	}
	require.Equal(t, []string{"slack-msg-1", "slack-msg-2"}, userTexts)
	require.True(t, foundAssistant)
}
