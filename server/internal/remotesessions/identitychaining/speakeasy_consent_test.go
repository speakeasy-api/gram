package identitychaining

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/signins"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// consentRequest is the consent page's request: the human, before any grant.
func (lf *speakeasyLiveFixture) consentRequest() Request {
	r := lf.f.req
	r.UserSessionJTI = ""
	return r
}

func (lf *speakeasyLiveFixture) recordSignIn(t *testing.T, workOSSessionID string) signins.Recorded {
	t.Helper()
	at := time.Now().Add(-time.Minute)
	signIn, err := signins.Record(t.Context(), lf.f.db, signins.RecordParams{OrganizationID: lf.f.req.OrganizationID, UserID: lf.f.req.UserID, WorkOSSessionID: workOSSessionID, AuthenticationMethod: signins.MethodSSO, AuthenticatedAt: at, Evidence: signins.Evidence{VerifiedEmail: lf.f.req.UserID + "@example.test", IDPAuthenticatedAt: at}})
	require.NoError(t, err)
	return signIn
}

// newGrant creates an MCP grant backed by signInID and returns its request.
func (lf *speakeasyLiveFixture) newGrant(t *testing.T, signInID uuid.NullUUID) Request {
	t.Helper()
	r := lf.f.req
	r.UserSessionJTI = uuid.NewString()
	_, err := usersessionsrepo.New(lf.f.db).CreateUserSession(t.Context(), usersessionsrepo.CreateUserSessionParams{UserSessionIssuerID: r.UserSessionIssuerID, SubjectUrn: urn.NewUserSubject(r.UserID), Jti: r.UserSessionJTI, RefreshExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), ExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), SignInID: signInID, SignInUserID: conv.ToPGText(r.UserID)})
	require.NoError(t, err)
	return r
}

// consent runs the consent check for signInID and requires it to connect.
func (lf *speakeasyLiveFixture) consent(t *testing.T, signInID uuid.UUID, redeems bool) {
	t.Helper()
	outcome := lf.chainer.CheckConsent(t.Context(), lf.consentRequest(), signInID)
	if redeems {
		lf.checkExchange()
	}
	require.True(t, outcome.Succeeded(), "%+v", outcome)
}

func (lf *speakeasyLiveFixture) acquire(t *testing.T, req Request, redeems bool) {
	t.Helper()
	token, outcome := lf.chainer.Acquire(t.Context(), req)
	if redeems {
		lf.checkExchange()
	}
	require.True(t, outcome.Succeeded(), "%+v", outcome)
	require.Equal(t, "live-downstream", token.Value())
}

func TestSpeakeasyConsent_ConcurrentFirstUsesAdoptOnce(t *testing.T) {
	t.Parallel()
	lf := newSpeakeasyLiveFixture(t)
	lf.consent(t, lf.signIn.ID.UUID, true)
	grant := lf.newGrant(t, lf.signIn.ID)

	// firstUses is enough concurrent tool calls to race the adoption.
	const firstUses = 8
	var wg sync.WaitGroup
	outcomes := make([]Outcome, firstUses)
	tokens := make([]Token, firstUses)
	for i := range firstUses {
		wg.Go(func() { tokens[i], outcomes[i] = lf.chainer.Acquire(t.Context(), grant) })
	}
	wg.Wait()
	for i := range firstUses {
		require.True(t, outcomes[i].Succeeded(), "%+v", outcomes[i])
		require.Equal(t, "live-downstream", tokens[i].Value())
	}
	require.EqualValues(t, 1, lf.calls.Load(), "concurrent first uses share the adopted consent credential")
}

func TestSpeakeasyConsent_BindingGenerationChangeBlocksAdoption(t *testing.T) {
	t.Parallel()
	lf := newSpeakeasyLiveFixture(t)
	lf.consent(t, lf.signIn.ID.UUID, true)
	bindResource(t, lf.f.db, lf.f.req, lf.remoteID, lf.clientID, lf.resource, remotesessions.PreparationStateReady)
	lf.acquire(t, lf.newGrant(t, lf.signIn.ID), true)
	require.EqualValues(t, 2, lf.calls.Load(), "a rebind since consent leaves the consent credential unadopted")
}

func TestSpeakeasyConsent_KeyRotationBlocksAdoption(t *testing.T) {
	t.Parallel()
	lf := newSpeakeasyLiveFixture(t)
	lf.consent(t, lf.signIn.ID.UUID, true)
	lf.rotateKey(t)
	lf.acquire(t, lf.newGrant(t, lf.signIn.ID), true)
	require.EqualValues(t, 2, lf.calls.Load(), "a key rotated since consent leaves the consent credential unadopted")
}

// rotateKey retires the issuer's active signing key and activates another.
func (lf *speakeasyLiveFixture) rotateKey(t *testing.T) {
	t.Helper()
	ctx := t.Context()
	q := jwksrepo.New(lf.f.db)
	_, err := q.RetireJsonWebKey(ctx, jwksrepo.RetireJsonWebKeyParams{ID: lf.status.ActiveKey.ID, OrganizationID: lf.f.req.OrganizationID})
	require.NoError(t, err)
	var key jose.JSONWebKey
	require.NoError(t, key.UnmarshalJSON(lf.publicJWK))
	key.KeyID = "test-key-rotated"
	rotated, err := json.Marshal(key)
	require.NoError(t, err)
	_, err = q.CreateJsonWebKey(ctx, jwksrepo.CreateJsonWebKeyParams{OrganizationID: lf.f.req.OrganizationID, JsonWebKeySetID: lf.status.ActiveKey.JsonWebKeySetID, ExternalKeyID: lf.status.ActiveKey.ExternalKeyID, State: "active", Kid: key.KeyID, PublicJwk: rotated})
	require.NoError(t, err)
}

// speakeasySelection is grant's selection, authorized for the Speakeasy
// provider as Acquire authorizes it.
func (lf *speakeasyLiveFixture) speakeasySelection(t *testing.T, grant Request) selection {
	t.Helper()
	sel, outcome := lf.chainer.selectBinding(t.Context(), lf.logger, grant)
	require.True(t, outcome.Succeeded(), "%+v", outcome)
	order, outcome := lf.chainer.providerOrder(t.Context(), lf.logger, grant, sel)
	require.True(t, outcome.Succeeded(), "%+v", outcome)
	for _, p := range order.Providers {
		if p.Kind != remotesessions.EMAProviderSpeakeasy {
			continue
		}
		sel.provider = p
		outcome := lf.chainer.source(p).authorize(t.Context(), lf.logger, grant, &sel)
		require.True(t, outcome.Succeeded(), "%+v", outcome)
		return sel
	}
	require.FailNow(t, "no Speakeasy provider in the order")
	return sel
}

func TestSpeakeasyConsent_StaleFenceRetiresJudgedCredential(t *testing.T) {
	t.Parallel()
	lf := newSpeakeasyLiveFixture(t)
	lf.consent(t, lf.signIn.ID.UUID, true)
	grant := lf.newGrant(t, lf.signIn.ID)
	sel := lf.speakeasySelection(t, grant)
	pending := grant
	pending.UserSessionJTI = ""
	pending.consentSignInID = sel.signInID
	judged, err := readCredential(t.Context(), repo.New(lf.f.db), pending, sel, time.Now())
	require.NoError(t, err)

	lf.rotateKey(t)
	moved := judged
	moved.UpdatedAt = conv.ToPGTimestamptz(judged.UpdatedAt.Time.Add(-time.Second))
	adopted, err := lf.chainer.adopt(t.Context(), lf.logger, grant, pending, sel, moved)
	require.NoError(t, err, "a version that moved since it was judged is not reported dropped")
	require.False(t, adopted)
	rows := lf.f.credentials(t)
	require.Len(t, rows, 1)
	require.False(t, rows[0].Deleted, "a version that moved since it was judged is left alone")

	adopted, err = lf.chainer.adopt(t.Context(), lf.logger, grant, pending, sel, judged)
	require.ErrorIs(t, err, errStale)
	require.False(t, adopted)
	rows = lf.f.credentials(t)
	require.Len(t, rows, 1)
	require.True(t, rows[0].Deleted, "a credential the fence no longer admits is retired before it is reported dropped")
	require.False(t, rows[0].AccessTokenEncrypted.Valid, "retirement erases the ciphertext")
}

func TestSpeakeasyConsent_OtherSignInKeepsPendingConsent(t *testing.T) {
	t.Parallel()
	lf := newSpeakeasyLiveFixture(t)
	other := lf.recordSignIn(t, uuid.NewString())
	lf.consent(t, lf.signIn.ID.UUID, true)
	// The legacy index keeps one live credential per subject, so the second
	// sign-in's consent reports its outcome without evicting the first's.
	lf.consent(t, other.ID.UUID, true)
	require.EqualValues(t, 2, lf.calls.Load())
	lf.consent(t, other.ID.UUID, false)
	require.EqualValues(t, 2, lf.calls.Load(), "a consent reload remembers an outcome it could not store")
	lf.acquire(t, lf.newGrant(t, lf.signIn.ID), false)
	require.EqualValues(t, 2, lf.calls.Load(), "the first sign-in's grant still adopts its consent credential")
	lf.acquire(t, lf.newGrant(t, other.ID), true)
	require.EqualValues(t, 3, lf.calls.Load(), "the unstored consent leaves its sign-in's grant to mint")
}

func TestSpeakeasyConsent_LegacySlotHeldByGrant(t *testing.T) {
	t.Parallel()
	lf := newSpeakeasyLiveFixture(t)
	grant := lf.newGrant(t, lf.signIn.ID)
	lf.acquire(t, grant, true)
	other := lf.recordSignIn(t, uuid.NewString())
	lf.consent(t, other.ID.UUID, true)
	require.EqualValues(t, 2, lf.calls.Load())
	require.True(t, lf.chainer.HasUsableCredential(t.Context(), grant), "consent leaves the grant's credential in place")
	lf.consent(t, other.ID.UUID, false)
	require.EqualValues(t, 2, lf.calls.Load(), "a consent reload does not redeem again")
}

// Consent-time discovery calls the upstream with the credential, so a
// recorded acceptance whose credential could not be stored never stands in
// for one: discovery exchanges again and receives a real token.
func TestSpeakeasyConsent_AcquireAfterUnstoredAcceptanceReturnsToken(t *testing.T) {
	t.Parallel()
	lf := newSpeakeasyLiveFixture(t)
	lf.acquire(t, lf.newGrant(t, lf.signIn.ID), true)
	other := lf.recordSignIn(t, uuid.NewString())
	lf.consent(t, other.ID.UUID, true)
	require.EqualValues(t, 2, lf.calls.Load(), "the held legacy slot leaves the acceptance unstored")

	token, outcome := lf.chainer.AcquireConsent(t.Context(), lf.consentRequest(), other.ID.UUID)
	lf.checkExchange()
	require.True(t, outcome.Succeeded(), "%+v", outcome)
	require.Equal(t, "live-downstream", token.Value())
	require.EqualValues(t, 3, lf.calls.Load(), "discovery exchanges again rather than reusing a tokenless acceptance")

	lf.consent(t, other.ID.UUID, false)
	require.EqualValues(t, 3, lf.calls.Load(), "the outcome-only check still reuses the acceptance")
}

func TestSpeakeasyConsent_BelongsToItsSignIn(t *testing.T) {
	t.Parallel()
	lf := newSpeakeasyLiveFixture(t)
	_, err := lf.f.db.Exec(t.Context(), "DROP INDEX remote_session_ema_credentials_subject_key") //nolint:glint // notestingrawsql: DDL simulating the contract migration in this cloned database; sqlc does not generate DDL.
	require.NoError(t, err)
	later := lf.recordSignIn(t, uuid.NewString())
	lf.consent(t, later.ID.UUID, true)
	lf.acquire(t, lf.newGrant(t, lf.signIn.ID), true)
	require.EqualValues(t, 2, lf.calls.Load(), "another sign-in's consent credential is never adopted")
	lf.acquire(t, lf.newGrant(t, later.ID), false)
	require.EqualValues(t, 2, lf.calls.Load(), "any grant of the consent's sign-in adopts it")
}

func TestSpeakeasyConsent_RefusalWithinKeyWindowReadsUnknown(t *testing.T) {
	t.Parallel()
	lf := newSpeakeasyLiveFixture(t)
	lf.rejectRedemption.Store(new(oautherr.CodeInvalidGrant))
	lf.chainer.now = func() time.Time { return lf.status.ActiveKey.CreatedAt.Time.Add(time.Minute) }
	refused := lf.chainer.CheckConsent(t.Context(), lf.consentRequest(), lf.signIn.ID.UUID)
	lf.checkExchange()
	require.Equal(t, ReasonInvalidGrant, refused.Reason)
	require.Equal(t, StageRedemption, refused.Stage)
	require.True(t, refused.Retryable, "the vendor may not have fetched a just-published key yet")
	lf.chainer.now = func() time.Time { return time.Now().Add(recentKeyWindow + time.Minute) }
	refused = lf.chainer.CheckConsent(t.Context(), lf.consentRequest(), lf.signIn.ID.UUID)
	lf.checkExchange()
	require.Equal(t, ReasonInvalidGrant, refused.Reason)
	require.False(t, refused.Retryable, "past the key window a refusal is definitive")
	require.EqualValues(t, 2, lf.calls.Load())
}

func TestSpeakeasyConsent_DefinitiveRefusalWithinKeyWindow(t *testing.T) {
	t.Parallel()
	for code, reason := range map[string]Reason{oautherr.CodeInvalidClient: ReasonInvalidClient, oautherr.CodeInvalidScope: ReasonScopePolicyDenied} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			lf := newSpeakeasyLiveFixture(t)
			lf.rejectRedemption.Store(&code)
			lf.chainer.now = func() time.Time { return lf.status.ActiveKey.CreatedAt.Time.Add(time.Minute) }
			refused := lf.chainer.CheckConsent(t.Context(), lf.consentRequest(), lf.signIn.ID.UUID)
			lf.checkExchange()
			require.Equal(t, reason, refused.Reason)
			require.Equal(t, StageRedemption, refused.Stage)
			require.False(t, refused.Retryable, "a client or scope refusal is not a stale key set")
		})
	}
}

func TestSignedWithRecentKey_OnlyStaleKeyRejections(t *testing.T) {
	t.Parallel()
	publishedAt := time.Now()
	c := &Chainer{Governor: Governor{now: func() time.Time { return publishedAt.Add(time.Minute) }}}
	sel := selection{provider: remotesessions.EMAProvider{Kind: remotesessions.EMAProviderSpeakeasy}, internalKeyPublishedAt: publishedAt}
	redemption := func(reason Reason) Outcome { return newOutcome(StageRedemption, reason, ConfidenceUnknown, false) }
	for _, tc := range []struct {
		name    string
		outcome Outcome
		signed  bool
		want    bool
	}{
		{name: "invalid grant", outcome: redemption(ReasonInvalidGrant), signed: true, want: true},
		{name: "unknown rejection", outcome: redemption(ReasonUnknownRejection), signed: true, want: true},
		{name: "configuration before signing", outcome: newOutcome(StageRedemption, ReasonConfigurationRequired, ConfidenceVerified, false), signed: false, want: false},
		{name: "invalid grant never signed", outcome: redemption(ReasonInvalidGrant), signed: false, want: false},
		{name: "invalid client", outcome: redemption(ReasonInvalidClient), signed: true, want: false},
		{name: "scope policy", outcome: redemption(ReasonScopePolicyDenied), signed: true, want: false},
		{name: "insufficient scope", outcome: redemption(ReasonInsufficientScope), signed: true, want: false},
		{name: "invalid target", outcome: redemption(ReasonInvalidTarget), signed: true, want: false},
		{name: "access denied", outcome: redemption(ReasonAccessDenied), signed: true, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, c.signedWithRecentKey(sel, tc.outcome, tc.signed))
		})
	}
	old := sel
	old.internalKeyPublishedAt = publishedAt.Add(-recentKeyWindow)
	require.False(t, c.signedWithRecentKey(old, redemption(ReasonInvalidGrant), true), "past the key window")
}

func TestSpeakeasyConsent_ConcurrentChecksShareUnstoredAcceptance(t *testing.T) {
	t.Parallel()
	lf := newSpeakeasyLiveFixture(t)
	lf.acquire(t, lf.newGrant(t, lf.signIn.ID), true)
	other := lf.recordSignIn(t, uuid.NewString())

	// checks is enough concurrent consent loads to race single flight.
	const checks = 8
	var wg sync.WaitGroup
	outcomes := make([]Outcome, checks)
	for i := range checks {
		wg.Go(func() {
			// The consent page's deadline is shorter than waitBudget.
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			outcomes[i] = lf.chainer.CheckConsent(ctx, lf.consentRequest(), other.ID.UUID)
		})
	}
	wg.Wait()
	lf.checkExchange()
	for i := range checks {
		require.True(t, outcomes[i].Succeeded(), "%+v", outcomes[i])
	}
	require.EqualValues(t, 2, lf.calls.Load(), "concurrent checks share the one acceptance the held slot could not store")
}

func TestSpeakeasyConsent_RevokedBeforeApproval(t *testing.T) {
	t.Parallel()
	lf := newSpeakeasyLiveFixture(t)
	session := uuid.NewString()
	signIn := lf.recordSignIn(t, session)
	lf.consent(t, signIn.ID.UUID, true)
	grant := lf.newGrant(t, signIn.ID)
	_, err := signins.RevokeSession(t.Context(), lf.f.db, []string{lf.f.req.OrganizationID}, lf.f.req.UserID, session)
	require.NoError(t, err)
	_, outcome := lf.chainer.Acquire(t.Context(), grant)
	require.Equal(t, ReasonReauthenticationRequired, outcome.Reason)
	require.False(t, lf.chainer.HasUsableCredential(t.Context(), grant))
	require.EqualValues(t, 1, lf.calls.Load(), "a revoked sign-in neither adopts nor mints")
}

func TestSpeakeasyConsent_DenyRetiresPendingCredential(t *testing.T) {
	t.Parallel()
	lf := newSpeakeasyLiveFixture(t)
	lf.consent(t, lf.signIn.ID.UUID, true)
	q := repo.New(lf.f.db)
	otherProject, err := testrepo.New(lf.f.db).CreateProjectFixture(t.Context(), testrepo.CreateProjectFixtureParams{ID: uuid.New(), OrganizationID: lf.f.req.OrganizationID, Name: "Other project", Slug: "other-" + uuid.NewString()[:8]})
	require.NoError(t, err)
	pending := lf.f.credentials(t)
	require.Len(t, pending, 1)
	require.NoError(t, testrepo.New(lf.f.db).CopyConsentEMACredentialToProjectFixture(t.Context(), testrepo.CopyConsentEMACredentialToProjectFixtureParams{ID: pending[0].ID, OrganizationID: conv.ToPGText(lf.f.req.OrganizationID), ProjectID: otherProject}))
	other, err := q.ListEMACredentialsFixture(t.Context(), uuid.NullUUID{UUID: otherProject, Valid: true})
	require.NoError(t, err)
	require.Len(t, other, 1)
	require.False(t, other[0].Deleted)
	require.NoError(t, lf.chainer.RetireConsentCredentials(t.Context(), lf.f.req.OrganizationID, lf.f.req.UserSessionIssuerID, lf.f.req.UserID, lf.signIn.ID.UUID))
	for _, row := range lf.f.credentials(t) {
		require.True(t, row.Deleted, "denied consent leaves no live credential")
		require.False(t, row.AccessTokenEncrypted.Valid, "denied consent erases the ciphertext")
	}
	other, err = q.ListEMACredentialsFixture(t.Context(), uuid.NullUUID{UUID: otherProject, Valid: true})
	require.NoError(t, err)
	require.Len(t, other, 1)
	require.True(t, other[0].Deleted, "Deny retires every member project's pending consent")
	require.False(t, other[0].AccessTokenEncrypted.Valid)
	lf.acquire(t, lf.newGrant(t, lf.signIn.ID), true)
	require.EqualValues(t, 2, lf.calls.Load(), "nothing is left to adopt")
}
