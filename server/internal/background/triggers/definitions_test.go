package triggers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"

	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

func TestListIncludesSlackAndCron(t *testing.T) {
	t.Parallel()

	definitions := List()
	require.Len(t, definitions, 2)
	require.Equal(t, "cron", definitions[0].Slug)
	require.Equal(t, "slack", definitions[1].Slug)
	require.NotEmpty(t, definitions[0].ConfigSchema)
	require.NotEmpty(t, definitions[1].ConfigSchema)
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
