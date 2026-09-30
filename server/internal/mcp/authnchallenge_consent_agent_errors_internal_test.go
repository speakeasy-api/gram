package mcp

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestConsentHumanDenialAndDependencyFailureRemainDistinct(t *testing.T) {
	t.Parallel()
	pool, err := pgxpool.New(t.Context(), "postgres://localhost/unused")
	require.NoError(t, err)
	pool.Close()
	service := &Service{db: pool, logger: testenv.NewLogger(t)}
	endpoint := &ResolvedMcpEndpoint{OrganizationID: "organization", ProjectID: uuid.New(), UserSessionIssuerID: uuid.New(), ToolsetID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	target, ok := agentAuthorizationTarget(endpoint)
	require.True(t, ok)
	subject := urn.NewUserSubject("human")
	state := AuthnChallengeState{Subject: &subject, AuthorizerUserID: subject.ID, AuthorizerImpersonated: new(bool), AgentAuthorizationTarget: target}
	_, err = service.loadConsentHuman(t.Context(), state, *target)
	require.Error(t, err)
	require.NotErrorIs(t, err, errConsentAgentDenied)
	require.Equal(t, oops.CodeUnavailable, consentAgentAuthorizationError(err, "consent authorizer is not eligible").Code)
	// The attachment route must preserve that classification, rather than
	// showing the unavailable-identity UI reserved for positive denials.
	err = service.serveConsentAgentConnections(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), endpoint, state)
	var failure *oops.ShareableError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, oops.CodeUnavailable, failure.Code)
	state.AuthorizerImpersonated = nil
	_, err = service.loadConsentHuman(t.Context(), state, *target)
	require.ErrorIs(t, err, errConsentAgentDenied)
	// Classification survives wrapping through authorizeConsentAgent callers.
	require.Equal(t, oops.CodeForbidden, consentAgentAuthorizationError(fmt.Errorf("authorize: %w", err), "selected agent is not eligible").Code)
}
