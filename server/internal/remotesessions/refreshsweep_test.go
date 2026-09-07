package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type sweepWindow struct {
	now             time.Time
	keepaliveCutoff time.Time
}

func newSweepWindow() sweepWindow {
	now := time.Now()
	return sweepWindow{now: now, keepaliveCutoff: now.Add(-24 * time.Hour)}
}

func (w sweepWindow) claimParams() repo.ClaimDueRemoteSessionRefreshCandidatesParams {
	return repo.ClaimDueRemoteSessionRefreshCandidatesParams{
		NowTs:           conv.ToPGTimestamptz(w.now),
		KeepaliveCutoff: conv.ToPGTimestamptz(w.keepaliveCutoff),
		AttemptCutoff:   conv.ToPGTimestamptz(w.keepaliveCutoff),
		LimitValue:      1000,
	}
}

func (w sweepWindow) candidateParams(id uuid.UUID, org string) repo.GetDueRemoteSessionRefreshCandidateParams {
	return repo.GetDueRemoteSessionRefreshCandidateParams{
		ID:              id,
		OrganizationID:  org,
		NowTs:           conv.ToPGTimestamptz(w.now),
		KeepaliveCutoff: conv.ToPGTimestamptz(w.keepaliveCutoff),
	}
}

func seedGramSession(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	subject urn.SessionSubject,
	userIssuerID uuid.UUID,
	slug string,
	refreshExpiresIn time.Duration,
) {
	t.Helper()
	_, err := usersessionsrepo.New(ti.conn).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{
		UserSessionIssuerID: userIssuerID,
		UserSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SubjectUrn:          subject,
		Jti:                 "jti-" + slug,
		RefreshTokenHash:    "hash-" + slug,
		RefreshExpiresAt:    conv.ToPGTimestamptz(time.Now().Add(refreshExpiresIn)),
		ExpiresAt:           conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
	})
	require.NoError(t, err)
}

// enableOrgAutoRefreshFeature turns on one of the organization features that
// back the automatic-refresh policy the sweep queries evaluate.
func enableOrgAutoRefreshFeature(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	org string,
	feature productfeatures.Feature,
) {
	t.Helper()
	_, err := productfeaturesrepo.New(ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{
		OrganizationID: org,
		FeatureName:    string(feature),
	})
	require.NoError(t, err)
}

func seedSweepSession(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	slug string,
	withRefreshToken bool,
	autoRefresh bool,
	updatedAgo time.Duration,
	withGramSession bool,
) (uuid.UUID, string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createRemoteIssuer(t, ctx, ti, slug+"-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, slug+"-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), slug+"-client")
	clientUUID, err := uuid.Parse(clientID)
	require.NoError(t, err)

	var refreshToken pgtype.Text
	if withRefreshToken {
		refreshToken = conv.ToPGText("refresh-ciphertext")
	}
	subject := urn.NewUserSubject("subject-" + slug)
	session, err := repo.New(ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   userIssuerID,
		RemoteSessionClientID: clientUUID,
		AccessTokenEncrypted:  "access-ciphertext",
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		RefreshTokenEncrypted: refreshToken,
		RefreshExpiresAt:      pgtype.Timestamptz{},
		Scopes:                []string{},
		Resource:              pgtype.Text{},
		AutoRefresh:           autoRefresh,
	})
	require.NoError(t, err)

	if updatedAgo > 0 {
		require.NoError(t, repo.New(ti.conn).SetRemoteSessionUpdatedAt(ctx, repo.SetRemoteSessionUpdatedAtParams{
			ID:        session.ID,
			ProjectID: conv.ToNullUUID(*authCtx.ProjectID),
			UpdatedAt: conv.ToPGTimestamptz(time.Now().Add(-updatedAgo)),
		}))
	}
	if withGramSession {
		seedGramSession(t, ctx, ti, subject, userIssuerID, slug, 24*time.Hour)
	}

	return session.ID, authCtx.ActiveOrganizationID
}

func TestRefreshSweep_DueSessionDiscoveredAndRechecked(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	sessionID, org := seedSweepSession(t, ctx, ti, "sweep-due", true, true, 25*time.Hour, true)
	enableOrgAutoRefreshFeature(t, ctx, ti, org, productfeatures.FeatureRemoteSessionAutoRefresh)
	window := newSweepWindow()
	q := repo.New(ti.conn)

	rows, err := q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, sessionID, rows[0].ID)
	require.Equal(t, org, rows[0].OrganizationID)

	candidate, err := q.GetDueRemoteSessionRefreshCandidate(ctx, window.candidateParams(sessionID, org))
	require.NoError(t, err)
	require.Equal(t, sessionID, candidate.RemoteSession.ID)
	require.True(t, candidate.RemoteSession.LastRefreshAttemptAt.Valid)
	require.WithinDuration(t, window.now, candidate.RemoteSession.LastRefreshAttemptAt.Time, time.Millisecond)

	rows, err = q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	require.Empty(t, rows, "a claimed failure must rotate out until the next keepalive window")
}

func TestRefreshSweep_RequiresDueOptedInRenewableSession(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		slug             string
		withRefreshToken bool
		autoRefresh      bool
		updatedAgo       time.Duration
		withGramSession  bool
	}{
		{name: "fresh", slug: "fresh", withRefreshToken: true, autoRefresh: true, updatedAgo: 23 * time.Hour, withGramSession: true},
		{name: "no refresh grant", slug: "no-refresh", autoRefresh: true, updatedAgo: 25 * time.Hour, withGramSession: true},
		{name: "opted out", slug: "opted-out", withRefreshToken: true, updatedAgo: 25 * time.Hour, withGramSession: true},
		{name: "no live Gram session", slug: "no-gram", withRefreshToken: true, autoRefresh: true, updatedAgo: 25 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			sessionID, org := seedSweepSession(
				t,
				ctx,
				ti,
				"sweep-"+tt.slug,
				tt.withRefreshToken,
				tt.autoRefresh,
				tt.updatedAgo,
				tt.withGramSession,
			)
			// The organization lets subjects choose, so each case is skipped
			// for the reason it names rather than for the policy.
			enableOrgAutoRefreshFeature(t, ctx, ti, org, productfeatures.FeatureRemoteSessionAutoRefresh)

			rows, err := repo.New(ti.conn).ClaimDueRemoteSessionRefreshCandidates(ctx, newSweepWindow().claimParams())
			require.NoError(t, err)
			for _, row := range rows {
				require.NotEqual(t, sessionID, row.ID)
			}
		})
	}
}

func TestRefreshSweep_RequiredOrgClaimsOptedOutSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	// An opted-out session (auto_refresh = false) that would normally be
	// skipped by the keepalive.
	sessionID, org := seedSweepSession(t, ctx, ti, "sweep-required", true, false, 25*time.Hour, true)

	q := repo.New(ti.conn)
	window := newSweepWindow()

	// Baseline: while subjects choose, an opted-out session is not claimed.
	enableOrgAutoRefreshFeature(t, ctx, ti, org, productfeatures.FeatureRemoteSessionAutoRefresh)
	rows, err := q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	for _, row := range rows {
		require.NotEqual(t, sessionID, row.ID)
	}

	// Requiring refresh org-wide makes every eligible session due regardless of
	// its persisted per-session preference.
	enableOrgAutoRefreshFeature(t, ctx, ti, org, productfeatures.FeatureRemoteSessionAutoRefreshEnforced)

	rows, err = q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	var claimed bool
	for _, row := range rows {
		if row.ID == sessionID {
			claimed = true
			require.Equal(t, org, row.OrganizationID)
		}
	}
	require.True(t, claimed, "an organization requiring refresh must claim the opted-out session")

	// The authoritative re-check applies the same policy.
	candidate, err := q.GetDueRemoteSessionRefreshCandidate(ctx, window.candidateParams(sessionID, org))
	require.NoError(t, err)
	require.Equal(t, sessionID, candidate.RemoteSession.ID)
	require.False(t, candidate.RemoteSession.AutoRefresh, "the per-session preference is untouched; the policy is applied at query time")
}

func TestRefreshSweep_DisabledOrgSkipsOptedInSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	// A session that opted in while the organization still offered the choice.
	sessionID, org := seedSweepSession(t, ctx, ti, "sweep-disabled", true, true, 25*time.Hour, true)

	q := repo.New(ti.conn)
	window := newSweepWindow()

	// With refresh disabled for the organization, a preference left over from
	// an earlier policy must not keep renewing the connection — otherwise the
	// consent page would report "Off" while the keepalive kept working.
	rows, err := q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	for _, row := range rows {
		require.NotEqual(t, sessionID, row.ID)
	}

	_, err = q.GetDueRemoteSessionRefreshCandidate(ctx, window.candidateParams(sessionID, org))
	require.ErrorIs(t, err, pgx.ErrNoRows)

	// Restoring the opt-in policy restores the subject's own choice, because
	// the preference was read rather than rewritten.
	enableOrgAutoRefreshFeature(t, ctx, ti, org, productfeatures.FeatureRemoteSessionAutoRefresh)

	rows, err = q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	var claimed bool
	for _, row := range rows {
		if row.ID == sessionID {
			claimed = true
		}
	}
	require.True(t, claimed, "restoring the opt-in policy must honor the stored preference again")
}

// TestRefreshSweep_ClaimAndRecheckAgreeOnPolicy pins the claim sweep and the
// authoritative pre-refresh re-check to the same eligibility rule. Both queries
// spell out the organization-policy predicate separately, so a change applied to
// only one of them would let the re-check disagree with the claim and silently
// skip (or refresh) sessions. Every policy branch is exercised from both sides.
func TestRefreshSweep_ClaimAndRecheckAgreeOnPolicy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		slug string
		// features is the organization policy: empty means refresh is disabled.
		features []productfeatures.Feature
		// autoRefresh is the subject's stored preference.
		autoRefresh bool
		wantDue     bool
	}{
		{slug: "agree-disabled-opted-in", features: nil, autoRefresh: true, wantDue: false},
		{slug: "agree-user-opted-in", features: []productfeatures.Feature{productfeatures.FeatureRemoteSessionAutoRefresh}, autoRefresh: true, wantDue: true},
		{slug: "agree-user-opted-out", features: []productfeatures.Feature{productfeatures.FeatureRemoteSessionAutoRefresh}, autoRefresh: false, wantDue: false},
		{slug: "agree-required-opted-out", features: []productfeatures.Feature{productfeatures.FeatureRemoteSessionAutoRefreshEnforced}, autoRefresh: false, wantDue: true},
	}

	for _, tt := range cases {
		ctx, ti := newTestService(t)
		sessionID, org := seedSweepSession(t, ctx, ti, "sweep-"+tt.slug, true, tt.autoRefresh, 25*time.Hour, true)
		for _, feature := range tt.features {
			enableOrgAutoRefreshFeature(t, ctx, ti, org, feature)
		}

		q := repo.New(ti.conn)
		window := newSweepWindow()

		// The re-check runs first because claiming stamps the attempt clock.
		_, recheckErr := q.GetDueRemoteSessionRefreshCandidate(ctx, window.candidateParams(sessionID, org))
		if recheckErr != nil {
			require.ErrorIs(t, recheckErr, pgx.ErrNoRows, tt.slug)
		}
		recheckDue := recheckErr == nil

		rows, err := q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
		require.NoError(t, err, tt.slug)
		claimDue := false
		for _, row := range rows {
			if row.ID == sessionID {
				claimDue = true
			}
		}

		require.Equal(t, tt.wantDue, recheckDue, "re-check eligibility for %s", tt.slug)
		require.Equal(t, recheckDue, claimDue, "claim sweep and re-check must agree for %s", tt.slug)
	}
}

func TestRefreshSweep_CandidateRejectsWrongOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	sessionID, org := seedSweepSession(t, ctx, ti, "sweep-wrong-org", true, true, 25*time.Hour, true)
	enableOrgAutoRefreshFeature(t, ctx, ti, org, productfeatures.FeatureRemoteSessionAutoRefresh)

	_, err := repo.New(ti.conn).GetDueRemoteSessionRefreshCandidate(
		ctx,
		newSweepWindow().candidateParams(sessionID, "wrong-organization"),
	)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestRefreshSweep_ExpiredGramSessionSkipped(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID, org := seedSweepSession(t, ctx, ti, "sweep-expired-gram", true, true, 25*time.Hour, false)
	enableOrgAutoRefreshFeature(t, ctx, ti, org, productfeatures.FeatureRemoteSessionAutoRefresh)
	session, err := repo.New(ti.conn).GetRemoteSessionByID(ctx, repo.GetRemoteSessionByIDParams{
		ID:        sessionID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	seedGramSession(
		t,
		ctx,
		ti,
		urn.NewUserSubject("subject-sweep-expired-gram"),
		session.UserSessionIssuerID,
		"sweep-expired-gram",
		-time.Hour,
	)

	rows, err := repo.New(ti.conn).ClaimDueRemoteSessionRefreshCandidates(ctx, newSweepWindow().claimParams())
	require.NoError(t, err)
	for _, row := range rows {
		require.NotEqual(t, sessionID, row.ID)
	}
}

func TestRefreshSweep_ClaimAdvancesPastOldestSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	firstID, org := seedSweepSession(t, ctx, ti, "sweep-rotate-first", true, true, 26*time.Hour, true)
	secondID, _ := seedSweepSession(t, ctx, ti, "sweep-rotate-second", true, true, 25*time.Hour, true)
	enableOrgAutoRefreshFeature(t, ctx, ti, org, productfeatures.FeatureRemoteSessionAutoRefresh)
	window := newSweepWindow()
	params := window.claimParams()
	params.LimitValue = 1
	q := repo.New(ti.conn)

	firstPage, err := q.ClaimDueRemoteSessionRefreshCandidates(ctx, params)
	require.NoError(t, err)
	require.Len(t, firstPage, 1)
	require.Equal(t, firstID, firstPage[0].ID)

	secondPage, err := q.ClaimDueRemoteSessionRefreshCandidates(ctx, params)
	require.NoError(t, err)
	require.Len(t, secondPage, 1)
	require.Equal(t, secondID, secondPage[0].ID)
}

func seedSweepSharedGrant(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	slug string,
	gramOnIssuerB bool,
) (sessionID uuid.UUID, issuerA uuid.UUID, issuerB uuid.UUID, org string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	remoteIssuerID := createRemoteIssuer(t, ctx, ti, slug+"-issuer", "")
	issuerA = createUserSessionIssuer(t, ctx, ti.conn, slug+"-usi-a")
	issuerB = createUserSessionIssuer(t, ctx, ti.conn, slug+"-usi-b")
	clientID := createRemoteClient(t, ctx, ti, remoteIssuerID, issuerA.String(), slug+"-client")
	clientUUID, err := uuid.Parse(clientID)
	require.NoError(t, err)
	require.NoError(t, repo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: clientUUID,
		UserSessionIssuerID:   issuerB,
	}))

	subject := urn.NewUserSubject("subject-" + slug)
	session, err := repo.New(ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   issuerA,
		RemoteSessionClientID: clientUUID,
		AccessTokenEncrypted:  "access-ciphertext",
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		RefreshTokenEncrypted: conv.ToPGText("refresh-ciphertext"),
		RefreshExpiresAt:      pgtype.Timestamptz{},
		Scopes:                []string{},
		Resource:              pgtype.Text{},
		AutoRefresh:           true,
	})
	require.NoError(t, err)
	require.NoError(t, repo.New(ti.conn).SetRemoteSessionUpdatedAt(ctx, repo.SetRemoteSessionUpdatedAtParams{
		ID:        session.ID,
		ProjectID: conv.ToNullUUID(*authCtx.ProjectID),
		UpdatedAt: conv.ToPGTimestamptz(time.Now().Add(-25 * time.Hour)),
	}))

	gramIssuer := issuerA
	if gramOnIssuerB {
		gramIssuer = issuerB
	}
	seedGramSession(t, ctx, ti, subject, gramIssuer, slug, 24*time.Hour)

	return session.ID, issuerA, issuerB, authCtx.ActiveOrganizationID
}

func TestRefreshSweep_FindsGrantAfterMintingIssuerSoftDeleted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID, issuerA, _, org := seedSweepSharedGrant(t, ctx, ti, "sweep-v2-deleted-a", true)
	enableOrgAutoRefreshFeature(t, ctx, ti, org, productfeatures.FeatureRemoteSessionAutoRefresh)
	err := testrepo.New(ti.conn).ForceSoftDeleteUserSessionIssuer(ctx, testrepo.ForceSoftDeleteUserSessionIssuerParams{
		ID:        issuerA,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)

	window := newSweepWindow()
	q := repo.New(ti.conn)
	rows, err := q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, sessionID, rows[0].ID)
	require.Equal(t, org, rows[0].OrganizationID)

	candidate, err := q.GetDueRemoteSessionRefreshCandidate(ctx, window.candidateParams(sessionID, org))
	require.NoError(t, err)
	require.Equal(t, sessionID, candidate.RemoteSession.ID)
	require.Equal(t, issuerA, candidate.RemoteSession.UserSessionIssuerID)
}

func TestRefreshSweep_SkipsWhenLiveBindingHasNoGramSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID, issuerA, _, org := seedSweepSharedGrant(t, ctx, ti, "sweep-v2-session-on-a", false)
	enableOrgAutoRefreshFeature(t, ctx, ti, org, productfeatures.FeatureRemoteSessionAutoRefresh)
	err := testrepo.New(ti.conn).ForceSoftDeleteUserSessionIssuer(ctx, testrepo.ForceSoftDeleteUserSessionIssuerParams{
		ID:        issuerA,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)

	rows, err := repo.New(ti.conn).ClaimDueRemoteSessionRefreshCandidates(ctx, newSweepWindow().claimParams())
	require.NoError(t, err)
	for _, row := range rows {
		require.NotEqual(t, sessionID, row.ID)
	}
}
