package triggers

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestMSTeamsConfigSchemaConstrainsEventTypeItems(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("msteams")
	require.True(t, ok)

	var schema struct {
		Properties struct {
			EventTypes struct {
				Items struct {
					Enum []string `json:"enum"`
				} `json:"items"`
			} `json:"event_types"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(definition.ConfigSchema, &schema))
	require.ElementsMatch(t, supportedMSTeamsEventTypes, schema.Properties.EventTypes.Items.Enum)
}

func TestMSTeamsDecodeConfigRejectsUnsupportedEventType(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("msteams")
	require.True(t, ok)

	_, err := definition.DecodeConfig(map[string]any{
		"event_types": []any{"typing"},
	})
	require.Error(t, err)
}

func TestMSTeamsDecodeConfigRejectsInvalidFilter(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("msteams")
	require.True(t, ok)

	_, err := definition.DecodeConfig(map[string]any{
		"filter": "event.does_not_exist == ",
	})
	require.Error(t, err)
}

func TestMSTeamsIngestMessageActivity(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"type": "message",
		"id": "1485983408511",
		"timestamp": "2026-07-01T12:00:00.000Z",
		"serviceUrl": "https://smba.trafficmanager.net/teams/",
		"channelId": "msteams",
		"from": {"id": "29:user-id", "name": "Jo Doe", "aadObjectId": "aad-1"},
		"recipient": {"id": "28:bot-app-id", "name": "Gram"},
		"conversation": {"id": "19:channel@thread.tacv2;messageid=123", "conversationType": "channel", "tenantId": "tenant-1"},
		"text": "hello bot",
		"replyToId": "123",
		"channelData": {"channel": {"id": "19:channel@thread.tacv2"}, "team": {"id": "19:team@thread.tacv2"}, "tenant": {"id": "tenant-1"}}
	}`)

	ingest, err := msteamsIngest(body, http.Header{})
	require.NoError(t, err)
	require.Equal(t, "1485983408511", ingest.EventID)
	require.Equal(t, "19:channel@thread.tacv2;messageid=123", ingest.CorrelationID)

	event, ok := ingest.Event.(msteamsTriggerEvent)
	require.True(t, ok)
	require.Equal(t, "message", event.EventType)
	require.Equal(t, "hello bot", event.Text)
	require.Equal(t, "29:user-id", event.UserID)
	require.Equal(t, "Jo Doe", event.UserName)
	require.Equal(t, "aad-1", event.UserAADObjectID)
	require.Equal(t, "28:bot-app-id", event.BotID)
	require.Equal(t, "tenant-1", event.TenantID)
	require.Equal(t, "https://smba.trafficmanager.net/teams/", event.ServiceURL)
	require.Equal(t, "19:channel@thread.tacv2", event.TeamsChannelID)
	require.Equal(t, "19:team@thread.tacv2", event.TeamsTeamID)
	require.Equal(t, "123", event.ReplyToID)
	require.Equal(t, "channel", event.ConversationType)
}

func TestMSTeamsIngestMessageReaction(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"type": "messageReaction",
		"id": "act-2",
		"from": {"id": "29:user-id"},
		"conversation": {"id": "a:personal-chat", "conversationType": "personal", "tenantId": "tenant-1"},
		"reactionsAdded": [{"type": "like"}],
		"reactionsRemoved": [{"type": "heart"}]
	}`)

	ingest, err := msteamsIngest(body, http.Header{})
	require.NoError(t, err)

	event, ok := ingest.Event.(msteamsTriggerEvent)
	require.True(t, ok)
	require.Equal(t, "messageReaction", event.EventType)
	require.Equal(t, []string{"like"}, event.ReactionsAdded)
	require.Equal(t, []string{"heart"}, event.ReactionsRemoved)
}

func TestMSTeamsIngestConversationUpdateMembers(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"type": "conversationUpdate",
		"conversation": {"id": "19:group-chat"},
		"membersAdded": [{"id": "29:new-user"}, {"id": "28:bot-app-id"}],
		"membersRemoved": [{"id": "29:old-user"}],
		"channelData": {"tenant": {"id": "tenant-2"}}
	}`)

	ingest, err := msteamsIngest(body, http.Header{})
	require.NoError(t, err)
	// conversationUpdate can arrive without an activity id; the dispatcher
	// applies the instance-scoped content-hash fallback.
	require.Empty(t, ingest.EventID)

	event, ok := ingest.Event.(msteamsTriggerEvent)
	require.True(t, ok)
	require.Equal(t, []string{"29:new-user", "28:bot-app-id"}, event.MembersAdded)
	require.Equal(t, []string{"29:old-user"}, event.MembersRemoved)
	// Tenant falls back to channelData when conversation.tenantId is absent.
	require.Equal(t, "tenant-2", event.TenantID)
}

func TestMSTeamsIngestInstallationUpdate(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"type": "installationUpdate",
		"action": "add",
		"conversation": {"id": "a:install", "tenantId": "tenant-3"}
	}`)

	ingest, err := msteamsIngest(body, http.Header{})
	require.NoError(t, err)

	event, ok := ingest.Event.(msteamsTriggerEvent)
	require.True(t, ok)
	require.Equal(t, "installationUpdate", event.EventType)
	require.Equal(t, "add", event.Action)
}

func TestMSTeamsIngestMissingTypeErrors(t *testing.T) {
	t.Parallel()

	_, err := msteamsIngest([]byte(`{"text":"no type"}`), http.Header{})
	require.Error(t, err)
}

func TestMSTeamsCorrelationFallsBackToTenant(t *testing.T) {
	t.Parallel()

	ingest, err := msteamsIngest([]byte(`{"type":"installationUpdate","action":"remove","channelData":{"tenant":{"id":"tenant-9"}}}`), http.Header{})
	require.NoError(t, err)
	require.Equal(t, "tenant-9", ingest.CorrelationID)
}

func TestMSTeamsFilterMatchesEventTypeAndCEL(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("msteams")
	require.True(t, ok)

	config, err := definition.DecodeConfig(map[string]any{
		"event_types": []any{"message"},
		"filter":      `event.text.contains("deploy")`,
	})
	require.NoError(t, err)

	match, err := config.Filter(msteamsTriggerEvent{EventType: "message", Text: "please deploy now"})
	require.NoError(t, err)
	require.True(t, match)

	match, err = config.Filter(msteamsTriggerEvent{EventType: "message", Text: "unrelated"})
	require.NoError(t, err)
	require.False(t, match)
}

func TestMSTeamsFilterRejectsDisabledEventType(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("msteams")
	require.True(t, ok)

	config, err := definition.DecodeConfig(map[string]any{
		"event_types": []any{"message"},
	})
	require.NoError(t, err)

	match, err := config.Filter(msteamsTriggerEvent{EventType: "messageReaction"})
	require.NoError(t, err)
	require.False(t, match)
}

func TestMSTeamsAuthenticateRejectsMissingAppID(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("msteams")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	err = definition.AuthenticateWebhook(t.Context(), []byte(`{"type":"message"}`), http.Header{}, map[string]string{}, config)
	require.Error(t, err)
	require.Contains(t, err.Error(), "MSTEAMS_APP_ID")
}

func TestMSTeamsAuthenticateRejectsMissingBearer(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("msteams")
	require.True(t, ok)

	config, err := definition.DecodeConfig(nil)
	require.NoError(t, err)

	err = definition.AuthenticateWebhook(t.Context(), []byte(`{"type":"message"}`), http.Header{}, map[string]string{
		"MSTEAMS_APP_ID": "app-1",
	}, config)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing bearer token")
}

// botFrameworkTestServer stands in for Microsoft's OpenID metadata + JWKS
// endpoints and signs tokens with a matching test key.
type botFrameworkTestServer struct {
	authenticator *botFrameworkAuthenticator
	key           *rsa.PrivateKey
}

// botFrameworkTestKey is shared across the auth tests; generating a fresh
// 2048-bit key per test server wastes ~1s of wall clock across the suite.
var botFrameworkTestKey = sync.OnceValues(func() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
})

func newBotFrameworkTestServer(t *testing.T) *botFrameworkTestServer {
	t.Helper()

	key, err := botFrameworkTestKey()
	require.NoError(t, err)

	jwks, err := json.Marshal(jose.JSONWebKeySet{
		Keys: []jose.JSONWebKey{{
			Key:       key.Public(),
			KeyID:     "test-key",
			Algorithm: "RS256",
			Use:       "sig",
		}},
	})
	require.NoError(t, err)

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	mux.HandleFunc("/metadata", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"jwks_uri": server.URL + "/keys"})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(jwks)
	})

	authenticator := newBotFrameworkAuthenticator(server.URL+"/metadata", botFrameworkIssuer)
	// The production client's guardian policy blocks loopback, so reach the
	// httptest JWKS server with its own client instead.
	authenticator.clientFn = server.Client

	return &botFrameworkTestServer{
		authenticator: authenticator,
		key:           key,
	}
}

func (s *botFrameworkTestServer) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key"
	raw, err := token.SignedString(s.key)
	require.NoError(t, err)
	return raw
}

func bearerHeaders(token string) http.Header {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)
	return headers
}

func TestMSTeamsAuthenticateAcceptsValidToken(t *testing.T) {
	t.Parallel()

	server := newBotFrameworkTestServer(t)
	token := server.sign(t, jwt.MapClaims{
		"iss":        botFrameworkIssuer,
		"aud":        "app-1",
		"exp":        time.Now().Add(time.Hour).Unix(),
		"serviceurl": "https://smba.trafficmanager.net/teams",
	})

	body := []byte(`{"type":"message","serviceUrl":"https://smba.trafficmanager.net/teams/"}`)
	err := server.authenticator.authenticate(t.Context(), body, bearerHeaders(token), "app-1")
	require.NoError(t, err)
}

func TestMSTeamsAuthenticateRejectsWrongAudience(t *testing.T) {
	t.Parallel()

	server := newBotFrameworkTestServer(t)
	token := server.sign(t, jwt.MapClaims{
		"iss":        botFrameworkIssuer,
		"aud":        "some-other-app",
		"exp":        time.Now().Add(time.Hour).Unix(),
		"serviceurl": "https://smba.trafficmanager.net/teams",
	})

	body := []byte(`{"type":"message","serviceUrl":"https://smba.trafficmanager.net/teams"}`)
	err := server.authenticator.authenticate(t.Context(), body, bearerHeaders(token), "app-1")
	require.Error(t, err)
}

func TestMSTeamsAuthenticateRejectsWrongIssuer(t *testing.T) {
	t.Parallel()

	server := newBotFrameworkTestServer(t)
	token := server.sign(t, jwt.MapClaims{
		"iss":        "https://evil.example.com",
		"aud":        "app-1",
		"exp":        time.Now().Add(time.Hour).Unix(),
		"serviceurl": "https://smba.trafficmanager.net/teams",
	})

	body := []byte(`{"type":"message","serviceUrl":"https://smba.trafficmanager.net/teams"}`)
	err := server.authenticator.authenticate(t.Context(), body, bearerHeaders(token), "app-1")
	require.Error(t, err)
}

func TestMSTeamsAuthenticateRejectsExpiredToken(t *testing.T) {
	t.Parallel()

	server := newBotFrameworkTestServer(t)
	token := server.sign(t, jwt.MapClaims{
		"iss":        botFrameworkIssuer,
		"aud":        "app-1",
		"exp":        time.Now().Add(-time.Hour).Unix(),
		"serviceurl": "https://smba.trafficmanager.net/teams",
	})

	body := []byte(`{"type":"message","serviceUrl":"https://smba.trafficmanager.net/teams"}`)
	err := server.authenticator.authenticate(t.Context(), body, bearerHeaders(token), "app-1")
	require.Error(t, err)
}

func TestMSTeamsAuthenticateRejectsServiceURLMismatch(t *testing.T) {
	t.Parallel()

	server := newBotFrameworkTestServer(t)
	token := server.sign(t, jwt.MapClaims{
		"iss":        botFrameworkIssuer,
		"aud":        "app-1",
		"exp":        time.Now().Add(time.Hour).Unix(),
		"serviceurl": "https://smba.trafficmanager.net/teams",
	})

	body := []byte(`{"type":"message","serviceUrl":"https://attacker.example.com"}`)
	err := server.authenticator.authenticate(t.Context(), body, bearerHeaders(token), "app-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "serviceUrl")
}

func TestMSTeamsAuthenticateRejectsMissingServiceURLClaim(t *testing.T) {
	t.Parallel()

	server := newBotFrameworkTestServer(t)
	token := server.sign(t, jwt.MapClaims{
		"iss": botFrameworkIssuer,
		"aud": "app-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	body := []byte(`{"type":"message","serviceUrl":"https://smba.trafficmanager.net/teams"}`)
	err := server.authenticator.authenticate(t.Context(), body, bearerHeaders(token), "app-1")
	require.Error(t, err)
}

func TestMSTeamsAuthenticateNegativeCachesMetadataFailure(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	mux.HandleFunc("/metadata", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	authenticator := newBotFrameworkAuthenticator(server.URL+"/metadata", botFrameworkIssuer)
	authenticator.clientFn = server.Client

	err := authenticator.authenticate(t.Context(), []byte(`{"type":"message"}`), bearerHeaders("x.y.z"), "app-1")
	require.ErrorContains(t, err, "fetch openid metadata")

	// The failure is negative-cached: the next delivery fails fast without
	// re-fetching the metadata document.
	err = authenticator.authenticate(t.Context(), []byte(`{"type":"message"}`), bearerHeaders("x.y.z"), "app-1")
	require.ErrorContains(t, err, "retrying later")
}
