package remotesessions_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

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
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
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

func setEMAProvider(ctx context.Context, ti *testInstance, vendor uuid.UUID, provider *types.EMAProviderInput) (*types.EMAProviderSelection, error) {
	//nolint:wrapcheck // Tests assert the service's own error codes.
	return ti.service.SetEMAProvider(ctx, &orgissuersgen.SetEMAProviderPayload{SessionToken: nil, RemoteSessionIssuerID: vendor.String(), Provider: provider})
}

func TestSetEMAProvider_SavesSelectionAndAudits(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	f := newEMAProvidersFixture(t, ctx, ti)
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdateEMAProvider)
	require.NoError(t, err)

	selection, err := setEMAProvider(ctx, ti, f.vendor, speakeasyInput())
	require.NoError(t, err)
	require.False(t, selection.IsDefault)
	require.Equal(t, "https://vendor.example.com", selection.VendorIssuer)
	require.Equal(t, &types.EMAProvider{Kind: "speakeasy", RemoteSessionIssuerID: nil, InternalSessionIssuerID: conv.PtrEmpty(f.internalIssuer.String()), UnavailableReason: conv.PtrEmpty("signing_unavailable")}, selection.Provider)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdateEMAProvider)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdateEMAProvider)
	require.NoError(t, err)
	require.Equal(t, f.vendor.String(), record.SubjectID)
	snapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "https://vendor.example.com", snapshot["vendor_issuer"])
	provider, ok := snapshot["provider"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "speakeasy", provider["kind"])

	view, err := ti.service.GetIssuer(ctx, &orgissuersgen.GetIssuerPayload{SessionToken: nil, ApikeyToken: nil, ID: f.vendor.String()})
	require.NoError(t, err)
	require.Equal(t, selection, view.EmaProvider, "the issuer view shows the saved selection")

	_, err = setEMAProvider(ctx, ti, f.vendor, speakeasyInput())
	require.NoError(t, err)
	unchanged, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdateEMAProvider)
	require.NoError(t, err)
	require.Equal(t, after, unchanged, "saving the same selection writes no audit entry")

	restored, err := setEMAProvider(ctx, ti, f.vendor, nil)
	require.NoError(t, err)
	require.True(t, restored.IsDefault)
	require.Equal(t, &types.EMAProvider{Kind: "external", RemoteSessionIssuerID: nil, InternalSessionIssuerID: nil, UnavailableReason: nil}, restored.Provider, "the default keeps the endpoint's external provider")
	record, err = audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdateEMAProvider)
	require.NoError(t, err)
	snapshot, err = audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Nil(t, snapshot["provider"], "clearing records no provider")
}

func TestSetEMAProvider_SameVendorAcrossIssuerSpellings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	f := newEMAProvidersFixture(t, ctx, ti)
	spelled := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(f.org), "vendor-port-"+uuid.NewString()[:8], "https://VENDOR.example.com:443/")

	_, err := setEMAProvider(ctx, ti, spelled, externalInput(f.external))
	require.NoError(t, err)
	view, err := ti.service.GetIssuer(ctx, &orgissuersgen.GetIssuerPayload{SessionToken: nil, ApikeyToken: nil, ID: f.vendor.String()})
	require.NoError(t, err)
	require.False(t, view.EmaProvider.IsDefault, "both spellings name one vendor")
}

func TestSetEMAProvider_RejectsInvalidProviders(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	f := newEMAProvidersFixture(t, ctx, ti)
	untrusted := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, f.org, "untrusted-"+uuid.NewString()[:8])

	for name, provider := range map[string]*types.EMAProviderInput{
		"unknown kind":              {Kind: "okta", RemoteSessionIssuerID: nil},
		"external without issuer":   {Kind: "external", RemoteSessionIssuerID: nil},
		"speakeasy with an issuer":  {Kind: "speakeasy", RemoteSessionIssuerID: conv.PtrEmpty(f.external.String())},
		"untrusted external issuer": externalInput(untrusted),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := setEMAProvider(ctx, ti, f.vendor, provider)
			requireOopsCode(t, err, oops.CodeBadRequest)
		})
	}
}

func TestSetEMAProvider_SpeakeasyNeedsAnInternalSessionIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	f := newEMAProvidersFixture(t, ctx, ti)
	_, err := testrepo.New(ti.conn).DisableInternalSessionIssuerFixture(ctx, testrepo.DisableInternalSessionIssuerFixtureParams{OrganizationID: f.org, ID: f.internalIssuer})
	require.NoError(t, err)
	selection, err := setEMAProvider(ctx, ti, f.vendor, speakeasyInput())
	require.NoError(t, err, "a disabled internal session issuer can be named; it reads as unavailable")
	require.Equal(t, conv.PtrEmpty("disabled"), selection.Provider.UnavailableReason)

	otherCtx, otherTI := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(otherCtx)
	require.True(t, ok)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagEMAProviders, feature.AnyDistinctID, true)
	otherTI.service.SetFeatureFlags(flags)
	vendor := seedRemoteIssuerWithURL(t, otherCtx, otherTI.conn, uuid.NullUUID{}, conv.ToPGText(authCtx.ActiveOrganizationID), "vendor-"+uuid.NewString()[:8], "https://vendor.example.com")
	_, err = setEMAProvider(otherCtx, otherTI, vendor, speakeasyInput())
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSetEMAProvider_Gating(t *testing.T) {
	t.Parallel()

	t.Run("rollout off", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		f := newEMAProvidersFixture(t, ctx, ti)
		_, err := setEMAProvider(ctx, ti, f.vendor, speakeasyInput())
		require.NoError(t, err)
		ti.service.SetFeatureFlags(&feature.InMemory{})
		_, err = setEMAProvider(ctx, ti, f.vendor, externalInput(f.external))
		requireOopsCode(t, err, oops.CodeForbidden)
		ti.service.SetFeatureFlags(nil)
		_, err = setEMAProvider(ctx, ti, f.vendor, externalInput(f.external))
		requireOopsCode(t, err, oops.CodeForbidden)

		restored, err := setEMAProvider(ctx, ti, f.vendor, nil)
		require.NoError(t, err, "a saved selection can be cleared with the rollout off")
		require.True(t, restored.IsDefault)
	})

	t.Run("org read only", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		f := newEMAProvidersFixture(t, ctx, ti)
		readOnly := withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, f.org)})
		_, err := setEMAProvider(readOnly, ti, f.vendor, externalInput(f.external))
		requireOopsCode(t, err, oops.CodeForbidden)
		_, err = setEMAProvider(readOnly, ti, f.vendor, nil)
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
		supportCtx := contextvalues.WithValidatedSupportSession(ctx, &support)
		_, err := setEMAProvider(supportCtx, ti, f.vendor, externalInput(f.external))
		requireOopsCode(t, err, oops.CodeForbidden)
		_, err = setEMAProvider(supportCtx, ti, f.vendor, nil)
		requireOopsCode(t, err, oops.CodeForbidden)
	})

	t.Run("legacy impersonated session", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		f := newEMAProvidersFixture(t, ctx, ti)
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		impersonated := contextvalues.WithValidatedGramSession(ctx, authCtx, true)
		_, err := setEMAProvider(impersonated, ti, f.vendor, speakeasyInput())
		requireOopsCode(t, err, oops.CodeForbidden)
		_, err = setEMAProvider(impersonated, ti, f.vendor, nil)
		requireOopsCode(t, err, oops.CodeForbidden)
		view, err := ti.service.GetIssuer(impersonated, &orgissuersgen.GetIssuerPayload{SessionToken: nil, ApikeyToken: nil, ID: f.vendor.String()})
		require.NoError(t, err, "an impersonated session can still read the selection")
		require.True(t, view.EmaProvider.IsDefault)
	})

	t.Run("another organization's vendor", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		f := newEMAProvidersFixture(t, ctx, ti)
		otherOrg := "org-" + uuid.NewString()
		require.NoError(t, testrepo.New(ti.conn).SeedDelegationLoaderOrganizationFixture(ctx, testrepo.SeedDelegationLoaderOrganizationFixtureParams{OrganizationID: otherOrg, Name: "Other organization", Slug: "other-" + uuid.NewString()[:8]}))
		foreign := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: otherOrg, Valid: true}, "foreign-"+uuid.NewString()[:8], "https://vendor.example.com")
		_, err := setEMAProvider(ctx, ti, foreign, externalInput(f.external))
		requireOopsCode(t, err, oops.CodeNotFound)
	})
}

func TestSetEMAProvider_ConcurrentChangesSerialize(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	f := newEMAProvidersFixture(t, ctx, ti)
	inputs := []*types.EMAProviderInput{speakeasyInput(), externalInput(f.external), nil}

	var wg sync.WaitGroup
	errs := make([]error, len(inputs)*3)
	results := make([]*types.EMAProviderSelection, len(errs))
	for i := range errs {
		wg.Go(func() {
			results[i], errs[i] = setEMAProvider(ctx, ti, f.vendor, inputs[i%len(inputs)])
		})
	}
	wg.Wait()
	for i, err := range errs {
		require.NoError(t, err, "changes serialize instead of colliding")
		submitted := inputs[i%len(inputs)]
		if submitted == nil {
			require.True(t, results[i].IsDefault, "each response is its own write's selection")
			continue
		}
		require.Equal(t, submitted.Kind, results[i].Provider.Kind, "each response is its own write's selection")
	}
	rows, err := repo.New(ti.conn).ListEMAProviders(ctx, repo.ListEMAProvidersParams{OrganizationID: f.org, VendorIssuer: "https://vendor.example.com"})
	require.NoError(t, err)
	require.LessOrEqual(t, len(rows), 1, "at most one provider is ever saved")
}

func TestSetEMAProvider_ReadinessRunsAfterCommitAndNeverFailsIt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestServiceWithConfig(t, testServiceConfig{maxDBConns: 1})
	f := newEMAProvidersFixture(t, ctx, ti)
	ti.service.SetSpeakeasySigningAvailable(true)
	var reason remotesessions.EMAProviderUnavailableReason
	var readinessErr error
	ti.service.SetSpeakeasyReadiness(func(ctx context.Context, org string, issuerID uuid.UUID) (remotesessions.EMAProviderUnavailableReason, error) {
		require.Equal(t, f.org, org)
		require.Equal(t, f.internalIssuer, issuerID)
		// A query on the only connection proves the caller released its transaction.
		_, err := repo.New(ti.conn).GetEMAInternalSessionIssuer(ctx, org)
		if err != nil {
			return "", fmt.Errorf("read issuer after commit: %w", err)
		}
		return reason, readinessErr
	})
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, want := range []remotesessions.EMAProviderUnavailableReason{"", remotesessions.EMAProviderSignInEnforcement, remotesessions.EMAProviderSigningPaused} {
		reason = want
		selection, err := setEMAProvider(ctx, ti, f.vendor, speakeasyInput())
		require.NoError(t, err, "both changed and unchanged selections release their transaction before readiness")
		if want == "" {
			require.Nil(t, selection.Provider.UnavailableReason)
		} else {
			require.Equal(t, string(want), *selection.Provider.UnavailableReason, "the specific reason is kept")
		}
	}
	readinessErr = errors.New("temporary issuer lookup failure")
	_, err := setEMAProvider(ctx, ti, f.vendor, nil)
	require.NoError(t, err)
	selection, err := setEMAProvider(ctx, ti, f.vendor, speakeasyInput())
	require.NoError(t, err, "a committed write is never reported as failed")
	require.Equal(t, "readiness_unknown", *selection.Provider.UnavailableReason)
	rows, err := repo.New(ti.conn).ListEMAProviders(ctx, repo.ListEMAProvidersParams{OrganizationID: f.org, VendorIssuer: "https://vendor.example.com"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	view, err := ti.service.GetIssuer(ctx, &orgissuersgen.GetIssuerPayload{SessionToken: nil, ApikeyToken: nil, ID: f.vendor.String()})
	require.NoError(t, err, "the issuer view reads with readiness unknown")
	require.Equal(t, "readiness_unknown", *view.EmaProvider.Provider.UnavailableReason)
}
