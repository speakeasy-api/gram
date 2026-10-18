package analytics

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/analytics"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sigint/chrepo"
	signalquery "github.com/speakeasy-api/gram/server/internal/telemetry/analytics/signals"
	"github.com/stretchr/testify/require"
)

type permittedConversation string

func (p permittedConversation) ReadableConversations(_ context.Context, ids []string) ([]string, error) {
	var allowed []string
	for _, id := range ids {
		if string(p) == id || p == "*" {
			allowed = append(allowed, id)
		}
	}
	return allowed, nil
}

func TestSignalFindingsSeparateMessageAndSessionCooccurrence(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	now := time.Now().UTC().Truncate(time.Second)
	project, sensorA, sensorB, signal := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	conversations := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	org := "findings-" + uuid.NewString()
	// A and B hit different messages in the first session; both hit one in the second.
	// A hit in the third session cannot enter a jointly answered denominator.
	cases := []struct {
		session int
		a, b    float64
		fail    bool
	}{{0, .9, .1, false}, {0, .1, .9, false}, {1, .9, .9, false}, {2, .9, .9, true}, {0, .9, .1, false}}
	var rows []chrepo.ReadingRow
	for i, sample := range cases {
		message := uuid.NewString()
		for side, sensor := range []uuid.UUID{sensorA, sensorB} {
			probability := sample.a
			if side == 1 {
				probability = sample.b
			}
			row := chrepo.ReadingRow{ID: uuid.New(), OrganizationID: org, ProjectID: project, SensorID: sensor, EventKind: "conversation.message", EventID: message, TriggerKind: "stream", EvaluationRequestedAt: now, EventOccurredAt: now.Add(-time.Duration(i+10) * time.Minute), EvaluatedAt: now, InsertedAt: now, Status: "answered", ResultType: "multi_label", DefinitionHash: "stable", ConfiguredModel: "classifier", CompilerVersion: "compiler", InputStrategy: "single_message", ConversationID: &conversations[sample.session], ConversationCaptureKind: "original", Probabilities: []chrepo.ProbabilityRow{{SignalID: signal, SignalSlug: "topic", Probability: probability}}}
			rows = append(rows, row, row)
			if side == 1 && sample.fail {
				row.Status, row.ResultType, row.Probabilities, row.EvaluatedAt = "failed", "", nil, now.Add(time.Second)
				rows = append(rows, row)
			}
		}
	}
	require.NoError(t, chrepo.New(conn).InsertReadings(t.Context(), rows))
	engine, err := NewEngine(nil, conn)
	require.NoError(t, err)
	request := SignalFindingsRequest{Query: signalquery.Request{Selection: signalquery.Selection{SensorID: sensorA.String(), SignalID: signal.String(), Threshold: .5}}, Peer: &signalquery.Selection{SensorID: sensorB.String(), SignalID: signal.String(), Threshold: .5}, ComparePrevious: true, Evidence: true}
	result, err := engine.SignalFindings(t.Context(), Tenant{OrganizationID: org, ProjectID: project.String()}, Window{FromUnixNano: now.Add(-time.Hour).UnixNano(), ToUnixNano: now.UnixNano()}, request, permittedConversation(conversations[1].String()))
	require.NoError(t, err)
	require.EqualValues(t, 4, result.Measurement.Summary["matched_messages"])
	require.EqualValues(t, 3, result.Measurement.Summary["matched_sessions"])
	require.InDelta(t, .5, result.Concentration["top_session_share"], .000001)
	require.InDelta(t, 1.0, result.Concentration["top_five_session_share"], .000001)
	require.EqualValues(t, 4, result.Cooccurrence[0]["joint_answered"])
	require.EqualValues(t, 1, result.Cooccurrence[0]["joint_matches"])
	require.InDelta(t, .25, result.Cooccurrence[0]["joint_share"], .000001)
	require.Equal(t, "same_message", result.Cooccurrence[0]["unit"])
	require.EqualValues(t, 2, result.Cooccurrence[1]["joint_answered"])
	require.EqualValues(t, 2, result.Cooccurrence[1]["joint_matches"])
	require.Equal(t, "same_session", result.Cooccurrence[1]["unit"])
	require.Len(t, result.Evidence, 1)
	require.Equal(t, conversations[1].String(), result.Evidence[0]["conversation_id"])
	require.False(t, result.Comparison.Comparable)
	require.Contains(t, result.Comparison.Reasons, "insufficient_observations")
	require.Contains(t, result.Comparison.Reasons, "recent_window_unsettled")
	require.Nil(t, result.Comparison.DeltaShare)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), conversations[0].String(), "unreadable conversations must not appear in aggregate or evidence results")
	all, err := engine.SignalFindings(t.Context(), Tenant{OrganizationID: org, ProjectID: project.String()}, Window{FromUnixNano: now.Add(-time.Hour).UnixNano(), ToUnixNano: now.UnixNano()}, request, permittedConversation("*"))
	require.NoError(t, err)
	require.Len(t, all.Evidence, 2, "paired examples require hits for both predicates in the same session")
	_, err = engine.SignalFindings(t.Context(), Tenant{OrganizationID: org, ProjectID: project.String()}, Window{FromUnixNano: now.Add(-time.Hour).UnixNano(), ToUnixNano: now.UnixNano()}, request, nil)
	require.ErrorContains(t, err, "access checks are unavailable")
}

func TestSignalComparisonRequiresStableDefinitionsAndSufficientBreadth(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	ctx := signalquery.Context{From: now.Add(-48 * time.Hour), To: now.Add(-24 * time.Hour), Now: now}
	baseline := Row{"answered": int64(100), "evaluated_sessions": int64(10), "definition_count": int64(1), "definition_hashes": []string{"stable"}, "classifier_models": []string{"model"}, "compiler_versions": []string{"compiler"}, "input_strategies": []string{"single_message"}, "match_share": .2}
	current := Row{"answered": int64(200), "evaluated_sessions": int64(15), "definition_count": int64(1), "definition_hashes": []string{"stable"}, "classifier_models": []string{"model"}, "compiler_versions": []string{"compiler"}, "input_strategies": []string{"single_message"}, "match_share": .3}
	for _, row := range []Row{baseline, current} {
		row["unknown_measured_definitions"] = int64(0)
		row["unknown_measured_models"] = int64(0)
		row["classifier_providers"] = []string{"provider"}
		row["evaluated_models"] = []string{"model"}
	}
	result := compareSignals(ctx, baseline, current)
	require.True(t, result.Comparable)
	require.InDelta(t, .1, *result.DeltaShare, .000001)
	current["definition_hashes"] = []string{"changed"}
	result = compareSignals(ctx, baseline, current)
	require.False(t, result.Comparable)
	require.Contains(t, result.Reasons, "definition_changed_mixed_or_unknown")
	current["definition_hashes"] = []string{"stable"}
	current["unknown_measured_definitions"] = int64(1)
	require.False(t, compareSignals(ctx, baseline, current).Comparable, "one known definition does not conceal missing definitions")
}

func TestSignalFindingsAPIRequiresEntitlement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	_, err := ti.service.SignalFindings(ctx, &gen.SignalFindingsPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
}
