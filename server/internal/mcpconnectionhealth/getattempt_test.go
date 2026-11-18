package mcpconnectionhealth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_connection_health"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestGetAttempt_ReturnsTheAttemptWithEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	subject := urn.NewUserSubject(ti.userID)
	id := ti.insert(t, ti.org, ti.project, ti.now.Add(-3*time.Hour), mcpconnectionattempts.StepProviderRefresh, mcpconnectionattempts.CodeProviderUnavailable,
		mcpconnectionattempts.Subject(&subject), mcpconnectionattempts.UserSessionIssuer(ti.issuer))

	res, err := ti.service.GetAttempt(ctx, &gen.GetAttemptPayload{Reference: id.String()})
	require.NoError(t, err)
	require.True(t, *res.Found)
	require.Equal(t, gen.ConnectionStepGroup("service_renewal"), res.Attempt.Step)
	require.Equal(t, ti.issuer.String(), *res.Attempt.UserSessionIssuerID)
	require.NotNil(t, res.Attempt.Person.Email)
	require.Len(t, res.Attempt.Steps, 1)
	require.True(t, res.Attempt.Steps[0].Final)
}

func TestGetAttempt_NoMatchForAnotherOrganizationOrAnUnreadableReference(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	elsewhere := ti.insert(t, "org-"+uuid.NewString(), ti.project, ti.now.Add(-time.Hour), mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken)

	for _, reference := range []string{elsewhere.String(), "not-a-reference"} {
		res, err := ti.service.GetAttempt(ctx, &gen.GetAttemptPayload{Reference: reference})
		require.NoError(t, err)
		require.True(t, res.LoggingEnabled)
		require.False(t, *res.Found)
		require.Nil(t, res.Attempt)
	}
}
