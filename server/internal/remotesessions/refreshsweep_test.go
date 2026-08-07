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
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
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
			sessionID, _ := seedSweepSession(
				t,
				ctx,
				ti,
				"sweep-"+tt.slug,
				tt.withRefreshToken,
				tt.autoRefresh,
				tt.updatedAgo,
				tt.withGramSession,
			)

			rows, err := repo.New(ti.conn).ClaimDueRemoteSessionRefreshCandidates(ctx, newSweepWindow().claimParams())
			require.NoError(t, err)
			for _, row := range rows {
				require.NotEqual(t, sessionID, row.ID)
			}
		})
	}
}

func TestRefreshSweep_CandidateRejectsWrongOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	sessionID, _ := seedSweepSession(t, ctx, ti, "sweep-wrong-org", true, true, 25*time.Hour, true)

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

	sessionID, _ := seedSweepSession(t, ctx, ti, "sweep-expired-gram", true, true, 25*time.Hour, false)
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
	firstID, _ := seedSweepSession(t, ctx, ti, "sweep-rotate-first", true, true, 26*time.Hour, true)
	secondID, _ := seedSweepSession(t, ctx, ti, "sweep-rotate-second", true, true, 25*time.Hour, true)
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
