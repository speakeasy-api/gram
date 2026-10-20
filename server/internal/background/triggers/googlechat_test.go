package triggers

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoogleChatAuthenticate(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}}))
	}))
	t.Cleanup(server.Close)
	keys := oidc.NewRemoteKeySet(t.Context(), server.URL)
	for _, tt := range []struct {
		name, issuer, audience string
		expiry, issued         time.Time
		key                    *rsa.PrivateKey
		valid                  bool
	}{
		{"valid", googleChatIssuer, "12345", time.Now().Add(time.Hour), time.Now(), key, true},
		{"valid longer lifetime", googleChatIssuer, "12345", time.Now().Add(2 * time.Hour), time.Now(), key, true},
		{"wrong issuer", "https://accounts.google.com", "12345", time.Now().Add(time.Hour), time.Now(), key, false},
		{"wrong audience", googleChatIssuer, "99999", time.Now().Add(time.Hour), time.Now(), key, false},
		{"expired", googleChatIssuer, "12345", time.Now().Add(-time.Minute), time.Now().Add(-time.Hour), key, false},
		{"future issuance", googleChatIssuer, "12345", time.Now().Add(time.Hour), time.Now().Add(10 * time.Minute), key, false},
		{"bad signature", googleChatIssuer, "12345", time.Now().Add(time.Hour), time.Now(), other, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{Issuer: tt.issuer, Audience: jwt.ClaimStrings{tt.audience}, IssuedAt: jwt.NewNumericDate(tt.issued), ExpiresAt: jwt.NewNumericDate(tt.expiry)})
			token.Header["kid"] = "test"
			raw, err := token.SignedString(tt.key)
			require.NoError(t, err)
			err = authenticateGoogleChat(t.Context(), keys, http.Header{"Authorization": []string{"Bearer " + raw}}, "12345")
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	require.Error(t, authenticateGoogleChat(t.Context(), keys, http.Header{}, "12345"))
	require.Error(t, authenticateGoogleChat(t.Context(), keys, http.Header{"Authorization": []string{"Bearer invalid"}}, "12345"))
	require.Error(t, authenticateGoogleChat(t.Context(), keys, http.Header{}, "https://endpoint.invalid"))
}

const googlechatTestMessage = `{"type":"MESSAGE","space":{"name":"spaces/test","type":"ROOM"},"message":{"name":"spaces/test/messages/msg","text":"@app help","sender":{"name":"users/test","type":"HUMAN"},"thread":{"name":"spaces/test/threads/thread"},"createTime":"2026-01-01T12:00:00Z"}}`

func TestGoogleChatIngressAndIsolation(t *testing.T) {
	t.Parallel()
	def, ok := GetDefinition("googlechat")
	require.True(t, ok)
	cfg, err := def.DecodeConfig(map[string]any{})
	require.NoError(t, err)
	first, err := def.HandleWebhook([]byte(googlechatTestMessage), nil, cfg)
	require.NoError(t, err)
	retry, err := def.HandleWebhook([]byte(googlechatTestMessage), nil, cfg)
	require.NoError(t, err)
	require.Equal(t, first.Event.EventID, retry.Event.EventID)
	require.Equal(t, "spaces/test/messages/msg", first.Event.EventID)
	require.Equal(t, "spaces/test/threads/thread", first.Event.CorrelationID)
	require.Equal(t, scopeWebhookEventID("connection1", first.Event.EventID, nil), scopeWebhookEventID("connection1", retry.Event.EventID, nil))
	require.NotEqual(t, scopeWebhookEventID("connection1", first.Event.EventID, nil), scopeWebhookEventID("connection2", first.Event.EventID, nil))
	require.NotEqual(t, scopeGoogleChatCorrelation("connection1", first.Event.CorrelationID), scopeGoogleChatCorrelation("connection2", first.Event.CorrelationID))
	require.NotEqual(t, scopeGoogleChatCorrelation("connection1", first.Event.CorrelationID), scopeGoogleChatCorrelation("connection1", "spaces/other/threads/thread"))
}

func TestGoogleChatOnlyHumanMessages(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		kind, sender, space string
		want                bool
	}{
		{"MESSAGE", "HUMAN", "DM", true},
		{"MESSAGE", "HUMAN", "ROOM", true},
		{"MESSAGE", "BOT", "ROOM", false},
		{"ADDED_TO_SPACE", "HUMAN", "ROOM", false},
		{"REMOVED_FROM_SPACE", "HUMAN", "ROOM", false},
		{"CARD_CLICKED", "HUMAN", "ROOM", false},
		{"", "HUMAN", "ROOM", false},
	} {
		t.Run(tt.kind+tt.sender+tt.space, func(t *testing.T) {
			t.Parallel()
			var event googlechatInteraction
			require.NoError(t, json.Unmarshal([]byte(googlechatTestMessage), &event))
			event.Type = tt.kind
			event.Message.Sender.Type = tt.sender
			event.Space.Type = tt.space
			body, err := json.Marshal(event)
			require.NoError(t, err)
			got, err := googlechatIngest(body, nil)
			require.NoError(t, err)
			if tt.want {
				require.NotNil(t, got)
				event, ok := got.Event.(googlechatTriggerEvent)
				require.True(t, ok)
				require.True(t, event.Addressed)
			} else {
				require.NotNil(t, got)
				require.Nil(t, got.Event)
			}
		})
	}
}

func TestGoogleChatFilters(t *testing.T) {
	t.Parallel()
	def, _ := GetDefinition("googlechat")
	cfg, err := def.DecodeConfig(map[string]any{"space_names": []any{"spaces/allowed"}, "filter": "event.text.contains('help')"})
	require.NoError(t, err)
	for _, tt := range []struct {
		space, text string
		want        bool
	}{{"spaces/allowed", "help", true}, {"spaces/denied", "help", false}, {"spaces/allowed", "ignore", false}} {
		ok, err := cfg.Filter(googlechatTriggerEvent{EventType: "MESSAGE", SpaceName: tt.space, Text: tt.text})
		require.NoError(t, err)
		require.Equal(t, tt.want, ok)
	}
	_, err = def.DecodeConfig(map[string]any{"filter": "invalid ("})
	require.Error(t, err)
	_, err = def.DecodeConfig(map[string]any{"space_names": []any{"invalid"}})
	require.Error(t, err)
}

// Exercise the provider boundary consumed by the HTTP handler. ProcessWebhook
// preserves Response when no event is emitted, filtering returns no task, or a
// duplicate dispatch is acknowledged; the HTTP handler writes it verbatim.
func TestGoogleChatWebhookAcknowledgment(t *testing.T) {
	t.Parallel()
	definition, ok := GetDefinition("googlechat")
	require.True(t, ok)
	for _, tt := range []struct {
		name, body string
		config     map[string]any
		event      bool
		filtered   bool
	}{
		{"message", googlechatTestMessage, map[string]any{}, true, false},
		{"retry", googlechatTestMessage, map[string]any{}, true, false},
		{"filtered", googlechatTestMessage, map[string]any{"space_names": []any{"spaces/other"}}, true, true},
		{"lifecycle", `{"type":"ADDED_TO_SPACE"}`, map[string]any{}, false, false},
		{"bot", `{"type":"MESSAGE","message":{"sender":{"type":"BOT"}}}`, map[string]any{}, false, false},
		{"unknown", `{"type":"UNKNOWN"}`, map[string]any{}, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config, err := definition.DecodeConfig(tt.config)
			require.NoError(t, err)
			result, err := definition.HandleWebhook([]byte(tt.body), nil, config)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, result.Response)
			require.Equal(t, http.StatusOK, result.Response.Status)
			require.Equal(t, "application/json", result.Response.ContentType)
			require.JSONEq(t, `{}`, string(result.Response.Body))
			require.Equal(t, "{}", string(result.Response.Body))
			if tt.event {
				require.NotNil(t, result.Event)
				accepted, err := config.Filter(result.Event.Event)
				require.NoError(t, err)
				require.Equal(t, !tt.filtered, accepted)
			} else {
				require.Nil(t, result.Event)
			}
		})
	}
}

func TestGoogleChatSpaceTypeNormalization(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ legacy, current, want string }{
		{"DM", "", "DIRECT_MESSAGE"}, {"ROOM", "", "SPACE"}, {"ROOM", "GROUP_CHAT", "GROUP_CHAT"}, {"DM", "DIRECT_MESSAGE", "DIRECT_MESSAGE"}, {"ROOM", "SPACE", "SPACE"},
	} {
		var event googlechatInteraction
		require.NoError(t, json.Unmarshal([]byte(googlechatTestMessage), &event))
		event.Space.Type = tt.legacy
		event.Space.SpaceType = tt.current
		body, err := json.Marshal(event)
		require.NoError(t, err)
		result, err := googlechatIngest(body, nil)
		require.NoError(t, err)
		require.NotNil(t, result.Event)
		normalized, ok := result.Event.(googlechatTriggerEvent)
		require.True(t, ok)
		require.Equal(t, tt.want, normalized.SpaceType)
	}
}
