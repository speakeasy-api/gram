package networkingress_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
)

func TestCreateIngressWithAuthKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionNetworkIngressCreate)
	require.NoError(t, err)

	ingress := createAuthKeyIngress(t, ctx, ti)

	require.Equal(t, ti.orgID, ingress.OrganizationID)
	require.Equal(t, "tailscale", ingress.Provider)
	require.Equal(t, "gram-mcp", ingress.Hostname)
	require.Equal(t, []string{"tag:gram"}, ingress.Tags)
	require.True(t, ingress.Enabled)
	require.False(t, ingress.PrivateNetworkOnly)
	require.True(t, ingress.IdentityRequired)
	require.Equal(t, "auth_key", ingress.CredentialKind)
	require.True(t, ingress.AuthKeyConfigured)
	require.False(t, ingress.OauthClientConfigured)
	require.Equal(t, "pending", ingress.Status)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionNetworkIngressCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestCreateIngressWithOAuthClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ingress, err := ti.service.CreateIngress(adminCtx(t, ctx), &gen.CreateIngressPayload{
		SessionToken:       nil,
		Provider:           new("tailscale"),
		Hostname:           new("mcp-internal"),
		Tags:               []string{"tag:gram", "tag:prod"},
		AuthKey:            nil,
		OauthClientID:      new("k123CNTRL"),
		OauthClientSecret:  new("tskey-client-secret"),
		PrivateNetworkOnly: new(true),
		IdentityRequired:   new(false),
	})
	require.NoError(t, err)

	require.Equal(t, "mcp-internal", ingress.Hostname)
	require.Equal(t, []string{"tag:gram", "tag:prod"}, ingress.Tags)
	require.True(t, ingress.PrivateNetworkOnly)
	require.False(t, ingress.IdentityRequired)
	require.Equal(t, "oauth_client", ingress.CredentialKind)
	require.False(t, ingress.AuthKeyConfigured)
	require.True(t, ingress.OauthClientConfigured)
}

func TestCreateIngressRequiresExactlyOneCredential(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// No credential at all.
	_, err := ti.service.CreateIngress(adminCtx(t, ctx), &gen.CreateIngressPayload{
		SessionToken:       nil,
		Provider:           nil,
		Hostname:           nil,
		Tags:               nil,
		AuthKey:            nil,
		OauthClientID:      nil,
		OauthClientSecret:  nil,
		PrivateNetworkOnly: nil,
		IdentityRequired:   nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	// Both modes at once.
	_, err = ti.service.CreateIngress(adminCtx(t, ctx), &gen.CreateIngressPayload{
		SessionToken:       nil,
		Provider:           nil,
		Hostname:           nil,
		Tags:               nil,
		AuthKey:            new("tskey-auth-test"),
		OauthClientID:      new("k123CNTRL"),
		OauthClientSecret:  new("secret"),
		PrivateNetworkOnly: nil,
		IdentityRequired:   nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	// OAuth client id without its secret.
	_, err = ti.service.CreateIngress(adminCtx(t, ctx), &gen.CreateIngressPayload{
		SessionToken:       nil,
		Provider:           nil,
		Hostname:           nil,
		Tags:               nil,
		AuthKey:            nil,
		OauthClientID:      new("k123CNTRL"),
		OauthClientSecret:  nil,
		PrivateNetworkOnly: nil,
		IdentityRequired:   nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateIngressRejectsSecondIngress(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	createAuthKeyIngress(t, ctx, ti)

	_, err := ti.service.CreateIngress(adminCtx(t, ctx), &gen.CreateIngressPayload{
		SessionToken:       nil,
		Provider:           nil,
		Hostname:           nil,
		Tags:               nil,
		AuthKey:            new("tskey-auth-other"),
		OauthClientID:      nil,
		OauthClientSecret:  nil,
		PrivateNetworkOnly: nil,
		IdentityRequired:   nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestCreateIngressRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateIngress(adminCtx(t, ctx), &gen.CreateIngressPayload{
		SessionToken:       nil,
		Provider:           new("wireguard"),
		Hostname:           nil,
		Tags:               nil,
		AuthKey:            new("tskey-auth-test"),
		OauthClientID:      nil,
		OauthClientSecret:  nil,
		PrivateNetworkOnly: nil,
		IdentityRequired:   nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.CreateIngress(adminCtx(t, ctx), &gen.CreateIngressPayload{
		SessionToken:       nil,
		Provider:           nil,
		Hostname:           new("Not_A_Label!"),
		Tags:               nil,
		AuthKey:            new("tskey-auth-test"),
		OauthClientID:      nil,
		OauthClientSecret:  nil,
		PrivateNetworkOnly: nil,
		IdentityRequired:   nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.CreateIngress(adminCtx(t, ctx), &gen.CreateIngressPayload{
		SessionToken:       nil,
		Provider:           nil,
		Hostname:           nil,
		Tags:               []string{"not-a-tag"},
		AuthKey:            new("tskey-auth-test"),
		OauthClientID:      nil,
		OauthClientSecret:  nil,
		PrivateNetworkOnly: nil,
		IdentityRequired:   nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateIngressRequiresFeature(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureNetworkIngress)

	_, err := ti.service.CreateIngress(adminCtx(t, ctx), &gen.CreateIngressPayload{
		SessionToken:       nil,
		Provider:           nil,
		Hostname:           nil,
		Tags:               nil,
		AuthKey:            new("tskey-auth-test"),
		OauthClientID:      nil,
		OauthClientSecret:  nil,
		PrivateNetworkOnly: nil,
		IdentityRequired:   nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateIngressRequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateIngress(readCtx(t, ctx), &gen.CreateIngressPayload{
		SessionToken:       nil,
		Provider:           nil,
		Hostname:           nil,
		Tags:               nil,
		AuthKey:            new("tskey-auth-test"),
		OauthClientID:      nil,
		OauthClientSecret:  nil,
		PrivateNetworkOnly: nil,
		IdentityRequired:   nil,
	})
	require.Error(t, err)
}
