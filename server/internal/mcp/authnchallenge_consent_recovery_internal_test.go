package mcp

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

func TestConsentUpstreamRecovery(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		status    int
		discovery bool
		personal  bool
		stripped  bool
		want      string
	}{
		{name: "rejected grant", status: http.StatusUnauthorized, discovery: true, personal: true, want: consentRecoveryReconnect},
		{name: "forbidden may be a permission error", status: http.StatusForbidden, discovery: true, personal: true},
		{name: "server credential", status: http.StatusUnauthorized, discovery: true},
		{name: "redirect stripped grant", status: http.StatusUnauthorized, discovery: true, personal: true, stripped: true},
		{name: "session cleanup", status: http.StatusUnauthorized, personal: true},
		{name: "rate limit", status: http.StatusTooManyRequests, discovery: true, personal: true},
		{name: "outage", status: http.StatusServiceUnavailable, discovery: true, personal: true},
		{name: "upstream session not found", status: http.StatusNotFound, discovery: true, personal: true},
		{name: "success", status: http.StatusOK, discovery: true, personal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			p := &proxy.Proxy{}
			personal := ""
			if tc.personal {
				personal = "synthetic-bearer"
			}
			observeConsentUpstreamResponse(p, w, slog.New(slog.DiscardHandler), tc.discovery, personal)
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			if !tc.stripped {
				req.Header.Set("Authorization", "Bearer synthetic-bearer")
			}
			resp := &http.Response{StatusCode: tc.status, Header: make(http.Header), Request: req}
			resp.Header.Set(consentRecoveryHeader, consentRecoveryRestart)
			require.NoError(t, p.UpstreamResponseInterceptor(t.Context(), resp))
			require.Empty(t, resp.Header.Get(consentRecoveryHeader), "upstream cannot supply the hint")
			require.Equal(t, tc.want, w.Header().Get(consentRecoveryHeader))
		})
	}
}

func TestConsentReconnectTargetsReadySelectedCredential(t *testing.T) {
	t.Parallel()
	first, second, shared := uuid.New(), uuid.New(), uuid.New()
	firstRegistration, secondRegistration := uuid.New(), uuid.New()
	cards := []remoteSessionCard{{ClientID: firstRegistration.String(), Connected: true}, {ClientID: secondRegistration.String(), Connected: true}}
	auth := &consentServerAuth{Status: consentAuthChoose, Options: []consentAuthClientOption{
		{ID: first, Type: authz.AuthClientTypeRemoteSession, State: consentClientReady, registrationID: uuid.NullUUID{UUID: firstRegistration, Valid: true}},
		{ID: second, Type: authz.AuthClientTypeRemoteSession, State: consentClientReady, Selected: true, registrationID: uuid.NullUUID{UUID: secondRegistration, Valid: true}},
		{ID: shared, State: consentClientReady, registrationID: uuid.NullUUID{UUID: firstRegistration, Valid: true}},
	}}
	formID, clients := consentReconnectTargets(auth, cards)
	require.Equal(t, "consent-reconnect-"+secondRegistration.String(), formID)
	require.Equal(t, []string{firstRegistration.String(), secondRegistration.String()}, clients)
	require.Equal(t, "consent-reconnect-"+firstRegistration.String(), auth.Options[0].ReconnectFormID)
	require.Empty(t, auth.Options[2].ReconnectFormID)

	legacyID, legacyClients := consentReconnectTargets(nil, cards)
	require.Empty(t, legacyID, "never guess which credential to use")
	require.Empty(t, legacyClients)

	cards[1].Connected = false
	cards[1].Expired = true
	require.True(t, consentPreviewGrantMissing(auth, second, cards))
	require.False(t, consentPreviewGrantMissing(auth, first, cards))
	require.False(t, consentPreviewGrantMissing(auth, shared, cards))
	cards[1].Unavailable = true
	require.False(t, consentPreviewGrantMissing(auth, second, cards), "an outage is not a missing grant")
}

func TestConsentToolsListJSONRPCFailureDiagnostics(t *testing.T) {
	t.Parallel()
	var log bytes.Buffer
	observer := &consentMethodAllowlistInterceptor{logger: slog.New(slog.NewJSONHandler(&log, nil))}
	request, err := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","id":"client-request-id","method":"tools/list"}`))
	require.NoError(t, err)
	require.NoError(t, observer.InterceptUserRequest(t.Context(), &proxy.UserRequest{JSONRPCMessages: []jsonrpc.Message{request}}))
	for _, id := range []string{"unexpected-id", "client-request-id", "client-request-id"} {
		response, err := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","id":"` + id + `","error":{"code":-32000,"message":"private-detail","data":{"secret":"payload"}}}`))
		require.NoError(t, err)
		require.NoError(t, observer.InterceptRemoteMessage(t.Context(), &proxy.RemoteMessage{
			Message: response, RemoteHTTPResponse: &http.Response{StatusCode: http.StatusOK},
		}))
		if id == "unexpected-id" {
			require.Empty(t, log.String())
		}
	}
	require.Equal(t, 1, strings.Count(log.String(), "tools_list_jsonrpc"))
	for _, private := range []string{"private-detail", "payload", "client-request-id"} {
		require.NotContains(t, log.String(), private)
	}
}
