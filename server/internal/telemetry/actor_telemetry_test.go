package telemetry

import (
	"context"
	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"log/slog"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestRecordAuthenticatedActorOverridesForgedAttributes(t *testing.T) {
	t.Parallel()
	base := &contextvalues.AuthContext{ActiveOrganizationID: "org_test", APIKeyID: "key_test"}
	actor := urn.NewPrincipal(urn.PrincipalTypeAgent, "018f8d7b-58d7-7cc4-bb16-9f8c6b99a001")
	ctx := contextvalues.WithPrincipalAPIKeyAuthorization(t.Context(), base, actor, contextvalues.PrincipalCredential{AuthorizerUserID: "user_authorizer"})
	ctx = contextvalues.WithPrincipalCredentialOwner(ctx, "user_owner")
	h := HTTPLogAttributes{attr.Key("test.property"): "test"}
	for key := range contextvalues.ActorTelemetryAttributes(ctx) {
		h[attr.Key(key)] = "forged"
	}
	h.RecordAuthenticatedActor(ctx)
	for key, value := range contextvalues.ActorTelemetryAttributes(ctx) {
		require.Equal(t, value, h[attr.Key(key)])
	}
	require.Equal(t, "test", h[attr.Key("test.property")])
	base.UserID = "user_legacy"
	h.RecordAuthenticatedActor(contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), base))
	require.Equal(t, "user_legacy", h[attr.Key("gram.authorization.actor.id")])
	require.NotContains(t, h, attr.Key("gram.authorization.owner_user_id"))
	require.NotContains(t, h, attr.Key("gram.authorization.authorizer_user_id"))
	h.RecordAuthenticatedActor(t.Context())
	for key := range contextvalues.ActorTelemetryAttributes(t.Context()) {
		require.NotContains(t, h, attr.Key(key))
	}
}

type actorCapture struct {
	properties map[string]any
	distinctID string
}

func (c *actorCapture) CaptureEvent(_ context.Context, _ string, distinctID string, properties map[string]any) error {
	c.properties = properties
	c.distinctID = distinctID
	return nil
}

func TestCaptureEventTrustedActor(t *testing.T) {
	t.Parallel()
	actor := urn.NewPrincipal(urn.PrincipalTypeAgent, "018f8d7b-58d7-7cc4-bb16-9f8c6b99a001")
	ctx := contextvalues.WithPrincipalAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org_test", APIKeyID: "key_test",
	}, actor, contextvalues.PrincipalCredential{AuthorizerUserID: "user_authorizer"})
	ctx = contextvalues.WithPrincipalCredentialOwner(ctx, "user_owner")
	capture := &actorCapture{}
	svc := &Service{posthog: capture, logger: slog.Default()}
	properties := map[string]any{"email": "forged", "user_id": "forged"}
	for key := range contextvalues.ActorTelemetryAttributes(ctx) {
		properties[key] = "forged"
	}
	_, err := svc.CaptureEvent(ctx, &telem_gen.CaptureEventPayload{Event: "test", Properties: properties})
	require.NoError(t, err)
	require.Equal(t, "org_test", capture.distinctID)
	require.Equal(t, "", capture.properties["user_id"])
	require.NotContains(t, capture.properties, "email")
	for key, value := range contextvalues.ActorTelemetryAttributes(ctx) {
		require.Equal(t, value, capture.properties[key])
	}
}
