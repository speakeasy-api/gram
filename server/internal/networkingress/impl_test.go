package networkingress_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
)

func TestCreateIngressRequiresEntitlementAndRollout(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	payload := &gen.CreateIngressPayload{Provider: networkingress.ProviderTailscale, Hostname: "private-mcp", OauthClientID: "client", OauthClientSecret: "secret"}

	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureNetworkIngress)
	_, err := ti.service.CreateIngress(ctx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)

	productfeaturestest.Enable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureNetworkIngress)
	ti.flags.SetFlag(feature.FlagNetworkIngressRollout, ti.orgID, false)
	_, err = ti.service.CreateIngress(ctx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)

	ti.flags.SetFlag(feature.FlagNetworkIngressRollout, ti.orgID, true)
	result, err := ti.service.CreateIngress(ctx, payload)
	require.NoError(t, err)
	require.True(t, result.CredentialsConfigured)
}

func TestCreateIngressEncryptsCredentialsAndPinsResources(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	result := ti.create(t, ctx)
	row := loadRow(t, ctx, ti)

	require.NotContains(t, row.CredentialsEncrypted.String, "client-id")
	require.NotContains(t, row.CredentialsEncrypted.String, "client-secret")
	require.NotContains(t, strings.Join([]string{result.ID, result.Hostname, result.Provider}, " "), "client-secret")
	require.Equal(t, "platform", row.EndpointNamespaceKind)
	require.False(t, row.CustomDomainID.Valid)
	require.NotEqual(t, []byte("{}"), row.ProviderResources)
	require.Equal(t, row.AttestorNamespace+"-attestor", row.AttestorServiceAccount)
}

func TestDeleteRetainsCleanupIdentityAndBlocksReplacement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created := ti.create(t, ctx)
	require.NoError(t, ti.service.DeleteIngress(ctx, &gen.DeleteIngressPayload{}))

	deleted, err := repo.New(ti.conn).GetNetworkIngressByID(ctx, uuid.MustParse(created.ID))
	require.NoError(t, err)
	require.True(t, deleted.Deleted)
	require.True(t, deleted.CredentialsEncrypted.Valid)
	require.NotEqual(t, []byte("{}"), deleted.ProviderResources)

	_, err = ti.service.CreateIngress(ctx, &gen.CreateIngressPayload{Provider: networkingress.ProviderTailscale, Hostname: "replacement", OauthClientID: "next", OauthClientSecret: "next-secret"})
	requireOopsCode(t, err, oops.CodeConflict)

	// Repeated delete resumes cleanup rather than returning not-found.
	require.NoError(t, ti.service.DeleteIngress(ctx, &gen.DeleteIngressPayload{}))
}

func TestDisableAndDeleteRemainAvailableAfterGateRemoval(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ti.create(t, ctx)
	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureNetworkIngress)
	ti.flags.SetFlag(feature.FlagNetworkIngressRollout, ti.orgID, false)

	disabled := false
	result, err := ti.service.UpdateIngress(ctx, &gen.UpdateIngressPayload{Enabled: &disabled})
	require.NoError(t, err)
	require.False(t, result.Enabled)

	_, err = ti.service.RotateCredentials(ctx, &gen.RotateCredentialsPayload{OauthClientID: "blocked", OauthClientSecret: "blocked"})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.NoError(t, ti.service.DeleteIngress(ctx, &gen.DeleteIngressPayload{}))
}

func TestDeleteImpactCountsModes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ti.create(t, ctx)
	// The zero case pins the API shape and query against an organization with no servers.
	impact, err := ti.service.GetDeleteImpact(ctx, &gen.GetDeleteImpactPayload{})
	require.NoError(t, err)
	require.Zero(t, impact.McpServersDual)
	require.Zero(t, impact.McpServersPrivateOnly)
	require.Zero(t, impact.MetaMcpServersDual)
	require.Zero(t, impact.MetaMcpServersPrivateOnly)
}

func TestCreateIngressRejectsInvalidHostnameAndProvider(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	_, err := ti.service.CreateIngress(ctx, &gen.CreateIngressPayload{Provider: "other", Hostname: "private", OauthClientID: "id", OauthClientSecret: "secret"})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = ti.service.CreateIngress(ctx, &gen.CreateIngressPayload{Provider: networkingress.ProviderTailscale, Hostname: "Not Valid", OauthClientID: "id", OauthClientSecret: "secret"})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// compile-time coverage for generated nullable custom-domain shape.
var _ = pgtype.Text{}
var _ context.Context
