package identitychaining

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	identityproviderconnectionsrepo "github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// policyRow is one supplier in a vendor policy: an external remote session
// issuer or the organization's Speakeasy issuer.
type policyRow struct {
	remoteIssuer uuid.UUID
	issuer       uuid.UUID
}

func external(remoteIssuer uuid.UUID) policyRow {
	return policyRow{remoteIssuer: remoteIssuer, issuer: uuid.Nil}
}

func speakeasy(issuer uuid.UUID) policyRow {
	return policyRow{remoteIssuer: uuid.Nil, issuer: issuer}
}

// seedSpeakeasyIssuer creates the organization's Speakeasy issuer row.
func seedSpeakeasyIssuer(t *testing.T, f devIDPFixture, organizationID string) uuid.UUID {
	t.Helper()
	fixtures := testrepo.New(f.db)
	set, err := fixtures.SeedJsonWebKeySetFixture(t.Context(), testrepo.SeedJsonWebKeySetFixtureParams{OrganizationID: organizationID, Name: "speakeasy-issuer-" + uuid.NewString()[:8]})
	require.NoError(t, err)
	issuer, err := fixtures.SeedInternalSessionIssuerFixture(t.Context(), testrepo.SeedInternalSessionIssuerFixtureParams{OrganizationID: organizationID, Issuer: "https://speakeasy.example.test/oauth/idp/org/" + organizationID, JsonWebKeySetID: set})
	require.NoError(t, err)
	return issuer
}

// setPolicy records the organization's ordered provider policy for the
// fixture's vendor.
func setPolicy(t *testing.T, f devIDPFixture, organizationID string, rows ...policyRow) {
	t.Helper()
	vendor, err := remotesessions.NormalizeVendorIssuer(f.vendorIssuer)
	require.NoError(t, err)
	q := identityproviderconnectionsrepo.New(f.db)
	for i, row := range rows {
		require.NoError(t, q.InsertVendorIdentityProvider(t.Context(), identityproviderconnectionsrepo.InsertVendorIdentityProviderParams{
			OrganizationID: organizationID, VendorIssuer: vendor,
			RemoteSessionIssuerID:   uuid.NullUUID{UUID: row.remoteIssuer, Valid: row.remoteIssuer != uuid.Nil},
			InternalSessionIssuerID: uuid.NullUUID{UUID: row.issuer, Valid: row.issuer != uuid.Nil},
			Position:                int32(i),
		}))
	}
}

// failingSource fails at one step and counts the steps it reached.
type failingSource struct {
	authorizeOutcome Outcome
	reuseOutcome     Outcome
	issueOutcome     Outcome
	issued           int
}

func (*failingSource) kind() remotesessions.EMAProviderKind {
	return remotesessions.EMAProviderSpeakeasy
}

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

func TestProviders_DefaultOrderIsExternalThenSpeakeasy(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})

	require.Equal(t, []remotesessions.EMAProviderKind{remotesessions.EMAProviderExternal, remotesessions.EMAProviderSpeakeasy}, f.chainer.Providers(t.Context(), f.req))
	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.Equal(t, remotesessions.EMAProviderExternal, outcome.Provider)
}

func TestProviders_PolicyOrderFallsThroughToExternal(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)), external(f.trustedIssuerID))

	require.Equal(t, []remotesessions.EMAProviderKind{remotesessions.EMAProviderSpeakeasy, remotesessions.EMAProviderExternal}, f.chainer.Providers(t.Context(), f.req))
	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.Equal(t, remotesessions.EMAProviderExternal, outcome.Provider)
	observed := f.observed.all()
	require.Len(t, observed, 1, "an unavailable source is not an attempt")
	require.Equal(t, remotesessions.EMAProviderExternal, observed[0].Provider)
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
			f.chainer.sourceOverrides = map[remotesessions.EMAProviderKind]tokenSource{remotesessions.EMAProviderSpeakeasy: tc.source, remotesessions.EMAProviderExternal: externalSource{c: f.chainer}}
			setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)), external(f.trustedIssuerID))

			token, outcome := f.chainer.Acquire(t.Context(), f.req)
			require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
			require.NotEmpty(t, token.Value())
			require.Equal(t, remotesessions.EMAProviderExternal, outcome.Provider)
			require.Equal(t, tc.issues, tc.source.issued)
		})
	}
}

func TestProviders_FirstRealFailureIsReported(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{unassigned: true})
	setPolicy(t, f, f.req.OrganizationID, external(f.trustedIssuerID), speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))

	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.Equal(t, Outcome{Stage: StageExchange, Reason: ReasonAccessDenied, Confidence: ConfidenceInferred, Retryable: false, Cached: false, Provider: remotesessions.EMAProviderExternal}, outcome)
}

func TestProviders_PolicyWithoutUsableProviderServesNobody(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// serves: an allowed Speakeasy issuer still reads as served; it
		// signs once AIM-523 lands.
		serves bool
		setup  func(t *testing.T, f devIDPFixture)
	}{
		{name: "speakeasy only", serves: true, setup: func(t *testing.T, f devIDPFixture) {
			t.Helper()
			setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))
		}},
		{name: "disabled speakeasy issuer", serves: false, setup: func(t *testing.T, f devIDPFixture) {
			t.Helper()
			issuer := seedSpeakeasyIssuer(t, f, f.req.OrganizationID)
			setPolicy(t, f, f.req.OrganizationID, speakeasy(issuer))
			_, err := testrepo.New(f.db).DisableInternalSessionIssuerFixture(t.Context(), testrepo.DisableInternalSessionIssuerFixtureParams{OrganizationID: f.req.OrganizationID, ID: issuer})
			require.NoError(t, err)
		}},
		{name: "an issuer the endpoint does not trust", serves: false, setup: func(t *testing.T, f devIDPFixture) {
			t.Helper()
			setPolicy(t, f, f.req.OrganizationID, external(f.resourceIssuerID))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newDevIDPFixture(t, devIDPOptions{})
			tc.setup(t, f)
			requests := f.idp.Requests()

			_, outcome := f.chainer.Acquire(t.Context(), f.req)
			require.Equal(t, ReasonProviderUnavailable, outcome.Reason)
			require.False(t, outcome.Applicable(), "the interactive path applies unchanged")
			require.Equal(t, requests, f.idp.Requests(), "no provider is contacted")
			_, serves := f.chainer.Serves(t.Context(), f.req)
			require.Equal(t, tc.serves, serves)
		})
	}
}

func TestProviders_PolicyStopsStoredCredentialReuse(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.True(t, f.chainer.HasUsableCredential(t.Context(), f.req))

	setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))
	require.False(t, f.chainer.HasUsableCredential(t.Context(), f.req), "a credential from a provider the policy removed is never usable")
	_, outcome = f.chainer.Acquire(t.Context(), f.req)
	require.Equal(t, ReasonProviderUnavailable, outcome.Reason)
}

func TestProviders_AnotherOrganizationsPolicyDoesNotApply(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	other := "org-" + uuid.NewString()
	require.NoError(t, testrepo.New(f.db).SeedDelegationLoaderOrganizationFixture(t.Context(), testrepo.SeedDelegationLoaderOrganizationFixtureParams{OrganizationID: other, Name: "Other organization", Slug: "other-" + uuid.NewString()[:8]}))
	setPolicy(t, f, other, speakeasy(seedSpeakeasyIssuer(t, f, other)))

	require.Equal(t, []remotesessions.EMAProviderKind{remotesessions.EMAProviderExternal, remotesessions.EMAProviderSpeakeasy}, f.chainer.Providers(t.Context(), f.req))
	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
}
