package auth

import (
	"context"
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
	ctx = wide.Start(ctx)
	assertActorWideAttributes(t, ctx)
	require.Empty(t, base.UserID)
	require.Nil(t, base.Email)
}

func TestLogAuthContextLegacyActor(t *testing.T) {
	t.Parallel()
	for _, userID := range []string{"user_legacy", ""} {
		t.Run("user="+userID, func(t *testing.T) {
			t.Parallel()
			ctx := contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{
				ActiveOrganizationID: "org_test", APIKeyID: "key_test", UserID: userID,
			})
			assertActorWideAttributes(t, wide.Start(ctx))
		})
	}
}

func assertActorWideAttributes(t *testing.T, ctx context.Context) {
	t.Helper()
	svc := &Auth{}
	for range 2 {
		svc.logAuthContext(ctx, nil, constants.KeySecurityScheme)
		// Inspect the emitted slice directly: collapsing into a map would hide
		// duplicate keys in this append-only collector.
		emitted := wide.Emit(ctx)
		for key, value := range contextvalues.ActorTelemetryAttributes(ctx) {
			count := 0
			for _, a := range emitted {
				if a.Key == key {
					count++
					require.Equal(t, value, a.Value.String(), key)
				}
			}
			if value == "" {
				require.Zero(t, count, key)
			} else {
				require.Equal(t, 1, count, key)
			}
		}
	}
}
