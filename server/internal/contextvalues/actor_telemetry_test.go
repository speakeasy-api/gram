package contextvalues

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestActorTelemetryAttributes(t *testing.T) {
	t.Parallel()
	base := &AuthContext{ActiveOrganizationID: "org_test", APIKeyID: "key_test"}
	principal := urn.NewPrincipal(urn.PrincipalTypeAgent, "018f8d7b-58d7-7cc4-bb16-9f8c6b99a001")
	ctx := WithPrincipalAPIKeyAuthorization(t.Context(), base, principal, PrincipalCredential{AuthorizerUserID: "user_authorizer"})
	ctx = WithPrincipalCredentialOwner(ctx, "user_owner")
	attrs := ActorTelemetryAttributes(ctx)
	require.Equal(t, map[string]string{
		"gram.authorization.organization_id":    "org_test",
		"gram.authorization.actor.type":         "agent",
		"gram.authorization.actor.id":           principal.ID,
		"gram.authorization.api_key_id":         "key_test",
		"gram.authorization.authorizer_user_id": "user_authorizer",
		"gram.authorization.owner_user_id":      "user_owner",
	}, attrs)
	authCtx, _ := GetAuthContext(ctx)
	require.Empty(t, authCtx.UserID)
	require.Nil(t, authCtx.Email)

	legacy := *base
	legacy.UserID = "user_legacy"
	attrs = ActorTelemetryAttributes(WithLegacyAPIKeyAuthorization(t.Context(), &legacy))
	require.Equal(t, "user", attrs["gram.authorization.actor.type"])
	require.Equal(t, legacy.UserID, attrs["gram.authorization.actor.id"])
	require.Empty(t, attrs["gram.authorization.authorizer_user_id"])
	require.Empty(t, attrs["gram.authorization.owner_user_id"])

	// Public identity fields alone cannot claim a canonical actor.
	attrs = ActorTelemetryAttributes(SetAuthContext(t.Context(), &legacy))
	require.Empty(t, attrs["gram.authorization.actor.type"])
	require.Empty(t, attrs["gram.authorization.actor.id"])
	for _, value := range ActorTelemetryAttributes(t.Context()) {
		require.Empty(t, value)
	}
}
