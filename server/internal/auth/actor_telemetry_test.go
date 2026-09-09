package auth

import (
	"log/slog"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/wide"
	"github.com/stretchr/testify/require"
)

func TestLogAuthContextTrustedActor(t *testing.T) {
	t.Parallel()
	base := &contextvalues.AuthContext{ActiveOrganizationID: "org_test", APIKeyID: "key_test"}
	actor := urn.NewPrincipal(urn.PrincipalTypeAgent, "018f8d7b-58d7-7cc4-bb16-9f8c6b99a001")
	ctx := contextvalues.WithPrincipalAPIKeyAuthorization(t.Context(), base, actor, contextvalues.PrincipalCredential{AuthorizerUserID: "user_authorizer"})
	ctx = contextvalues.WithPrincipalCredentialOwner(ctx, "user_owner")
	ctx = wide.Start(ctx, slog.String("gram.authorization.actor.id", "forged"))
	svc := &Auth{}
	svc.logAuthContext(ctx, nil, constants.KeySecurityScheme)
	attrs := map[string]string{}
	for _, a := range wide.Emit(ctx) {
		attrs[a.Key] = a.Value.String()
	}
	for key, value := range contextvalues.ActorTelemetryAttributes(ctx) {
		require.Equal(t, value, attrs[key])
	}
	require.Empty(t, base.UserID)
	require.Nil(t, base.Email)
}
