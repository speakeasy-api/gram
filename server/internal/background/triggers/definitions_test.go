package triggers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"

	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

func TestListIncludesAllDefinitions(t *testing.T) {
	t.Parallel()

	definitions := List()
	require.Len(t, definitions, 3)
	require.Equal(t, "cron", definitions[0].Slug)
	require.Equal(t, "slack", definitions[1].Slug)
	require.Equal(t, "wake", definitions[2].Slug)
	for _, d := range definitions {
		require.NotEmpty(t, d.ConfigSchema, d.Slug)
	}
}

func TestSlackConfigSchemaConstrainsEventTypeItems(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("slack")
	require.True(t, ok)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(definition.ConfigSchema, &schema))

	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)

	eventTypes, ok := properties["event_types"].(map[string]any)
	require.True(t, ok)

	items, ok := eventTypes["items"].(map[string]any)
	require.True(t, ok)

	enumValues, ok := items["enum"].([]any)
	require.True(t, ok)
	require.Len(t, enumValues, len(supportedSlackEventTypes))
	for _, evt := range supportedSlackEventTypes {
		require.Contains(t, enumValues, evt)
	}
}

func TestSlackDecodeConfigRejectsInvalidFilter(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("slack")
	require.True(t, ok)

	_, err := definition.DecodeConfig(map[string]any{
		"filter": "event.text",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "filter must evaluate to bool")
}

func TestSlackDecodeConfigAcceptsEventTypesList(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("slack")
	require.True(t, ok)

	config, err := definition.DecodeConfig(map[string]any{
		"event_types": []any{"message"},
	})
	require.NoError(t, err)

	slackConfig, ok := config.(slackTriggerConfig)
	require.True(t, ok)
	require.Equal(t, []string{"message"}, slackConfig.EventTypes)
}

func TestSlackHandleWebhookChallenge(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("slack")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	body := []byte(`{"type":"url_verification","challenge":"abc123"}`)
	headers := signedSlackHeaders(t, body, "secret")

	err = definition.AuthenticateWebhook(body, headers, map[string]string{
		"SLACK_SIGNING_SECRET": "secret",
	}, config)
	require.NoError(t, err)

	result, err := definition.HandleWebhook(body, headers, config)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Response)
	require.Nil(t, result.Event)
	require.Equal(t, http.StatusOK, result.Response.Status)
	require.Equal(t, []byte("abc123"), result.Response.Body)
}

func TestCronBuildScheduledEventIsDeterministic(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("cron")
	require.True(t, ok)

	config, err := definition.DecodeConfig(map[string]any{
		"schedule": "0 * * * *",
	})
	require.NoError(t, err)

	instance := triggerrepo.TriggerInstance{
		ID:             uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		DefinitionSlug: "cron",
	}
	firedAt := time.Date(2026, 4, 9, 12, 0, 0, 123, time.UTC)

	first, err := definition.BuildScheduledEvent(instance, config, firedAt)
	require.NoError(t, err)
	second, err := definition.BuildScheduledEvent(instance, config, firedAt)
	require.NoError(t, err)

	require.Equal(t, first.EventID, second.EventID)
	require.Equal(t, instance.ID.String(), first.CorrelationID)
	require.Equal(t, instance.ID.String(), first.TriggerInstanceID)
}

func TestBuildScheduleOptionsUsesSharedScheduleWorkflowInput(t *testing.T) {
	t.Parallel()

	instance := triggerrepo.TriggerInstance{
		ID:     uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Status: "active",
	}

	options := BuildScheduleOptions(instance, "0 * * * *", "trigger-queue", "TriggerCronWorkflow")

	action, ok := options.Action.(*client.ScheduleWorkflowAction)
	require.True(t, ok)
	require.Len(t, action.Args, 1)

	input, ok := action.Args[0].(ScheduleWorkflowInput)
	require.True(t, ok)
	require.Equal(t, instance.ID.String(), input.TriggerInstanceID)
	require.Empty(t, input.FiredAt)
}

func TestSlackFilterMatchesEventTypeAndCEL(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("slack")
	require.True(t, ok)

	config, err := definition.DecodeConfig(map[string]any{
		"event_types": []any{"message"},
		"filter":      `event.event_type == "message"`,
	})
	require.NoError(t, err)

	match, err := config.Filter(slackTriggerEvent{
		EnvelopeType: "event_callback",
		EventType:    "message",
	})
	require.NoError(t, err)
	require.True(t, match)
}

func TestSlackFilterRejectsDisabledEventType(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("slack")
	require.True(t, ok)

	config, err := definition.DecodeConfig(map[string]any{
		"event_types": []any{"app_mention"},
	})
	require.NoError(t, err)

	match, err := config.Filter(slackTriggerEvent{
		EnvelopeType: "event_callback",
		EventType:    "message",
	})
	require.NoError(t, err)
	require.False(t, match)
}

func TestDecodeSlackEventStringUser(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"type":"app_mention","user":"U123","channel":"C1","ts":"1.0","text":"hi"}`)
	got, err := decodeSlackEvent(raw)
	require.NoError(t, err)
	require.Equal(t, "app_mention", got.Type)
	require.Equal(t, "U123", got.User)
	require.Equal(t, "C1", got.Channel)
	require.Equal(t, "hi", got.Text)
}

func TestDecodeSlackEventUserObject(t *testing.T) {
	t.Parallel()

	for _, eventType := range []string{"team_join", "user_change"} {
		t.Run(eventType, func(t *testing.T) {
			t.Parallel()

			raw := json.RawMessage(fmt.Sprintf(
				`{"type":%q,"user":{"id":"U999","team_id":"T1","name":"alice","real_name":"Alice","is_bot":false}}`,
				eventType,
			))
			got, err := decodeSlackEvent(raw)
			require.NoError(t, err)
			require.Equal(t, eventType, got.Type)
			require.Equal(t, "U999", got.User)
		})
	}
}

func TestDecodeSlackEventChannelObject(t *testing.T) {
	t.Parallel()

	for _, eventType := range []string{"channel_rename", "group_rename"} {
		t.Run(eventType, func(t *testing.T) {
			t.Parallel()

			raw := json.RawMessage(fmt.Sprintf(
				`{"type":%q,"channel":{"id":"C123","name":"new-name","created":1360782804}}`,
				eventType,
			))
			got, err := decodeSlackEvent(raw)
			require.NoError(t, err)
			require.Equal(t, eventType, got.Type)
			require.Equal(t, "C123", got.Channel)
			require.Empty(t, got.User)
		})
	}
}

func TestDecodeSlackEventChannelCreated(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"type":"channel_created","channel":{"id":"C024BE91L","name":"fun","created":1360782804,"creator":"U024BE7LH"}}`)
	got, err := decodeSlackEvent(raw)
	require.NoError(t, err)
	require.Equal(t, "channel_created", got.Type)
	require.Equal(t, "C024BE91L", got.Channel)
	require.Equal(t, "U024BE7LH", got.User)
}

func TestDecodeSlackEventChannelIDChanged(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"type":"channel_id_changed","old_channel_id":"G012Y48650T","new_channel_id":"C012Y48650T","event_ts":"1612206778.000000"}`)
	got, err := decodeSlackEvent(raw)
	require.NoError(t, err)
	require.Equal(t, "channel_id_changed", got.Type)
	require.Equal(t, "C012Y48650T", got.Channel)
}

func TestDecodeSlackEventFileShared(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"type":"file_shared","file_id":"F2147483862","user_id":"U0Z7K8SRH","channel_id":"C0Z7K8SRH","file":{"id":"F2147483862"},"event_ts":"1361482916.000004"}`)
	got, err := decodeSlackEvent(raw)
	require.NoError(t, err)
	require.Equal(t, "file_shared", got.Type)
	require.Equal(t, "U0Z7K8SRH", got.User)
	require.Equal(t, "C0Z7K8SRH", got.Channel)
}

func TestDecodeSlackEventMemberJoinedChannel(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"type":"member_joined_channel","user":"W123ABC456","channel":"C0698JE0H","channel_type":"C","team":"T0E2GE343","inviter":"U123456789"}`)
	got, err := decodeSlackEvent(raw)
	require.NoError(t, err)
	require.Equal(t, "member_joined_channel", got.Type)
	require.Equal(t, "W123ABC456", got.User)
	require.Equal(t, "C0698JE0H", got.Channel)
	require.Equal(t, "U123456789", got.Inviter)
}

func TestSlackHandleWebhookBlockActionsRoutesToThread(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("slack")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	payload := `{"type":"block_actions","api_app_id":"A1","team":{"id":"T1"},"user":{"id":"U1"},"channel":{"id":"C1"},"message":{"ts":"1700000001.000200","thread_ts":"1700000000.000100"},"actions":[{"action_id":"approve","block_id":"b1","value":"approved","type":"button"}]}`
	body := []byte("payload=" + url.QueryEscape(payload))
	headers := signedSlackHeaders(t, body, "secret")
	headers.Set("Content-Type", "application/x-www-form-urlencoded")

	require.NoError(t, definition.AuthenticateWebhook(body, headers, map[string]string{
		"SLACK_SIGNING_SECRET": "secret",
	}, config))

	result, err := definition.HandleWebhook(body, headers, config)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Response)
	require.NotNil(t, result.Event)

	require.Equal(t, "C1:1700000000.000100", result.Event.CorrelationID)

	normalized, ok := result.Event.Event.(slackTriggerEvent)
	require.True(t, ok)
	require.Equal(t, "interactive", normalized.EnvelopeType)
	require.Equal(t, "block_actions", normalized.EventType)
	require.Equal(t, "T1", normalized.TeamID)
	require.Equal(t, "C1", normalized.ChannelID)
	require.Equal(t, "1700000000.000100", normalized.ThreadID)
	require.Equal(t, "U1", normalized.UserID)
	require.Equal(t, "approve", normalized.ActionID)
	require.Equal(t, "approved", normalized.ActionValue)
	require.Equal(t, "b1", normalized.BlockID)
	require.Equal(t, "approved", normalized.Text)
}

func TestSlackHandleWebhookBlockActionsTopLevelMessageKeysOnMessageTs(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("slack")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	// Click on a top-level (non-threaded) message: message.thread_ts is
	// empty. Correlation keys on the message's own ts so each top-level
	// message maps to its own assistant thread.
	payload := `{"type":"block_actions","team":{"id":"T1"},"user":{"id":"U1"},"channel":{"id":"C1"},"message":{"ts":"1700000001.000200"},"actions":[{"action_id":"x","block_id":"b","value":"v","type":"button"}]}`
	body := []byte("payload=" + url.QueryEscape(payload))
	headers := signedSlackHeaders(t, body, "secret")
	headers.Set("Content-Type", "application/x-www-form-urlencoded")

	result, err := definition.HandleWebhook(body, headers, config)
	require.NoError(t, err)
	require.NotNil(t, result.Event)

	require.Equal(t, "C1:1700000001.000200", result.Event.CorrelationID)
	normalized, ok := result.Event.Event.(slackTriggerEvent)
	require.True(t, ok)
	require.Empty(t, normalized.ThreadID)
	require.Equal(t, "1700000001.000200", normalized.Timestamp)
}

func TestSlackHandleWebhookEventTopLevelMessageKeysOnEventTs(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("slack")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	// Top-level Slack message has no thread_ts; correlation must fold the
	// event ts in so distinct top-level messages route to distinct
	// assistant threads instead of collapsing onto a channel-only key.
	body := []byte(`{"type":"event_callback","team_id":"T1","event_id":"Ev1","event":{"type":"message","channel":"C1","user":"U1","text":"hello","ts":"1700000050.000900"}}`)
	headers := signedSlackHeaders(t, body, "secret")

	require.NoError(t, definition.AuthenticateWebhook(body, headers, map[string]string{
		"SLACK_SIGNING_SECRET": "secret",
	}, config))

	result, err := definition.HandleWebhook(body, headers, config)
	require.NoError(t, err)
	require.NotNil(t, result.Event)

	require.Equal(t, "C1:1700000050.000900", result.Event.CorrelationID)
	normalized, ok := result.Event.Event.(slackTriggerEvent)
	require.True(t, ok)
	require.Empty(t, normalized.ThreadID)
	require.Equal(t, "1700000050.000900", normalized.Timestamp)
}

func TestSlackHandleWebhookIgnoresUnsupportedInteractionType(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("slack")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	payload := `{"type":"view_submission","team":{"id":"T1"},"user":{"id":"U1"}}`
	body := []byte("payload=" + url.QueryEscape(payload))
	headers := signedSlackHeaders(t, body, "secret")
	headers.Set("Content-Type", "application/x-www-form-urlencoded")

	result, err := definition.HandleWebhook(body, headers, config)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Event)
	require.Nil(t, result.Response)
}

func TestCronFilterAlwaysReturnsTrue(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("cron")
	require.True(t, ok)

	config, err := definition.DecodeConfig(map[string]any{
		"schedule": "0 * * * *",
	})
	require.NoError(t, err)

	match, err := config.Filter(cronTriggerEvent{
		Schedule: "0 * * * *",
		FiredAt:  "2026-04-09T12:00:00Z",
	})
	require.NoError(t, err)
	require.True(t, match)
}

func signedSlackHeaders(t *testing.T, body []byte, signingSecret string) http.Header {
	t.Helper()

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	base := "v0:" + timestamp + ":" + string(body)
	mac := hmac.New(sha256.New, []byte(signingSecret))
	_, err := mac.Write([]byte(base))
	require.NoError(t, err)

	headers := http.Header{}
	headers.Set("X-Slack-Request-Timestamp", timestamp)
	headers.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	return headers
}
