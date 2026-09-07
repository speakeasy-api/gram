package remotesessions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The stamp runs whenever a brokered call resolves an upstream token, so the
// properties that matter are that it records use at all, that it coalesces
// within the cutoff window (otherwise every proxied call writes a row), and
// that it cannot reach a session outside its own
// (subject_urn, remote_session_client_id) binding — the pair standing in for
// the project_id this table does not carry.
func TestTouchRemoteSessionLastUsed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	enc := testenv.NewEncryptionClient(t)
	q := repo.New(ti.conn)

	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-touch-last-used")
	clientID, _ := seedActiveClient(t, ctx, ti.conn, *authCtx.ProjectID, userIssuerID, authCtx.ActiveOrganizationID, "rsi-touch-last-used")

	subject := urn.NewUserSubject("touch-last-used-subject")
	accessEnc, err := enc.Encrypt([]byte("upstream-access-token"))
	require.NoError(t, err)
	session, err := q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   userIssuerID,
		RemoteSessionClientID: clientID,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		Scopes:                []string{},
	})
	require.NoError(t, err)
	require.False(t, session.LastUsedAt.Valid, "a freshly upserted session has never been used")

	touch := func(now time.Time, subj urn.SessionSubject, client uuid.UUID) error {
		return q.TouchRemoteSessionLastUsed(ctx, repo.TouchRemoteSessionLastUsedParams{
			NowTs:                 pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
			SubjectUrn:            subj,
			RemoteSessionClientID: client,
			UsedCutoff:            pgtype.Timestamptz{Time: now.Add(-5 * time.Minute), Valid: true, InfinityModifier: pgtype.Finite},
		})
	}

	reload := func() repo.RemoteSession {
		t.Helper()
		got, err := q.GetRemoteSessionByID(ctx, repo.GetRemoteSessionByIDParams{
			ID:        session.ID,
			ProjectID: *authCtx.ProjectID,
		})
		require.NoError(t, err)
		return got
	}

	first := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, touch(first, subject, clientID))

	stamped := reload()
	require.True(t, stamped.LastUsedAt.Valid, "first use stamps the column")
	require.WithinDuration(t, first, stamped.LastUsedAt.Time, time.Second)

	// A second call inside the cutoff window must not write: this is what keeps
	// a chatty proxied session down to one row per window.
	require.NoError(t, touch(first.Add(30*time.Second), subject, clientID))
	require.Equal(t, stamped.LastUsedAt.Time, reload().LastUsedAt.Time,
		"a touch inside the cutoff window leaves the stamp untouched")

	// Past the window, the next call advances it.
	later := first.Add(6 * time.Minute)
	require.NoError(t, touch(later, subject, clientID))
	advanced := reload().LastUsedAt.Time
	require.WithinDuration(t, later, advanced, time.Second,
		"a touch past the cutoff window advances the stamp")

	// Neither half of the binding on its own reaches the row: a different
	// subject on the same client, or the same subject on another client, both
	// match nothing even though the cutoff is long past.
	beyond := later.Add(10 * time.Minute)
	otherClientID, _ := seedActiveClient(t, ctx, ti.conn, *authCtx.ProjectID, createUserSessionIssuer(t, ctx, ti.conn, "usi-touch-last-used-other"), authCtx.ActiveOrganizationID, "rsi-touch-last-used-other")
	require.NoError(t, touch(beyond, urn.NewUserSubject("touch-last-used-other-subject"), clientID))
	require.NoError(t, touch(beyond, subject, otherClientID))
	require.Equal(t, advanced, reload().LastUsedAt.Time,
		"another binding must not stamp this session")
}
