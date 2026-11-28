package remotesessions_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestGetActiveRemoteSessionEnforcesClientLifetime(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-session-expiration")
	clientID, _ := seedActiveClient(t, ctx, ti.conn, *authCtx.ProjectID, userIssuerID, authCtx.ActiveOrganizationID, "rsi-session-expiration")
	err := repo.New(ti.conn).SetRemoteSessionClientLifetimeFixture(ctx, repo.SetRemoteSessionClientLifetimeFixtureParams{
		SessionInactivityTimeout: pgtype.Interval{Microseconds: (30 * time.Minute).Microseconds(), Valid: true},
		SessionMaximumDuration:   pgtype.Interval{Microseconds: (8 * time.Hour).Microseconds(), Valid: true},
		ID:                       clientID,
		ProjectID:                uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
	})
	require.NoError(t, err)

	enc := testenv.NewEncryptionClient(t)
	accessEnc, err := enc.Encrypt([]byte("upstream-access-token"))
	require.NoError(t, err)
	subject := urn.NewUserSubject("session-expiration-subject")
	q := repo.New(ti.conn)
	upsert := func() repo.RemoteSession {
		t.Helper()
		session, err := q.UpsertRemoteSessionFixture(ctx, repo.UpsertRemoteSessionFixtureParams{
			SubjectUrn:            subject,
			UserSessionIssuerID:   userIssuerID,
			RemoteSessionClientID: clientID,
			AccessTokenEncrypted:  accessEnc,
			AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
			Scopes:                []string{},
		})
		require.NoError(t, err)
		return session
	}
	lookup := func() (repo.RemoteSession, error) {
		return q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: clientID})
	}
	var session repo.RemoteSession
	age := func(authorizedAt, lastUsedAt time.Time) {
		t.Helper()
		err := q.SetRemoteSessionLifetimeFixture(ctx, repo.SetRemoteSessionLifetimeFixtureParams{
			AuthorizedAt: pgtype.Timestamptz{Time: authorizedAt, Valid: true},
			LastUsedAt:   pgtype.Timestamptz{Time: lastUsedAt, Valid: true},
			ID:           session.ID,
			ProjectID:    uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		})
		require.NoError(t, err)
	}

	session = upsert()
	require.True(t, session.AuthorizedAt.Valid)
	require.False(t, session.LastUsedAt.Valid)

	age(time.Now().Add(-2*time.Hour), time.Now().Add(-31*time.Minute))
	_, err = lookup()
	require.ErrorIs(t, err, pgx.ErrNoRows, "inactivity ends the session")

	age(time.Now().Add(-2*time.Hour), time.Now().Add(-time.Minute))
	_, err = lookup()
	require.NoError(t, err, "recent real use keeps the session active")

	age(time.Now().Add(-9*time.Hour), time.Now().Add(-time.Minute))
	_, err = lookup()
	require.ErrorIs(t, err, pgx.ErrNoRows, "activity cannot extend the maximum duration")

	reauthorized := upsert()
	require.True(t, reauthorized.AuthorizedAt.Valid)
	require.WithinDuration(t, time.Now(), reauthorized.AuthorizedAt.Time, time.Second)
	require.False(t, reauthorized.LastUsedAt.Valid, "a new authorization starts a new inactivity window")
	_, err = lookup()
	require.NoError(t, err)
}

func TestExpiredRemoteSessionIsNotResolvedOrReportedActive(t *testing.T) {
	t.Parallel()

	ctx, env := newSyntheticExpiryEnv(t, "client-lifetime", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"live-access","refresh_token":"live-refresh","token_type":"Bearer","expires_in":3600}`)
	})
	err := env.q.SetRemoteSessionClientLifetimeFixture(ctx, repo.SetRemoteSessionClientLifetimeFixtureParams{
		SessionInactivityTimeout: pgtype.Interval{Microseconds: (30 * time.Minute).Microseconds(), Valid: true},
		SessionMaximumDuration:   pgtype.Interval{Microseconds: (8 * time.Hour).Microseconds(), Valid: true},
		ID:                       env.clientID,
		ProjectID:                uuid.NullUUID{UUID: env.projectID, Valid: true},
	})
	require.NoError(t, err)
	err = env.q.SetRemoteSessionLifetimeFixture(ctx, repo.SetRemoteSessionLifetimeFixtureParams{
		AuthorizedAt: pgtype.Timestamptz{Time: time.Now().Add(-2 * time.Hour), Valid: true},
		LastUsedAt:   pgtype.Timestamptz{Time: time.Now().Add(-31 * time.Minute), Valid: true},
		ID:           env.session.ID,
		ProjectID:    uuid.NullUUID{UUID: env.projectID, Valid: true},
	})
	require.NoError(t, err)

	require.Equal(t, remotesessions.RemoteSessionExpired, sessionStatus(t, ctx, env))
	requireNoToken(t, ctx, env)
}
