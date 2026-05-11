package triggers

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWebhookDefaultDoesNotRequireSignature(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("webhook")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	require.NoError(t, definition.AuthenticateWebhook(
		[]byte(`{"foo":"bar"}`),
		http.Header{},
		map[string]string{},
		config,
	))
}

func TestWebhookLinearSchemeHexBareBody(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("webhook")
	require.True(t, ok)

	config, err := definition.DecodeConfig(map[string]any{
		"signature": map[string]any{
			"algorithm":  "hmac-sha256",
			"header":     "Linear-Signature",
			"secret_env": "LINEAR_SIGNING_SECRET",
		},
		"extractors": map[string]any{
			"event_type":     "body.type + '.' + body.action",
			"correlation_id": "body.data.id",
		},
	})
	require.NoError(t, err)

	body := []byte(`{"type":"Issue","action":"create","data":{"id":"issue-1"}}`)
	mac := hmac.New(sha256.New, []byte("shh"))
	mac.Write(body)
	headers := http.Header{}
	headers.Set("Linear-Signature", hex.EncodeToString(mac.Sum(nil)))

	require.NoError(t, definition.AuthenticateWebhook(body, headers, map[string]string{
		"LINEAR_SIGNING_SECRET": "shh",
	}, config))

	result, err := definition.HandleWebhook(body, headers, config)
	require.NoError(t, err)
	require.NotNil(t, result.Event)

	event, ok := result.Event.Event.(webhookTriggerEvent)
	require.True(t, ok)
	require.Equal(t, "Issue.create", event.EventType)
	require.Equal(t, "issue-1", event.CorrelationID)
	require.Equal(t, "issue-1", result.Event.CorrelationID)
}

func TestWebhookGitHubSchemePrefixedHex(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("webhook")
	require.True(t, ok)

	config, err := definition.DecodeConfig(map[string]any{
		"signature": map[string]any{
			"algorithm":  "hmac-sha256",
			"header":     "X-Hub-Signature-256",
			"prefix":     "sha256=",
			"secret_env": "GITHUB_WEBHOOK_SECRET",
		},
		"extractors": map[string]any{
			"event_type": "headers['X-Github-Event']",
		},
	})
	require.NoError(t, err)

	body := []byte(`{"action":"opened"}`)
	mac := hmac.New(sha256.New, []byte("shh"))
	mac.Write(body)
	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	headers.Set("X-Github-Event", "pull_request")

	require.NoError(t, definition.AuthenticateWebhook(body, headers, map[string]string{
		"GITHUB_WEBHOOK_SECRET": "shh",
	}, config))

	result, err := definition.HandleWebhook(body, headers, config)
	require.NoError(t, err)
	event, ok := result.Event.Event.(webhookTriggerEvent)
	require.True(t, ok)
	require.Equal(t, "pull_request", event.EventType)
}

func TestWebhookSlackSchemeTimestampedTemplate(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("webhook")
	require.True(t, ok)

	config, err := definition.DecodeConfig(map[string]any{
		"signature": map[string]any{
			"algorithm":        "hmac-sha256",
			"header":           "X-Slack-Signature",
			"prefix":           "v0=",
			"sign_template":    "v0:{timestamp}:{body}",
			"timestamp_header": "X-Slack-Request-Timestamp",
			"secret_env":       "SLACK_SIGNING_SECRET",
		},
	})
	require.NoError(t, err)

	body := []byte(`{"event":{"type":"app_mention"}}`)
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	mac := hmac.New(sha256.New, []byte("shh"))
	mac.Write([]byte("v0:" + timestamp + ":" + string(body)))
	headers := http.Header{}
	headers.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	headers.Set("X-Slack-Request-Timestamp", timestamp)

	require.NoError(t, definition.AuthenticateWebhook(body, headers, map[string]string{
		"SLACK_SIGNING_SECRET": "shh",
	}, config))
}

func TestWebhookRejectsStaleTimestamp(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("webhook")
	require.True(t, ok)

	config, err := definition.DecodeConfig(map[string]any{
		"signature": map[string]any{
			"algorithm":              "hmac-sha256",
			"header":                 "X-Sig",
			"sign_template":          "{timestamp}.{body}",
			"timestamp_header":       "X-Ts",
			"timestamp_skew_seconds": 60,
			"secret_env":             "SECRET",
		},
	})
	require.NoError(t, err)

	body := []byte(`{}`)
	timestamp := fmt.Sprintf("%d", time.Now().Add(-5*time.Minute).Unix())
	mac := hmac.New(sha256.New, []byte("shh"))
	mac.Write([]byte(timestamp + "." + string(body)))
	headers := http.Header{}
	headers.Set("X-Sig", hex.EncodeToString(mac.Sum(nil)))
	headers.Set("X-Ts", timestamp)

	err = definition.AuthenticateWebhook(body, headers, map[string]string{"SECRET": "shh"}, config)
	require.Error(t, err)
}

func TestWebhookSchemeBase64Encoded(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("webhook")
	require.True(t, ok)

	config, err := definition.DecodeConfig(map[string]any{
		"signature": map[string]any{
			"algorithm":  "hmac-sha1",
			"header":     "X-Sig",
			"encoding":   "base64",
			"secret_env": "SECRET",
		},
	})
	require.NoError(t, err)

	body := []byte(`{"a":1}`)
	mac := hmac.New(sha1.New, []byte("shh"))
	mac.Write(body)
	headers := http.Header{}
	headers.Set("X-Sig", base64.StdEncoding.EncodeToString(mac.Sum(nil)))

	require.NoError(t, definition.AuthenticateWebhook(body, headers, map[string]string{"SECRET": "shh"}, config))
}

func TestWebhookFilterAndAllowedEventTypes(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("webhook")
	require.True(t, ok)

	config, err := definition.DecodeConfig(map[string]any{
		"allowed_event_types": []any{"Issue.create"},
		"filter":              `event.event_type == "Issue.create"`,
	})
	require.NoError(t, err)

	match, err := config.Filter(webhookTriggerEvent{EventType: "Issue.create"})
	require.NoError(t, err)
	require.True(t, match)

	miss, err := config.Filter(webhookTriggerEvent{EventType: "Comment.create"})
	require.NoError(t, err)
	require.False(t, miss)
}

func TestWebhookRejectsBadExtractorReturnType(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("webhook")
	require.True(t, ok)

	_, err := definition.DecodeConfig(map[string]any{
		"extractors": map[string]any{
			"event_type": `["a","b"]`, // returns list, never string-convertible
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must evaluate to string")
}

func TestWebhookRejectsUnknownAlgorithm(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("webhook")
	require.True(t, ok)

	_, err := definition.DecodeConfig(map[string]any{
		"signature": map[string]any{
			"algorithm":  "ed25519",
			"header":     "X-Sig",
			"secret_env": "S",
		},
	})
	require.Error(t, err)
}
