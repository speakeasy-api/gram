package remotesessions_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

type emaProvidersFixture struct {
	org            string
	vendor         uuid.UUID
	external       uuid.UUID
	internalIssuer uuid.UUID
}

// newEMAProvidersFixture seeds a vendor issuer, an external issuer an
// organization user session issuer trusts, and the internal session issuer,
// and turns the rollout on.
func newEMAProvidersFixture(t *testing.T, ctx context.Context, ti *testInstance) emaProvidersFixture {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	org := authCtx.ActiveOrganizationID
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagEMAProviders, feature.AnyDistinctID, true)
	ti.service.SetFeatureFlags(flags)

	vendor := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(org), "vendor-"+uuid.NewString()[:8], "https://vendor.example.com")
	external := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, org, "idp-"+uuid.NewString()[:8])
	createTrustedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "usi-"+uuid.NewString()[:8], external)
	return emaProvidersFixture{org: org, vendor: vendor, external: external, internalIssuer: seedInternalSessionIssuer(t, ctx, ti, org)}
}

func seedInternalSessionIssuer(t *testing.T, ctx context.Context, ti *testInstance, org string) uuid.UUID {
	t.Helper()
	fixtures := testrepo.New(ti.conn)
	set, err := fixtures.SeedJsonWebKeySetFixture(ctx, testrepo.SeedJsonWebKeySetFixtureParams{OrganizationID: org, Name: "speakeasy-" + uuid.NewString()[:8]})
	require.NoError(t, err)
	issuer, err := fixtures.SeedInternalSessionIssuerFixture(ctx, testrepo.SeedInternalSessionIssuerFixtureParams{OrganizationID: org, Issuer: "https://speakeasy.example.test/oauth/idp/org/" + org, JsonWebKeySetID: set})
	require.NoError(t, err)
	return issuer
}

func externalInput(id uuid.UUID) *types.EMAProviderInput {
	return &types.EMAProviderInput{Kind: "external", RemoteSessionIssuerID: conv.PtrEmpty(id.String())}
}

func speakeasyInput() *types.EMAProviderInput {
	return &types.EMAProviderInput{Kind: "speakeasy", RemoteSessionIssuerID: nil}
}

func setEMAProviders(ctx context.Context, ti *testInstance, vendor uuid.UUID, providers ...*types.EMAProviderInput) (*types.EMAProviderOrder, error) {
	//nolint:wrapcheck // Tests assert the service's own error codes.
	return ti.service.SetEMAProviders(ctx, &orgissuersgen.SetEMAProvidersPayload{SessionToken: nil, RemoteSessionIssuerID: vendor.String(), Providers: providers})
}

func TestSetEMAProviders_SavesOrderAndAudits(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	f := newEMAProvidersFixture(t, ctx, ti)
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdateEMAProviders)
	require.NoError(t, err)

	order, err := setEMAProviders(ctx, ti, f.vendor, speakeasyInput(), externalInput(f.external))
	require.NoError(t, err)
	require.False(t, order.IsDefault)
	require.Equal(t, "https://vendor.example.com", order.VendorIssuer)
	require.Equal(t, []*types.EMAProvider{
		{Kind: "speakeasy", RemoteSessionIssuerID: nil, InternalSessionIssuerID: conv.PtrEmpty(f.internalIssuer.String()), UnavailableReason: conv.PtrEmpty("signing_unavailable")},
		{Kind: "external", RemoteSessionIssuerID: conv.PtrEmpty(f.external.String()), InternalSessionIssuerID: nil, UnavailableReason: nil},
	}, order.Providers)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdateEMAProviders)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdateEMAProviders)
	require.NoError(t, err)
	require.Equal(t, f.vendor.String(), record.SubjectID)
	snapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "https://vendor.example.com", snapshot["vendor_issuer"])

	view, err := ti.service.GetIssuer(ctx, &orgissuersgen.GetIssuerPayload{SessionToken: nil, ApikeyToken: nil, ID: f.vendor.String()})
	require.NoError(t, err)
	require.Equal(t, order, view.EmaProviders, "the issuer view shows the saved order")

	_, err = setEMAProviders(ctx, ti, f.vendor, speakeasyInput(), externalInput(f.external))
	require.NoError(t, err)
	unchanged, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdateEMAProviders)
	require.NoError(t, err)
	require.Equal(t, after, unchanged, "saving the same order writes no audit entry")

	restored, err := setEMAProviders(ctx, ti, f.vendor)
	require.NoError(t, err)
	require.True(t, restored.IsDefault)
	require.Equal(t, []*types.EMAProvider{{Kind: "external", RemoteSessionIssuerID: conv.PtrEmpty(f.external.String()), InternalSessionIssuerID: nil, UnavailableReason: nil}}, restored.Providers, "the default holds every available provider; Speakeasy joins once it signs")
}

func TestSetEMAProviders_SameVendorAcrossIssuerSpellings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	f := newEMAProvidersFixture(t, ctx, ti)
	spelled := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(f.org), "vendor-port-"+uuid.NewString()[:8], "https://VENDOR.example.com:443/")

	_, err := setEMAProviders(ctx, ti, spelled, externalInput(f.external))
	require.NoError(t, err)
	view, err := ti.service.GetIssuer(ctx, &orgissuersgen.GetIssuerPayload{SessionToken: nil, ApikeyToken: nil, ID: f.vendor.String()})
	require.NoError(t, err)
	require.False(t, view.EmaProviders.IsDefault, "both spellings name one vendor")
}

func TestSetEMAProviders_RejectsInvalidOrders(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	f := newEMAProvidersFixture(t, ctx, ti)
	untrusted := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, f.org, "untrusted-"+uuid.NewString()[:8])

	for name, providers := range map[string][]*types.EMAProviderInput{
		"unknown kind":              {{Kind: "okta", RemoteSessionIssuerID: nil}},
		"external without issuer":   {{Kind: "external", RemoteSessionIssuerID: nil}},
		"speakeasy with an issuer":  {{Kind: "speakeasy", RemoteSessionIssuerID: conv.PtrEmpty(f.external.String())}},
		"duplicate":                 {externalInput(f.external), externalInput(f.external)},
		"untrusted external issuer": {externalInput(untrusted)},
		"null entry":                {nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := setEMAProviders(ctx, ti, f.vendor, providers...)
			requireOopsCode(t, err, oops.CodeBadRequest)
		})
	}
}

func TestSetEMAProviders_SpeakeasyNeedsAnInternalSessionIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	f := newEMAProvidersFixture(t, ctx, ti)
	_, err := testrepo.New(ti.conn).DisableInternalSessionIssuerFixture(ctx, testrepo.DisableInternalSessionIssuerFixtureParams{OrganizationID: f.org, ID: f.internalIssuer})
	require.NoError(t, err)
	order, err := setEMAProviders(ctx, ti, f.vendor, speakeasyInput())
	require.NoError(t, err, "a disabled internal session issuer can be named; it reads as unavailable")
	require.Equal(t, conv.PtrEmpty("disabled"), order.Providers[0].UnavailableReason)

	otherCtx, otherTI := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(otherCtx)
	require.True(t, ok)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagEMAProviders, feature.AnyDistinctID, true)
	otherTI.service.SetFeatureFlags(flags)
	vendor := seedRemoteIssuerWithURL(t, otherCtx, otherTI.conn, uuid.NullUUID{}, conv.ToPGText(authCtx.ActiveOrganizationID), "vendor-"+uuid.NewString()[:8], "https://vendor.example.com")
	_, err = setEMAProviders(otherCtx, otherTI, vendor, speakeasyInput())
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSetEMAProviders_Gating(t *testing.T) {
	t.Parallel()

	t.Run("rollout off", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		f := newEMAProvidersFixture(t, ctx, ti)
		ti.service.SetFeatureFlags(&feature.InMemory{})
		_, err := setEMAProviders(ctx, ti, f.vendor, externalInput(f.external))
		requireOopsCode(t, err, oops.CodeForbidden)
		ti.service.SetFeatureFlags(nil)
		_, err = setEMAProviders(ctx, ti, f.vendor, externalInput(f.external))
		requireOopsCode(t, err, oops.CodeForbidden)
	})

	t.Run("org read only", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		f := newEMAProvidersFixture(t, ctx, ti)
		readOnly := withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, f.org)})
		_, err := setEMAProviders(readOnly, ti, f.vendor, externalInput(f.external))
		requireOopsCode(t, err, oops.CodeForbidden)
	})

	t.Run("support session", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		f := newEMAProvidersFixture(t, ctx, ti)
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		support := *authCtx
		support.IsAdmin, support.SupportOrganizationID = true, authCtx.ActiveOrganizationID
		_, err := setEMAProviders(contextvalues.WithValidatedSupportSession(ctx, &support), ti, f.vendor, externalInput(f.external))
		requireOopsCode(t, err, oops.CodeForbidden)
	})

	t.Run("another organization's vendor", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		f := newEMAProvidersFixture(t, ctx, ti)
		otherOrg := "org-" + uuid.NewString()
		require.NoError(t, testrepo.New(ti.conn).SeedDelegationLoaderOrganizationFixture(ctx, testrepo.SeedDelegationLoaderOrganizationFixtureParams{OrganizationID: otherOrg, Name: "Other organization", Slug: "other-" + uuid.NewString()[:8]}))
		foreign := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: otherOrg, Valid: true}, "foreign-"+uuid.NewString()[:8], "https://vendor.example.com")
		_, err := setEMAProviders(ctx, ti, foreign, externalInput(f.external))
		requireOopsCode(t, err, oops.CodeNotFound)
	})
}

func TestSetEMAProviders_ConcurrentReplacesSerialize(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	f := newEMAProvidersFixture(t, ctx, ti)
	orders := [][]*types.EMAProviderInput{
		{speakeasyInput(), externalInput(f.external)},
		{externalInput(f.external), speakeasyInput()},
		{externalInput(f.external)},
	}

	var wg sync.WaitGroup
	errs := make([]error, len(orders)*3)
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = setEMAProviders(ctx, ti, f.vendor, orders[i%len(orders)]...)
		})
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err, "replaces serialize instead of colliding on positions")
	}
	rows, err := repo.New(ti.conn).ListEMAProviders(ctx, repo.ListEMAProvidersParams{OrganizationID: f.org, VendorIssuer: "https://vendor.example.com"})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	for i, row := range rows {
		require.Equal(t, int32(i), row.Position, "the surviving order is one whole write")
	}
}
