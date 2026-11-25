package intentpolicy_test

import (
	"bytes"
	"encoding/json"
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
	require.Equal(t, intentpolicy.AssessmentID(chatID, 3, r.GetToolCall().GetToolCallId(), g.Version, "m").String(), r.GetId())
	require.Equal(t, "block", r.GetDecision())
	require.Contains(t, r.GetDecidedBy(), "no_remote_destruction")
	require.False(t, r.GetActor().HasUserEmail(), "an unknown email stays absent")
	require.Equal(t, int32(3), r.GetToolCall().GetGeneration())
	require.Equal(t, "2026-11-25T10:00:00Z", r.GetToolCall().GetOccurredAt())
	require.Positive(t, r.GetToolCall().GetArgumentsBytes())
	require.Equal(t, "2026-11-25T11:00:05Z", r.GetEvaluatedAt())
	require.InDelta(t, 0.01, r.GetUsage().GetCostUsd(), 1e-9)
	require.False(t, r.HasEvaluatorError())

	// Labels are sorted by key and typed by value.
	require.Len(t, r.GetLabels(), 3)
	effect, coverage := r.GetLabels()[0], r.GetLabels()[1]
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

	payload, err := proto.Marshal(sampleRecord(t))
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
	_, ok := f.Schema().Lookup("decision")
	require.True(t, ok)
	_, ok = f.Schema().Lookup("tool_call", "arguments_sha256")
	require.True(t, ok)
}

func sampleRecord(t *testing.T) *intentpolicyv1.Assessment {
	t.Helper()
	g := intentpolicy.DefaultGuardrail()
	engine, err := intentpolicy.NewEngine(g)
	require.NoError(t, err)

	action := bashCall("git push --force origin main")
	action.ChatID = uuid.New()
	action.OccurredAt = time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	labels := intentpolicy.LabelSet{
		"effect_class":     {Value: []string{"destructive", "outbound"}, Rank: -1, Probability: 0.9, Distribution: map[string]float64{"destructive": 0.9}, Rationale: "force push"},
		"target_locations": {Value: []string{"remote"}, Rank: -1, Probability: 0.9, Distribution: nil, Rationale: ""},
		"effect_coverage":  {Value: "understood", Rank: 2, Probability: 0.8, Distribution: nil, Rationale: ""},
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
