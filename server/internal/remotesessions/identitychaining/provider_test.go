package identitychaining

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// policyRow is a saved provider: an external remote session issuer or the
// organization's internal session issuer.
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

// setPolicy saves rows for the fixture's vendor, in order.
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

// useSpeakeasySignIn makes req's endpoint use Speakeasy's own sign-in, the
// only sign-in Speakeasy can vouch from, by clearing its trusted issuer.
func useSpeakeasySignIn(t *testing.T, db usersessionsrepo.DBTX, req Request) {
	t.Helper()
	_, err := usersessionsrepo.New(db).UpdateOrganizationUserSessionIssuer(t.Context(), usersessionsrepo.UpdateOrganizationUserSessionIssuerParams{
		Slug: pgtype.Text{}, AuthnChallengeMode: pgtype.Text{}, SessionDuration: pgtype.Interval{}, ClientIDMetadataAdmissionMode: pgtype.Text{},
		TrustedRemoteSessionIssuerID: pgtype.Text{String: "", Valid: true}, TrustedRemoteSessionClientID: pgtype.Text{String: "", Valid: true},
		ID: req.UserSessionIssuerID, OrganizationID: req.OrganizationID,
	})
	require.NoError(t, err)
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

	// authorized counts the attempts that reached the source at all.
	authorized int
}

func (*failingSource) kind() remotesessions.EMAProviderKind {
	return remotesessions.EMAProviderSpeakeasy
}

func (*failingSource) available() bool { return true }

func (s *failingSource) authorize(context.Context, *slog.Logger, Request, *selection) Outcome {
	s.authorized++
	return s.authorizeOutcome
}

func (s *failingSource) checkReuse(context.Context, Request, selection) Outcome {
	return s.reuseOutcome
}

func (s *failingSource) issue(context.Context, *slog.Logger, Request, selection, func() Outcome) (issuedGrant, Outcome) {
	s.issued++
	return issuedGrant{value: "", trustedSessionID: uuid.Nil, trustedObtainedAt: time.Time{}}, s.issueOutcome
}

// flakySource fails its wrapped source's first issues with a transient
// failure and counts every issue.
type flakySource struct {
	tokenSource
	transient int
	issued    *int
}

func (s flakySource) issue(ctx context.Context, logger *slog.Logger, req Request, sel selection, ready func() Outcome) (issuedGrant, Outcome) {
	*s.issued++
	if *s.issued <= s.transient {
		return issuedGrant{value: "", trustedSessionID: uuid.Nil, trustedObtainedAt: time.Time{}}, newOutcome(StageExchange, ReasonTransientFailure, ConfidenceInferred, true)
	}
	return s.tokenSource.issue(ctx, logger, req, sel, ready)
}

// useFlakyExternal makes the external provider fail its first transient
// issues; it returns the issue count.
func useFlakyExternal(f devIDPFixture, transient int) *int {
	issued := new(int)
	f.chainer.sources[remotesessions.EMAProviderExternal] = func(p remotesessions.EMAProvider) tokenSource {
		return flakySource{tokenSource: externalSource{g: &f.chainer.Governor, c: f.chainer, issuer: p.RemoteSessionIssuerID}, transient: transient, issued: issued}
	}
	return issued
}

func TestProviders_DefaultIsTheEndpointsExternalIssuerAlone(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	seedSpeakeasyIssuer(t, f, f.req.OrganizationID)
	src := &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: success, issued: 0}
	useSpeakeasySource(f, src)

	order, ok, _ := f.chainer.ProviderOrder(t.Context(), f.req)
	require.True(t, ok)
	require.True(t, order.IsDefault)
	require.Equal(t, []remotesessions.EMAProvider{
		{Kind: remotesessions.EMAProviderExternal, RemoteSessionIssuerID: f.trustedIssuerID, InternalSessionIssuerID: uuid.Nil, Unavailable: ""},
	}, order.Providers, "the derived default keeps the endpoint's trusted external issuer alone")
	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.Equal(t, remotesessions.EMAProviderExternal, outcome.Provider)
	require.Zero(t, src.issued, "Speakeasy is not tried while the external issuer serves")
}

func TestProviders_DefaultWithoutSpeakeasyIssuerIsExternalOnly(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})

	order, ok, _ := f.chainer.ProviderOrder(t.Context(), f.req)
	require.True(t, ok)
	require.True(t, order.IsDefault)
	require.Equal(t, []remotesessions.EMAProvider{{Kind: remotesessions.EMAProviderExternal, RemoteSessionIssuerID: f.trustedIssuerID, InternalSessionIssuerID: uuid.Nil, Unavailable: ""}}, order.Providers)
}

func TestProviders_DefaultDoesNotAddSpeakeasyWhenItCanSign(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	seedSpeakeasyIssuer(t, f, f.req.OrganizationID)
	useSpeakeasySource(f, &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: success, issued: 0})

	order, ok, _ := f.chainer.ProviderOrder(t.Context(), f.req)
	require.True(t, ok)
	require.True(t, order.IsDefault)
	require.Equal(t, []remotesessions.EMAProvider{{Kind: remotesessions.EMAProviderExternal, RemoteSessionIssuerID: f.trustedIssuerID, InternalSessionIssuerID: uuid.Nil, Unavailable: "", LastAcceptedAt: nil}}, order.Providers,
		"setting up the Speakeasy issuer never adds an authority behind an endpoint's trusted external issuer")
}

func TestProviders_DefaultNeverRetriesAnExternalDenialWithSpeakeasy(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	seedSpeakeasyIssuer(t, f, f.req.OrganizationID)
	denied := &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: newOutcome(StageExchange, ReasonAccessDenied, ConfidenceInferred, false), issued: 0}
	f.chainer.sources[remotesessions.EMAProviderExternal] = func(remotesessions.EMAProvider) tokenSource { return denied }
	src := &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: success, issued: 0}
	useSpeakeasySource(f, src)

	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.False(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.Equal(t, ReasonAccessDenied, outcome.Reason)
	require.Equal(t, 1, denied.issued)
	require.Zero(t, src.issued, "with no saved order the identity provider's denial stands")
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

			runtime, ok, _ := f.chainer.ProviderOrder(t.Context(), f.req)
			require.True(t, ok)
			vendor, err := remotesessions.NormalizeVendorIssuer(f.vendorIssuer)
			require.NoError(t, err)
			read, err := remotesessions.EndpointEMAProviders(t.Context(), repo.New(f.db), f.req.OrganizationID, f.req.UserSessionIssuerID, vendor, f.chainer.sourceAvailable(remotesessions.EMAProviderSpeakeasy))
			require.NoError(t, err)
			require.Equal(t, runtime, read)
		})
	}
}

func TestProviders_AnOrderWithoutExternalNeverUsesIt(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	useSpeakeasySignIn(t, f.db, f.req)
	src := &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: newOutcome(StageExchange, ReasonAccessDenied, ConfidenceInferred, false), issued: 0}
	useSpeakeasySource(f, src)
	setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))
	requests := f.idp.Requests()

	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.False(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.Equal(t, remotesessions.EMAProviderSpeakeasy, outcome.Provider)
	require.Equal(t, 1, src.issued)
	require.Equal(t, requests, f.idp.Requests(), "the external issuer is never contacted")
}

func TestProviders_SpeakeasyNeverVouchesOnAFederatedSignIn(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	reauth := newOutcome(StageAuthorization, ReasonReauthenticationRequired, ConfidenceVerified, false)
	src := &failingSource{authorizeOutcome: reauth, reuseOutcome: reauth, issueOutcome: success, issued: 0}
	useSpeakeasySource(f, src)
	setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))
	requests := f.idp.Requests()

	_, serves := f.chainer.Serves(t.Context(), f.req)
	require.False(t, serves)
	token, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.Empty(t, token.Value())
	require.Equal(t, ReasonProviderUnavailable, outcome.Reason, "signing in again can never give Speakeasy a sign-in to vouch from")
	require.Equal(t, remotesessions.EMAProviderNotTrustedByEndpoint, outcome.Unavailable)
	require.Zero(t, src.authorized, "the Speakeasy source is never invoked")
	require.Equal(t, requests, f.idp.Requests(), "no provider is contacted")
}

func TestProviders_TransientFailureIsRetriedThenSucceeds(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	issued := useFlakyExternal(f, 1)

	token, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.NotEmpty(t, token.Value())
	require.Equal(t, remotesessions.EMAProviderExternal, outcome.Provider)
	require.Equal(t, 2, *issued, "one transient failure is retried against the same provider")
}

func TestProviders_SpeakeasyAheadOfTheSignInsIssuerIsSkipped(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	src := &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: success, issued: 0}
	useSpeakeasySource(f, src)
	setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)), external(f.trustedIssuerID))

	order, ok, _ := f.chainer.ProviderOrder(t.Context(), f.req)
	require.True(t, ok)
	require.Equal(t, remotesessions.EMAProviderNotTrustedByEndpoint, order.Providers[0].Unavailable)
	token, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.NotEmpty(t, token.Value())
	require.Equal(t, remotesessions.EMAProviderExternal, outcome.Provider, "the sign-in's own issuer serves")
	require.Zero(t, src.authorized, "Speakeasy is never tried on a federated sign-in")
}

func TestProviders_FirstProviderThatSucceedsServes(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	src := &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: success, issued: 0}
	useSpeakeasySource(f, src)
	setPolicy(t, f, f.req.OrganizationID, external(f.trustedIssuerID), speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))

	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.Equal(t, remotesessions.EMAProviderExternal, outcome.Provider)
	require.Zero(t, src.issued, "later providers are not tried once one succeeds")
}

func TestProviders_UntrustedProviderIsSkipped(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	untrusted := &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: success, issued: 0}
	issued := new(int)
	f.chainer.sources[remotesessions.EMAProviderExternal] = func(p remotesessions.EMAProvider) tokenSource {
		if p.RemoteSessionIssuerID != f.trustedIssuerID {
			return untrusted
		}
		return flakySource{tokenSource: externalSource{g: &f.chainer.Governor, c: f.chainer, issuer: p.RemoteSessionIssuerID}, transient: 0, issued: issued}
	}
	setPolicy(t, f, f.req.OrganizationID, external(f.resourceIssuerID), external(f.trustedIssuerID))

	order, ok, _ := f.chainer.ProviderOrder(t.Context(), f.req)
	require.True(t, ok)
	require.Equal(t, remotesessions.EMAProviderNotTrustedByEndpoint, order.Providers[0].Unavailable)
	require.True(t, order.Providers[1].Available())
	_, serves := f.chainer.Serves(t.Context(), f.req)
	require.True(t, serves, "a trusted provider later in the order serves")

	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.Zero(t, untrusted.issued, "an issuer the endpoint does not trust is never asked")
	require.Equal(t, 1, *issued)
}

func TestProviders_AllProvidersFailReportsEachReason(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{unassigned: true})
	src := &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: newOutcome(StageRedemption, ReasonInvalidGrant, ConfidenceUnknown, false), issued: 0}
	useSpeakeasySource(f, src)
	setPolicy(t, f, f.req.OrganizationID, external(f.resourceIssuerID), external(f.trustedIssuerID), speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))

	token, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.Empty(t, token.Value())
	require.Equal(t, ReasonAccessDenied, outcome.Reason, "the first provider actually tried explains the failure")
	require.Equal(t, remotesessions.EMAProviderExternal, outcome.Provider)
	require.True(t, outcome.Applicable(), "an attempted provider's failure is reported, not the interactive fallback")
	require.Equal(t, []ProviderFailure{
		{Provider: remotesessions.EMAProviderExternal, Reason: ReasonProviderUnavailable, Unavailable: remotesessions.EMAProviderNotTrustedByEndpoint},
		{Provider: remotesessions.EMAProviderExternal, Reason: ReasonAccessDenied, Unavailable: ""},
		{Provider: remotesessions.EMAProviderSpeakeasy, Reason: ReasonProviderUnavailable, Unavailable: remotesessions.EMAProviderNotTrustedByEndpoint},
	}, outcome.Failures, "every provider's reason is reported")
	require.Zero(t, src.authorized)
}

func TestProviders_SkippedSpeakeasyNeverAsksToSignInAgain(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{unassigned: true})
	reauth := newOutcome(StageAuthorization, ReasonReauthenticationRequired, ConfidenceVerified, false)
	src := &failingSource{authorizeOutcome: reauth, reuseOutcome: reauth, issueOutcome: success, issued: 0}
	useSpeakeasySource(f, src)
	setPolicy(t, f, f.req.OrganizationID, external(f.trustedIssuerID), speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))

	token, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.Empty(t, token.Value())
	require.Equal(t, ReasonAccessDenied, outcome.Reason, "the sign-in's issuer explains the failure; Speakeasy never asks for a sign-in it cannot use")
	require.Equal(t, remotesessions.EMAProviderExternal, outcome.Provider)
	require.Equal(t, []ProviderFailure{
		{Provider: remotesessions.EMAProviderExternal, Reason: ReasonAccessDenied, Unavailable: ""},
		{Provider: remotesessions.EMAProviderSpeakeasy, Reason: ReasonProviderUnavailable, Unavailable: remotesessions.EMAProviderNotTrustedByEndpoint},
	}, outcome.Failures, "every provider's reason is reported")
	require.Zero(t, src.authorized)
}

func TestProviders_RecentKeyRedemptionFailureIsNotCached(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		sinceKey time.Duration
		issues   int32
	}{
		{name: "recent key", sinceKey: time.Minute, issues: 2},
		{name: "key past the vendor refresh window", sinceKey: recentKeyWindow + time.Minute, issues: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lf := newSpeakeasyLiveFixture(t)
			lf.rejectRedemption.Store(new(oautherr.CodeInvalidGrant))
			lf.chainer.now = func() time.Time { return lf.status.ActiveKey.CreatedAt.Time.Add(tc.sinceKey) }
			grant := lf.newGrant(t, lf.signIn.ID)

			_, first := lf.chainer.Acquire(t.Context(), grant)
			lf.checkExchange()
			require.Equal(t, ReasonInvalidGrant, first.Reason)
			_, second := lf.chainer.Acquire(t.Context(), grant)
			if tc.issues == 2 {
				lf.checkExchange()
			}
			require.Equal(t, ReasonInvalidGrant, second.Reason)
			require.Equal(t, tc.issues == 1, second.Cached)
			require.Equal(t, tc.issues, lf.calls.Load())
		})
	}
}

func TestProviders_AnOrderWithNoServingProviderIsNotServed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		reasons []remotesessions.EMAProviderUnavailableReason
		setup   func(t *testing.T, f devIDPFixture)
	}{
		{name: "speakeasy cannot sign yet", reasons: []remotesessions.EMAProviderUnavailableReason{remotesessions.EMAProviderSigningUnavailable}, setup: func(t *testing.T, f devIDPFixture) {
			t.Helper()
			useSpeakeasySignIn(t, f.db, f.req)
			setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))
		}},
		{name: "disabled speakeasy issuer", reasons: []remotesessions.EMAProviderUnavailableReason{remotesessions.EMAProviderDisabled}, setup: func(t *testing.T, f devIDPFixture) {
			t.Helper()
			issuer := seedSpeakeasyIssuer(t, f, f.req.OrganizationID)
			setPolicy(t, f, f.req.OrganizationID, speakeasy(issuer))
			_, err := testrepo.New(f.db).DisableInternalSessionIssuerFixture(t.Context(), testrepo.DisableInternalSessionIssuerFixtureParams{OrganizationID: f.req.OrganizationID, ID: issuer})
			require.NoError(t, err)
		}},
		{name: "only providers the endpoint's sign-in does not go through", reasons: []remotesessions.EMAProviderUnavailableReason{remotesessions.EMAProviderNotTrustedByEndpoint, remotesessions.EMAProviderNotTrustedByEndpoint}, setup: func(t *testing.T, f devIDPFixture) {
			t.Helper()
			setPolicy(t, f, f.req.OrganizationID, external(f.resourceIssuerID), speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newDevIDPFixture(t, devIDPOptions{})
			tc.setup(t, f)
			requests := f.idp.Requests()

			order, ok, _ := f.chainer.ProviderOrder(t.Context(), f.req)
			require.True(t, ok)
			reasons := make([]remotesessions.EMAProviderUnavailableReason, 0, len(order.Providers))
			for _, p := range order.Providers {
				reasons = append(reasons, p.Unavailable)
			}
			require.Equal(t, tc.reasons, reasons)
			_, serves := f.chainer.Serves(t.Context(), f.req)
			require.False(t, serves, "the interactive card renders instead")
			_, outcome := f.chainer.Acquire(t.Context(), f.req)
			require.Equal(t, ReasonProviderUnavailable, outcome.Reason)
			require.Equal(t, tc.reasons[0], outcome.Unavailable, "the specific reason is kept")
			require.False(t, outcome.Applicable(), "the interactive path applies unchanged")
			require.Equal(t, requests, f.idp.Requests(), "no provider is contacted")
		})
	}
}

func TestProviders_AnOrderWithoutTheIssuingProviderStopsReuse(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.True(t, f.chainer.HasUsableCredential(t.Context(), f.req))

	setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))
	require.False(t, f.chainer.HasUsableCredential(t.Context(), f.req), "a credential from a provider no longer in the order is never usable")
	_, outcome = f.chainer.Acquire(t.Context(), f.req)
	require.Equal(t, ReasonProviderUnavailable, outcome.Reason)
}

func TestProviders_FallbackCredentialIsReusedWhileItsProviderServes(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	src := &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: newOutcome(StageExchange, ReasonTransientFailure, ConfidenceInferred, true), issued: 0}
	useSpeakeasySource(f, src)
	setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)), external(f.trustedIssuerID))

	first, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.Equal(t, remotesessions.EMAProviderExternal, outcome.Provider)
	issued, requests := src.issued, f.idp.Requests()
	require.True(t, f.chainer.HasUsableCredential(t.Context(), f.req), "a fallback provider's credential counts while it serves")

	second, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.Equal(t, first.Value(), second.Value(), "the fallback provider's credential is reused")
	require.Equal(t, issued, src.issued, "the failing first provider is not retried while a stored credential serves")
	require.Equal(t, requests, f.idp.Requests(), "reuse contacts no provider")
}

// interceptingSource runs afterIssue once its wrapped source has issued, so a
// test can change the order while the attempt is between issue and publish.
type interceptingSource struct {
	tokenSource
	afterIssue func()
}

func (s interceptingSource) issue(ctx context.Context, logger *slog.Logger, req Request, sel selection, ready func() Outcome) (issuedGrant, Outcome) {
	grant, outcome := s.tokenSource.issue(ctx, logger, req, sel, ready)
	s.afterIssue()
	return grant, outcome
}

func TestProviders_OrderChangedMidAttemptDiscardsTheToken(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	internalIssuer := seedSpeakeasyIssuer(t, f, f.req.OrganizationID)
	f.chainer.sources[remotesessions.EMAProviderExternal] = func(p remotesessions.EMAProvider) tokenSource {
		return interceptingSource{
			tokenSource: externalSource{g: &f.chainer.Governor, c: f.chainer, issuer: p.RemoteSessionIssuerID},
			afterIssue:  func() { setPolicy(t, f, f.req.OrganizationID, speakeasy(internalIssuer)) },
		}
	}

	token, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.Equal(t, ReasonStaleConfiguration, outcome.Reason, "a provider removed mid-attempt never publishes or releases its token")
	require.Empty(t, token.Value())
	require.False(t, f.chainer.HasUsableCredential(t.Context(), f.req))
	_, outcome = f.chainer.Acquire(t.Context(), f.req)
	require.Equal(t, ReasonProviderUnavailable, outcome.Reason)
}

func TestCacheKey_ExternalKeepsTheLegacyFormat(t *testing.T) {
	t.Parallel()
	req := Request{OrganizationID: "org", ProjectID: uuid.New(), UserSessionIssuerID: uuid.New(), UserID: "user", UpstreamResource: "https://upstream.example.test/mcp", RemoteSessionIssuerID: uuid.NullUUID{}}
	sel := selection{bindingID: uuid.New(), generation: 3, trustedIssuerID: uuid.New(), trustedClientID: uuid.New(), audience: "aud", clientID: uuid.New(), resource: "https://upstream.example.test/mcp", scopes: []string{"read"}}
	legacy := sha256.Sum256([]byte(strings.Join([]string{
		req.OrganizationID, req.ProjectID.String(), req.UserSessionIssuerID.String(), req.UserID,
		sel.bindingID.String(), strconv.FormatInt(sel.generation, 10),
		sel.trustedIssuerID.String(), sel.trustedClientID.String(), sel.audience,
		sel.clientID.String(), sel.resource, strings.Join(sel.scopes, " "),
	}, "\n")))

	sel.provider = remotesessions.EMAProvider{Kind: remotesessions.EMAProviderExternal, RemoteSessionIssuerID: sel.trustedIssuerID, InternalSessionIssuerID: uuid.Nil, Unavailable: ""}
	require.Equal(t, "lock:"+hex.EncodeToString(legacy[:]), cacheKey("lock", req, sel), "old and new processes share single flight")
	external := cacheKey("lock", req, sel)
	sel.provider.Kind = remotesessions.EMAProviderSpeakeasy
	require.NotEqual(t, external, cacheKey("lock", req, sel))
}

func TestPublish_SerializesWithOrderChanges(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	ctx := t.Context()
	fixtures := testrepo.New(f.db)
	set, err := fixtures.SeedJsonWebKeySetFixture(ctx, testrepo.SeedJsonWebKeySetFixtureParams{OrganizationID: f.req.OrganizationID, Name: "speakeasy-" + uuid.NewString()[:8]})
	require.NoError(t, err)
	internalIssuer, err := fixtures.SeedInternalSessionIssuerFixture(ctx, testrepo.SeedInternalSessionIssuerFixtureParams{OrganizationID: f.req.OrganizationID, Issuer: "https://speakeasy.example.test/oauth/idp/org/" + f.req.OrganizationID, JsonWebKeySetID: set})
	require.NoError(t, err)
	vendor, err := remotesessions.NormalizeVendorIssuer(f.sel.issuer)
	require.NoError(t, err)

	// An order change holds the lock, as setEMAProviders does.
	tx := testenv.BeginTx(t, ctx, f.db)
	q := repo.New(tx)
	require.NoError(t, q.LockEMAProviders(ctx, f.req.OrganizationID))

	published := make(chan error, 1)
	go func() {
		published <- f.chainer.publish(ctx, f.chainer.logger, f.req, f.sel, f.credential("raced-token"))
	}()
	require.Eventually(t, func() bool {
		waiting, err := fixtures.CountWaitingAdvisoryLocksFixture(ctx)
		return err == nil && waiting > 0
	}, 5*time.Second, 20*time.Millisecond, "publish waits for the order change")

	require.NoError(t, q.InsertEMAProvider(ctx, repo.InsertEMAProviderParams{
		OrganizationID: f.req.OrganizationID, VendorIssuer: vendor,
		RemoteSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		InternalSessionIssuerID: uuid.NullUUID{UUID: internalIssuer, Valid: true},
		Position:                0,
	}))
	require.NoError(t, tx.Commit(ctx))

	require.ErrorIs(t, <-published, errStale, "the write sees the committed order, which no longer names the provider")
	_, err = repo.New(f.db).GetEMACredentialForUse(ctx, credentialForUseParams(f.req, f.sel, time.Now()))
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestPublish_SerializesWithUnlink(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	ctx := t.Context()

	// An unlink holds the binding row, as preparation does.
	tx := testenv.BeginTx(t, ctx, f.db)
	q := repo.New(tx)
	key := repo.GetEMABindingParams{ProjectID: f.req.ProjectID, OrganizationID: f.req.OrganizationID, UserSessionIssuerID: f.req.UserSessionIssuerID, RemoteSessionIssuerID: f.sel.remoteIssuerID, Resource: f.sel.resource}
	current, err := q.LockEMABinding(ctx, repo.LockEMABindingParams(key))
	require.NoError(t, err)

	published := make(chan error, 1)
	go func() {
		published <- f.chainer.publish(ctx, f.chainer.logger, f.req, f.sel, f.credential("raced-token"))
	}()
	testenv.WaitForQueryBlockedBy(t, ctx, f.db, testenv.BackendPID(tx), "%LockEMABindingForPublish :one%")

	_, err = q.SetEMABinding(ctx, repo.SetEMABindingParams{
		State: pgtype.Text{String: remotesessions.PreparationStateUnlinked, Valid: true}, RemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Generation: current.Generation + 1, GrantSource: current.GrantSource, RequestedScopes: current.RequestedScopes,
		ClaimID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ClaimedAt: pgtype.Timestamptz{},
		ID: current.ID, ProjectID: f.req.ProjectID, OrganizationID: f.req.OrganizationID, ExpectedGeneration: current.Generation,
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	require.ErrorIs(t, <-published, errStale, "a publish that waited on an unlink sees the unlinked binding")
	require.Empty(t, f.credentials(t), "no credential is stored for an unlinked binding")
}

// revokedAfterPublishSource wraps a source whose authority holds for the
// pre-exchange check and fails every check after it.
type revokedAfterPublishSource struct {
	tokenSource
	checks *int
}

func (s revokedAfterPublishSource) checkReuse(ctx context.Context, req Request, sel selection) Outcome {
	*s.checks++
	if *s.checks > 1 {
		return newOutcome(StageDelegation, ReasonReauthenticationRequired, ConfidenceVerified, false)
	}
	return s.tokenSource.checkReuse(ctx, req, sel)
}

func TestProviders_CredentialIsRetiredWhenItsAuthorityFailsAfterPublish(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	checks := 0
	external := f.chainer.sources[remotesessions.EMAProviderExternal]
	f.chainer.sources[remotesessions.EMAProviderExternal] = func(p remotesessions.EMAProvider) tokenSource {
		return revokedAfterPublishSource{tokenSource: external(p), checks: &checks}
	}

	token, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.Empty(t, token.Value())
	require.Equal(t, ReasonReauthenticationRequired, outcome.Reason)
	require.Equal(t, 2, checks, "the delegation is rechecked after the publish")

	rows, err := repo.New(f.db).ListEMACredentialsFixture(t.Context(), uuid.NullUUID{UUID: f.req.ProjectID, Valid: true})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].Deleted, "the credential published under revoked authority is retired")
	require.False(t, rows[0].AccessTokenEncrypted.Valid, "its ciphertext is erased")
}
