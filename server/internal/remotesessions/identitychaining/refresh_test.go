package identitychaining

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestRefreshDecisionFor(t *testing.T) {
	t.Parallel()
	reauth := newOutcome(StageDelegation, ReasonReauthenticationRequired, ConfidenceVerified, false)
	refused := newOutcome(StageExchange, ReasonAccessDenied, ConfidenceVerified, false)
	refused.Cached = true
	configuration := newOutcome(StageSelection, ReasonConfigurationRequired, ConfidenceVerified, false)
	// A reconnected interactive session routes without chaining, so even a
	// refusal no sign-in repairs is served once the human reconnects.
	cachedRefusal := func(stage Stage, reason Reason) Outcome {
		o := newOutcome(stage, reason, ConfidenceVerified, false)
		o.Cached = true
		return o
	}
	for _, tc := range []struct {
		name     string
		failures []Outcome
		want     RefreshDecision
	}{
		{"signing paused", []Outcome{unavailableBecause(remotesessions.EMAProviderSigningPaused)}, RefreshUnconfirmed},
		{"keyless", []Outcome{unavailableBecause(remotesessions.EMAProviderKeyless)}, RefreshUnconfirmed},
		{"readiness unknown", []Outcome{unavailableBecause(remotesessions.EMAProviderReadinessUnknown)}, RefreshUnconfirmed},
		{"sign-in enforcement unread or off", []Outcome{unavailableBecause(remotesessions.EMAProviderSignInEnforcement)}, RefreshUnconfirmed},
		{"deleted", []Outcome{unavailableBecause(remotesessions.EMAProviderDeleted)}, RefreshNotChained},
		{"disabled", []Outcome{unavailableBecause(remotesessions.EMAProviderDisabled)}, RefreshNotChained},
		{"not trusted by the endpoint", []Outcome{unavailableBecause(remotesessions.EMAProviderNotTrustedByEndpoint)}, RefreshNotChained},
		{"deployment cannot sign", []Outcome{unavailableBecause(remotesessions.EMAProviderSigningUnavailable)}, RefreshNotChained},
		{"not rolled out", []Outcome{unavailableBecause(remotesessions.EMAProviderNotRolledOut)}, RefreshNotChained},
		{"verified domain required", []Outcome{unavailableBecause(remotesessions.EMAProviderVerifiedDomain)}, RefreshNotChained},
		{"setup required", []Outcome{unavailableBecause(remotesessions.EMAProviderSetupRequired)}, RefreshNotChained},
		{"no reason", []Outcome{providerUnavailable}, RefreshNotChained},
		{"paused before a provider needing a sign-in", []Outcome{unavailableBecause(remotesessions.EMAProviderSigningPaused), reauth}, RefreshReauthenticate},
		{"deleted after a paused provider", []Outcome{unavailableBecause(remotesessions.EMAProviderDeleted), unavailableBecause(remotesessions.EMAProviderSigningPaused)}, RefreshUnconfirmed},
		{"recent refusal", []Outcome{refused}, RefreshRefused},
		{"recent refusal after a disabled provider", []Outcome{unavailableBecause(remotesessions.EMAProviderDisabled), refused}, RefreshRefused},
		{"recent refusal while another provider is paused", []Outcome{refused, unavailableBecause(remotesessions.EMAProviderSigningPaused)}, RefreshUnconfirmed},
		{"configuration failure", []Outcome{configuration}, RefreshUnconfirmed},
		{"recent configuration refusal", []Outcome{cachedRefusal(StageExchange, ReasonConfigurationRequired)}, RefreshRefused},
		{"recent invalid target", []Outcome{cachedRefusal(StageExchange, ReasonInvalidTarget)}, RefreshRefused},
		{"recent unknown rejection", []Outcome{cachedRefusal(StageRedemption, ReasonUnknownRejection)}, RefreshRefused},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, refreshDecisionFor(exhausted(tc.failures), tc.failures))
		})
	}
}

// saveOrder saves rows as the fixture vendor's provider order.
func (f chainStoreFixture) saveOrder(t *testing.T, rows ...policyRow) {
	t.Helper()
	vendor, err := remotesessions.NormalizeVendorIssuer(testResourceIssuer)
	require.NoError(t, err)
	for i, row := range rows {
		require.NoError(t, repo.New(f.db).InsertEMAProvider(t.Context(), repo.InsertEMAProviderParams{
			OrganizationID: f.req.OrganizationID, VendorIssuer: vendor,
			RemoteSessionIssuerID:   uuid.NullUUID{UUID: row.remoteIssuer, Valid: row.remoteIssuer != uuid.Nil},
			InternalSessionIssuerID: uuid.NullUUID{UUID: row.internalIssuer, Valid: row.internalIssuer != uuid.Nil},
			Position:                int32(i),
		}))
	}
}

// seedSpeakeasyIssuer creates the fixture organization's internal session issuer.
func (f chainStoreFixture) seedSpeakeasyIssuer(t *testing.T) uuid.UUID {
	t.Helper()
	fixtures := testrepo.New(f.db)
	keys, err := fixtures.SeedJsonWebKeySetFixture(t.Context(), testrepo.SeedJsonWebKeySetFixtureParams{OrganizationID: f.req.OrganizationID, Name: "speakeasy-issuer-" + uuid.NewString()[:8]})
	require.NoError(t, err)
	internal, err := fixtures.SeedInternalSessionIssuerFixture(t.Context(), testrepo.SeedInternalSessionIssuerFixtureParams{OrganizationID: f.req.OrganizationID, Issuer: "https://speakeasy.example.test/oauth/idp/org/" + f.req.OrganizationID, JsonWebKeySetID: keys})
	require.NoError(t, err)
	return internal
}

// speakeasyAnswers makes g's Speakeasy provider answer authorization with outcome.
func speakeasyAnswers(g *Governor, outcome Outcome) {
	g.sources[remotesessions.EMAProviderSpeakeasy] = func(remotesessions.EMAProvider) tokenSource {
		return &failingSource{authorizeOutcome: outcome, reuseOutcome: outcome, issueOutcome: outcome, issued: 0}
	}
}

func TestGovernor_RefreshDecisionSpeakeasyUnavailability(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		reason remotesessions.EMAProviderUnavailableReason
		want   RefreshDecision
	}{
		{remotesessions.EMAProviderSigningPaused, RefreshUnconfirmed},
		{remotesessions.EMAProviderKeyless, RefreshUnconfirmed},
		{remotesessions.EMAProviderSignInEnforcement, RefreshUnconfirmed},
		{remotesessions.EMAProviderDisabled, RefreshNotChained},
		{remotesessions.EMAProviderSetupRequired, RefreshNotChained},
	} {
		t.Run(string(tc.reason), func(t *testing.T) {
			t.Parallel()
			f := newChainStoreFixture(t)
			useSpeakeasySignIn(t, f.db, f.req)
			f.saveOrder(t, speakeasy(f.seedSpeakeasyIssuer(t)))
			g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
			speakeasyAnswers(g, unavailableBecause(tc.reason))
			verdict := g.RefreshDecision(t.Context(), f.req)
			require.Equal(t, tc.want, verdict.Decision)
			require.Equal(t, remotesessions.EMAProviderSpeakeasy, verdict.Provider)
			require.Equal(t, ReasonProviderUnavailable, verdict.Reason)
		})
	}
}

func TestGovernor_RefreshDecisionFollowsTheProviderOrder(t *testing.T) {
	t.Parallel()
	externalKind, speakeasyKind := remotesessions.EMAProviderExternal, remotesessions.EMAProviderSpeakeasy
	t.Run("speakeasy unable to sign before a trusted external", func(t *testing.T) {
		t.Parallel()
		f := newChainStoreFixture(t)
		f.retainRefreshableDelegation(t, "durable_credential_present")
		f.saveOrder(t, speakeasy(f.seedSpeakeasyIssuer(t)), policyRow{remoteIssuer: f.sel.trustedIssuerID, internalIssuer: uuid.Nil})
		g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
		require.Equal(t, RefreshVerdict{Decision: RefreshServed, RemoteSessionIssuerID: f.sel.remoteIssuerID, Provider: externalKind, Reason: ReasonSuccess}, g.RefreshDecision(t.Context(), f.req))
	})
	t.Run("serving speakeasy on a federated sign-in", func(t *testing.T) {
		t.Parallel()
		f := newChainStoreFixture(t)
		f.saveOrder(t, speakeasy(f.seedSpeakeasyIssuer(t)), policyRow{remoteIssuer: f.sel.trustedIssuerID, internalIssuer: uuid.Nil})
		g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
		speakeasyAnswers(g, success)
		require.Equal(t, externalKind, g.RefreshDecision(t.Context(), f.req).Provider, "Speakeasy never vouches for a federated sign-in")
	})
	t.Run("trusted external before a serving speakeasy", func(t *testing.T) {
		t.Parallel()
		f := newChainStoreFixture(t)
		f.retainRefreshableDelegation(t, "durable_credential_present")
		f.saveOrder(t, policyRow{remoteIssuer: f.sel.trustedIssuerID, internalIssuer: uuid.Nil}, speakeasy(f.seedSpeakeasyIssuer(t)))
		g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
		speakeasyAnswers(g, success)
		require.Equal(t, externalKind, g.RefreshDecision(t.Context(), f.req).Provider, "the first provider that serves decides")
	})
	t.Run("external needing a sign-in never falls through to speakeasy", func(t *testing.T) {
		t.Parallel()
		f := newChainStoreFixture(t)
		f.saveOrder(t, policyRow{remoteIssuer: f.sel.trustedIssuerID, internalIssuer: uuid.Nil}, speakeasy(f.seedSpeakeasyIssuer(t)))
		g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
		speakeasyAnswers(g, success)
		verdict := g.RefreshDecision(t.Context(), f.req)
		require.Equal(t, RefreshReauthenticate, verdict.Decision)
		require.Equal(t, externalKind, verdict.Provider)
	})
	t.Run("speakeasy serves a speakeasy sign-in behind an external", func(t *testing.T) {
		t.Parallel()
		f := newChainStoreFixture(t)
		useSpeakeasySignIn(t, f.db, f.req)
		f.saveOrder(t, policyRow{remoteIssuer: f.sel.trustedIssuerID, internalIssuer: uuid.Nil}, speakeasy(f.seedSpeakeasyIssuer(t)))
		g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
		speakeasyAnswers(g, success)
		verdict := g.RefreshDecision(t.Context(), f.req)
		require.Equal(t, RefreshServed, verdict.Decision)
		require.Equal(t, speakeasyKind, verdict.Provider)
	})
}

func TestGovernor_RefreshDecisionStoredCredentialMustDecrypt(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	f.retainRefreshableDelegation(t, "assertion_only")
	require.NoError(t, f.chainer.publish(t.Context(), f.chainer.logger, f.req, f.sel, f.credential("downstream-token")))
	healthy := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
	require.Equal(t, RefreshServed, healthy.RefreshDecision(t.Context(), f.req).Decision, "a usable stored credential serves after the assertion expires")

	rotated, err := encryption.NewWithBytes(bytes.Repeat([]byte{0x24}, 32))
	require.NoError(t, err)
	g := NewGovernor(testenv.NewLogger(t), f.db, rotated, f.chainer.delegation)
	verdict := g.RefreshDecision(t.Context(), f.req)
	require.Equal(t, RefreshReauthenticate, verdict.Decision, "a stored credential the runtime cannot decrypt does not stand in for the expired assertion")
	require.Equal(t, ReasonReauthenticationRequired, verdict.Reason)
}

func TestGovernor_RefreshDecisionEmptyStoredCredential(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	f.retainRefreshableDelegation(t, "assertion_only")
	require.NoError(t, f.chainer.publish(t.Context(), f.chainer.logger, f.req, f.sel, f.credential("")))
	g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
	require.Equal(t, RefreshReauthenticate, g.RefreshDecision(t.Context(), f.req).Decision, "an empty stored credential is unusable")
}

func TestChainer_RefreshDecisionAfterARecentRefusal(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	f.retainRefreshableDelegation(t, "durable_credential_present")
	c := New(testenv.NewLogger(t), f.db, f.chainer.enc, nil, f.chainer.delegation, nil, testenv.NewMemoryCache())
	require.Equal(t, RefreshServed, c.RefreshDecision(t.Context(), f.req).Decision)

	refused := newOutcome(StageExchange, ReasonAccessDenied, ConfidenceVerified, false)
	require.NoError(t, c.locks.Set(t.Context(), cacheKey("identityChainingFailure", f.req, f.sel), refused, time.Minute))
	require.Equal(t, RefreshVerdict{Decision: RefreshRefused, RemoteSessionIssuerID: f.sel.remoteIssuerID, Provider: remotesessions.EMAProviderExternal, Reason: ReasonAccessDenied}, c.RefreshDecision(t.Context(), f.req),
		"a provider that just refused for good does not serve the refreshed grant")

	require.NoError(t, f.chainer.publish(t.Context(), f.chainer.logger, f.req, f.sel, f.credential("downstream-token")))
	require.Equal(t, RefreshServed, c.RefreshDecision(t.Context(), f.req).Decision, "a usable stored credential still serves, as at runtime")
}
