package networkingress_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestRotateCredentialsToOAuthClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	createAuthKeyIngress(t, ctx, ti)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionNetworkIngressRotateCredentials)
	require.NoError(t, err)

	rotated, err := ti.service.RotateCredentials(adminCtx(t, ctx), &gen.RotateCredentialsPayload{
		SessionToken:      nil,
		AuthKey:           nil,
		OauthClientID:     new("k456CNTRL"),
		OauthClientSecret: new("tskey-client-rotated"),
	})
	require.NoError(t, err)
	require.Equal(t, "oauth_client", rotated.CredentialKind)
	require.False(t, rotated.AuthKeyConfigured)
	require.True(t, rotated.OauthClientConfigured)
	// Rotation resets learned node health so the gateway re-authenticates.
	require.Equal(t, "pending", rotated.Status)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionNetworkIngressRotateCredentials)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestRotateCredentialsRejectsAmbiguousInput(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	createAuthKeyIngress(t, ctx, ti)

	_, err := ti.service.RotateCredentials(adminCtx(t, ctx), &gen.RotateCredentialsPayload{
		SessionToken:      nil,
		AuthKey:           new("tskey-auth-test"),
		OauthClientID:     new("k456CNTRL"),
		OauthClientSecret: new("secret"),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestRotateCredentialsNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.RotateCredentials(adminCtx(t, ctx), &gen.RotateCredentialsPayload{
		SessionToken:      nil,
		AuthKey:           new("tskey-auth-test"),
		OauthClientID:     nil,
		OauthClientSecret: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
