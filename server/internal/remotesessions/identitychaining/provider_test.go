package identitychaining

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// policyRow is one provider in a saved order: an external remote session
// issuer or the organization's internal session issuer.
type policyRow struct {
	remoteIssuer   uuid.UUID
	internalIssuer uuid.UUID
}

func external(remoteIssuer uuid.UUID) policyRow {
	return policyRow{remoteIssuer: remoteIssuer, internalIssuer: uuid.Nil}
}

func speakeasy(internalIssuer uuid.UUID) policyRow {
	return policyRow{remoteIssuer: uuid.Nil, internalIssuer: internalIssuer}
}

// seedSpeakeasyIssuer creates the organization's internal session issuer.
func seedSpeakeasyIssuer(t *testing.T, f devIDPFixture, organizationID string) uuid.UUID {
	t.Helper()
	fixtures := testrepo.New(f.db)
	set, err := fixtures.SeedJsonWebKeySetFixture(t.Context(), testrepo.SeedJsonWebKeySetFixtureParams{OrganizationID: organizationID, Name: "speakeasy-issuer-" + uuid.NewString()[:8]})
	require.NoError(t, err)
	issuer, err := fixtures.SeedInternalSessionIssuerFixture(t.Context(), testrepo.SeedInternalSessionIssuerFixtureParams{OrganizationID: organizationID, Issuer: "https://speakeasy.example.test/oauth/idp/org/" + organizationID, JsonWebKeySetID: set})
	require.NoError(t, err)
	return issuer
}

// setPolicy saves the organization's ordered providers for the fixture's vendor.
func setPolicy(t *testing.T, f devIDPFixture, organizationID string, rows ...policyRow) {
	t.Helper()
	vendor, err := remotesessions.NormalizeVendorIssuer(f.vendorIssuer)
	require.NoError(t, err)
	q := repo.New(f.db)
	for i, row := range rows {
		require.NoError(t, q.InsertEMAProvider(t.Context(), repo.InsertEMAProviderParams{
			OrganizationID: organizationID, VendorIssuer: vendor,
			RemoteSessionIssuerID:   uuid.NullUUID{UUID: row.remoteIssuer, Valid: row.remoteIssuer != uuid.Nil},
			InternalSessionIssuerID: uuid.NullUUID{UUID: row.internalIssuer, Valid: row.internalIssuer != uuid.Nil},
			Position:                int32(i),
		}))
	}
}

// useSpeakeasySource makes the Speakeasy provider serve through src.
func useSpeakeasySource(f devIDPFixture, src tokenSource) {
	f.chainer.sources[remotesessions.EMAProviderSpeakeasy] = func(remotesessions.EMAProvider) tokenSource { return src }
}

// failingSource is an available source that fails at one step and counts
// the issues it reached.
type failingSource struct {
	authorizeOutcome Outcome
	reuseOutcome     Outcome
	issueOutcome     Outcome
	issued           int
}

func (*failingSource) kind() remotesessions.EMAProviderKind {
	return remotesessions.EMAProviderSpeakeasy
}

func (*failingSource) available() bool { return true }

func (s *failingSource) authorize(context.Context, *slog.Logger, Request, *selection) Outcome {
	return s.authorizeOutcome
}

func (s *failingSource) checkReuse(context.Context, Request, selection) Outcome {
	return s.reuseOutcome
}

func (s *failingSource) issue(context.Context, *slog.Logger, Request, selection, func() Outcome) (issuedGrant, Outcome) {
	s.issued++
	return issuedGrant{value: "", trustedSessionID: uuid.Nil, trustedObtainedAt: time.Time{}}, s.issueOutcome
}

func kinds(order remotesessions.EMAProviderOrder) []remotesessions.EMAProviderKind {
	out := []remotesessions.EMAProviderKind{}
	for _, p := range order.Available() {
		out = append(out, p.Kind)
	}
	return out
}

func TestProviders_DefaultOrderIsTheEndpointsExternalIssuer(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	seedSpeakeasyIssuer(t, f, f.req.OrganizationID)

	order, ok := f.chainer.ProviderOrder(t.Context(), f.req)
	require.True(t, ok)
	require.True(t, order.IsDefault)
	require.Equal(t, []remotesessions.EMAProvider{{Kind: remotesessions.EMAProviderExternal, RemoteSessionIssuerID: f.trustedIssuerID, InternalSessionIssuerID: uuid.Nil, Unavailable: ""}}, order.Providers, "Speakeasy joins the default once it can sign")
	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.Equal(t, remotesessions.EMAProviderExternal, outcome.Provider)
}

func TestProviders_DefaultAddsSpeakeasyLastOnceItSigns(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	issuer := seedSpeakeasyIssuer(t, f, f.req.OrganizationID)
	useSpeakeasySource(f, &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: success, issued: 0})

	order, ok := f.chainer.ProviderOrder(t.Context(), f.req)
	require.True(t, ok)
	require.Equal(t, []remotesessions.EMAProviderKind{remotesessions.EMAProviderExternal, remotesessions.EMAProviderSpeakeasy}, kinds(order))
	require.Equal(t, issuer, order.Providers[1].InternalSessionIssuerID)
}

func TestProviders_ManagementReadMatchesRuntime(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, f devIDPFixture)
	}{
		{name: "default", setup: func(*testing.T, devIDPFixture) {}},
		{name: "saved", setup: func(t *testing.T, f devIDPFixture) {
			t.Helper()
			setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)), external(f.resourceIssuerID), external(f.trustedIssuerID))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newDevIDPFixture(t, devIDPOptions{})
			tc.setup(t, f)

			runtime, ok := f.chainer.ProviderOrder(t.Context(), f.req)
			require.True(t, ok)
			vendor, err := remotesessions.NormalizeVendorIssuer(f.vendorIssuer)
			require.NoError(t, err)
			read, err := remotesessions.EndpointEMAProviders(t.Context(), repo.New(f.db), f.req.OrganizationID, f.req.UserSessionIssuerID, vendor, remotesessions.SpeakeasyEMASigning)
			require.NoError(t, err)
			require.Equal(t, runtime, read)
		})
	}
}

func TestProviders_OrderFallsThroughToExternal(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	src := &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: newOutcome(StageExchange, ReasonAccessDenied, ConfidenceInferred, false), issued: 0}
	useSpeakeasySource(f, src)
	setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)), external(f.trustedIssuerID))

	order, ok := f.chainer.ProviderOrder(t.Context(), f.req)
	require.True(t, ok)
	require.Equal(t, []remotesessions.EMAProviderKind{remotesessions.EMAProviderSpeakeasy, remotesessions.EMAProviderExternal}, kinds(order))
	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.Equal(t, remotesessions.EMAProviderExternal, outcome.Provider)
	require.Equal(t, 1, src.issued)
}

func TestProviders_FallsThroughOnEveryFailure(t *testing.T) {
	t.Parallel()
	denied := newOutcome(StageExchange, ReasonAccessDenied, ConfidenceInferred, false)
	for _, tc := range []struct {
		name   string
		source *failingSource
		issues int
	}{
		{name: "authorization", source: &failingSource{authorizeOutcome: newOutcome(StageAuthorization, ReasonConfigurationRequired, ConfidenceVerified, false), reuseOutcome: success, issueOutcome: success, issued: 0}, issues: 0},
		{name: "reuse check", source: &failingSource{authorizeOutcome: success, reuseOutcome: newOutcome(StageDelegation, ReasonReauthenticationRequired, ConfidenceVerified, false), issueOutcome: success, issued: 0}, issues: 0},
		{name: "policy denial", source: &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: denied, issued: 0}, issues: 1},
		{name: "transient", source: &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: newOutcome(StageExchange, ReasonTransientFailure, ConfidenceInferred, true), issued: 0}, issues: 1},
		{name: "invalid grant", source: &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: newOutcome(StageRedemption, ReasonInvalidGrant, ConfidenceUnknown, false), issued: 0}, issues: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newDevIDPFixture(t, devIDPOptions{})
			useSpeakeasySource(f, tc.source)
			setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)), external(f.trustedIssuerID))

			token, outcome := f.chainer.Acquire(t.Context(), f.req)
			require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
			require.NotEmpty(t, token.Value())
			require.Equal(t, remotesessions.EMAProviderExternal, outcome.Provider)
			require.Equal(t, tc.issues, tc.source.issued)
		})
	}
}

func TestProviders_FirstFailureIsReported(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{unassigned: true})
	useSpeakeasySource(f, &failingSource{authorizeOutcome: newOutcome(StageAuthorization, ReasonConfigurationRequired, ConfidenceVerified, false), reuseOutcome: success, issueOutcome: success, issued: 0})
	setPolicy(t, f, f.req.OrganizationID, external(f.trustedIssuerID), speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))

	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.Equal(t, Outcome{Stage: StageExchange, Reason: ReasonAccessDenied, Confidence: ConfidenceInferred, Retryable: false, Cached: false, Provider: remotesessions.EMAProviderExternal}, outcome)
}

func TestProviders_OrderWithoutAvailableProviderIsNotServed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		reason remotesessions.EMAProviderUnavailableReason
		setup  func(t *testing.T, f devIDPFixture)
	}{
		{name: "speakeasy cannot sign yet", reason: remotesessions.EMAProviderSigningUnavailable, setup: func(t *testing.T, f devIDPFixture) {
			t.Helper()
			setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))
		}},
		{name: "disabled speakeasy issuer", reason: remotesessions.EMAProviderDisabled, setup: func(t *testing.T, f devIDPFixture) {
			t.Helper()
			issuer := seedSpeakeasyIssuer(t, f, f.req.OrganizationID)
			setPolicy(t, f, f.req.OrganizationID, speakeasy(issuer))
			_, err := testrepo.New(f.db).DisableInternalSessionIssuerFixture(t.Context(), testrepo.DisableInternalSessionIssuerFixtureParams{OrganizationID: f.req.OrganizationID, ID: issuer})
			require.NoError(t, err)
		}},
		{name: "an issuer the endpoint does not trust", reason: remotesessions.EMAProviderNotTrustedByEndpoint, setup: func(t *testing.T, f devIDPFixture) {
			t.Helper()
			setPolicy(t, f, f.req.OrganizationID, external(f.resourceIssuerID))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newDevIDPFixture(t, devIDPOptions{})
			tc.setup(t, f)
			requests := f.idp.Requests()

			order, ok := f.chainer.ProviderOrder(t.Context(), f.req)
			require.True(t, ok)
			require.Len(t, order.Providers, 1)
			require.Equal(t, tc.reason, order.Providers[0].Unavailable)
			_, serves := f.chainer.Serves(t.Context(), f.req)
			require.False(t, serves, "the interactive card renders instead")
			_, outcome := f.chainer.Acquire(t.Context(), f.req)
			require.Equal(t, ReasonProviderUnavailable, outcome.Reason)
			require.False(t, outcome.Applicable(), "the interactive path applies unchanged")
			require.Equal(t, requests, f.idp.Requests(), "no provider is contacted")
		})
	}
}

func TestProviders_OrderStopsStoredCredentialReuse(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.True(t, f.chainer.HasUsableCredential(t.Context(), f.req))

	setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))
	require.False(t, f.chainer.HasUsableCredential(t.Context(), f.req), "a credential from a provider the order removed is never usable")
	_, outcome = f.chainer.Acquire(t.Context(), f.req)
	require.Equal(t, ReasonProviderUnavailable, outcome.Reason)
}

func TestProviders_AnotherOrganizationsOrderDoesNotApply(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	other := "org-" + uuid.NewString()
	require.NoError(t, testrepo.New(f.db).SeedDelegationLoaderOrganizationFixture(t.Context(), testrepo.SeedDelegationLoaderOrganizationFixtureParams{OrganizationID: other, Name: "Other organization", Slug: "other-" + uuid.NewString()[:8]}))
	setPolicy(t, f, other, speakeasy(seedSpeakeasyIssuer(t, f, other)))

	order, ok := f.chainer.ProviderOrder(t.Context(), f.req)
	require.True(t, ok)
	require.True(t, order.IsDefault)
	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
}
