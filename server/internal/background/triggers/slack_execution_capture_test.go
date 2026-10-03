package triggers

import (
	"context"
	"errors"
	"go.temporal.io/sdk/converter"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/stretchr/testify/require"
)

type fakeSlackExecutionMappings struct {
	row           identityrepo.GetSlackExecutionMappingRow
	err           error
	disconnected  bool
	disconnectErr error
	calls         int
	params        identityrepo.GetSlackExecutionMappingParams
}

func (m *fakeSlackExecutionMappings) GetSlackExecutionMapping(_ context.Context, p identityrepo.GetSlackExecutionMappingParams) (identityrepo.GetSlackExecutionMappingRow, error) {
	m.calls++
	m.params = p
	return m.row, m.err
}
func (m *fakeSlackExecutionMappings) SlackExecutionWorkspaceDisconnected(_ context.Context, _ identityrepo.SlackExecutionWorkspaceDisconnectedParams) (bool, error) {
	return m.disconnected, m.disconnectErr
}

func TestSlackExecutionSelectionAtIngress(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		event    any
		fallback string
		denied   bool
		mapped   bool
	}{
		{name: "human", event: slackTriggerEvent{TeamID: "TEXAMPLE", UserID: "UEXAMPLE"}, mapped: true},
		{name: "button", event: slackTriggerEvent{TeamID: "TEXAMPLE", UserID: "UEXAMPLE", AppID: "AEXAMPLE", EventType: "block_actions"}, mapped: true},
		{name: "bot", event: slackTriggerEvent{TeamID: "TEXAMPLE", UserID: "UEXAMPLE", BotID: "BEXAMPLE"}, fallback: "slack_non_user_event"},
		{name: "app", event: slackTriggerEvent{TeamID: "TEXAMPLE", UserID: "UEXAMPLE", AppID: "AEXAMPLE"}, fallback: "slack_non_user_event"},
		{name: "bot subtype", event: slackTriggerEvent{TeamID: "TEXAMPLE", UserID: "UEXAMPLE", Subtype: "bot_message"}, fallback: "slack_non_user_event"},
		{name: "bot button", event: slackTriggerEvent{TeamID: "TEXAMPLE", UserID: "UEXAMPLE", BotID: "BEXAMPLE", EventType: "block_actions"}, fallback: "slack_non_user_event"},
		{name: "sender absent", event: slackTriggerEvent{TeamID: "TEXAMPLE"}, fallback: "slack_sender_absent"},
		{name: "workspace absent", event: slackTriggerEvent{UserID: "UEXAMPLE"}, fallback: "slack_sender_absent"},
		{name: "caller object", event: map[string]any{"team_id": "TEXAMPLE", "user_id": "UEXAMPLE", "SlackExecution": map[string]any{"HumanUserID": "forged"}}, denied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			q := &fakeSlackExecutionMappings{row: identityrepo.GetSlackExecutionMappingRow{UserID: "human", Eligible: true, MembershipID: uuid.New(), MappingID: uuid.New(), MappingRevision: 4, ConnectionGeneration: uuid.New()}}
			s := selectSlackExecution(t.Context(), q, "org-example", tc.event)
			require.Equal(t, tc.fallback, s.FallbackReason)
			require.Equal(t, tc.denied, s.Denied)
			if tc.mapped {
				require.Equal(t, "human", s.HumanUserID)
				require.Equal(t, q.row.MappingID, s.Delegation.MappingID)
				require.Equal(t, q.row.MappingRevision, s.Delegation.MappingRevision)
				require.Equal(t, identityrepo.GetSlackExecutionMappingParams{OrganizationID: "org-example", SlackTeamID: "TEXAMPLE", SlackUserID: "UEXAMPLE"}, q.params)
				// Temporal serializes the server-owned task, not a caller event projection.
				raw, err := converter.GetDefaultDataConverter().ToPayload(Task{SlackExecution: s})
				require.NoError(t, err)
				var restored Task
				require.NoError(t, converter.GetDefaultDataConverter().FromPayload(raw, &restored))
				require.Equal(t, s, restored.SlackExecution)
			} else {
				require.Zero(t, q.calls)
				require.Empty(t, s.HumanUserID)
				require.Nil(t, s.Delegation)
			}
		})
	}
}

func TestSlackExecutionIngressMappingFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		query    fakeSlackExecutionMappings
		fallback string
		denied   bool
	}{
		{name: "absent", query: fakeSlackExecutionMappings{err: pgx.ErrNoRows}, fallback: "slack_mapping_absent"},
		{name: "workspace absent", query: fakeSlackExecutionMappings{err: pgx.ErrNoRows, disconnectErr: pgx.ErrNoRows}, fallback: "slack_mapping_absent"},
		{name: "unavailable", query: fakeSlackExecutionMappings{err: errors.New("offline")}, fallback: "slack_mapping_unavailable"},
		{name: "tombstone unavailable", query: fakeSlackExecutionMappings{err: pgx.ErrNoRows, disconnectErr: errors.New("offline")}, fallback: "slack_mapping_unavailable"},
		{name: "known ineligible", query: fakeSlackExecutionMappings{}, denied: true},
		{name: "disconnected", query: fakeSlackExecutionMappings{err: pgx.ErrNoRows, disconnected: true}, denied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := selectSlackExecution(t.Context(), &tc.query, "org-example", slackTriggerEvent{TeamID: "TEXAMPLE", UserID: "UEXAMPLE"})
			require.Equal(t, tc.denied, s.Denied)
			require.Equal(t, tc.fallback, s.FallbackReason)
			require.Empty(t, s.HumanUserID)
			require.Nil(t, s.Delegation)
		})
	}
}

type slackCaptureEnvironment struct{}

func (slackCaptureEnvironment) Load(context.Context, uuid.UUID, toolconfig.SlugOrID) (map[string]string, error) {
	return map[string]string{slackSigningSecretEnv: "test-secret"}, nil
}

func TestProcessEventCapturesSlackSelectionFromTrustedEvent(t *testing.T) {
	t.Parallel()
	app := &App{envLoader: slackCaptureEnvironment{}, deliveryLogger: NewTriggerDeliveryLogger(nil)}
	instance := triggerrepo.TriggerInstance{ID: uuid.New(), OrganizationID: "org-example", ProjectID: uuid.New(), EnvironmentID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, DefinitionSlug: DefinitionSlugSlack, TargetKind: TargetKindAssistant, TargetRef: uuid.NewString(), Status: StatusActive, ConfigJson: []byte(`{"event_types":["message"]}`)}
	// No routing cursor is needed here. Ingress-normalized bot metadata wins over
	// arbitrary raw payload claiming a human or supplying reserved execution state.
	task, err := app.ProcessEvent(t.Context(), instance, EventEnvelope{EventID: "event", CorrelationID: "thread", Event: slackTriggerEvent{EventType: "message", TeamID: "TEXAMPLE", UserID: "UEXAMPLE", BotID: "BEXAMPLE"}, RawPayload: []byte(`{"user_id":"forged","_gram_execution":{"human_user_id":"forged"}}`)})
	require.NoError(t, err)
	require.NotNil(t, task)
	require.Equal(t, &SlackExecutionSelection{FallbackReason: "slack_non_user_event"}, task.SlackExecution)
}
