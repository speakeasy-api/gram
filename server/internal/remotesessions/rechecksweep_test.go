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
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type recheckWindow struct {
	now           time.Time
	recheckCutoff time.Time
	attemptCutoff time.Time
}

// recheckTestInterval is the re-check interval every window here runs under; the lease derives from it as the sweep does.
const recheckTestInterval = 24 * time.Hour

func newRecheckWindow() recheckWindow {
	now := time.Now()
	return recheckWindow{now: now, recheckCutoff: now.Add(-recheckTestInterval), attemptCutoff: now.Add(-remotesessions.RecheckLease(recheckTestInterval))}
}

func (w recheckWindow) claimParams() repo.ClaimDueRemoteSessionRecheckCandidatesParams {
	return repo.ClaimDueRemoteSessionRecheckCandidatesParams{
		NowTs:         conv.ToPGTimestamptz(w.now),
		RecheckCutoff: conv.ToPGTimestamptz(w.recheckCutoff),
		AttemptCutoff: conv.ToPGTimestamptz(w.attemptCutoff),
		LimitValue:    1000,
	}
}

func (w recheckWindow) candidateParams(id uuid.UUID, org string) repo.GetDueRemoteSessionRecheckCandidateParams {
	return repo.GetDueRemoteSessionRecheckCandidateParams{
		ID:             id,
		NowTs:          conv.ToPGTimestamptz(w.now),
		RecheckCutoff:  conv.ToPGTimestamptz(w.recheckCutoff),
		OrganizationID: org,
	}
}

type recheckSeed struct {
	withRefreshToken   bool
	refreshExpiresAt   pgtype.Timestamptz
	accessExpiresAt    pgtype.Timestamptz
	createdAgo         time.Duration
	lastValidatedAgo   time.Duration
	lastAttemptAgo     time.Duration
	withGramSession    bool
	orgTierIssuer      bool
	deleteAfterSeeding bool
}

// pastInterval is a grant created before the interval with nothing else set: the plain due shape.
func pastInterval(seed recheckSeed) recheckSeed {
	seed.createdAgo = recheckTestInterval + time.Hour
	return seed
}

// seedRecheckSession stores one grant shaped by seed under a fresh client and issuer pair.
func seedRecheckSession(t *testing.T, ctx context.Context, ti *testInstance, slug string, seed recheckSeed) (uuid.UUID, string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createRemoteIssuer(t, ctx, ti, slug+"-issuer", "")
	var userIssuerID uuid.UUID
	if seed.orgTierIssuer {
		userIssuerID = seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, slug+"-usi")
	} else {
		userIssuerID = createUserSessionIssuer(t, ctx, ti.conn, slug+"-usi")
	}
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), slug+"-client")
	clientUUID, err := uuid.Parse(clientID)
	require.NoError(t, err)

	var refreshToken pgtype.Text
	if seed.withRefreshToken {
		refreshToken = conv.ToPGText("refresh-ciphertext")
	}
	subject := urn.NewUserSubject("subject-" + slug)
	q := repo.New(ti.conn)
	session, err := q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   userIssuerID,
		RemoteSessionClientID: clientUUID,
		AccessTokenEncrypted:  "access-ciphertext",
		AccessExpiresAt:       seed.accessExpiresAt,
		RefreshTokenEncrypted: refreshToken,
		RefreshExpiresAt:      seed.refreshExpiresAt,
		Scopes:                []string{},
		Resource:              pgtype.Text{},
		AutoRefresh:           false,
	})
	require.NoError(t, err)

	tracking := repo.SetRemoteSessionValidationTrackingFixtureParams{
		LastValidatedAt:      pgtype.Timestamptz{},
		LastRefreshAttemptAt: pgtype.Timestamptz{},
		CreatedAt:            pgtype.Timestamptz{},
		ID:                   session.ID,
		ProjectID:            conv.ToNullUUID(*authCtx.ProjectID),
	}
	if seed.createdAgo > 0 {
		tracking.CreatedAt = conv.ToPGTimestamptz(time.Now().Add(-seed.createdAgo))
	}
	if seed.lastValidatedAgo > 0 {
		tracking.LastValidatedAt = conv.ToPGTimestamptz(time.Now().Add(-seed.lastValidatedAgo))
	}
	if seed.lastAttemptAgo > 0 {
		tracking.LastRefreshAttemptAt = conv.ToPGTimestamptz(time.Now().Add(-seed.lastAttemptAgo))
	}
	require.NoError(t, q.SetRemoteSessionValidationTrackingFixture(ctx, tracking))

	if seed.withGramSession {
		seedGramSession(t, ctx, ti, subject, userIssuerID, slug, 24*time.Hour)
	}
	if seed.deleteAfterSeeding {
		_, err := q.SoftDeleteRemoteSessionsByClientID(ctx, clientUUID)
		require.NoError(t, err)
	}
	return session.ID, authCtx.ActiveOrganizationID
}

// A never-validated grant with no refresh token is claimed once, re-reads as due, and rotates out under its lease.
func TestRecheckSweep_ClaimAndRecheckAgree(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	sessionID, org := seedRecheckSession(t, ctx, ti, "recheck-due", pastInterval(recheckSeed{withGramSession: true}))
	window := newRecheckWindow()
	q := repo.New(ti.conn)

	rows, err := q.ClaimDueRemoteSessionRecheckCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, sessionID, rows[0].ID)
	require.Equal(t, org, rows[0].OrganizationID)
	require.NotEmpty(t, rows[0].IssuerUrl)

	candidate, err := q.GetDueRemoteSessionRecheckCandidate(ctx, window.candidateParams(sessionID, org))
	require.NoError(t, err)
	require.Equal(t, sessionID, candidate.RemoteSession.ID)
	require.True(t, candidate.RemoteSession.LastRefreshAttemptAt.Valid, "the claim stamps the lease")
	require.WithinDuration(t, window.now, candidate.RemoteSession.LastRefreshAttemptAt.Time, time.Millisecond)
	require.Equal(t, rows[0].IssuerUrl, candidate.IssuerUrl)

	rows, err = q.ClaimDueRemoteSessionRecheckCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	require.Empty(t, rows, "a claimed grant rotates out until its lease lapses")

	_, err = q.GetDueRemoteSessionRecheckCandidate(ctx, window.candidateParams(sessionID, "org_other"))
	require.ErrorIs(t, err, pgx.ErrNoRows, "the re-read is bound to the organization the claim ran under")
}

// A verdict, or before any the grant itself, older than the interval is due once the lease has lapsed; a recent
// verdict, a grant connected inside the interval (the connect auto-verify's), or a live lease is not.
func TestRecheckSweep_DueOnlyPastIntervalAndLease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		seed recheckSeed
		due  bool
	}{
		{name: "verdict older than the interval", seed: recheckSeed{lastValidatedAgo: 25 * time.Hour, withGramSession: true}, due: true},
		{name: "verdict inside the interval", seed: recheckSeed{createdAgo: 48 * time.Hour, lastValidatedAgo: 23 * time.Hour, withGramSession: true}, due: false},
		{name: "never validated, created inside the interval", seed: recheckSeed{createdAgo: 23 * time.Hour, withGramSession: true}, due: false},
		{name: "never validated, created past the interval", seed: pastInterval(recheckSeed{withGramSession: true}), due: true},
		{name: "never validated, lease lapsed", seed: pastInterval(recheckSeed{lastAttemptAgo: 7 * time.Hour, withGramSession: true}), due: true},
		{name: "never validated, lease live", seed: pastInterval(recheckSeed{lastAttemptAgo: 5 * time.Hour, withGramSession: true}), due: false},
		{name: "project client under an organization-tier issuer", seed: pastInterval(recheckSeed{withGramSession: true, orgTierIssuer: true}), due: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			sessionID, _ := seedRecheckSession(t, ctx, ti, "recheck-"+uuid.NewString()[:8], tt.seed)
			rows, err := repo.New(ti.conn).ClaimDueRemoteSessionRecheckCandidates(ctx, newRecheckWindow().claimParams())
			require.NoError(t, err)
			if tt.due {
				require.Len(t, rows, 1)
				require.Equal(t, sessionID, rows[0].ID)
			} else {
				require.Empty(t, rows)
			}
		})
	}
}

// The sweep owns only grants the refresh sweep cannot touch, that are still usable, and that some live surface still
// serves; the claim and the pre-probe re-read must both refuse every exclusion.
func TestRecheckSweep_OwnsOnlyUnrenewableRoutableGrants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		seed recheckSeed
	}{
		{name: "refresh token present", seed: pastInterval(recheckSeed{withRefreshToken: true, withGramSession: true})},
		{name: "refresh expiry present", seed: pastInterval(recheckSeed{refreshExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), withGramSession: true})},
		{name: "access token expired", seed: pastInterval(recheckSeed{accessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Minute)), withGramSession: true})},
		{name: "no live Gram session", seed: pastInterval(recheckSeed{})},
		{name: "deleted", seed: pastInterval(recheckSeed{withGramSession: true, deleteAfterSeeding: true})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			sessionID, org := seedRecheckSession(t, ctx, ti, "recheck-"+uuid.NewString()[:8], tt.seed)
			window := newRecheckWindow()
			rows, err := repo.New(ti.conn).ClaimDueRemoteSessionRecheckCandidates(ctx, window.claimParams())
			require.NoError(t, err)
			require.Empty(t, rows)
			_, err = repo.New(ti.conn).GetDueRemoteSessionRecheckCandidate(ctx, window.candidateParams(sessionID, org))
			require.ErrorIs(t, err, pgx.ErrNoRows, "the re-read refuses what the claim refuses")
		})
	}
}

// A future access expiry with no refresh grant is still the sweep's: nothing else will ever present it.
func TestRecheckSweep_ClaimsUnexpiredAccessWithoutRefreshGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	sessionID, _ := seedRecheckSession(t, ctx, ti, "recheck-expiring", pastInterval(recheckSeed{accessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), withGramSession: true}))
	rows, err := repo.New(ti.conn).ClaimDueRemoteSessionRecheckCandidates(ctx, newRecheckWindow().claimParams())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, sessionID, rows[0].ID)
}
