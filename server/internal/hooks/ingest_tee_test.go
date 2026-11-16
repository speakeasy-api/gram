package hooks

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	otelsvc "github.com/speakeasy-api/gram/server/internal/otel"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/otel/enrich"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// teeFeatures stubs the product features client with the two gates the
// event feed tee consults; every other feature reads as disabled.
type teeFeatures struct {
	logs   bool
	toolIO bool
}

func (f teeFeatures) IsFeatureEnabled(_ context.Context, _ string, feature productfeatures.Feature) (bool, error) {
	switch feature {
	case productfeatures.FeatureLogs:
		return f.logs, nil
	case productfeatures.FeatureToolIOLogs:
		return f.toolIO, nil
	default:
		return false, nil
	}
}

// teeTestRows is what the ingest endpoint records for a Codex tool result:
// the attributes hookTelemetryBaseAttrs and the tool call block write.
func teeTestRows(timestamp time.Time) []hookTelemetryRow {
	return []hookTelemetryRow{{
		timestamp: timestamp,
		toolName:  "shell",
		attrs: map[attr.Key]any{
			attr.EventSourceKey:            "hook",
			attr.HookEventKey:              "PostToolUse",
			attr.HookSourceKey:             "codex",
			attr.TraceIDKey:                "0123456789abcdef0123456789abcdef",
			attr.SpanIDKey:                 "0123456789abcdef",
			attr.ToolNameKey:               "shell",
			attr.GenAIToolCallIDKey:        "call-1",
			attr.GenAIToolCallArgumentsKey: `{"command":"ls"}`,
			attr.HookIsInterruptKey:        false,
			attr.ToolCallDurationKey:       0.75,
			attr.GenAIUsageInputTokensKey:  12,
			attr.HookReplayedKey:           true,
			attr.Key("gram.source.json"):   map[string]any{"nested": "value"},
		},
	}}
}

func teeTestPayload(idempotencyKey string) *gen.IngestPayload {
	payload := canonicalIngestPayload("codex", "tool.completed", "codex-session-1")
	version := "1.2.3"
	payload.Source.AdapterVersion = &version
	if idempotencyKey != "" {
		payload.IdempotencyKey = &idempotencyKey
	}
	return payload
}

// teeAgentAttributes runs the agent attribute enrichers over a published
// record, as the transform does, and indexes what they wrote by key.
func teeAgentAttributes(t *testing.T, record *otelv1.InboundLogRecord) map[attribute.Key]attribute.Value {
	t.Helper()
	out := map[attribute.Key]attribute.Value{}
	for _, enricher := range enrich.LogAgentAttributes() {
		attrs, err := enricher.Enrich(t.Context(), record)
		require.NoError(t, err)
		for _, kv := range attrs {
			out[kv.Key] = kv.Value
		}
	}
	return out
}

func teeStringAttr(t *testing.T, record *otelv1.InboundLogRecord, key string) string {
	t.Helper()
	return teeAttrByKey(t, record.GetAttributes(), key).GetStringValue()
}

func TestTeeCanonicalHookPublishesWhatTheIngestEdgeWould(t *testing.T) {
	t.Parallel()

	ctx, ti, published := newTeeTestService(t)
	authCtx := hookAuthContext(t, ctx)
	timestamp := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

	ti.service.teeCanonicalHookToEventFeed(ctx, teeTestPayload("idem-1"), authCtx, &SessionMetadata{}, "codex", "", teeTestRows(timestamp))
	ti.service.otelTeeDrains.Wait()

	records := published.records()
	require.Len(t, records, 1)
	record := records[0]
	require.NoError(t, otelsvc.ValidateInboundLogRecord(record))
	require.Equal(t, hookIngestRecordID("idem-1", 0), record.GetRecordId())
	require.Equal(t, uint64(timestamp.UnixNano()), record.GetTimeUnixNano())
	require.NotZero(t, record.GetObservedTimeUnixNano())
	require.Equal(t, "PostToolUse", record.GetEventName())
	require.Equal(t, []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}, record.GetTraceId())
	require.Equal(t, []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}, record.GetSpanId())
	require.Nil(t, record.GetBody())
	require.Equal(t, dialect.HooksLogScope, record.GetScope().GetName())
	require.Equal(t, "codex", teeAttrByKey(t, record.GetResource().GetAttributes(), string(attr.ServiceNameKey)).GetStringValue())
	require.Equal(t, "1.2.3", teeAttrByKey(t, record.GetResource().GetAttributes(), string(attr.ServiceVersionKey)).GetStringValue())
	require.Equal(t, otelsvc.ProvenanceSource, record.GetProvenance().GetSource())
	require.Equal(t, authCtx.ActiveOrganizationID, record.GetProvenance().GetOrganizationId())
	require.Equal(t, authCtx.ProjectID.String(), record.GetProvenance().GetProjectId())

	// Go-typed attributes keep their type on the wire; anything without a
	// scalar form rides as JSON.
	require.Equal(t, "shell", teeStringAttr(t, record, string(attr.ToolNameKey)))
	require.False(t, teeAttrByKey(t, record.GetAttributes(), string(attr.HookIsInterruptKey)).GetBoolValue())
	require.True(t, teeAttrByKey(t, record.GetAttributes(), string(attr.HookReplayedKey)).GetBoolValue())
	require.InDelta(t, 0.75, teeAttrByKey(t, record.GetAttributes(), string(attr.ToolCallDurationKey)).GetDoubleValue(), 0)
	require.Equal(t, int64(12), teeAttrByKey(t, record.GetAttributes(), string(attr.GenAIUsageInputTokensKey)).GetIntValue())
	require.JSONEq(t, `{"nested":"value"}`, teeStringAttr(t, record, "gram.source.json"))
}

func TestTeeCanonicalHookRecordIDs(t *testing.T) {
	t.Parallel()

	timestamp := time.Now()
	rows := append(teeTestRows(timestamp), teeTestRows(timestamp)...)
	publish := func(t *testing.T, idempotencyKey string) []*otelv1.InboundLogRecord {
		t.Helper()
		ctx, ti, published := newTeeTestService(t)
		ti.service.teeCanonicalHookToEventFeed(ctx, teeTestPayload(idempotencyKey), hookAuthContext(t, ctx), &SessionMetadata{}, "codex", "", cloneHookTelemetryRows(rows))
		ti.service.otelTeeDrains.Wait()
		records := published.records()
		require.Len(t, records, 2)
		return records
	}

	t.Run("an idempotency key makes the ids deterministic", func(t *testing.T) {
		t.Parallel()
		first := publish(t, "idem-1")
		second := publish(t, "idem-1")
		require.NotEqual(t, first[0].GetRecordId(), first[1].GetRecordId(), "each row of one delivery has its own id")
		require.Equal(t, first[0].GetRecordId(), second[0].GetRecordId(), "a replay of the delivery yields the same id")
		require.Equal(t, first[1].GetRecordId(), second[1].GetRecordId())
		require.NotEqual(t, first[0].GetRecordId(), publish(t, "idem-2")[0].GetRecordId())
	})

	t.Run("no idempotency key means fresh ids", func(t *testing.T) {
		t.Parallel()
		first := publish(t, "")
		second := publish(t, "")
		require.NotEqual(t, first[0].GetRecordId(), second[0].GetRecordId())
		require.NotEqual(t, first[0].GetRecordId(), first[1].GetRecordId())
	})
}

func TestStampHookTeeAttributesSaysWhatTheRequestKnew(t *testing.T) {
	t.Parallel()

	payload := teeTestPayload("idem-1")
	raw := "tool.completed"
	payload.Source.RawEventName = &raw
	turnID := "turn-1"
	payload.Session.TurnID = &turnID
	permission := "ask"
	payload.Data = &gen.HookIngestData{ToolCall: &gen.HookToolCallData{PermissionType: &permission}}
	metadata := &SessionMetadata{UserID: "user-1", UserEmail: "dev@example.com"}

	allowed := map[attr.Key]any{}
	stampHookTeeAttributes(allowed, payload, metadata, "")
	require.Equal(t, "codex", allowed[attr.HookAdapterKey])
	require.Equal(t, "tool.completed", allowed[attr.HookRawEventNameKey])
	require.Equal(t, "tool.completed", allowed[attr.HookCanonicalEventTypeKey])
	require.Equal(t, "ask", allowed[attr.HookPermissionTypeKey])
	require.Equal(t, "allow", allowed[attr.HookDecisionKey])
	require.Equal(t, "turn-1", allowed[attr.HookTurnIDKey])
	require.Equal(t, "codex-session-1", allowed[attr.SessionIDKey], "the agent's own session id, raw")
	require.Equal(t, "user-1", allowed[attr.UserIDKey])
	require.Equal(t, "dev@example.com", allowed[attr.UserEmailKey])

	denied := map[attr.Key]any{}
	stampHookTeeAttributes(denied, payload, metadata, "shadow MCP")
	require.Equal(t, "deny", denied[attr.HookDecisionKey])
}

// teePublished is what a capturing publisher took, safe to read while the
// tee's goroutines publish.
type teePublished struct {
	mu   sync.Mutex
	list []*otelv1.InboundLogRecord
}

func (p *teePublished) records() []*otelv1.InboundLogRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.list)
}

// byRecordID finds a record, since tees of separate requests publish in no
// particular order.
func (p *teePublished) byRecordID(t *testing.T, id string) *otelv1.InboundLogRecord {
	t.Helper()
	for _, record := range p.records() {
		if record.GetRecordId() == id {
			return record
		}
	}
	t.Fatalf("no record with id %s", id)
	return nil
}

// newTeeTestService wires a hooks service with a capturing publisher, the
// way the request path sees it.
func newTeeTestService(t *testing.T) (context.Context, *testInstance, *teePublished) {
	t.Helper()
	ctx, ti := newTestHooksService(t)
	published := &teePublished{mu: sync.Mutex{}, list: nil}
	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		record, ok := args.Get(1).(*otelv1.InboundLogRecord)
		require.True(t, ok)
		published.mu.Lock()
		defer published.mu.Unlock()
		published.list = append(published.list, record)
	}).Return(gcp.NewSuccessPublishResult()).Maybe()
	ti.service.otelLogPublisher = publisher
	return ctx, ti, published
}

func teeToolPayload(eventType, sessionID, toolCallID, idempotencyKey string) *gen.IngestPayload {
	payload := canonicalIngestPayload("codex", eventType, sessionID)
	toolName := "shell"
	durationMs := 750.0
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:         &toolCallID,
			Name:       &toolName,
			Input:      map[string]any{"command": "ls"},
			Output:     map[string]any{"stdout": "README.md"},
			DurationMs: &durationMs,
		},
	}
	if idempotencyKey != "" {
		payload.IdempotencyKey = &idempotencyKey
	}
	return payload
}

func TestIngest_TeesToolCallIntoEventFeed(t *testing.T) {
	t.Parallel()

	ctx, ti, published := newTeeTestService(t)
	authCtx := hookAuthContext(t, ctx)

	result, err := ti.service.Ingest(ctx, teeToolPayload("tool.requested", "codex-tee-session", "call-tee-1", "idem-tee-1"))
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)
	result, err = ti.service.Ingest(ctx, teeToolPayload("tool.completed", "codex-tee-session", "call-tee-1", "idem-tee-2"))
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)
	ti.service.otelTeeDrains.Wait()

	require.Len(t, published.records(), 2)
	request := published.byRecordID(t, hookIngestRecordID("idem-tee-1", 0))
	response := published.byRecordID(t, hookIngestRecordID("idem-tee-2", 0))
	for _, record := range []*otelv1.InboundLogRecord{request, response} {
		require.NoError(t, otelsvc.ValidateInboundLogRecord(record))
		require.Equal(t, dialect.HooksLogScope, record.GetScope().GetName())
		require.Equal(t, otelsvc.ProvenanceSource, record.GetProvenance().GetSource())
		require.Equal(t, authCtx.ActiveOrganizationID, record.GetProvenance().GetOrganizationId())
		require.Equal(t, authCtx.ProjectID.String(), record.GetProvenance().GetProjectId())
		require.Equal(t, "codex", teeAttrByKey(t, record.GetResource().GetAttributes(), string(attr.ServiceNameKey)).GetStringValue())
		require.Equal(t, "codex", teeStringAttr(t, record, string(attr.HookSourceKey)))
		require.Equal(t, "codex", teeStringAttr(t, record, string(attr.HookAdapterKey)))
		require.Equal(t, "allow", teeStringAttr(t, record, string(attr.HookDecisionKey)))
		require.Equal(t, "codex-tee-session", teeStringAttr(t, record, string(attr.SessionIDKey)))
		require.Equal(t, "call-tee-1", teeStringAttr(t, record, string(attr.GenAIToolCallIDKey)))
		require.Equal(t, "shell", teeStringAttr(t, record, string(attr.ToolNameKey)))
		require.Equal(t, conv.PtrValOr(authCtx.Email, ""), teeStringAttr(t, record, string(attr.UserEmailKey)), "the actor the endpoint attributed the event to")
		require.NotZero(t, record.GetTimeUnixNano())
		require.NotZero(t, record.GetObservedTimeUnixNano())
	}

	// The cross-package contract: the enrichers name what the tee published.
	require.Equal(t, "PreToolUse", request.GetEventName())
	require.Equal(t, "tool.requested", teeStringAttr(t, request, string(attr.HookCanonicalEventTypeKey)))
	require.Equal(t, dialect.EventTypeToolCall, teeAgentAttributes(t, request)[enrich.AgentEventTypeKey].AsString())

	require.Equal(t, "PostToolUse", response.GetEventName())
	columns := teeAgentAttributes(t, response)
	require.Equal(t, dialect.EventTypeToolCallResult, columns[enrich.AgentEventTypeKey].AsString())
	require.Equal(t, dialect.OutcomeOK, columns[enrich.AgentOutcomeKey].AsString())
	require.Equal(t, int64(750_000_000), columns[enrich.AgentDurationNanoKey].AsInt64())
	require.JSONEq(t, `{"stdout":"README.md"}`, teeStringAttr(t, response, string(attr.GenAIToolCallResultKey)), "tool IO rides along when the org keeps it")
}

func TestIngest_TeesEveryEventKindTheDialectClassifies(t *testing.T) {
	t.Parallel()

	ctx, ti, published := newTeeTestService(t)

	withRaw := func(adapter, eventType, raw, session, idem string) *gen.IngestPayload {
		payload := canonicalIngestPayload(adapter, eventType, session)
		payload.Source.RawEventName = &raw
		payload.IdempotencyKey = &idem
		return payload
	}
	toolCallID := "call-kinds-1"
	toolName := "shell"
	toolCall := func() *gen.HookToolCallData {
		return &gen.HookToolCallData{ID: &toolCallID, Name: &toolName, Input: map[string]any{"command": "ls"}}
	}

	// A Cursor tool request, a source other than Codex: a tool_call.
	cursor := withRaw("cursor", "tool.requested", "beforeMCPExecution", "cursor-kinds-session", "idem-kinds-cursor")
	cursor.Data = &gen.HookIngestData{ToolCall: toolCall()}
	// A Claude Code prompt: a prompt whose words go to chat, not here.
	prompt := withRaw("claude", "prompt.submitted", "UserPromptSubmit", "claude-kinds-session", "idem-kinds-prompt")
	prompt.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: new("list the files")}}
	// A Codex permission request: Speakeasy's verdict rides as a tool_decision.
	permission := withRaw("codex", "tool.requested", "PermissionRequest", "codex-kinds-session", "idem-kinds-permission")
	permission.Data = &gen.HookIngestData{ToolCall: toolCall()}
	permission.Data.ToolCall.PermissionType = new("default")
	// A Claude Code notification: nothing the vocabulary names, raw name kept.
	notification := withRaw("claude", "notification.reported", "Notification", "claude-kinds-session", "idem-kinds-notification")
	notification.Data = &gen.HookIngestData{Notification: &gen.HookNotificationData{Message: new("waiting for input")}}

	for _, payload := range []*gen.IngestPayload{cursor, prompt, permission, notification} {
		_, err := ti.service.Ingest(ctx, payload)
		require.NoError(t, err)
	}
	ti.service.otelTeeDrains.Wait()

	byName := map[string]*otelv1.InboundLogRecord{}
	for _, record := range published.records() {
		byName[record.GetEventName()] = record
	}
	classify := func(name string) string {
		t.Helper()
		record, ok := byName[name]
		require.True(t, ok, "no record named %s among %v", name, slices.Sorted(maps.Keys(byName)))
		return teeAgentAttributes(t, record)[enrich.AgentEventTypeKey].AsString()
	}
	require.Equal(t, dialect.EventTypeToolCall, classify("BeforeMCPExecution"))
	require.Equal(t, "cursor", teeStringAttr(t, byName["BeforeMCPExecution"], string(attr.HookSourceKey)))
	require.Equal(t, dialect.EventTypePrompt, classify("UserPromptSubmit"))
	require.Equal(t, dialect.EventTypeToolDecision, classify("PermissionRequest"))
	require.Equal(t, "allow", teeStringAttr(t, byName["PermissionRequest"], string(attr.HookDecisionKey)))
	require.Equal(t, dialect.EventTypeUnclassified, classify("Notification"), "an event the vocabulary does not name keeps its raw name and gets no type")
}

func TestIngest_TeeSkipsADuplicateDelivery(t *testing.T) {
	t.Parallel()

	ctx, ti, published := newTeeTestService(t)

	payload := teeToolPayload("tool.completed", "codex-dup-session", "call-dup-1", "idem-dup-1")
	_, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	_, err = ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	ti.service.otelTeeDrains.Wait()

	require.Len(t, published.records(), 1, "a retried delivery is persisted once and republished once")
}

func TestIngest_TeeFailureKeepsTheVerdict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Return(errors.New("broker unavailable")).Once()
	ti.service.otelLogPublisher = publisher

	result, err := ti.service.Ingest(ctx, teeToolPayload("tool.completed", "codex-fail-session", "call-fail-1", "idem-fail-1"))
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision, "a publish failure never changes the hook's verdict")
	ti.service.otelTeeDrains.Wait()

	publisher.AssertExpectations(t)
}

func TestIngest_TeeWithoutAPublisherPublishesNothing(t *testing.T) {
	t.Parallel()

	ctx, ti, published := newTeeTestService(t)
	ti.service.otelLogPublisher = nil

	_, err := ti.service.Ingest(ctx, teeToolPayload("tool.completed", "codex-nopub-session", "call-nopub-1", "idem-nopub-1"))
	require.NoError(t, err)

	ti.service.otelTeeDrains.Wait()

	require.Empty(t, published.records())
}

func TestIngest_TeeAppliesTheOrgGates(t *testing.T) {
	t.Parallel()

	t.Run("an org without tool IO logs gets the row with its tool IO scrubbed", func(t *testing.T) {
		t.Parallel()
		ctx, ti, published := newTeeTestService(t)
		ti.service.productFeatures = teeFeatures{logs: true, toolIO: false}

		_, err := ti.service.Ingest(ctx, teeToolPayload("tool.completed", "codex-scrub-session", "call-scrub-1", "idem-scrub-1"))
		require.NoError(t, err)
		ti.service.otelTeeDrains.Wait()

		records := published.records()
		require.Len(t, records, 1)
		record := records[0]
		require.False(t, teeHasAttr(record.GetAttributes(), string(attr.GenAIToolCallArgumentsKey)))
		require.False(t, teeHasAttr(record.GetAttributes(), string(attr.GenAIToolCallResultKey)))
		require.Equal(t, "shell", teeStringAttr(t, record, string(attr.ToolNameKey)), "the tool itself is not tool IO")
	})

	t.Run("an org without logs gets nothing", func(t *testing.T) {
		t.Parallel()
		ctx, ti, published := newTeeTestService(t)
		ti.service.productFeatures = teeFeatures{logs: false, toolIO: true}

		result, err := ti.service.Ingest(ctx, teeToolPayload("tool.completed", "codex-nologs-session", "call-nologs-1", "idem-nologs-1"))
		require.NoError(t, err)
		require.Equal(t, "allow", result.Decision)
		ti.service.otelTeeDrains.Wait()

		require.Empty(t, published.records())
	})
}

func TestIngest_TeeStripsAnAgentActorsIdentityLikeTheStoredRow(t *testing.T) {
	t.Parallel()

	ctx, ti, published := newTeeTestService(t)
	agentCtx := agentKeyContext(t, ctx, ti)

	_, err := ti.service.Ingest(agentCtx, teeToolPayload("tool.completed", "codex-agent-session", "call-agent-1", "idem-agent-1"))
	require.NoError(t, err)
	ti.service.otelTeeDrains.Wait()

	records := published.records()
	require.Len(t, records, 1)
	record := records[0]
	require.False(t, teeHasAttr(record.GetAttributes(), string(attr.UserEmailKey)), "an agent's self-reported identity is stripped from the copy too")
	require.False(t, teeHasAttr(record.GetAttributes(), string(attr.UserIDKey)))
	require.Equal(t, "agent", teeStringAttr(t, record, string(attr.AuthorizationActorTypeKey)))
}

func TestIngest_TeesTheDerivedSkillRowToo(t *testing.T) {
	t.Parallel()

	ctx, ti, published := newTeeTestService(t)

	raw := "PreToolUse"
	toolName := "Bash"
	toolID := "call-skill-tee"
	payload := canonicalIngestPayload("codex", "tool.requested", "codex-skill-tee-session")
	payload.Source.RawEventName = &raw
	payload.IdempotencyKey = new("idem-skill-tee")
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolID,
			Name:  &toolName,
			Input: map[string]any{"command": "cat .agents/skills/repo-review/SKILL.md"},
		},
		Skill: &gen.HookSkillData{Name: "repo-review"},
	}

	_, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	ti.service.otelTeeDrains.Wait()

	records := published.records()
	require.Len(t, records, 2, "the tool row and the derived skill activation")
	require.Equal(t, "PreToolUse", records[0].GetEventName())
	require.Equal(t, "Bash", teeStringAttr(t, records[0], string(attr.ToolNameKey)))
	require.Equal(t, "skill.activated", records[1].GetEventName())
	require.Equal(t, "Skill", teeStringAttr(t, records[1], string(attr.ToolNameKey)))
	require.NotEqual(t, records[0].GetRecordId(), records[1].GetRecordId())
}
