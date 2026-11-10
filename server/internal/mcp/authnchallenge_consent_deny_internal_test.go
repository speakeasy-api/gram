package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// retireRecorder records the consent credentials a denial retires.
type retireRecorder struct {
	funcGovernor
	retired []string
}

func (r *retireRecorder) RetireConsentCredentials(_ context.Context, organizationID string, userSessionIssuerID uuid.UUID, userID string, signInID uuid.UUID) error {
	r.retired = append(r.retired, organizationID+"|"+userSessionIssuerID.String()+"|"+userID+"|"+signInID.String())
	return nil
}

func TestRetireDeniedConsentCredentials(t *testing.T) {
	t.Parallel()
	endpoint := &ResolvedMcpEndpoint{OrganizationID: chainingTestOrg, ProjectID: uuid.New(), UserSessionIssuerID: uuid.New()}
	subject := urn.NewUserSubject("user-1")
	signInID := uuid.New()
	r := &retireRecorder{}
	s := &Service{identityChainer: r, logger: testenv.NewLogger(t)}

	s.retireDeniedConsentCredentials(t.Context(), s.logger, endpoint, AuthnChallengeState{Subject: &subject, SignInID: nil})
	require.Empty(t, r.retired, "a flow without a recorded sign-in minted nothing")

	s.retireDeniedConsentCredentials(t.Context(), s.logger, endpoint, AuthnChallengeState{Subject: &subject, SignInID: &signInID})
	require.Equal(t, []string{chainingTestOrg + "|" + endpoint.UserSessionIssuerID.String() + "|user-1|" + signInID.String()}, r.retired)
}
