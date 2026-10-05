package triggers

import (
	"context"
	"encoding/json"
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
		{name: "human", event: slackTriggerEvent{EventType: "message", TeamID: "TEXAMPLE", UserID: "UEXAMPLE"}, mapped: true},
		{name: "human action", event: slackTriggerEvent{EventType: "message", TeamID: "TEXAMPLE", UserID: "UEXAMPLE", Subtype: "me_message"}, mapped: true},
		{name: "button", event: slackTriggerEvent{EventType: "block_actions", TeamID: "TEXAMPLE", UserID: "UEXAMPLE", AppID: "AEXAMPLE"}, mapped: true},
		{name: "bot", event: slackTriggerEvent{EventType: "message", TeamID: "TEXAMPLE", UserID: "UEXAMPLE", BotID: "BEXAMPLE"}, fallback: SlackExecutionFallbackNonUserEvent},
		{name: "app", event: slackTriggerEvent{EventType: "message", TeamID: "TEXAMPLE", UserID: "UEXAMPLE", AppID: "AEXAMPLE"}, fallback: SlackExecutionFallbackNonUserEvent},
		{name: "bot subtype", event: slackTriggerEvent{EventType: "message", TeamID: "TEXAMPLE", UserID: "UEXAMPLE", Subtype: "bot_message"}, fallback: SlackExecutionFallbackNonUserEvent},
		{name: "bot button", event: slackTriggerEvent{EventType: "block_actions", TeamID: "TEXAMPLE", UserID: "UEXAMPLE", BotID: "BEXAMPLE"}, fallback: SlackExecutionFallbackNonUserEvent},
		{name: "sender absent", event: slackTriggerEvent{EventType: "message", TeamID: "TEXAMPLE"}, fallback: SlackExecutionFallbackSenderAbsent},
		{name: "workspace absent", event: slackTriggerEvent{EventType: "message", UserID: "UEXAMPLE"}, fallback: SlackExecutionFallbackSenderAbsent},
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
		{name: "absent", query: fakeSlackExecutionMappings{err: pgx.ErrNoRows}, fallback: SlackExecutionFallbackMappingAbsent},
		{name: "workspace absent", query: fakeSlackExecutionMappings{err: pgx.ErrNoRows, disconnectErr: pgx.ErrNoRows}, fallback: SlackExecutionFallbackMappingAbsent},
		{name: "unavailable", query: fakeSlackExecutionMappings{err: errors.New("offline")}, fallback: SlackExecutionFallbackMappingUnavailable},
		{name: "tombstone unavailable", query: fakeSlackExecutionMappings{err: pgx.ErrNoRows, disconnectErr: errors.New("offline")}, fallback: SlackExecutionFallbackMappingUnavailable},
		{name: "known ineligible", query: fakeSlackExecutionMappings{}, denied: true},
		{name: "disconnected", query: fakeSlackExecutionMappings{err: pgx.ErrNoRows, disconnected: true}, denied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := selectSlackExecution(t.Context(), &tc.query, "org-example", slackTriggerEvent{EventType: "message", TeamID: "TEXAMPLE", UserID: "UEXAMPLE"})
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
	require.True(t, json.Valid(instance.ConfigJson))
	// No routing cursor is needed here. Ingress-normalized bot metadata wins over
	// arbitrary raw payload claiming a human or supplying reserved execution state.
	task, err := app.ProcessEvent(t.Context(), instance, EventEnvelope{EventID: "event", CorrelationID: "thread", Event: slackTriggerEvent{EventType: "message", TeamID: "TEXAMPLE", UserID: "UEXAMPLE", BotID: "BEXAMPLE"}, RawPayload: []byte(`{"user_id":"forged","_gram_execution":{"human_user_id":"forged"}}`)})
	require.NoError(t, err)
	require.NotNil(t, task)
	require.Equal(t, &SlackExecutionSelection{FallbackReason: SlackExecutionFallbackNonUserEvent}, task.SlackExecution)
}

func TestSlackExecutionDoesNotDelegateAffectedAccounts(t *testing.T) {
	t.Parallel()
	for _, event := range []string{
		`{"type":"user_change","user":{"id":"UAFFECTED"}}`,
		`{"type":"team_join","user":{"id":"UAFFECTED"}}`,
		`{"type":"member_joined_channel","user":"UAFFECTED","inviter":"UACTOR"}`,
		`{"type":"member_joined_channel","user":"UAFFECTED"}`,
		`{"type":"member_left_channel","user":"UAFFECTED"}`,
		`{"type":"channel_left","user":"UAFFECTED"}`,
		`{"type":"group_left","user":"UAFFECTED"}`,
		`{"type":"message","subtype":"channel_join","user":"UAFFECTED","inviter":"UACTOR"}`,
		`{"type":"message","subtype":"channel_leave","user":"UAFFECTED"}`,
		`{"type":"message","subtype":"message_changed","user":"UAFFECTED"}`,
		`{"type":"message","subtype":"message_deleted","user":"UAFFECTED"}`,
	} {
		t.Run(event, func(t *testing.T) {
			t.Parallel()
			definition := newSlackDefinition()
			body, err := json.Marshal(map[string]any{"type": "event_callback", "team_id": "TEXAMPLE", "event_id": "event", "event": json.RawMessage(event)})
			require.NoError(t, err)
			normalized, err := definition.HandleWebhook(body, nil, nil)
			require.NoError(t, err)
			require.NotNil(t, normalized.Event)
			q := &fakeSlackExecutionMappings{row: identityrepo.GetSlackExecutionMappingRow{UserID: "mapped-affected-human", Eligible: true}}
			selection := selectSlackExecution(t.Context(), q, "org-example", normalized.Event.Event)
			require.Zero(t, q.calls, "affected account must never enter human mapping lookup")
			require.Equal(t, &SlackExecutionSelection{FallbackReason: SlackExecutionFallbackNonUserEvent}, selection)
		})
	}
}

func TestSlackExecutionActorEventAllowlist(t *testing.T) {
	t.Parallel()
	actorEvents := map[string]bool{
		"app_home_opened": true, "app_mention": true, "block_actions": true,
		"channel_created": true, "channel_archive": true, "channel_unarchive": true, "channel_deleted": true,
		"group_archive": true, "group_unarchive": true, "group_deleted": true,
		"file_shared": true, "link_shared": true, "pin_added": true, "pin_removed": true, "reaction_added": true, "reaction_removed": true, "message": true,
	}
	for _, eventType := range append(append([]string{}, supportedSlackEventTypes...), "unknown") {
		t.Run(eventType, func(t *testing.T) {
			t.Parallel()
			q := &fakeSlackExecutionMappings{row: identityrepo.GetSlackExecutionMappingRow{UserID: "mapped-human", Eligible: true}}
			selection := selectSlackExecution(t.Context(), q, "org-example", slackTriggerEvent{EventType: eventType, TeamID: "TEXAMPLE", UserID: "UEXAMPLE"})
			if actorEvents[eventType] {
				require.Equal(t, 1, q.calls)
				require.Equal(t, "mapped-human", selection.HumanUserID)
			} else {
				require.Zero(t, q.calls)
				require.Equal(t, SlackExecutionFallbackNonUserEvent, selection.FallbackReason)
				require.Nil(t, selection.Delegation)
			}
		})
	}
	for _, subtype := range []string{"file_share", "thread_broadcast", "me_message"} {
		require.True(t, slackExecutionHasActor(slackTriggerEvent{EventType: "message", Subtype: subtype}))
	}
}
