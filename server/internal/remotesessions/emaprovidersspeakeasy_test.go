package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func addSpeakeasy(ctx context.Context, ti *testInstance, vendor, userIssuer uuid.UUID, dryRun bool) (*types.EMAProviderOrder, error) {
	//nolint:wrapcheck // Tests assert the service's own error codes.
	return ti.service.AddSpeakeasyEMAProvider(ctx, &orgissuersgen.AddSpeakeasyEMAProviderPayload{
		RemoteSessionIssuerID: vendor.String(), UserSessionIssuerID: userIssuer.String(), DryRun: dryRun, SessionToken: nil,
	})
}

// seedReadyBinding gives userIssuer a ready identity chaining binding to vendor.
func seedReadyBinding(t *testing.T, ctx context.Context, ti *testInstance, userIssuer, vendor uuid.UUID) {
	t.Helper()
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	q := repo.New(ti.conn)
	key := repo.EnsureEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: userIssuer, RemoteSessionIssuerID: vendor, Resource: "https://vendor.example.com/mcp-" + uuid.NewString()[:8]}
	require.NoError(t, q.EnsureEMABinding(ctx, key))
	binding, err := q.GetEMABinding(ctx, repo.GetEMABindingParams(key))
	require.NoError(t, err)
	_, err = q.SetEMABinding(ctx, repo.SetEMABindingParams{ID: binding.ID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, ExpectedGeneration: binding.Generation, Generation: binding.Generation, State: conv.ToPGText("ready"), GrantSource: conv.ToPGText("unknown"), RequestedScopes: []string{}})
	require.NoError(t, err)
}

func TestAddSpeakeasyEMAProvider_DefaultOrderSavesSpeakeasyAndAudits(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	f := newEMAProvidersFixture(t, ctx, ti)
	plain := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "plain-"+uuid.NewString()[:8])
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdateEMAProviders)
	require.NoError(t, err)

	checked, err := addSpeakeasy(ctx, ti, f.vendor, plain, true)
	require.NoError(t, err)
	require.True(t, checked.IsDefault, "a dry run saves nothing")

	order, err := addSpeakeasy(ctx, ti, f.vendor, plain, false)
	require.NoError(t, err)
	require.False(t, order.IsDefault)
	require.Len(t, order.Providers, 1)
	require.Equal(t, "speakeasy", order.Providers[0].Kind)
	require.Equal(t, conv.PtrEmpty(f.internalIssuer.String()), order.Providers[0].InternalSessionIssuerID)
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdateEMAProviders)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	again, err := addSpeakeasy(ctx, ti, f.vendor, plain, false)
	require.NoError(t, err)
	require.Equal(t, order.Providers, again.Providers, "adding Speakeasy twice is a no-op")
	unchanged, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdateEMAProviders)
	require.NoError(t, err)
	require.Equal(t, after, unchanged)
}

func TestAddSpeakeasyEMAProvider_AppendsWithoutReordering(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	f := newEMAProvidersFixture(t, ctx, ti)
	plain := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "plain-"+uuid.NewString()[:8])
	_, err := setEMAProviders(ctx, ti, f.vendor, externalInput(f.external))
	require.NoError(t, err)

	order, err := addSpeakeasy(ctx, ti, f.vendor, plain, false)
	require.NoError(t, err)
	require.Len(t, order.Providers, 2)
	require.Equal(t, "external", order.Providers[0].Kind)
	require.Equal(t, conv.PtrEmpty(f.external.String()), order.Providers[0].RemoteSessionIssuerID)
	require.Equal(t, "speakeasy", order.Providers[1].Kind, "Speakeasy goes after existing entries")
}

func TestAddSpeakeasyEMAProvider_Refusals(t *testing.T) {
	t.Parallel()

	t.Run("sign-in trusts an external identity provider", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		f := newEMAProvidersFixture(t, ctx, ti)
		trusting := createTrustedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "trusting-"+uuid.NewString()[:8], f.external)
		_, err := addSpeakeasy(ctx, ti, f.vendor, trusting, false)
		requireOopsCode(t, err, oops.CodeFailedPrecondition)
		_, err = addSpeakeasy(ctx, ti, f.vendor, trusting, true)
		requireOopsCode(t, err, oops.CodeFailedPrecondition)
	})

	t.Run("default order another endpoint chains through", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		f := newEMAProvidersFixture(t, ctx, ti)
		idp, idpClient := seedTrustedIdentityProviderClient(t, ctx, ti.conn, "okta-"+uuid.NewString()[:8])
		okta := createTrustedClientOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "okta-usi-"+uuid.NewString()[:8], idp, idpClient)
		// Same vendor under another spelling.
		alias := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(f.org), "alias-"+uuid.NewString()[:8], "https://VENDOR.example.com/")
		seedReadyBinding(t, ctx, ti, okta, alias)
		plain := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "plain-"+uuid.NewString()[:8])

		_, err := addSpeakeasy(ctx, ti, f.vendor, plain, true)
		requireOopsCode(t, err, oops.CodeFailedPrecondition)
		_, err = addSpeakeasy(ctx, ti, f.vendor, plain, false)
		requireOopsCode(t, err, oops.CodeFailedPrecondition)
		view, err := ti.service.GetIssuer(ctx, &orgissuersgen.GetIssuerPayload{SessionToken: nil, ApikeyToken: nil, ID: f.vendor.String()})
		require.NoError(t, err)
		require.True(t, view.EmaProviders.IsDefault, "the default order is untouched")

		// Once an order is saved, appending no longer replaces anyone's default.
		_, err = setEMAProviders(ctx, ti, f.vendor, externalInput(f.external))
		require.NoError(t, err)
		order, err := addSpeakeasy(ctx, ti, f.vendor, plain, false)
		require.NoError(t, err)
		require.Len(t, order.Providers, 2)
	})

	t.Run("full order", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		f := newEMAProvidersFixture(t, ctx, ti)
		plain := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "plain-"+uuid.NewString()[:8])
		inputs := []*types.EMAProviderInput{externalInput(f.external)}
		for range 9 {
			other := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, f.org, "idp-"+uuid.NewString()[:8])
			createTrustedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "usi-"+uuid.NewString()[:8], other)
			inputs = append(inputs, externalInput(other))
		}
		_, err := setEMAProviders(ctx, ti, f.vendor, inputs...)
		require.NoError(t, err)
		_, err = addSpeakeasy(ctx, ti, f.vendor, plain, false)
		requireOopsCode(t, err, oops.CodeFailedPrecondition)
	})

	t.Run("rollout off", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		f := newEMAProvidersFixture(t, ctx, ti)
		plain := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "plain-"+uuid.NewString()[:8])
		ti.service.SetFeatureFlags(&feature.InMemory{})
		_, err := addSpeakeasy(ctx, ti, f.vendor, plain, false)
		requireOopsCode(t, err, oops.CodeForbidden)
	})

	t.Run("support session", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		f := newEMAProvidersFixture(t, ctx, ti)
		plain := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "plain-"+uuid.NewString()[:8])
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		support := *authCtx
		support.IsAdmin, support.SupportOrganizationID = true, authCtx.ActiveOrganizationID
		_, err := addSpeakeasy(contextvalues.WithValidatedSupportSession(ctx, &support), ti, f.vendor, plain, false)
		requireOopsCode(t, err, oops.CodeForbidden)
	})

	t.Run("no Speakeasy issuer, even on a dry run", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		flags := &feature.InMemory{}
		flags.SetFlag(feature.FlagEMAProviders, feature.AnyDistinctID, true)
		ti.service.SetFeatureFlags(flags)
		vendor := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(authCtx.ActiveOrganizationID), "vendor-"+uuid.NewString()[:8], "https://vendor.example.com")
		plain := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "plain-"+uuid.NewString()[:8])
		_, err := addSpeakeasy(ctx, ti, vendor, plain, true)
		requireOopsCode(t, err, oops.CodeBadRequest)
		_, err = addSpeakeasy(ctx, ti, vendor, plain, false)
		requireOopsCode(t, err, oops.CodeBadRequest)
	})

	t.Run("unknown user session issuer", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		f := newEMAProvidersFixture(t, ctx, ti)
		_, err := addSpeakeasy(ctx, ti, f.vendor, uuid.New(), false)
		requireOopsCode(t, err, oops.CodeNotFound)
	})
}
