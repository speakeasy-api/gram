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

// policyRow is one provider in a vendor policy; connection is uuid.Nil for
// Speakeasy.
type policyRow struct {
	kind       remotesessions.EMAProviderKind
	connection uuid.UUID
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
			OrganizationID: organizationID, VendorIssuer: vendor, ProviderKind: string(row.kind),
			IdentityProviderConnectionID: uuid.NullUUID{UUID: row.connection, Valid: row.connection != uuid.Nil},
			Position:                     int32(i),
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
	f := newDevIDPFixture(t, devIDPOptions{connection: true})
	setPolicy(t, f, f.req.OrganizationID, policyRow{kind: remotesessions.EMAProviderSpeakeasy, connection: uuid.Nil}, policyRow{kind: remotesessions.EMAProviderExternal, connection: f.connectionID})

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
			f := newDevIDPFixture(t, devIDPOptions{connection: true})
			f.chainer.sourceOverrides = map[remotesessions.EMAProviderKind]tokenSource{remotesessions.EMAProviderSpeakeasy: tc.source}
			setPolicy(t, f, f.req.OrganizationID, policyRow{kind: remotesessions.EMAProviderSpeakeasy, connection: uuid.Nil}, policyRow{kind: remotesessions.EMAProviderExternal, connection: f.connectionID})

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
	f := newDevIDPFixture(t, devIDPOptions{unassigned: true, connection: true})
	setPolicy(t, f, f.req.OrganizationID, policyRow{kind: remotesessions.EMAProviderExternal, connection: f.connectionID}, policyRow{kind: remotesessions.EMAProviderSpeakeasy, connection: uuid.Nil})

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
			setPolicy(t, f, f.req.OrganizationID, policyRow{kind: remotesessions.EMAProviderSpeakeasy, connection: uuid.Nil})
		}},
		{name: "deleted connection", serves: false, setup: func(t *testing.T, f devIDPFixture) {
			t.Helper()
			setPolicy(t, f, f.req.OrganizationID, policyRow{kind: remotesessions.EMAProviderExternal, connection: f.connectionID})
			_, err := identityproviderconnectionsrepo.New(f.db).SoftDeleteIdentityProviderConnection(t.Context(), identityproviderconnectionsrepo.SoftDeleteIdentityProviderConnectionParams{ID: f.connectionID, OrganizationID: f.req.OrganizationID})
			require.NoError(t, err)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newDevIDPFixture(t, devIDPOptions{connection: true})
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
	f := newDevIDPFixture(t, devIDPOptions{connection: true})
	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.True(t, f.chainer.HasUsableCredential(t.Context(), f.req))

	setPolicy(t, f, f.req.OrganizationID, policyRow{kind: remotesessions.EMAProviderSpeakeasy, connection: uuid.Nil})
	require.False(t, f.chainer.HasUsableCredential(t.Context(), f.req), "a credential from a provider the policy removed is never usable")
	_, outcome = f.chainer.Acquire(t.Context(), f.req)
	require.Equal(t, ReasonProviderUnavailable, outcome.Reason)
}

func TestProviders_AnotherOrganizationsPolicyDoesNotApply(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{connection: true})
	other := "org-" + uuid.NewString()
	require.NoError(t, testrepo.New(f.db).SeedDelegationLoaderOrganizationFixture(t.Context(), testrepo.SeedDelegationLoaderOrganizationFixtureParams{OrganizationID: other, Name: "Other organization", Slug: "other-" + uuid.NewString()[:8]}))
	setPolicy(t, f, other, policyRow{kind: remotesessions.EMAProviderSpeakeasy, connection: uuid.Nil})

	require.Equal(t, []remotesessions.EMAProviderKind{remotesessions.EMAProviderExternal, remotesessions.EMAProviderSpeakeasy}, f.chainer.Providers(t.Context(), f.req))
	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
}
