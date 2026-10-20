package chrepo

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
)

func testReading(t *testing.T, at time.Time) *sigintv1.Reading {
	t.Helper()
	event := sigintv1.Reading_Event_builder{Kind: new("custom.event"), Id: new("opaque/event"), OccurredAt: new(at.Format(time.RFC3339Nano))}.Build()
	probabilities := []*sigintv1.Reading_Probability{
		sigintv1.Reading_Probability_builder{SignalId: new(uuid.NewString()), SignalSlug: new("first"), Probability: new(0.2)}.Build(),
		sigintv1.Reading_Probability_builder{SignalId: new(uuid.NewString()), SignalSlug: new("second"), Probability: new(0.8)}.Build(),
	}
	return sigintv1.Reading_builder{
		Id: new(uuid.NewString()), EvaluationAttemptId: new(uuid.NewString()),
		TriggerKind: new("stream"), EvaluationRequestedAt: new(at.Format(time.RFC3339Nano)),
		OrganizationId: new("org-test-" + uuid.NewString()), ProjectId: new(uuid.NewString()),
		Event: event, SensorId: new(uuid.NewString()), SensorSlug: new("example-sensor"),
		EvaluatedAt: new(at.Add(time.Second).Format(time.RFC3339Nano)), DefinitionHash: new("example-definition-v1"),
		ConfiguredModel: new("example-model"), CompilerVersion: new("example-compiler-v1"), Provider: new("example-provider"),
		Attributes: map[string]string{"department": "engineering"},
		MultiLabel: sigintv1.Reading_MultiLabel_builder{Signals: probabilities}.Build(),
	}.Build()
}

func TestParseReadingPreservesAttributionAndOrderedProbabilities(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC()
	m := testReading(t, at)
	m.GetMultiLabel().GetSignals()[0].SetProbability(0)
	m.SetBillingUserId("explicit-allocation")
	m.SetReplayed(false)
	row, err := ParseReading(m, at.Add(2*time.Second))
	require.NoError(t, err)
	require.Equal(t, at, row.EventOccurredAt)
	require.Equal(t, "stream", row.TriggerKind)
	require.Equal(t, uuid.Nil, row.RunID)
	require.Equal(t, at, row.EvaluationRequestedAt)
	require.Nil(t, row.Confidence)
	require.Nil(t, row.ExpectedIndex)
	require.Nil(t, row.SelectedSignalID)
	require.Empty(t, row.ActorUserID)
	require.Equal(t, "explicit-allocation", row.BillingUserID)
	require.NotNil(t, row.Replayed)
	require.False(t, *row.Replayed)
	require.Equal(t, "first", row.Probabilities[0].SignalSlug)
	require.Zero(t, row.Probabilities[0].Probability)
	m.GetAttributes()["department"] = "changed-after-capture"
	require.Equal(t, "engineering", row.Attributes["department"])
	require.Equal(t, StatusAnswered, row.Status)
	require.Empty(t, row.FailureReason)
	require.Empty(t, row.InputStrategy, "absent input context remains unrecorded")
	require.Zero(t, row.InputMessageCount)
}

func TestParseReadingRecordsSuppliedInputContext(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC()
	m := testReading(t, at)
	m.SetInput(sigintv1.Reading_Input_builder{Strategy: new("sliding_window"), RequestedMessageCount: new(int32(10)), MessageCount: new(int32(2)), Truncated: new(true)}.Build())
	row, err := ParseReading(m, at)
	require.NoError(t, err)
	require.Equal(t, "sliding_window", row.InputStrategy)
	require.Equal(t, uint8(10), row.InputRequestedMessageCount)
	require.Equal(t, uint8(2), row.InputMessageCount)
	require.True(t, row.InputTruncated)
}

func TestParseReadingRejectsMalformedMeasurements(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*sigintv1.Reading)
	}{
		{name: "it rejects nonfinite probability", mutate: func(m *sigintv1.Reading) { m.GetMultiLabel().GetSignals()[0].SetProbability(math.NaN()) }},
		{name: "it rejects omitted probability", mutate: func(m *sigintv1.Reading) { m.GetMultiLabel().GetSignals()[0].ClearProbability() }},
		{name: "it rejects duplicate members", mutate: func(m *sigintv1.Reading) {
			m.GetMultiLabel().GetSignals()[1].SetSignalId(m.GetMultiLabel().GetSignals()[0].GetSignalId())
		}},
		{name: "it rejects missing result", mutate: func(m *sigintv1.Reading) { m.ClearMultiLabel() }},
		{name: "it rejects unsupported timestamp", mutate: func(m *sigintv1.Reading) { m.GetEvent().SetOccurredAt("9999-01-01T00:00:00Z") }},
		{name: "it rejects missing trigger", mutate: func(m *sigintv1.Reading) { m.ClearTriggerKind() }},
		{name: "it rejects unknown trigger", mutate: func(m *sigintv1.Reading) { m.SetTriggerKind("scheduled") }},
		{name: "it rejects streaming run", mutate: func(m *sigintv1.Reading) { m.SetRunId(uuid.NewString()) }},
		{name: "it rejects missing manual run", mutate: func(m *sigintv1.Reading) { m.SetTriggerKind("manual") }},
		{name: "it rejects zero manual run", mutate: func(m *sigintv1.Reading) { m.SetTriggerKind("manual"); m.SetRunId(uuid.Nil.String()) }},
		{name: "it rejects missing request time", mutate: func(m *sigintv1.Reading) { m.ClearEvaluationRequestedAt() }},
		{name: "it rejects invalid request time", mutate: func(m *sigintv1.Reading) { m.SetEvaluationRequestedAt("yesterday") }},
		{name: "it rejects more supplied than requested messages", mutate: func(m *sigintv1.Reading) {
			m.SetInput(sigintv1.Reading_Input_builder{Strategy: new("sliding_window"), RequestedMessageCount: new(int32(2)), MessageCount: new(int32(3))}.Build())
		}},
		{name: "it rejects input without a target message", mutate: func(m *sigintv1.Reading) {
			m.SetInput(sigintv1.Reading_Input_builder{Strategy: new("single_message"), RequestedMessageCount: new(int32(1)), MessageCount: new(int32(0))}.Build())
		}},
		{name: "it rejects oversized labels", mutate: func(m *sigintv1.Reading) {
			m.SetAttributes(map[string]string{"large": string(make([]byte, MaxAttributeBytes))})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			at := time.Now().UTC()
			m := testReading(t, at)
			tc.mutate(m)
			_, err := ParseReading(m, at)
			require.ErrorIs(t, err, ErrInvalidReading)
		})
	}
}

func TestParseChoiceDistinguishesMissingAndZeroConfidence(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC()
	m := testReading(t, at)
	p := m.GetMultiLabel().GetSignals()
	choice := sigintv1.Reading_Choice_builder{SelectedSignalId: new(p[1].GetSignalId()), SelectedSignalSlug: new(p[1].GetSignalSlug()), Distribution: p}.Build()
	m.SetChoice(choice)
	row, err := ParseReading(m, at)
	require.NoError(t, err)
	require.Nil(t, row.Confidence)
	choice.SetConfidence(0)
	row, err = ParseReading(m, at)
	require.NoError(t, err)
	require.NotNil(t, row.Confidence)
	require.Zero(t, *row.Confidence)
}
