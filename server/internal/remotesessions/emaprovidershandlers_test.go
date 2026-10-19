package remotesessions_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	orgissuerssrv "github.com/speakeasy-api/gram/server/gen/http/organization_remote_session_issuers/server"
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
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
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
	if providers == nil {
		providers = []*types.EMAProviderInput{}
	}
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
	}, order.Providers, "the order round-trips as saved")

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdateEMAProviders)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdateEMAProviders)
	require.NoError(t, err)
	require.Equal(t, f.vendor.String(), record.SubjectID)
	snapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "https://vendor.example.com", snapshot["vendor_issuer"])
	providers, ok := snapshot["providers"].([]any)
	require.True(t, ok)
	require.Len(t, providers, 2)
	first, ok := providers[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "speakeasy", first["kind"])

	view, err := ti.service.GetIssuer(ctx, &orgissuersgen.GetIssuerPayload{SessionToken: nil, ApikeyToken: nil, ID: f.vendor.String()})
	require.NoError(t, err)
	require.Equal(t, order, view.EmaProviders, "the issuer view shows the saved order")

	_, err = setEMAProviders(ctx, ti, f.vendor, speakeasyInput(), externalInput(f.external))
	require.NoError(t, err)
	unchanged, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdateEMAProviders)
	require.NoError(t, err)
	require.Equal(t, after, unchanged, "saving the same order writes no audit entry")

	reordered, err := setEMAProviders(ctx, ti, f.vendor, externalInput(f.external), speakeasyInput())
	require.NoError(t, err)
	require.Equal(t, "external", reordered.Providers[0].Kind, "reordering is a change")
	require.Equal(t, "speakeasy", reordered.Providers[1].Kind)

	restored, err := setEMAProviders(ctx, ti, f.vendor)
	require.NoError(t, err)
	require.True(t, restored.IsDefault)
	require.Equal(t, []*types.EMAProvider{
		{Kind: "external", RemoteSessionIssuerID: nil, InternalSessionIssuerID: nil, UnavailableReason: nil},
		{Kind: "speakeasy", RemoteSessionIssuerID: nil, InternalSessionIssuerID: conv.PtrEmpty(f.internalIssuer.String()), UnavailableReason: conv.PtrEmpty("signing_unavailable")},
	}, restored.Providers, "an empty order restores the derived default: the endpoint's external provider, then Speakeasy")
	record, err = audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdateEMAProviders)
	require.NoError(t, err)
	snapshot, err = audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Empty(t, snapshot["providers"], "clearing records no providers")
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

func TestSetEMAProviders_RejectsInvalidProviders(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	f := newEMAProvidersFixture(t, ctx, ti)
	untrusted := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, f.org, "untrusted-"+uuid.NewString()[:8])

	tooMany := make([]*types.EMAProviderInput, 0, 11)
	for range 11 {
		tooMany = append(tooMany, externalInput(uuid.New()))
	}
	// Cleanup runs after every parallel rejection has finished.
	t.Cleanup(func() {
		rows, err := repo.New(ti.conn).ListEMAProviders(context.WithoutCancel(ctx), repo.ListEMAProvidersParams{OrganizationID: f.org, VendorIssuer: "https://vendor.example.com"})
		require.NoError(t, err)
		require.Empty(t, rows, "a rejected order saves nothing")
	})
	for name, providers := range map[string][]*types.EMAProviderInput{
		"unknown kind":              {{Kind: "okta", RemoteSessionIssuerID: nil}},
		"external without issuer":   {{Kind: "external", RemoteSessionIssuerID: nil}},
		"speakeasy with an issuer":  {{Kind: "speakeasy", RemoteSessionIssuerID: conv.PtrEmpty(f.external.String())}},
		"untrusted external issuer": {externalInput(f.external), externalInput(untrusted)},
		"duplicate external":        {externalInput(f.external), speakeasyInput(), externalInput(f.external)},
		"duplicate speakeasy":       {speakeasyInput(), speakeasyInput()},
		"null entry":                {speakeasyInput(), nil},
		"more than ten":             tooMany,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := setEMAProviders(ctx, ti, f.vendor, providers...)
			requireOopsCode(t, err, oops.CodeBadRequest)
		})
	}
}

func TestSetEMAProviders_TooManyHasAReadableMessage(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	f := newEMAProvidersFixture(t, ctx, ti)

	entries := make([]string, 0, 11)
	for range 11 {
		entries = append(entries, fmt.Sprintf(`{"kind":"external","remote_session_issuer_id":%q}`, uuid.NewString()))
	}
	body := fmt.Sprintf(`{"remote_session_issuer_id":%q,"providers":[%s]}`, f.vendor, strings.Join(entries, ","))
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/rpc/organizationRemoteSessionIssuers.setEMAProviders", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	payload, err := orgissuerssrv.DecodeSetEMAProvidersRequest(goahttp.NewMuxer(), goahttp.RequestDecoder)(req)
	require.NoError(t, err, "the request decoder leaves the length check to the handler")

	_, err = ti.service.SetEMAProviders(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.Equal(t, "an EMA provider order has at most 10 providers", err.Error())
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
		_, err := setEMAProviders(ctx, ti, f.vendor, speakeasyInput())
		require.NoError(t, err)
		ti.service.SetFeatureFlags(&feature.InMemory{})
		_, err = setEMAProviders(ctx, ti, f.vendor, externalInput(f.external))
		requireOopsCode(t, err, oops.CodeForbidden)
		ti.service.SetFeatureFlags(nil)
		_, err = setEMAProviders(ctx, ti, f.vendor, externalInput(f.external))
		requireOopsCode(t, err, oops.CodeForbidden)

		restored, err := setEMAProviders(ctx, ti, f.vendor)
		require.NoError(t, err, "a saved order can be cleared with the rollout off")
		require.True(t, restored.IsDefault)
	})

	t.Run("org read only", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		f := newEMAProvidersFixture(t, ctx, ti)
		readOnly := withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, f.org)})
		_, err := setEMAProviders(readOnly, ti, f.vendor, externalInput(f.external))
		requireOopsCode(t, err, oops.CodeForbidden)
		_, err = setEMAProviders(readOnly, ti, f.vendor)
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
		_, err := setEMAProviders(supportCtx, ti, f.vendor, externalInput(f.external))
		requireOopsCode(t, err, oops.CodeForbidden)
		_, err = setEMAProviders(supportCtx, ti, f.vendor)
		requireOopsCode(t, err, oops.CodeForbidden)
	})

	t.Run("legacy impersonated session", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		f := newEMAProvidersFixture(t, ctx, ti)
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		impersonated := contextvalues.WithValidatedGramSession(ctx, authCtx, true)
		_, err := setEMAProviders(impersonated, ti, f.vendor, speakeasyInput())
		requireOopsCode(t, err, oops.CodeForbidden)
		_, err = setEMAProviders(impersonated, ti, f.vendor)
		requireOopsCode(t, err, oops.CodeForbidden)
		view, err := ti.service.GetIssuer(impersonated, &orgissuersgen.GetIssuerPayload{SessionToken: nil, ApikeyToken: nil, ID: f.vendor.String()})
		require.NoError(t, err, "an impersonated session can still read the order")
		require.True(t, view.EmaProviders.IsDefault)
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

func TestSetEMAProviders_ConcurrentChangesSerialize(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	f := newEMAProvidersFixture(t, ctx, ti)
	inputs := [][]*types.EMAProviderInput{{speakeasyInput(), externalInput(f.external)}, {externalInput(f.external)}, {}}

	var wg sync.WaitGroup
	errs := make([]error, len(inputs)*3)
	results := make([]*types.EMAProviderOrder, len(errs))
	for i := range errs {
		wg.Go(func() {
			results[i], errs[i] = setEMAProviders(ctx, ti, f.vendor, inputs[i%len(inputs)]...)
		})
	}
	wg.Wait()
	for i, err := range errs {
		require.NoError(t, err, "changes serialize instead of colliding")
		submitted := inputs[i%len(inputs)]
		if len(submitted) == 0 {
			require.True(t, results[i].IsDefault, "each response is its own write's order")
			continue
		}
		require.Len(t, results[i].Providers, len(submitted), "each response is its own write's order")
		for j, p := range submitted {
			require.Equal(t, p.Kind, results[i].Providers[j].Kind)
		}
	}
	rows, err := repo.New(ti.conn).ListEMAProviders(ctx, repo.ListEMAProvidersParams{OrganizationID: f.org, VendorIssuer: "https://vendor.example.com"})
	require.NoError(t, err)
	require.Contains(t, []int{0, 1, 2}, len(rows), "one write's order is saved whole, never interleaved")
	for i, row := range rows {
		require.Equal(t, int32(i), row.Position)
	}
}

func TestSetEMAProviders_ReadinessRunsAfterCommitAndNeverFailsIt(t *testing.T) {
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
		order, err := setEMAProviders(ctx, ti, f.vendor, speakeasyInput())
		require.NoError(t, err, "both changed and unchanged orders release their transaction before readiness")
		if want == "" {
			require.Nil(t, order.Providers[0].UnavailableReason)
		} else {
			require.Equal(t, string(want), *order.Providers[0].UnavailableReason, "the specific reason is kept")
		}
	}
	readinessErr = errors.New("temporary issuer lookup failure")
	_, err := setEMAProviders(ctx, ti, f.vendor)
	require.NoError(t, err)
	order, err := setEMAProviders(ctx, ti, f.vendor, speakeasyInput())
	require.NoError(t, err, "a committed write is never reported as failed")
	require.Equal(t, "readiness_unknown", *order.Providers[0].UnavailableReason)
	rows, err := repo.New(ti.conn).ListEMAProviders(ctx, repo.ListEMAProvidersParams{OrganizationID: f.org, VendorIssuer: "https://vendor.example.com"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	view, err := ti.service.GetIssuer(ctx, &orgissuersgen.GetIssuerPayload{SessionToken: nil, ApikeyToken: nil, ID: f.vendor.String()})
	require.NoError(t, err, "the issuer view reads with readiness unknown")
	require.Equal(t, "readiness_unknown", *view.EmaProviders.Providers[0].UnavailableReason)
}

func TestResolveEMAProviders_DerivedDefault(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	org := authCtx.ActiveOrganizationID
	q := repo.New(ti.conn)
	vendor := "https://vendor.example.com"
	external := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, org, "idp-"+uuid.NewString()[:8])
	client := seedOrgLevelRemoteClient(t, ctx, ti.conn, org, external, "client-"+uuid.NewString()[:8])
	trusting := createTrustedClientOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "usi-"+uuid.NewString()[:8], external, client)
	untrusting, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               conv.ToPGText(org),
		Slug:                         "usi-" + uuid.NewString()[:8],
		AuthnChallengeMode:           "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TrustedRemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	placeholder := remotesessions.EMAProvider{Kind: remotesessions.EMAProviderExternal, RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: uuid.Nil, Unavailable: ""}
	trusted := remotesessions.EMAProvider{Kind: remotesessions.EMAProviderExternal, RemoteSessionIssuerID: external, InternalSessionIssuerID: uuid.Nil, Unavailable: ""}

	order, err := remotesessions.EndpointEMAProviders(ctx, q, org, trusting, vendor, true)
	require.NoError(t, err)
	require.True(t, order.IsDefault)
	require.Equal(t, []remotesessions.EMAProvider{trusted}, order.Providers, "without a Speakeasy issuer the default is the trusted external issuer")
	order, err = remotesessions.EndpointEMAProviders(ctx, q, org, untrusting.ID, vendor, true)
	require.NoError(t, err)
	require.Equal(t, []remotesessions.EMAProvider{placeholder}, order.Providers, "neither issuer keeps the external placeholder")

	internalIssuer := seedInternalSessionIssuer(t, ctx, ti, org)
	speakeasy := remotesessions.EMAProvider{Kind: remotesessions.EMAProviderSpeakeasy, RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: internalIssuer, Unavailable: ""}
	order, err = remotesessions.EndpointEMAProviders(ctx, q, org, trusting, vendor, true)
	require.NoError(t, err)
	require.True(t, order.IsDefault)
	require.Equal(t, []remotesessions.EMAProvider{trusted}, order.Providers, "a sign-in that trusts an external issuer keeps it alone; Speakeasy is never added behind it")
	order, err = remotesessions.EndpointEMAProviders(ctx, q, org, untrusting.ID, vendor, true)
	require.NoError(t, err)
	require.Equal(t, []remotesessions.EMAProvider{speakeasy}, order.Providers, "a sign-in that trusts no external identity provider defaults to Speakeasy alone")
	order, err = remotesessions.ResolveEMAProviders(ctx, q, org, vendor, remotesessions.EMAProviderScope{Endpoint: false, TrustedIssuer: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, SpeakeasySigns: true})
	require.NoError(t, err)
	require.Equal(t, []remotesessions.EMAProvider{placeholder, speakeasy}, order.Providers, "the organization-wide view lists each endpoint's own issuer, then Speakeasy for endpoints that trust none")

	cannotSign := speakeasy
	cannotSign.Unavailable = remotesessions.EMAProviderSigningUnavailable
	order, err = remotesessions.EndpointEMAProviders(ctx, q, org, untrusting.ID, vendor, false)
	require.NoError(t, err)
	require.Equal(t, []remotesessions.EMAProvider{cannotSign}, order.Providers, "a deployment that cannot sign keeps Speakeasy flagged")
	require.Empty(t, order.Available())

	rows, err := testrepo.New(ti.conn).DisableInternalSessionIssuerFixture(ctx, testrepo.DisableInternalSessionIssuerFixtureParams{OrganizationID: org, ID: internalIssuer})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	disabled := speakeasy
	disabled.Unavailable = remotesessions.EMAProviderDisabled
	order, err = remotesessions.EndpointEMAProviders(ctx, q, org, untrusting.ID, vendor, true)
	require.NoError(t, err)
	require.Equal(t, []remotesessions.EMAProvider{disabled}, order.Providers, "a disabled Speakeasy issuer stays in the default, flagged")

	require.NoError(t, testrepo.New(ti.conn).TombstoneInternalSessionIssuerFixture(ctx, org))
	order, err = remotesessions.EndpointEMAProviders(ctx, q, org, untrusting.ID, vendor, true)
	require.NoError(t, err)
	require.Equal(t, []remotesessions.EMAProvider{placeholder}, order.Providers, "a deleted Speakeasy issuer leaves the default")
}

func TestResolveEMAProviders_SpeakeasyVouchesOnlyForItsOwnSignIn(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	org := authCtx.ActiveOrganizationID
	q := repo.New(ti.conn)
	vendor, err := remotesessions.NormalizeVendorIssuer("https://vendor.example.com")
	require.NoError(t, err)
	external := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, org, "idp-"+uuid.NewString()[:8])
	client := seedOrgLevelRemoteClient(t, ctx, ti.conn, org, external, "client-"+uuid.NewString()[:8])
	federated := createTrustedClientOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "usi-"+uuid.NewString()[:8], external, client)
	ownSignIn := createUserSessionIssuer(t, ctx, ti.conn, "usi-"+uuid.NewString()[:8])
	internalIssuer := seedInternalSessionIssuer(t, ctx, ti, org)
	speakeasy := remotesessions.EMAProvider{Kind: remotesessions.EMAProviderSpeakeasy, RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: internalIssuer, Unavailable: ""}
	okta := remotesessions.EMAProvider{Kind: remotesessions.EMAProviderExternal, RemoteSessionIssuerID: external, InternalSessionIssuerID: uuid.Nil, Unavailable: ""}
	notTrusted := func(p remotesessions.EMAProvider) remotesessions.EMAProvider {
		p.Unavailable = remotesessions.EMAProviderNotTrustedByEndpoint
		return p
	}

	require.NoError(t, q.InsertEMAProvider(ctx, repo.InsertEMAProviderParams{OrganizationID: org, VendorIssuer: vendor, RemoteSessionIssuerID: uuid.NullUUID{}, InternalSessionIssuerID: conv.ToNullUUID(internalIssuer), Position: 0}))
	order, err := remotesessions.EndpointEMAProviders(ctx, q, org, federated, vendor, true)
	require.NoError(t, err)
	require.False(t, order.IsDefault)
	require.Equal(t, []remotesessions.EMAProvider{notTrusted(speakeasy)}, order.Providers, "Speakeasy has no sign-in to vouch from on a server that signs people in through an external issuer")
	require.Empty(t, order.Available())
	order, err = remotesessions.EndpointEMAProviders(ctx, q, org, ownSignIn, vendor, true)
	require.NoError(t, err)
	require.Equal(t, []remotesessions.EMAProvider{speakeasy}, order.Providers, "Speakeasy vouches on a server that uses Speakeasy's own sign-in")

	both, err := remotesessions.NormalizeVendorIssuer("https://both.example.com")
	require.NoError(t, err)
	require.NoError(t, q.InsertEMAProvider(ctx, repo.InsertEMAProviderParams{OrganizationID: org, VendorIssuer: both, RemoteSessionIssuerID: conv.ToNullUUID(external), InternalSessionIssuerID: uuid.NullUUID{}, Position: 0}))
	require.NoError(t, q.InsertEMAProvider(ctx, repo.InsertEMAProviderParams{OrganizationID: org, VendorIssuer: both, RemoteSessionIssuerID: uuid.NullUUID{}, InternalSessionIssuerID: conv.ToNullUUID(internalIssuer), Position: 1}))
	order, err = remotesessions.EndpointEMAProviders(ctx, q, org, federated, both, true)
	require.NoError(t, err)
	require.Equal(t, []remotesessions.EMAProvider{okta, notTrusted(speakeasy)}, order.Providers, "the sign-in's own issuer vouches; Speakeasy never stands behind it")
	require.Equal(t, []remotesessions.EMAProvider{okta}, order.Available())
	order, err = remotesessions.EndpointEMAProviders(ctx, q, org, ownSignIn, both, true)
	require.NoError(t, err)
	require.Equal(t, []remotesessions.EMAProvider{notTrusted(okta), speakeasy}, order.Providers)

	order, err = remotesessions.ResolveEMAProviders(ctx, q, org, both, remotesessions.EMAProviderScope{Endpoint: false, TrustedIssuer: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, SpeakeasySigns: true})
	require.NoError(t, err)
	require.Equal(t, []remotesessions.EMAProvider{okta, speakeasy}, order.Providers, "the organization-wide view, for no one endpoint, flags neither")
}

func TestSetEMAProviders_RefusesAStaleExpectedOrder(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	f := newEMAProvidersFixture(t, ctx, ti)
	setExpecting := func(expected []*types.EMAProviderInput, providers ...*types.EMAProviderInput) (*types.EMAProviderOrder, error) {
		return ti.service.SetEMAProviders(ctx, &orgissuersgen.SetEMAProvidersPayload{SessionToken: nil, RemoteSessionIssuerID: f.vendor.String(), Providers: providers, ExpectedProviders: expected})
	}

	_, err := setExpecting([]*types.EMAProviderInput{}, externalInput(f.external), speakeasyInput())
	require.NoError(t, err, "the default is the empty expected order")

	// Another administrator removes Speakeasy while this editor still holds
	// the two-provider order it started from.
	_, err = setEMAProviders(ctx, ti, f.vendor, externalInput(f.external))
	require.NoError(t, err)
	_, err = setExpecting([]*types.EMAProviderInput{externalInput(f.external), speakeasyInput()}, speakeasyInput(), externalInput(f.external))
	requireOopsCode(t, err, oops.CodeConflict)
	rows, err := repo.New(ti.conn).ListEMAProviders(ctx, repo.ListEMAProvidersParams{OrganizationID: f.org, VendorIssuer: "https://vendor.example.com"})
	require.NoError(t, err)
	require.Len(t, rows, 1, "a stale editor writes nothing")

	order, err := setExpecting([]*types.EMAProviderInput{externalInput(f.external)}, externalInput(f.external), speakeasyInput())
	require.NoError(t, err, "an editor that started from the current order saves")
	require.Len(t, order.Providers, 2)
}

func TestSetEMAProviders_StaleEditNamingADeletedProviderConflicts(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	f := newEMAProvidersFixture(t, ctx, ti)
	_, err := setEMAProviders(ctx, ti, f.vendor, externalInput(f.external), speakeasyInput())
	require.NoError(t, err)

	// Another administrator drops the external provider and deletes its
	// issuer while this editor still holds the two-provider order.
	_, err = setEMAProviders(ctx, ti, f.vendor, speakeasyInput())
	require.NoError(t, err)
	deleted, err := repo.New(ti.conn).SoftDeleteRemoteSessionIssuerFixture(ctx, f.external)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	_, err = setEMAProviders(ctx, ti, f.vendor, speakeasyInput(), externalInput(f.external))
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = ti.service.SetEMAProviders(ctx, &orgissuersgen.SetEMAProvidersPayload{
		SessionToken: nil, RemoteSessionIssuerID: f.vendor.String(),
		Providers:         []*types.EMAProviderInput{speakeasyInput(), externalInput(f.external)},
		ExpectedProviders: []*types.EMAProviderInput{externalInput(f.external), speakeasyInput()},
	})
	requireOopsCode(t, err, oops.CodeConflict)
}
