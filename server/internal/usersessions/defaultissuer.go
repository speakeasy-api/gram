package usersessions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// DefaultIssuerSlug is the reserved slug of the implicit project-default
// user_session_issuer. Private MCP servers with no explicit issuer binding
// are gated on this issuer, materialised on demand by
// GetOrCreateDefaultIssuer. The slug is deterministic so every resolution
// converges on the same row — at most one implicit issuer exists per
// project.
const DefaultIssuerSlug = "gram-default"

// defaultIssuerSessionDuration is the issued-session lifetime for the
// implicit project-default issuer. Explicit issuers carry a user-chosen
// session_duration; the implicit one gets a conservative 30 days,
// adjustable later via the standard issuer-update API since the row is a
// plain user_session_issuers row once materialised.
const defaultIssuerSessionDuration = 30 * 24 * time.Hour

// GetDefaultIssuer is the read-only resolution of the project's implicit
// default user_session_issuer. found=false means no OAuth flow has
// materialised the issuer yet — runtime hot paths (serve dispatch,
// well-known metadata) treat that as "no session can validate" and never
// write; only the OAuth flow entry points and the dashboard mint create
// the row, via GetOrCreateDefaultIssuer.
func GetDefaultIssuer(ctx context.Context, db repo.DBTX, projectID uuid.UUID) (repo.UserSessionIssuer, bool, error) {
	var zero repo.UserSessionIssuer
	row, err := repo.New(db).GetUserSessionIssuerBySlug(ctx, repo.GetUserSessionIssuerBySlugParams{
		Slug:      DefaultIssuerSlug,
		ProjectID: projectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return zero, false, nil
	case err != nil:
		return zero, false, fmt.Errorf("get default user session issuer: %w", err)
	}
	return row, true, nil
}

// GetOrCreateDefaultIssuer returns the project's implicit default
// user_session_issuer, creating it on first touch. Reserved for the
// stateful entry points of the implicit OAuth surface — DCR registration,
// authorize/connect, and the dashboard mint — which need the row for the
// NOT NULL issuer FKs they write (user_session_clients, user_sessions).
// Runtime hot paths use the read-only GetDefaultIssuer instead. Reads
// before writing so repeat resolution stays a single SELECT; the insert
// uses ON CONFLICT DO NOTHING plus a re-read to stay race-safe when two
// first requests arrive concurrently.
func GetOrCreateDefaultIssuer(ctx context.Context, db repo.DBTX, projectID uuid.UUID) (repo.UserSessionIssuer, error) {
	var zero repo.UserSessionIssuer

	row, found, err := GetDefaultIssuer(ctx, db, projectID)
	if err != nil {
		return zero, err
	}
	if found {
		return row, nil
	}

	row, err = repo.New(db).CreateDefaultUserSessionIssuer(ctx, repo.CreateDefaultUserSessionIssuerParams{
		ProjectID:          projectID,
		Slug:               DefaultIssuerSlug,
		AuthnChallengeMode: "interactive",
		SessionDuration: pgtype.Interval{
			Microseconds: defaultIssuerSessionDuration.Microseconds(),
			Days:         0,
			Months:       0,
			Valid:        true,
		},
	})
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return zero, fmt.Errorf("create default user session issuer: %w", err)
	}

	// Lost the create race: a concurrent request inserted the row between
	// our read and write. Re-read it.
	row, found, err = GetDefaultIssuer(ctx, db, projectID)
	if err != nil {
		return zero, err
	}
	if !found {
		return zero, fmt.Errorf("default user session issuer missing after create race")
	}
	return row, nil
}
