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

	"github.com/stretchr/testify/require"
)

func TestLinearConfigSchemaConstrainsEventTypeItems(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("linear")
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
	require.Len(t, enumValues, len(supportedLinearEventTypes))
	for _, evt := range supportedLinearEventTypes {
		require.Contains(t, enumValues, evt)
	}
}

func TestLinearDecodeConfigRejectsUnsupportedEventType(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("linear")
	require.True(t, ok)

	_, err := definition.DecodeConfig(map[string]any{
		"event_types": []any{"Issue.bogus"},
	})
	require.Error(t, err)
}

func TestLinearSignatureVerification(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("linear")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	body := []byte(`{"type":"Issue","action":"create","data":{"id":"issue-1"}}`)
	mac := hmac.New(sha256.New, []byte("shh"))
	mac.Write(body)
	headers := http.Header{}
	headers.Set("Linear-Signature", hex.EncodeToString(mac.Sum(nil)))

	require.NoError(t, definition.AuthenticateWebhook(body, headers, map[string]string{
		"LINEAR_SIGNING_SECRET": "shh",
	}, config))
}

func TestLinearSignatureRejectsBadSecret(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("linear")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	body := []byte(`{"type":"Issue","action":"create"}`)
	mac := hmac.New(sha256.New, []byte("correct"))
	mac.Write(body)
	headers := http.Header{}
	headers.Set("Linear-Signature", hex.EncodeToString(mac.Sum(nil)))

	err = definition.AuthenticateWebhook(body, headers, map[string]string{
		"LINEAR_SIGNING_SECRET": "wrong",
	}, config)
	require.Error(t, err)
}

func TestLinearIngestIssue(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("linear")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	body := []byte(`{"type":"Issue","action":"create","data":{"id":"2174add1-f7c8-44e3-bbf3-2d60b5ea8bc9"},"url":"https://linear.app/issue/LIN-1/foo"}`)
	headers := http.Header{}
	headers.Set("Linear-Delivery", "delivery-1")

	result, err := definition.HandleWebhook(body, headers, config)
	require.NoError(t, err)
	require.NotNil(t, result.Event)

	require.Equal(t, "delivery-1", result.Event.EventID)
	require.Equal(t, "linear:issue:2174add1-f7c8-44e3-bbf3-2d60b5ea8bc9", result.Event.CorrelationID)

	event, ok := result.Event.Event.(linearTriggerEvent)
	require.True(t, ok)
	require.Equal(t, "Issue.create", event.EventType)
	require.Equal(t, "Issue", event.Type)
	require.Equal(t, "create", event.Action)
	require.Equal(t, "https://linear.app/issue/LIN-1/foo", event.URL)
}

func TestLinearIngestCommentFoldsOntoIssue(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("linear")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	// A comment carries its parent issueId; correlation must route to the
	// issue's conversation so the assistant sees the whole issue thread.
	body := []byte(`{"type":"Comment","action":"create","data":{"id":"comment-1","issueId":"issue-1"}}`)

	result, err := definition.HandleWebhook(body, http.Header{}, config)
	require.NoError(t, err)
	require.NotNil(t, result.Event)

	require.Equal(t, "linear:issue:issue-1", result.Event.CorrelationID)
}

func TestLinearIngestLeavesEventIDEmptyWithoutDeliveryHeader(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("linear")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	body := []byte(`{"type":"Issue","action":"update","data":{"id":"i1"}}`)

	result, err := definition.HandleWebhook(body, http.Header{}, config)
	require.NoError(t, err)
	require.NotNil(t, result.Event)
	// No Linear-Delivery header: the event id is left empty here and filled
	// with an instance-scoped content-hash fallback by the dispatcher.
	require.Empty(t, result.Event.EventID)
}

func TestLinearFilterDefaultDeny(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("linear")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	// Supported type passes.
	match, err := config.Filter(linearTriggerEvent{EventType: "Issue.create"})
	require.NoError(t, err)
	require.True(t, match)

	// Unsupported type is denied by default.
	match, err = config.Filter(linearTriggerEvent{EventType: "Issue.bogus"})
	require.NoError(t, err)
	require.False(t, match)

	// Linear sends "remove" (not "delete") for deletions; the allowlist must
	// match what Linear actually delivers.
	match, err = config.Filter(linearTriggerEvent{EventType: "Issue.remove"})
	require.NoError(t, err)
	require.True(t, match)

	match, err = config.Filter(linearTriggerEvent{EventType: "Issue.delete"})
	require.NoError(t, err)
	require.False(t, match)
}

func TestLinearFilterCEL(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("linear")
	require.True(t, ok)

	config, err := definition.DecodeConfig(map[string]any{
		"filter": `event.action == "create"`,
	})
	require.NoError(t, err)

	match, err := config.Filter(linearTriggerEvent{EventType: "Issue.create", Action: "create"})
	require.NoError(t, err)
	require.True(t, match)

	match, err = config.Filter(linearTriggerEvent{EventType: "Issue.update", Action: "update"})
	require.NoError(t, err)
	require.False(t, match)
}

func TestLinearIngestRejectsStaleTimestamp(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("linear")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	// A captured-and-replayed signed body carries an old webhookTimestamp; it
	// must be rejected even though the (body) signature would still verify.
	stale := time.Now().Add(-10 * time.Minute).UnixMilli()
	body := fmt.Appendf(nil, `{"type":"Issue","action":"update","data":{"id":"i1"},"webhookTimestamp":%d}`, stale)

	_, err = definition.HandleWebhook(body, http.Header{}, config)
	require.Error(t, err)
	require.Contains(t, err.Error(), "freshness")
}

func TestLinearIngestAcceptsFreshTimestamp(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("linear")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	fresh := time.Now().UnixMilli()
	body := fmt.Appendf(nil, `{"type":"Issue","action":"update","data":{"id":"i1"},"webhookTimestamp":%d}`, fresh)

	result, err := definition.HandleWebhook(body, http.Header{}, config)
	require.NoError(t, err)
	require.NotNil(t, result.Event)
}

func TestLinearIngestCarriesUpdatedFrom(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("linear")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	// Linear `update` webhooks include `updatedFrom` (prior values of the
	// changed fields); it must survive into the normalized event so the
	// assistant can act on the specific transition.
	body := []byte(`{"type":"Issue","action":"update","data":{"id":"i1"},"updatedFrom":{"stateId":"old-state"}}`)

	result, err := definition.HandleWebhook(body, http.Header{}, config)
	require.NoError(t, err)
	event, ok := result.Event.Event.(linearTriggerEvent)
	require.True(t, ok)
	require.JSONEq(t, `{"stateId":"old-state"}`, string(event.UpdatedFrom))
}

func TestLinearIngestRejectsMissingType(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("linear")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	_, err = definition.HandleWebhook([]byte(`{"action":"create"}`), http.Header{}, config)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing type")
}
