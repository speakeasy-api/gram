package triggers

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func workspaceConfig(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{
		"space_names": []any{"spaces/test"},
		"workspace_events": map[string]any{
			"subscription":           "projects/test-project/subscriptions/chat-push",
			"workspace_subscription": "subscriptions/chat-events",
			"audience":               "https://example.test/chat-events",
			"service_account_email":  "chat-push@test-project.iam.gserviceaccount.com",
			"user_names":             []any{"users/internal", "users/external"},
			"text_prefix":            "assistant:",
		},
	}
}

func workspacePush(t *testing.T, eventType, sender, text string) []byte {
	t.Helper()
	payload := map[string]any{"message": map[string]any{
		"name": "spaces/test/messages/message", "sender": map[string]string{"name": sender, "type": "HUMAN"},
		"thread": map[string]string{"name": "spaces/test/threads/digest"}, "text": text, "createTime": "2026-01-01T12:00:00Z",
	}}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	body, err := json.Marshal(map[string]any{
		"subscription": "projects/test-project/subscriptions/chat-push",
		"message": map[string]any{"data": data, "attributes": map[string]string{
			"ce-type": eventType, "ce-source": "//workspaceevents.googleapis.com/subscriptions/chat-events",
			"ce-subject": "//chat.googleapis.com/spaces/test", "ce-specversion": "1.0",
		}},
	})
	require.NoError(t, err)
	return body
}

func TestGoogleChatWorkspaceCommands(t *testing.T) {
	t.Parallel()
	definition, ok := GetDefinition("googlechat")
	require.True(t, ok)
	config, err := definition.DecodeConfig(workspaceConfig(t))
	require.NoError(t, err)
	for _, tt := range []struct {
		name, sender, text string
		want               bool
	}{
		{"internal participant", "users/internal", "assistant: summarize", true},
		{"external participant", "users/external", "assistant: explain the digest", true},
		{"leading whitespace", "users/external", "  assistant: explain", true},
		{"unlisted participant", "users/unlisted", "assistant: summarize", false},
		{"ordinary conversation", "users/external", "thanks for the digest", false},
		{"embedded command", "users/external", "someone said assistant: summarize", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := workspacePush(t, googleChatMessageCreated, tt.sender, tt.text)
			result, err := definition.HandleWebhook(body, nil, config)
			require.NoError(t, err)
			require.NotNil(t, result.Event)
			allowed, err := config.Filter(result.Event.Event)
			require.NoError(t, err)
			require.Equal(t, tt.want, allowed)
			require.Equal(t, "spaces/test/messages/message", result.Event.EventID)
			require.Equal(t, "spaces/test/threads/digest", result.Event.CorrelationID)
			require.NotContains(t, string(result.Event.RawPayload), "subscription")
			retry, err := definition.HandleWebhook(body, nil, config)
			require.NoError(t, err)
			require.Equal(t, result.Event.EventID, retry.Event.EventID)
		})
	}
}

func TestGoogleChatWorkspaceConfigFailsClosed(t *testing.T) {
	t.Parallel()
	definition, _ := GetDefinition("googlechat")
	for _, field := range []string{"subscription", "workspace_subscription", "audience", "service_account_email", "user_names", "text_prefix"} {
		t.Run("missing "+field, func(t *testing.T) {
			t.Parallel()
			raw := workspaceConfig(t)
			settings, ok := raw["workspace_events"].(map[string]any)
			require.True(t, ok)
			delete(settings, field)
			_, err := definition.DecodeConfig(raw)
			require.Error(t, err)
		})
	}
	for _, tt := range []struct {
		field string
		value any
	}{
		{"subscription", "chat-push"}, {"workspace_subscription", "chat-events"},
		{"audience", "http://example.test/push"}, {"audience", "https://user@example.test/push"},
		{"service_account_email", "user@example.test"}, {"user_names", []any{}},
		{"user_names", []any{"users/all"}}, {"user_names", []any{"users/app"}},
		{"user_names", []any{"users/external/other"}}, {"text_prefix", " "},
	} {
		t.Run(tt.field+" invalid "+fmt.Sprint(tt.value), func(t *testing.T) {
			t.Parallel()
			raw := workspaceConfig(t)
			settings, ok := raw["workspace_events"].(map[string]any)
			require.True(t, ok)
			settings[tt.field] = tt.value
			_, err := definition.DecodeConfig(raw)
			require.Error(t, err)
		})
	}
	for _, spaces := range [][]any{nil, {}, {"spaces/test", "spaces/other"}, {"spaces/test/messages/message"}} {
		raw := workspaceConfig(t)
		raw["space_names"] = spaces
		_, err := definition.DecodeConfig(raw)
		require.Error(t, err)
	}
}

func TestGoogleChatWorkspaceRejectsCrossSubscriptionAndSpace(t *testing.T) {
	t.Parallel()
	definition, _ := GetDefinition("googlechat")
	config, err := definition.DecodeConfig(workspaceConfig(t))
	require.NoError(t, err)
	for _, tt := range []struct{ name, from, to string }{
		{"push subscription", "projects/test-project/subscriptions/chat-push", "projects/other/subscriptions/chat-push"},
		{"workspace source", "subscriptions/chat-events", "subscriptions/other"},
		{"subject", "//chat.googleapis.com/spaces/test", "//chat.googleapis.com/spaces/other"},
		{"message space", "spaces/test/messages/message", "spaces/other/messages/message"},
		{"thread space", "spaces/test/threads/digest", "spaces/other/threads/digest"},
		{"thread traversal", "spaces/test/threads/digest", "spaces/test/threads/../digest"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := workspacePush(t, googleChatMessageCreated, "users/external", "assistant: explain")
			var push googlechatPushEnvelope
			require.NoError(t, json.Unmarshal(body, &push))
			push.Subscription = strings.ReplaceAll(push.Subscription, tt.from, tt.to)
			for key, value := range push.Message.Attributes {
				push.Message.Attributes[key] = strings.ReplaceAll(value, tt.from, tt.to)
			}
			push.Message.Data = []byte(strings.ReplaceAll(string(push.Message.Data), tt.from, tt.to))
			changedBody, err := json.Marshal(push)
			require.NoError(t, err)
			_, err = definition.HandleWebhook(changedBody, nil, config)
			require.Error(t, err)
		})
	}
}

func TestGoogleChatWorkspaceIgnoresBotsAndNonCreationEvents(t *testing.T) {
	t.Parallel()
	definition, _ := GetDefinition("googlechat")
	config, err := definition.DecodeConfig(workspaceConfig(t))
	require.NoError(t, err)
	for _, kind := range []string{"google.workspace.chat.message.v1.updated", "google.workspace.chat.message.v1.deleted", "google.workspace.events.subscription.v1.expirationReminder"} {
		result, err := definition.HandleWebhook(workspacePush(t, kind, "users/external", "assistant: explain"), nil, config)
		require.NoError(t, err)
		require.Nil(t, result.Event)
	}
	var push googlechatPushEnvelope
	require.NoError(t, json.Unmarshal(workspacePush(t, googleChatMessageCreated, "users/external", "assistant: explain"), &push))
	push.Message.Data = []byte(strings.ReplaceAll(string(push.Message.Data), "HUMAN", "BOT"))
	body, err := json.Marshal(push)
	require.NoError(t, err)
	result, err := definition.HandleWebhook(body, nil, config)
	require.NoError(t, err)
	require.Nil(t, result.Event)
	_, err = definition.HandleWebhook([]byte(googlechatTestMessage), nil, config)
	require.Error(t, err, "native Chat interactions cannot enter the Pub/Sub ingress")
}

func TestGoogleChatWorkspaceCELStillNarrowsCommands(t *testing.T) {
	t.Parallel()
	definition, _ := GetDefinition("googlechat")
	raw := workspaceConfig(t)
	raw["filter"] = "event.text.contains('digest')"
	config, err := definition.DecodeConfig(raw)
	require.NoError(t, err)
	result, err := definition.HandleWebhook(workspacePush(t, googleChatMessageCreated, "users/external", "assistant: explain"), nil, config)
	require.NoError(t, err)
	allowed, err := config.Filter(result.Event.Event)
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestGoogleChatPushAuthentication(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	keys := oidc.NewRemoteKeySet(t.Context(), server.URL)
	definition, _ := GetDefinition("googlechat")
	decoded, err := definition.DecodeConfig(workspaceConfig(t))
	require.NoError(t, err)
	config, ok := decoded.(googlechatTriggerConfig)
	require.True(t, ok)
	now := time.Now()
	for _, tt := range []struct {
		name, issuer, audience, email string
		verified                      bool
		expiry, issued                time.Time
		key                           *rsa.PrivateKey
		want                          bool
	}{
		{"valid", googleChatPushIssuer, config.WorkspaceEvents.Audience, config.WorkspaceEvents.ServiceAccountEmail, true, now.Add(time.Hour), now, key, true},
		{"wrong issuer", googleChatIssuer, config.WorkspaceEvents.Audience, config.WorkspaceEvents.ServiceAccountEmail, true, now.Add(time.Hour), now, key, false},
		{"wrong audience", googleChatPushIssuer, "https://other.test", config.WorkspaceEvents.ServiceAccountEmail, true, now.Add(time.Hour), now, key, false},
		{"wrong identity", googleChatPushIssuer, config.WorkspaceEvents.Audience, "other@test-project.iam.gserviceaccount.com", true, now.Add(time.Hour), now, key, false},
		{"unverified identity", googleChatPushIssuer, config.WorkspaceEvents.Audience, config.WorkspaceEvents.ServiceAccountEmail, false, now.Add(time.Hour), now, key, false},
		{"expired", googleChatPushIssuer, config.WorkspaceEvents.Audience, config.WorkspaceEvents.ServiceAccountEmail, true, now.Add(-time.Minute), now.Add(-time.Hour), key, false},
		{"future issued", googleChatPushIssuer, config.WorkspaceEvents.Audience, config.WorkspaceEvents.ServiceAccountEmail, true, now.Add(time.Hour), now.Add(10 * time.Minute), key, false},
		{"wrong signature", googleChatPushIssuer, config.WorkspaceEvents.Audience, config.WorkspaceEvents.ServiceAccountEmail, true, now.Add(time.Hour), now, other, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"iss": tt.issuer, "aud": tt.audience, "email": tt.email, "email_verified": tt.verified, "iat": tt.issued.Unix(), "exp": tt.expiry.Unix()})
			token.Header["kid"] = "test"
			raw, err := token.SignedString(tt.key)
			require.NoError(t, err)
			err = authenticateGoogleChatPush(t.Context(), keys, http.Header{"Authorization": []string{"Bearer " + raw}}, *config.WorkspaceEvents, now)
			if tt.want {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	require.Error(t, authenticateGoogleChatPush(t.Context(), keys, nil, *config.WorkspaceEvents, now))
}

func TestGoogleChatWorkspaceBatchKeepsThreadsAndPayloadsSeparate(t *testing.T) {
	t.Parallel()
	definition, _ := GetDefinition("googlechat")
	config, err := definition.DecodeConfig(workspaceConfig(t))
	require.NoError(t, err)
	var push googlechatPushEnvelope
	require.NoError(t, json.Unmarshal(workspacePush(t, googleChatMessagesCreated, "users/internal", "assistant: first"), &push))
	first := append(json.RawMessage(nil), push.Message.Data...)
	second := json.RawMessage(strings.NewReplacer("users/internal", "users/external", "messages/message", "messages/second", "threads/digest", "threads/other", "assistant: first", "assistant: second").Replace(string(first)))
	bot := json.RawMessage(strings.ReplaceAll(string(first), "HUMAN", "BOT"))
	push.Message.Data, err = json.Marshal(map[string]any{"messages": []json.RawMessage{first, bot, second}})
	require.NoError(t, err)
	body, err := json.Marshal(push)
	require.NoError(t, err)
	result, err := definition.HandleWebhook(body, nil, config)
	require.NoError(t, err)
	require.Nil(t, result.Event)
	require.Len(t, result.Events, 2)
	require.Equal(t, "spaces/test/threads/digest", result.Events[0].CorrelationID)
	require.Equal(t, "spaces/test/threads/other", result.Events[1].CorrelationID)
	require.NotEqual(t, result.Events[0].EventID, result.Events[1].EventID)
	require.NotContains(t, string(result.Events[0].RawPayload), "assistant: second")
	require.NotContains(t, string(result.Events[1].RawPayload), "assistant: first")
	for _, event := range result.Events {
		allowed, err := config.Filter(event.Event)
		require.NoError(t, err)
		require.True(t, allowed)
	}
}

func TestGoogleChatWorkspaceRequiresMessageResourceData(t *testing.T) {
	t.Parallel()
	definition, _ := GetDefinition("googlechat")
	config, err := definition.DecodeConfig(workspaceConfig(t))
	require.NoError(t, err)
	var push googlechatPushEnvelope
	require.NoError(t, json.Unmarshal(workspacePush(t, googleChatMessageCreated, "users/internal", "assistant: first"), &push))
	push.Message.Data = []byte(`{"message":{"name":"spaces/test/messages/message"}}`)
	body, err := json.Marshal(push)
	require.NoError(t, err)
	_, err = definition.HandleWebhook(body, nil, config)
	require.ErrorContains(t, err, "requires message resource data")
}
