package chrepo

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
)

// testOutcome mirrors the identity and event of a reading for the same sensor.
func testOutcome(t *testing.T, reading *sigintv1.Reading, at time.Time, status sigintv1.Outcome_Status, reason string) *sigintv1.Outcome {
	t.Helper()
	event := sigintv1.Outcome_Event_builder{Kind: new(reading.GetEvent().GetKind()), Id: new(reading.GetEvent().GetId()), OccurredAt: new(reading.GetEvent().GetOccurredAt())}.Build()
	o := sigintv1.Outcome_builder{
		Id: new(reading.GetId()), EvaluationAttemptId: new(uuid.NewString()),
		TriggerKind: new(reading.GetTriggerKind()), EvaluationRequestedAt: new(reading.GetEvaluationRequestedAt()),
		OrganizationId: new(reading.GetOrganizationId()), ProjectId: new(reading.GetProjectId()),
		Event: event, SensorId: new(reading.GetSensorId()), SensorSlug: new(reading.GetSensorSlug()),
		EvaluatedAt: new(at.Format(time.RFC3339Nano)), DefinitionHash: new(reading.GetDefinitionHash()),
		Status: new(status), Attributes: map[string]string{"department": "engineering"},
	}.Build()
	if reading.HasRunId() {
		o.SetRunId(reading.GetRunId())
	}
	if reason != "" {
		o.SetFailureReason(reason)
	}
	return o
}

// requireMirror proves wire conversion between the outcome's self-contained
// nested messages and the reading's loses nothing.
func requireMirror(t *testing.T, outcome, reading protoreflect.MessageDescriptor) {
	t.Helper()
	require.Equal(t, reading.Fields().Len(), outcome.Fields().Len(), "%s fields", outcome.FullName())
	for i := range outcome.Fields().Len() {
		of := outcome.Fields().Get(i)
		rf := reading.Fields().ByNumber(of.Number())
		require.NotNil(t, rf, "%s has no field %d", reading.FullName(), of.Number())
		require.Equal(t, rf.Name(), of.Name())
		require.Equal(t, rf.Kind(), of.Kind(), "%s kind", of.FullName())
		require.Equal(t, rf.Cardinality(), of.Cardinality(), "%s cardinality", of.FullName())
		require.Equal(t, rf.ContainingOneof() != nil, of.ContainingOneof() != nil, "%s oneof", of.FullName())
		if of.Message() != nil {
			requireMirror(t, of.Message(), rf.Message())
		}
		if of.Enum() != nil {
			require.Equal(t, rf.Enum().Values().Len(), of.Enum().Values().Len())
			for j := range of.Enum().Values().Len() {
				require.Equal(t, rf.Enum().Values().Get(j).Number(), of.Enum().Values().Get(j).Number())
				require.Equal(t, rf.Enum().Values().Get(j).Name(), of.Enum().Values().Get(j).Name())
			}
		}
	}
}

func TestOutcomeContextMirrorsReading(t *testing.T) {
	t.Parallel()
	for _, pair := range []struct{ outcome, reading protoreflect.ProtoMessage }{
		{&sigintv1.Outcome_Event{}, &sigintv1.Reading_Event{}},
		{&sigintv1.Outcome_Actor{}, &sigintv1.Reading_Actor{}},
		{&sigintv1.Outcome_Account{}, &sigintv1.Reading_Account{}},
		{&sigintv1.Outcome_Input{}, &sigintv1.Reading_Input{}},
	} {
		requireMirror(t, pair.outcome.ProtoReflect().Descriptor(), pair.reading.ProtoReflect().Descriptor())
	}
}

func TestParseOutcomePreservesContextAndLeavesResultEmpty(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC()
	o := testOutcome(t, testReading(t, at), at.Add(time.Second), sigintv1.Outcome_STATUS_FAILED, "classifier_rejected")
	o.GetEvent().SetConversationMessage(sigintv1.Outcome_ConversationMessage_builder{ConversationId: new(uuid.NewString()), Role: new(sigintv1.Outcome_ConversationMessage_ROLE_ASSISTANT)}.Build())
	o.SetActor(sigintv1.Outcome_Actor_builder{UserId: new("actor-user")}.Build())
	o.SetAccount(sigintv1.Outcome_Account_builder{UserAccountId: new(uuid.NewString()), AccountType: new("team")}.Build())
	o.SetInput(sigintv1.Outcome_Input_builder{Strategy: new("sliding_window"), RequestedMessageCount: new(int32(5)), MessageCount: new(int32(3)), Truncated: new(true)}.Build())
	o.SetReplayed(false)
	row, err := ParseOutcome(o, at)
	require.NoError(t, err)
	require.Equal(t, StatusFailed, row.Status)
	require.Equal(t, "classifier_rejected", row.FailureReason)
	require.Equal(t, "conversation_message", row.EventContextType)
	require.Equal(t, "assistant", row.ConversationRole)
	require.Equal(t, o.GetEvent().GetConversationMessage().GetConversationId(), row.ConversationID.String())
	require.Equal(t, "actor-user", row.ActorUserID)
	require.Equal(t, o.GetAccount().GetUserAccountId(), row.UserAccountID.String())
	require.Equal(t, "team", row.AccountType)
	require.Equal(t, uint8(3), row.InputMessageCount)
	require.True(t, row.InputTruncated)
	require.NotNil(t, row.Replayed)
	require.False(t, *row.Replayed)
	require.Equal(t, "engineering", row.Attributes["department"])
	require.Empty(t, row.ResultType)
	require.Empty(t, row.Probabilities)
	require.Nil(t, row.SelectedSignalID)
	require.Nil(t, row.ExpectedIndex)
	require.Nil(t, row.Confidence)
}

func TestParseOutcomeAcceptsDraftWithoutDefinition(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC()
	o := testOutcome(t, testReading(t, at), at, sigintv1.Outcome_STATUS_DRAFT, "")
	o.SetDefinitionHash("")
	o.SetSensorSlug("")
	row, err := ParseOutcome(o, at)
	require.NoError(t, err)
	require.Equal(t, StatusDraft, row.Status)
	require.Empty(t, row.DefinitionHash)
}

func TestParseOutcomeRejectsPoison(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC()
	for _, tc := range []struct {
		name   string
		mutate func(*sigintv1.Outcome)
	}{
		{"unspecified status", func(o *sigintv1.Outcome) { o.SetStatus(sigintv1.Outcome_STATUS_UNSPECIFIED) }},
		{"unknown status", func(o *sigintv1.Outcome) { o.SetStatus(sigintv1.Outcome_Status(99)) }},
		{"failed without reason", func(o *sigintv1.Outcome) { o.ClearFailureReason() }},
		{"free-form reason", func(o *sigintv1.Outcome) { o.SetFailureReason("Customer said: hello") }},
		{"oversized reason", func(o *sigintv1.Outcome) { o.SetFailureReason(strings.Repeat("a", maxFailureReasonBytes+1)) }},
		{"reason without failure", func(o *sigintv1.Outcome) { o.SetStatus(sigintv1.Outcome_STATUS_NOT_APPLICABLE) }},
		{"missing event", func(o *sigintv1.Outcome) { o.ClearEvent() }},
		{"invalid identity", func(o *sigintv1.Outcome) { o.SetId("not-a-uuid") }},
		{"missing organization", func(o *sigintv1.Outcome) { o.SetOrganizationId("") }},
		{"invalid evaluation time", func(o *sigintv1.Outcome) { o.SetEvaluatedAt("yesterday") }},
		{"missing trigger", func(o *sigintv1.Outcome) { o.ClearTriggerKind() }},
		{"unknown trigger", func(o *sigintv1.Outcome) { o.SetTriggerKind("scheduled") }},
		{"streaming run", func(o *sigintv1.Outcome) { o.SetRunId(uuid.NewString()) }},
		{"missing manual run", func(o *sigintv1.Outcome) { o.SetTriggerKind("manual") }},
		{"missing request time", func(o *sigintv1.Outcome) { o.ClearEvaluationRequestedAt() }},
		{"invalid request time", func(o *sigintv1.Outcome) { o.SetEvaluationRequestedAt("yesterday") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := testOutcome(t, testReading(t, at), at, sigintv1.Outcome_STATUS_FAILED, "invalid_content")
			tc.mutate(o)
			_, err := ParseOutcome(o, at)
			require.ErrorIs(t, err, ErrInvalidReading)
		})
	}
	_, err := ParseOutcome(nil, at)
	require.ErrorIs(t, err, ErrInvalidReading)
}

func TestParseOutcomeDropsLabelsOutsideTheStorageBudget(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC()
	o := testOutcome(t, testReading(t, at), at, sigintv1.Outcome_STATUS_FAILED, "classifier_rejected")
	labels := make(map[string]string, MaxAttributes+1)
	for i := range MaxAttributes + 1 {
		labels["label-"+strconv.Itoa(i)] = "value"
	}
	o.SetAttributes(labels)
	row, err := ParseOutcome(o, at)
	require.NoError(t, err, "coverage keeps the decision even when its labels do not fit")
	require.Empty(t, row.Attributes)
	require.Equal(t, StatusFailed, row.Status)
}
