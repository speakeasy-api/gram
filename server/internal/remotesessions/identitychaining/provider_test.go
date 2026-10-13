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

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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

// setPolicy saves rows for the fixture's vendor. Writes through the API save
// at most one; more exercise the stored-state guard.
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

func TestProviders_DefaultIsTheEndpointsExternalIssuer(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	seedSpeakeasyIssuer(t, f, f.req.OrganizationID)

	selection, ok := f.chainer.ProviderSelection(t.Context(), f.req)
	require.True(t, ok)
	require.True(t, selection.IsDefault)
	require.Equal(t, remotesessions.EMAProvider{Kind: remotesessions.EMAProviderExternal, RemoteSessionIssuerID: f.trustedIssuerID, InternalSessionIssuerID: uuid.Nil, Unavailable: ""}, selection.Provider, "Speakeasy requires an explicit selection")
	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.Equal(t, remotesessions.EMAProviderExternal, outcome.Provider)
}

func TestProviders_DefaultDoesNotSelectSpeakeasyWhenItCanSign(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	seedSpeakeasyIssuer(t, f, f.req.OrganizationID)
	useSpeakeasySource(f, &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: success, issued: 0})

	selection, ok := f.chainer.ProviderSelection(t.Context(), f.req)
	require.True(t, ok)
	require.Equal(t, remotesessions.EMAProviderExternal, selection.Provider.Kind)
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
			setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newDevIDPFixture(t, devIDPOptions{})
			tc.setup(t, f)

			runtime, ok := f.chainer.ProviderSelection(t.Context(), f.req)
			require.True(t, ok)
			vendor, err := remotesessions.NormalizeVendorIssuer(f.vendorIssuer)
			require.NoError(t, err)
			read, err := remotesessions.EndpointEMAProvider(t.Context(), repo.New(f.db), f.req.OrganizationID, f.req.UserSessionIssuerID, vendor, f.chainer.sourceAvailable(remotesessions.EMAProviderSpeakeasy))
			require.NoError(t, err)
			require.Equal(t, runtime, read)
		})
	}
}

func TestProviders_ExplicitSelectionNeverFallsThroughToExternal(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	src := &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: newOutcome(StageExchange, ReasonAccessDenied, ConfidenceInferred, false), issued: 0}
	useSpeakeasySource(f, src)
	setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))
	requests := f.idp.Requests()

	selection, ok := f.chainer.ProviderSelection(t.Context(), f.req)
	require.True(t, ok)
	require.Equal(t, remotesessions.EMAProviderSpeakeasy, selection.Provider.Kind)
	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.False(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.Equal(t, remotesessions.EMAProviderSpeakeasy, outcome.Provider)
	require.Equal(t, 1, src.issued)
	require.Equal(t, requests, f.idp.Requests(), "the external issuer is never contacted")
}

func TestProviders_ExplicitSelectionFailureNeverUsesAnotherProvider(t *testing.T) {
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
			setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))

			token, outcome := f.chainer.Acquire(t.Context(), f.req)
			require.False(t, outcome.Succeeded(), "outcome: %+v", outcome)
			require.Empty(t, token.Value())
			require.Equal(t, remotesessions.EMAProviderSpeakeasy, outcome.Provider)
			require.Equal(t, tc.issues, tc.source.issued)
		})
	}
}

func TestProviders_SelectedExternalFailureIsReported(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{unassigned: true})
	src := &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: success, issued: 0}
	useSpeakeasySource(f, src)
	seedSpeakeasyIssuer(t, f, f.req.OrganizationID)
	setPolicy(t, f, f.req.OrganizationID, external(f.trustedIssuerID))

	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.Equal(t, Outcome{Stage: StageExchange, Reason: ReasonAccessDenied, Confidence: ConfidenceInferred, Retryable: false, Cached: false, Provider: remotesessions.EMAProviderExternal}, outcome)
	require.Zero(t, src.issued, "a Speakeasy issuer that can sign is not tried after the selected provider fails")
}

func TestProviders_SeveralSavedProvidersFailClosed(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	src := &failingSource{authorizeOutcome: success, reuseOutcome: success, issueOutcome: success, issued: 0}
	useSpeakeasySource(f, src)
	setPolicy(t, f, f.req.OrganizationID, external(f.trustedIssuerID), speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))
	requests := f.idp.Requests()

	_, ok := f.chainer.ProviderSelection(t.Context(), f.req)
	require.False(t, ok, "no provider is picked from several")
	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.Equal(t, ReasonTransientFailure, outcome.Reason)
	require.Zero(t, src.issued)
	require.Equal(t, requests, f.idp.Requests(), "no provider is contacted")
}

func TestProviders_UnavailableSelectionIsNotServed(t *testing.T) {
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

			selection, ok := f.chainer.ProviderSelection(t.Context(), f.req)
			require.True(t, ok)
			require.Equal(t, tc.reason, selection.Provider.Unavailable)
			_, serves := f.chainer.Serves(t.Context(), f.req)
			require.False(t, serves, "the interactive card renders instead")
			_, outcome := f.chainer.Acquire(t.Context(), f.req)
			require.Equal(t, ReasonProviderUnavailable, outcome.Reason)
			require.Equal(t, tc.reason, outcome.Unavailable, "the specific reason is kept")
			require.False(t, outcome.Applicable(), "the interactive path applies unchanged")
			require.Equal(t, requests, f.idp.Requests(), "no provider is contacted")
		})
	}
}

func TestProviders_SelectionStopsStoredCredentialReuse(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.True(t, f.chainer.HasUsableCredential(t.Context(), f.req))

	setPolicy(t, f, f.req.OrganizationID, speakeasy(seedSpeakeasyIssuer(t, f, f.req.OrganizationID)))
	require.False(t, f.chainer.HasUsableCredential(t.Context(), f.req), "a credential from a provider no longer selected is never usable")
	_, outcome = f.chainer.Acquire(t.Context(), f.req)
	require.Equal(t, ReasonProviderUnavailable, outcome.Reason)
}

func TestProviders_AnotherOrganizationsSelectionDoesNotApply(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	other := "org-" + uuid.NewString()
	require.NoError(t, testrepo.New(f.db).SeedDelegationLoaderOrganizationFixture(t.Context(), testrepo.SeedDelegationLoaderOrganizationFixtureParams{OrganizationID: other, Name: "Other organization", Slug: "other-" + uuid.NewString()[:8]}))
	setPolicy(t, f, other, speakeasy(seedSpeakeasyIssuer(t, f, other)))

	selection, ok := f.chainer.ProviderSelection(t.Context(), f.req)
	require.True(t, ok)
	require.True(t, selection.IsDefault)
	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
}

// interceptingSource runs afterIssue once its wrapped source has issued, so a
// test can change the selection while the attempt is between issue and publish.
type interceptingSource struct {
	tokenSource
	afterIssue func()
}

func (s interceptingSource) issue(ctx context.Context, logger *slog.Logger, req Request, sel selection, ready func() Outcome) (issuedGrant, Outcome) {
	grant, outcome := s.tokenSource.issue(ctx, logger, req, sel, ready)
	s.afterIssue()
	return grant, outcome
}

func TestProviders_SelectionChangedMidAttemptDiscardsTheToken(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	internalIssuer := seedSpeakeasyIssuer(t, f, f.req.OrganizationID)
	f.chainer.sources[remotesessions.EMAProviderExternal] = func(p remotesessions.EMAProvider) tokenSource {
		return interceptingSource{
			tokenSource: externalSource{c: f.chainer, issuer: p.RemoteSessionIssuerID},
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

func TestPublish_SerializesWithSelectionChanges(t *testing.T) {
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

	// A selection change holds the lock, as setEMAProvider does.
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
	}, 5*time.Second, 20*time.Millisecond, "publish waits for the selection change")

	require.NoError(t, q.InsertEMAProvider(ctx, repo.InsertEMAProviderParams{
		OrganizationID: f.req.OrganizationID, VendorIssuer: vendor,
		RemoteSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		InternalSessionIssuerID: uuid.NullUUID{UUID: internalIssuer, Valid: true},
		Position:                0,
	}))
	require.NoError(t, tx.Commit(ctx))

	require.ErrorIs(t, <-published, errStale, "the write sees the committed selection, which no longer allows the provider")
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
