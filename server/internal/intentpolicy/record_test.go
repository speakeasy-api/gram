package intentpolicy_test

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/parquet-go/parquet-go"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	intentpolicyv1 "github.com/speakeasy-api/gram/infra/gen/gram/intentpolicy/v1"
	"github.com/speakeasy-api/gram/infra/pkg/storage"
	"github.com/speakeasy-api/gram/infra/pkg/storagebindings"
	"github.com/speakeasy-api/gram/server/internal/intentpolicy"
)

func TestRecordProjectsAssessment(t *testing.T) {
	t.Parallel()
	r := sampleRecord(t)
	g := intentpolicy.DefaultGuardrail()

	chatID := uuid.MustParse(r.GetToolCall().GetChatId())
	messageID := uuid.MustParse(r.GetToolCall().GetMessageId())
	require.Equal(t, intentpolicy.AssessmentID(chatID, 3, messageID, r.GetToolCall().GetToolCallId(), g.Version, "m").String(), r.GetId())
	require.NotEqual(t, r.GetId(), intentpolicy.AssessmentID(chatID, 3, uuid.New(), r.GetToolCall().GetToolCallId(), g.Version, "m").String(),
		"a reused tool-call id in another message is another assessment")
	require.Equal(t, "block", r.GetDecision())
	require.Equal(t, []string{"no_remote_destruction"}, r.GetDecidedBy())
	require.False(t, r.GetActor().HasUserEmail(), "an unknown email stays absent")
	require.Equal(t, int32(3), r.GetToolCall().GetGeneration())
	require.Equal(t, "2026-11-25T10:00:00Z", r.GetToolCall().GetOccurredAt())
	require.Positive(t, r.GetToolCall().GetArgumentsBytes())
	require.Equal(t, "2026-11-25T11:00:05Z", r.GetEvaluatedAt())
	require.InDelta(t, 0.01, r.GetUsage().GetCostUsd(), 1e-9)
	require.False(t, r.HasEvaluatorError())

	// Labels are sorted by key and typed by value.
	require.Len(t, r.GetLabels(), len(g.Questions))
	keys := make([]string, 0, len(r.GetLabels()))
	for _, l := range r.GetLabels() {
		keys = append(keys, l.GetKey())
	}
	require.True(t, slices.IsSorted(keys))
	effect, coverage := r.GetLabels()[1], r.GetLabels()[2]
	require.Equal(t, "effect_class", effect.GetKey())
	require.Equal(t, intentpolicyv1.Assessment_Label_KIND_MULTI_NOUL, effect.GetKind())
	require.Equal(t, []string{"destructive", "outbound"}, effect.GetMembers())
	require.Equal(t, "force push", effect.GetRationale())
	require.Equal(t, "effect_coverage", coverage.GetKey())
	require.Equal(t, intentpolicyv1.Assessment_Label_KIND_CHOICE, coverage.GetKind())
	require.Equal(t, "understood", coverage.GetChoiceValue())
	require.Equal(t, int32(2), coverage.GetRank())

	require.Len(t, r.GetContracts(), len(g.Contracts))
	for _, c := range r.GetContracts() {
		require.Empty(t, c.GetError(), "every label the contracts read is answered")
		for _, v := range c.GetInputs() {
			require.True(t, json.Valid([]byte(v)), "inputs are JSON-encoded")
		}
	}

	// Raw arguments never appear in the record.
	encoded, err := proto.Marshal(r)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "push --force")
}

func TestRecordEncodesThroughLakeBinding(t *testing.T) {
	t.Parallel()

	record := sampleRecord(t)
	payload, err := proto.Marshal(record)
	require.NoError(t, err)

	def := storagebindings.GramIntentpolicyV1LakePrimary()
	row, err := def.Decode(payload, storage.Metadata{MessageID: "m1", ReceivedMicros: 1})
	require.NoError(t, err)

	var buf bytes.Buffer
	w := parquet.NewWriter(&buf, def.Schema)
	_, err = w.WriteRows([]parquet.Row{row})
	require.NoError(t, err)
	require.NoError(t, w.Close())

	f, err := parquet.OpenFile(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	require.Equal(t, int64(1), f.NumRows())

	rows := make([]parquet.Row, 1)
	reader := parquet.NewReader(f)
	n, err := reader.ReadRows(rows)
	if err != nil {
		require.ErrorIs(t, err, io.EOF)
	}
	require.Equal(t, 1, n)
	require.NoError(t, reader.Close())

	value := func(path ...string) parquet.Value {
		t.Helper()
		col, ok := f.Schema().Lookup(path...)
		require.True(t, ok, "column %v exists", path)
		for _, v := range rows[0] {
			if v.Column() == col.ColumnIndex {
				return v
			}
		}
		require.FailNow(t, "column has no value", "%v", path)
		return parquet.Value{}
	}
	require.Equal(t, record.GetId(), value("id").String())
	require.Equal(t, "block", value("decision").String())
	require.Equal(t, record.GetToolCall().GetToolCallId(), value("tool_call", "tool_call_id").String())
	require.Equal(t, record.GetToolCall().GetArgumentsBytes(), value("tool_call", "arguments_bytes").Int64())
	require.Equal(t, record.GetGuardrail().GetVersion(), value("guardrail", "version").String())
	require.Equal(t, "m1", value("__pubsub", "message_id").String())
	_, ok := f.Schema().Lookup("tool_call", "arguments_sha256")
	require.False(t, ok, "no argument digest is stored")
}

func sampleRecord(t *testing.T) *intentpolicyv1.Assessment {
	t.Helper()
	g := intentpolicy.DefaultGuardrail()
	engine, err := intentpolicy.NewEngine(g)
	require.NoError(t, err)

	action := bashCall("git push --force origin main")
	action.ChatID = uuid.New()
	action.OccurredAt = time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	// Every default label is answered, so the block comes from the remote
	// destruction contract alone and not from unanswered labels.
	labels := intentpolicy.LabelSet{
		"audience":         {Value: "group", Rank: -1, Probability: 0.9, Distribution: nil, Rationale: ""},
		"effect_class":     {Value: []string{"destructive", "outbound"}, Rank: -1, Probability: 0.9, Distribution: map[string]float64{"destructive": 0.9}, Rationale: "force push"},
		"effect_coverage":  {Value: "understood", Rank: 2, Probability: 0.8, Distribution: nil, Rationale: ""},
		"exec_code_origin": {Value: "none", Rank: -1, Probability: 0.9, Distribution: nil, Rationale: ""},
		"payload_data":     {Value: []string{}, Rank: -1, Probability: 0.9, Distribution: nil, Rationale: ""},
		"target_locations": {Value: []string{"remote"}, Rank: -1, Probability: 0.9, Distribution: nil, Rationale: ""},
	}
	decision, decidedBy, outcomes := engine.Evaluate(action, labels)
	a := intentpolicy.Assessment{
		Action:         action,
		Labels:         labels,
		Decision:       decision,
		DecidedBy:      decidedBy,
		Outcomes:       outcomes,
		EvaluatorError: "",
		Usage:          intentpolicy.Usage{InputTokens: 10, OutputTokens: 5, CostUSD: 0.01, LatencyMs: 120},
		Descriptor:     intentpolicy.Descriptor{Evaluator: "gemini", Provider: "openrouter", Model: "m", PromptVersion: "p1"},
	}
	rc := intentpolicy.RecordContext{
		AnalysisID:     uuid.New(),
		OrganizationID: "org",
		ProjectID:      uuid.New(),
		UserID:         "user",
		UserEmail:      "",
		Generation:     3,
		TriggerKind:    intentpolicy.TriggerDashboard,
		RequestedAt:    time.Date(2026, 11, 25, 11, 0, 0, 0, time.UTC),
		EvaluatedAt:    time.Date(2026, 11, 25, 11, 0, 5, 0, time.UTC),
	}

	r := intentpolicy.Record(rc, g, a)
	return r
}
