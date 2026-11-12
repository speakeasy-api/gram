package networkingress_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestCreateIngressRequiresEntitlement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	payload := &gen.CreateIngressPayload{Provider: networkingress.ProviderTailscale, Hostname: new("private-mcp"), OauthClientID: "client", OauthClientSecret: "secret"}

	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureNetworkIngress)
	_, err := ti.service.CreateIngress(ctx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)

	productfeaturestest.Enable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureNetworkIngress)
	result, err := ti.service.CreateIngress(ctx, payload)
	require.NoError(t, err)
	require.True(t, result.CredentialsConfigured)
}

func TestGetIngressReturnsEmptyResultWhenNotConfigured(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	result, err := ti.service.GetIngress(ctx, &gen.GetIngressPayload{})
	require.NoError(t, err)
	require.Nil(t, result.Ingress)
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

	deleted, err := repo.New(ti.conn).GetNetworkIngressByID(ctx, repo.GetNetworkIngressByIDParams{ID: uuid.MustParse(created.ID), OrganizationID: ti.orgID})
	require.NoError(t, err)
	require.True(t, deleted.Deleted)
	require.True(t, deleted.CredentialsEncrypted.Valid)
	require.NotEqual(t, []byte("{}"), deleted.ProviderResources)

	visible, err := ti.service.GetIngress(ctx, &gen.GetIngressPayload{})
	require.NoError(t, err)
	require.NotNil(t, visible.Ingress)
	require.Equal(t, created.ID, visible.Ingress.ID)
	require.Equal(t, "deleting", visible.Ingress.Status)
	require.False(t, visible.Ingress.Enabled)

	_, err = ti.service.CreateIngress(ctx, &gen.CreateIngressPayload{Provider: networkingress.ProviderTailscale, Hostname: new("replacement"), OauthClientID: "next", OauthClientSecret: "next-secret"})
	requireOopsCode(t, err, oops.CodeConflict)

	// Repeated delete resumes cleanup rather than returning not-found.
	require.NoError(t, ti.service.DeleteIngress(ctx, &gen.DeleteIngressPayload{}))
}

func TestNetworkIngressManagementReadsRequireOrgAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ti.create(t, ctx)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource))

	_, err := ti.service.GetIngress(ctx, &gen.GetIngressPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.GetDeleteImpact(ctx, &gen.GetDeleteImpactPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.CheckHealth(ctx, &gen.CheckHealthPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestNetworkModeAdmissionLockSerializesIngressDisable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ti.create(t, ctx)
	admission := networkingress.NewExpansionAdmission(ti.features, true, true)
	finalizeNetworkAccess, err := admission.PrepareNetworkAccess(ctx, networkaccess.EligibilityInput{OrganizationID: ti.orgID, Mode: networkaccess.ModeDual})
	require.NoError(t, err)

	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, finalizeNetworkAccess.Finalize(ctx, tx))
	disabled := false
	type outcome struct {
		result *gen.NetworkIngress
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := ti.service.UpdateIngress(ctx, &gen.UpdateIngressPayload{Enabled: &disabled})
		done <- outcome{result: result, err: err}
	}()
	testenv.WaitForBackendsBlockedBy(t, ctx, ti.conn, testenv.BackendPID(tx), 1)
	require.NoError(t, tx.Commit(ctx))
	completed := <-done
	require.NoError(t, completed.err)
	updated := completed.result
	require.False(t, updated.Enabled)
}

func TestNetworkIngressIDLookupRequiresOwningOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created := ti.create(t, ctx)

	_, err := repo.New(ti.conn).GetNetworkIngressByID(ctx, repo.GetNetworkIngressByIDParams{ID: uuid.MustParse(created.ID), OrganizationID: "org_other"})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestIdentityRequiredUpdateMarksIngressPendingAndClearsError(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ti.create(t, ctx)
	err := testrepo.New(ti.conn).SetNetworkIngressObservationFixture(ctx, testrepo.SetNetworkIngressObservationFixtureParams{
		Status:         "error",
		LastError:      pgtype.Text{String: "old_error", Valid: true},
		OrganizationID: ti.orgID,
	})
	require.NoError(t, err)

	identityRequired := true
	result, err := ti.service.UpdateIngress(ctx, &gen.UpdateIngressPayload{IdentityRequired: &identityRequired})
	require.NoError(t, err)
	require.Equal(t, "pending", result.Status)
	require.Nil(t, result.LastError)
}

func TestDisableAndDeleteRemainAvailableAfterGateRemoval(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created := ti.create(t, ctx)
	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureNetworkIngress)

	// Existing desired state and deletion impact remain visible so the dashboard
	// can explain what is still enforced and offer safe recovery.
	current, err := ti.service.GetIngress(ctx, &gen.GetIngressPayload{})
	require.NoError(t, err)
	require.NotNil(t, current.Ingress)
	require.Equal(t, created.ID, current.Ingress.ID)
	_, err = ti.service.GetDeleteImpact(ctx, &gen.GetDeleteImpactPayload{})
	require.NoError(t, err)

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
	_, err := ti.service.CreateIngress(ctx, &gen.CreateIngressPayload{Provider: "other", Hostname: new("private"), OauthClientID: "id", OauthClientSecret: "secret"})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = ti.service.CreateIngress(ctx, &gen.CreateIngressPayload{Provider: networkingress.ProviderTailscale, Hostname: new("Not Valid"), OauthClientID: "id", OauthClientSecret: "secret"})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// compile-time coverage for generated nullable custom-domain shape.
var _ = pgtype.Text{}
var _ context.Context

// failingFlags is a feature provider whose lookups fail, as during a PostHog
// outage.
type failingFlags struct{ feature.InMemory }

func (*failingFlags) EvaluateFlag(context.Context, feature.Flag, string, map[string]string) (feature.Evaluation, error) {
	return feature.EvaluationIndeterminate, errors.New("posthog unavailable")
}

func TestCreateChoosesTheRuntimeFromThePrivateNetworkGatewayFlag(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		configure func(ti *testInstance) feature.Provider
		want      string
	}{
		{name: "flag enabled for the organization", want: "gateway", configure: func(ti *testInstance) feature.Provider {
			flags := &feature.InMemory{}
			flags.SetFlag(feature.FlagPrivateNetworkGateway, ti.orgID, true)
			return flags
		}},
		{name: "flag disabled", want: "operator", configure: func(ti *testInstance) feature.Provider {
			flags := &feature.InMemory{}
			flags.SetFlag(feature.FlagPrivateNetworkGateway, ti.orgID, false)
			return flags
		}},
		{name: "flag not defined", want: "operator", configure: func(*testInstance) feature.Provider { return &feature.InMemory{} }},
		{name: "flag lookup fails", want: "operator", configure: func(*testInstance) feature.Provider { return &failingFlags{} }},
		{name: "no feature provider", want: "operator", configure: func(*testInstance) feature.Provider { return nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			ti.service.WithFeatures(tc.configure(ti))

			created := ti.create(t, ctx)
			require.Equal(t, tc.want, created.Runtime)
			row, err := repo.New(ti.conn).GetNetworkIngressByID(ctx, repo.GetNetworkIngressByIDParams{ID: uuid.MustParse(created.ID), OrganizationID: ti.orgID})
			require.NoError(t, err)
			if tc.want == "gateway" {
				require.Equal(t, networkingress.RuntimeGateway, row.Runtime.String)
			} else {
				require.False(t, row.Runtime.Valid, "an operator connection stores no runtime, as every connection before the gateway did")
			}

			got, err := ti.service.GetIngress(ctx, &gen.GetIngressPayload{})
			require.NoError(t, err)
			require.Equal(t, tc.want, got.Ingress.Runtime, "the dashboard reads the runtime to show matching setup steps")
		})
	}
}

func TestCreateDefaultsHostnameToSpeakeasy(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created, err := ti.service.CreateIngress(ctx, &gen.CreateIngressPayload{Provider: networkingress.ProviderTailscale, Hostname: nil, OauthClientID: "client", OauthClientSecret: "secret"})
	require.NoError(t, err)
	require.Equal(t, networkingress.DefaultHostname, created.Hostname)
}

func TestLocalNamerPrefixesCreatedAndRenamedHostnames(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	namer, err := networkingress.NewLocalNamer("ab12")
	require.NoError(t, err)
	ti.service.UseNamer(namer)

	created, err := ti.service.CreateIngress(ctx, &gen.CreateIngressPayload{Provider: networkingress.ProviderTailscale, Hostname: nil, OauthClientID: "client", OauthClientSecret: "secret"})
	require.NoError(t, err)
	require.Equal(t, "dev-ab12-speakeasy", created.Hostname, "the default hostname is qualified too")

	renamed, err := ti.service.UpdateIngress(ctx, &gen.UpdateIngressPayload{Hostname: new("tools")})
	require.NoError(t, err)
	require.Equal(t, "dev-ab12-tools", renamed.Hostname)

	kept, err := ti.service.UpdateIngress(ctx, &gen.UpdateIngressPayload{Hostname: new("dev-ab12-tools")})
	require.NoError(t, err)
	require.Equal(t, "dev-ab12-tools", kept.Hostname, "an already qualified hostname is not prefixed twice")
}
