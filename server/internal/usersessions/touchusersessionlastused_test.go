package usersessions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// The stamp runs on the per-request MCP auth path, so the properties that
// matter are that it records use at all, that it coalesces within the cutoff
// window (otherwise every request writes a row), and that it cannot reach a
// session in another project.
func TestTouchUserSessionLastUsed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	q := repo.New(ti.conn)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "touch-last-used-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	issuerID := uuid.MustParse(issuer.ID)
	session, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewUserSubject("touch-target"))
	require.NoError(t, err)
	require.False(t, session.LastUsedAt.Valid, "a freshly seeded session has never been used")

	touch := func(now time.Time) error {
		return q.TouchUserSessionLastUsed(ctx, repo.TouchUserSessionLastUsedParams{
			NowTs:               pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
			ProjectID:           session.ProjectID.UUID,
			UserSessionIssuerID: issuerID,
			Jti:                 session.Jti,
			UsedCutoff:          pgtype.Timestamptz{Time: now.Add(-5 * time.Minute), Valid: true, InfinityModifier: pgtype.Finite},
		})
	}

	reload := func() repo.UserSession {
		t.Helper()
		got, err := q.GetUserSessionByID(ctx, repo.GetUserSessionByIDParams{
			ID:        session.ID,
			ProjectID: session.ProjectID.UUID,
		})
		require.NoError(t, err)
		return got
	}

	first := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, touch(first))

	stamped := reload()
	require.True(t, stamped.LastUsedAt.Valid, "first use stamps the column")
	require.WithinDuration(t, first, stamped.LastUsedAt.Time, time.Second)

	// A second request inside the cutoff window must not write: this is what
	// keeps a session doing hundreds of calls a minute to one row per window.
	require.NoError(t, touch(first.Add(30*time.Second)))
	require.Equal(t, stamped.LastUsedAt.Time, reload().LastUsedAt.Time,
		"a touch inside the cutoff window leaves the stamp untouched")

	// Past the window, the next request advances it.
	later := first.Add(6 * time.Minute)
	require.NoError(t, touch(later))
	require.WithinDuration(t, later, reload().LastUsedAt.Time, time.Second,
		"a touch past the cutoff window advances the stamp")
}

// The query is project-scoped, so a jti presented against one project can never
// stamp a same-named session in another.
func TestTouchUserSessionLastUsedIsProjectScoped(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	q := repo.New(ti.conn)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "touch-scope-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	issuerID := uuid.MustParse(issuer.ID)
	session, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewUserSubject("touch-scope-target"))
	require.NoError(t, err)

	now := time.Now().UTC()
	require.NoError(t, q.TouchUserSessionLastUsed(ctx, repo.TouchUserSessionLastUsedParams{
		NowTs:               pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
		ProjectID:           uuid.New(),
		UserSessionIssuerID: issuerID,
		Jti:                 session.Jti,
		UsedCutoff:          pgtype.Timestamptz{Time: now.Add(-5 * time.Minute), Valid: true, InfinityModifier: pgtype.Finite},
	}))

	got, err := q.GetUserSessionByID(ctx, repo.GetUserSessionByIDParams{
		ID:        session.ID,
		ProjectID: session.ProjectID.UUID,
	})
	require.NoError(t, err)
	require.False(t, got.LastUsedAt.Valid, "another project's id must not stamp this session")
}
