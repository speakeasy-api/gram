package mcp

import (
	"context"
	"errors"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

type assistantWorkloadIssuerFunc func(context.Context, AssistantWorkloadTokenRequest) (AssistantWorkloadToken, error)

func (f assistantWorkloadIssuerFunc) IssueAssistantWorkloadToken(ctx context.Context, req AssistantWorkloadTokenRequest) (AssistantWorkloadToken, error) {
	return f(ctx, req)
}

func TestAssistantClientCredentialsDispatch(t *testing.T) {
	t.Parallel()

	s, _, _ := newTokenDispatchTestService(t)
	r := newTokenDispatchRequest(t, url.Values{})
	_, ok := s.tokenGrantFor(r, "client_credentials", presentedClientCredentials{})
	require.True(t, ok)
	s.assistantWorkloadTokens = assistantWorkloadIssuerFunc(func(context.Context, AssistantWorkloadTokenRequest) (AssistantWorkloadToken, error) {
		return AssistantWorkloadToken{}, errors.New("denied")
	})
	g, ok := s.tokenGrantFor(r, "client_credentials", presentedClientCredentials{})
	require.True(t, ok)
	require.Equal(t, tokenClientAuthRequired, g.clientAuth)
	require.Equal(t, lookupClientOnly, g.resolveMode)
	require.Nil(t, g.clientless)
}

func TestAssistantClientCredentialsBoundRequest(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, resource, scope       string
		public, denied, unavailable bool
		status                      int
	}{
		{name: "success", resource: "https://gram.example/mcp/test", scope: "mcp:connect", status: 200},
		{name: "issuance unavailable", resource: "https://gram.example/mcp/test", scope: "mcp:connect", unavailable: true, status: 503},
		{name: "public client", resource: "https://gram.example/mcp/test", scope: "mcp:connect", public: true, status: 400},
		{name: "wrong resource", resource: "https://other.example/mcp/test", scope: "mcp:connect", status: 400},
		{name: "missing resource", scope: "mcp:connect", status: 400},
		{name: "unsupported scope", resource: "https://gram.example/mcp/test", scope: "mcp:connect tools:read", status: 400},
		{name: "missing scope", resource: "https://gram.example/mcp/test", status: 400},
		{name: "revoked or policy denied", resource: "https://gram.example/mcp/test", scope: "mcp:connect", denied: true, status: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s, endpoint, _ := newTokenDispatchTestService(t)
			calls := 0
			client := &usersessions_repo.UserSessionClient{ID: uuid.New(), ClientID: "test-client", UserSessionIssuerID: endpoint.UserSessionIssuerID, ClientSecretHash: pgtype.Text{String: "hashed", Valid: !tc.public}}
			s.assistantWorkloadTokens = assistantWorkloadIssuerFunc(func(_ context.Context, req AssistantWorkloadTokenRequest) (AssistantWorkloadToken, error) {
				calls++
				require.Equal(t, client.ID, req.ClientRegistrationID)
				require.Equal(t, endpoint.OrganizationID, req.OrganizationID)
				require.Equal(t, endpoint.ProjectID, req.ProjectID)
				require.Equal(t, tc.resource, req.Resource)
				if tc.unavailable {
					return AssistantWorkloadToken{}, errors.New("database unavailable")
				}
				if tc.denied {
					return AssistantWorkloadToken{}, refuseWorkloadGrant("policy", errors.New("denied"))
				}
				return AssistantWorkloadToken{AssistantID: uuid.New(), AccessToken: "opaque-token", ExpiresAt: time.Now().Add(time.Minute)}, nil
			})
			r := newTokenDispatchRequest(t, url.Values{"resource": {tc.resource}, "scope": {tc.scope}})
			w := httptest.NewRecorder()
			require.NoError(t, s.handleAssistantClientCredentialsGrant(r.Context(), w, r, endpoint, client, "https://gram.example", "client_secret_post", s.logger))
			require.Equal(t, tc.status, w.Code)
			if tc.status == 200 {
				require.Equal(t, 1, calls)
				require.Contains(t, w.Body.String(), `"scope":"mcp:connect"`)
				require.NotContains(t, w.Body.String(), "refresh_token")
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			} else if tc.unavailable {
				require.Equal(t, 1, calls)
				require.Contains(t, w.Body.String(), `"error":"temporarily_unavailable"`)
			} else if tc.denied {
				require.Contains(t, w.Body.String(), `"error":"unauthorized_client"`)
			} else {
				require.Zero(t, calls)
			}
		})
	}
}

func TestAssistantWorkloadScopeSyntax(t *testing.T) {
	t.Parallel()

	for _, scope := range []string{"", " a", "a ", "a  b", "a a", "a\tb", "a\nb", "a\\b", "a\"b", "é"} {
		require.False(t, validAssistantWorkloadScopes(scope), scope)
	}
	require.True(t, validAssistantWorkloadScopes("mcp:connect tools:read"))
}
