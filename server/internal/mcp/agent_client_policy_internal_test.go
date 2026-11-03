package mcp

import (
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/stretchr/testify/require"
)

func TestAgentClientMintRejectsLifetimeOverrides(t *testing.T) {
	now := time.Now()
	duration := time.Hour
	for _, tc := range []struct {
		name   string
		mutate func(*mintSessionParams)
	}{
		{"authorization deadline", func(p *mintSessionParams) { p.AuthorizationExpiresAt = &now }},
		{"desired duration", func(p *mintSessionParams) { p.DesiredSessionDuration = &duration }},
		{"replayable", func(p *mintSessionParams) { p.Replayable = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := mintSessionParams{Policy: sessionIssuancePolicyAgentClient, Audience: "https://example.com/mcp/test", Subject: urn.NewAgentSubject(uuid.New())}
			tc.mutate(&params)
			_, err := (&Service{}).mintSession(t.Context(), &ResolvedMcpEndpoint{}, nil, nil, params, slog.Default())
			var shared *oops.ShareableError
			require.ErrorAs(t, err, &shared)
			require.Equal(t, oops.CodeUnexpected, shared.Code)
			require.Contains(t, shared.Error(), "invalid agent-client session issuance parameters")
		})
	}
}

func TestAgentClientRegistrationRejectsRuntimeCredential(t *testing.T) {
	target := AgentAuthorizationTarget{OrganizationID: "org_test", ProjectID: uuid.New()}
	ctx := contextvalues.WithPrincipalCredentialAuthorization(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: target.OrganizationID, ProjectID: &target.ProjectID}, urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()), contextvalues.PrincipalCredential{AuthorizerUserID: "user_test", DerivesPolicyAtAdmission: true})
	// A nil transaction proves rejection precedes any client persistence or policy read.
	_, err := RegisterAgentOAuthClient(ctx, nil, target, usersessions.RegistrationRequest{})
	var denied *oops.ShareableError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, oops.CodeForbidden, denied.Code)
}

func TestAgentClientCredentialsRejectsUnboundClient(t *testing.T) {
	response := httptest.NewRecorder()
	err := (&Service{}).handleAgentClientCredentialsGrant(t.Context(), response, httptest.NewRequest(http.MethodPost, "/token", nil), &ResolvedMcpEndpoint{}, &usersessionsrepo.UserSessionClient{}, "https://example.com", "private_key_jwt", slog.Default())
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), "unauthorized_client")
}
