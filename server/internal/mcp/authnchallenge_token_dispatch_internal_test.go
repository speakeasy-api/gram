package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/privatekeyjwt"
	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// newTokenDispatchTestService returns a Service with no database, so any test
// that reaches client resolution fails loudly, plus the buffer its logs go to.
func newTokenDispatchTestService(t *testing.T) (*Service, *ResolvedMcpEndpoint, *bytes.Buffer) {
	t.Helper()

	serverURL, err := url.Parse("https://gram.example")
	require.NoError(t, err)
	logs := &bytes.Buffer{}
	service := new(Service)
	service.logger = slog.New(slog.NewJSONHandler(logs, nil))
	service.serverURL = serverURL
	endpoint := &ResolvedMcpEndpoint{
		OrganizationID:      "org_test",
		ProjectID:           uuid.New(),
		RouteBase:           "mcp",
		Slug:                "test",
		UserSessionIssuerID: uuid.New(),
	}
	return service, endpoint, logs
}

func newTokenDispatchRequest(t *testing.T, form url.Values) *http.Request {
	t.Helper()

	r := httptest.NewRequest(http.MethodPost, "/mcp/test/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	require.NoError(t, r.ParseForm())
	return r
}

func tokenDispatchLogEvents(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()

	var events []map[string]any
	scanner := bufio.NewScanner(logs)
	for scanner.Scan() {
		var event map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &event))
		events = append(events, event)
	}
	require.NoError(t, scanner.Err())
	return events
}

// Every grant this endpoint dispatches states its client-authentication
// requirement and supplies the matching handler.
func TestTokenGrantForDeclaresClientAuthentication(t *testing.T) {
	t.Parallel()

	service, _, _ := newTokenDispatchTestService(t)
	clientAssertion := privatekeyjwt.Assertion{Type: privatekeyjwt.AssertionType, Value: "header.payload.signature"}
	cases := []struct {
		name        string
		grantType   string
		creds       presentedClientCredentials
		basicHeader bool
		want        tokenClientAuth
		resolveMode clientIDResolveMode
	}{
		{name: "authorization_code without client", grantType: oauthwire.GrantTypeAuthorizationCode, want: tokenClientAuthRequired, resolveMode: lookupClientOnly},
		{name: "authorization_code with client", grantType: oauthwire.GrantTypeAuthorizationCode, creds: presentedClientCredentials{clientID: "c"}, want: tokenClientAuthRequired, resolveMode: lookupClientOnly},
		{name: "refresh_token without client", grantType: oauthwire.GrantTypeRefreshToken, want: tokenClientAuthRequired, resolveMode: lookupClientOnly},
		{name: "refresh_token with client", grantType: oauthwire.GrantTypeRefreshToken, creds: presentedClientCredentials{clientID: "c"}, want: tokenClientAuthRequired, resolveMode: lookupClientOnly},
		{name: "jwt-bearer with client_id", grantType: oauthwire.GrantTypeJWTBearer, creds: presentedClientCredentials{clientID: "c"}, want: tokenClientAuthRequired, resolveMode: resolveClientCIMD},
		{name: "jwt-bearer with secret only", grantType: oauthwire.GrantTypeJWTBearer, creds: presentedClientCredentials{secret: "s"}, want: tokenClientAuthRequired, resolveMode: resolveClientCIMD},
		{name: "jwt-bearer with client assertion only", grantType: oauthwire.GrantTypeJWTBearer, creds: presentedClientCredentials{assertion: clientAssertion}, want: tokenClientAuthRequired, resolveMode: resolveClientCIMD},
		{name: "jwt-bearer with Authorization header only", grantType: oauthwire.GrantTypeJWTBearer, basicHeader: true, want: tokenClientAuthRequired, resolveMode: resolveClientCIMD},
		{name: "jwt-bearer without client", grantType: oauthwire.GrantTypeJWTBearer, want: tokenClientAuthNone, resolveMode: ""},
	}
	for _, tc := range cases {
		r := newTokenDispatchRequest(t, url.Values{"grant_type": {tc.grantType}})
		if tc.basicHeader {
			r.SetBasicAuth("", "secret")
		}
		grant, ok := service.tokenGrantFor(r, tc.grantType, tc.creds)
		require.True(t, ok, tc.name)
		require.Equal(t, tc.want, grant.clientAuth, tc.name)
		require.Equal(t, tc.resolveMode, grant.resolveMode, tc.name)
		switch tc.want {
		case tokenClientAuthRequired:
			require.NotNil(t, grant.authenticated, tc.name)
			require.Nil(t, grant.clientless, tc.name)
		case tokenClientAuthNone:
			require.Nil(t, grant.authenticated, tc.name)
			require.NotNil(t, grant.clientless, tc.name)
		case tokenClientAuthUndeclared:
			require.Fail(t, "test case expects an undeclared requirement", tc.name)
		}
	}

	for _, grantType := range []string{"", "client_credentials", "password", "urn:example:grant-type:unknown"} {
		_, ok := service.tokenGrantFor(newTokenDispatchRequest(t, url.Values{}), grantType, presentedClientCredentials{})
		require.False(t, ok, grantType)
	}
}

// A client authentication parameter sent with an empty value is still an
// attempt at client authentication. The request takes the authenticated branch
// and fails client authentication there, rather than being handled as a caller
// that presented no client.
func TestTokenGrantForEmptyClientAuthParameterRequiresClientAuth(t *testing.T) {
	t.Parallel()

	service, _, _ := newTokenDispatchTestService(t)
	for _, key := range clientAuthFormParameters {
		r := newTokenDispatchRequest(t, url.Values{"grant_type": {oauthwire.GrantTypeJWTBearer}, key: {""}})
		grant, ok := service.tokenGrantFor(r, oauthwire.GrantTypeJWTBearer, extractClientCredentials(r))
		require.True(t, ok, key)
		require.Equal(t, tokenClientAuthRequired, grant.clientAuth, "an empty %s must not be treated as clientless", key)
	}

	r := newTokenDispatchRequest(t, url.Values{"grant_type": {oauthwire.GrantTypeJWTBearer}})
	grant, ok := service.tokenGrantFor(r, oauthwire.GrantTypeJWTBearer, extractClientCredentials(r))
	require.True(t, ok)
	require.Equal(t, tokenClientAuthNone, grant.clientAuth, "a request naming no client authentication parameter stays clientless")
}

// A grant that omits its client-authentication requirement, or pairs it with
// the wrong handler, is refused without running any handler.
func TestServeTokenGrantFailsClosedWithoutDeclaredClientAuthentication(t *testing.T) {
	t.Parallel()

	service, endpoint, _ := newTokenDispatchTestService(t)
	var called []string
	authenticated := func(context.Context, http.ResponseWriter, *http.Request, *ResolvedMcpEndpoint, *usersessions_repo.UserSessionClient, string, string, *slog.Logger) error {
		called = append(called, "authenticated")
		return nil
	}
	clientless := func(context.Context, http.ResponseWriter, *http.Request, presentedClientCredentials, *slog.Logger) error {
		called = append(called, "clientless")
		return nil
	}

	grants := map[string]tokenGrant{
		"undeclared with both handlers": {authenticated: authenticated, clientless: clientless},
		"zero value with no handlers":   {},
		"undeclared with no handlers":   {clientAuth: tokenClientAuthUndeclared},
		"required without handler":      {clientAuth: tokenClientAuthRequired, clientless: clientless},
		"none without handler":          {clientAuth: tokenClientAuthNone, authenticated: authenticated},
		"unknown requirement":           {clientAuth: tokenClientAuth("unknown"), authenticated: authenticated, clientless: clientless},
	}
	for name, grant := range grants {
		w := httptest.NewRecorder()
		r := newTokenDispatchRequest(t, url.Values{"grant_type": {"urn:example:grant-type:new"}})
		err := service.serveTokenGrant(t.Context(), w, r, endpoint, service.logger, "urn:example:grant-type:new", presentedClientCredentials{}, grant)
		require.ErrorContains(t, err, "dispatch token grant", name)
		require.Zero(t, w.Body.Len(), name)
	}
	require.Empty(t, called)
}

// A clientless grant runs its handler without resolving a client; the test
// service has no database, so any resolution attempt would panic.
func TestServeTokenGrantClientlessSkipsClientResolution(t *testing.T) {
	t.Parallel()

	service, endpoint, _ := newTokenDispatchTestService(t)
	grant := tokenGrant{
		clientAuth: tokenClientAuthNone,
		clientless: func(_ context.Context, w http.ResponseWriter, _ *http.Request, _ presentedClientCredentials, _ *slog.Logger) error {
			w.WriteHeader(http.StatusNoContent)
			return nil
		},
	}
	w := httptest.NewRecorder()
	r := newTokenDispatchRequest(t, url.Values{"grant_type": {oauthwire.GrantTypeJWTBearer}})
	require.NoError(t, service.serveTokenGrant(t.Context(), w, r, endpoint, service.logger, oauthwire.GrantTypeJWTBearer, presentedClientCredentials{}, grant))
	require.Equal(t, http.StatusNoContent, w.Code)
}

// Requests refused before any client authentication keep the credential
// event vocabulary: an unsupported grant logs unsupported_grant_type, and a
// clientless JWT bearer request logs missing_client_id.
func TestServeTokenRefusalsBeforeClientAuthenticationLogReasons(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		form      url.Values
		status    int
		body      string
		message   string
		reason    string
		grantType string
	}{
		{
			name:      "unsupported grant without client",
			form:      url.Values{"grant_type": {"urn:example:grant-type:unknown"}},
			status:    http.StatusBadRequest,
			body:      `{"error":"unsupported_grant_type","error_description":"unsupported grant_type"}`,
			message:   "oauth token request rejected",
			reason:    "unsupported_grant_type",
			grantType: "urn:example:grant-type:unknown",
		},
		{
			name:      "jwt-bearer without client",
			form:      url.Values{"grant_type": {oauthwire.GrantTypeJWTBearer}, "assertion": {"header.payload.signature"}},
			status:    http.StatusUnauthorized,
			body:      `{"error":"invalid_client","error_description":"client_id is required"}`,
			message:   "oauth token client authentication rejected",
			reason:    "missing_client_id",
			grantType: oauthwire.GrantTypeJWTBearer,
		},
	}
	for _, tc := range cases {
		service, endpoint, logs := newTokenDispatchTestService(t)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/mcp/test/token", strings.NewReader(tc.form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		require.NoError(t, service.ServeToken(w, r, endpoint), tc.name)
		require.Equal(t, tc.status, w.Code, tc.name)
		require.JSONEq(t, tc.body, w.Body.String(), tc.name)

		var credentialEvents []map[string]any
		for _, event := range tokenDispatchLogEvents(t, logs) {
			if _, ok := event[string(attr.OAuthGrantKey)]; ok {
				credentialEvents = append(credentialEvents, event)
			}
		}
		require.Len(t, credentialEvents, 1, tc.name)
		require.Equal(t, tc.message, credentialEvents[0]["msg"], tc.name)
		require.Equal(t, tc.reason, credentialEvents[0][string(attr.OAuthFailureReasonKey)], tc.name)
		require.Equal(t, tc.grantType, credentialEvents[0][string(attr.OAuthGrantKey)], tc.name)
		require.Equal(t, oauthwire.AuthMethodNone, credentialEvents[0][string(attr.OAuthPresentedAuthMethodKey)], tc.name)
		require.NotContains(t, credentialEvents[0], string(attr.OAuthClientIDKey), tc.name)
	}
}
