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
// user_session_issuer; deterministic so at most one exists per project.
const DefaultIssuerSlug = "gram-default"

const defaultIssuerSessionDuration = 30 * 24 * time.Hour

// GetDefaultIssuer is the read-only resolution of the project's implicit
// default issuer. found=false means no OAuth flow has materialised it yet;
// runtime hot paths never write — only GetOrCreateDefaultIssuer does.
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

// GetOrCreateDefaultIssuer returns the project's implicit default issuer,
// creating it on first touch. Reserved for stateful entry points (DCR,
// authorize/connect, dashboard mint) that write NOT NULL issuer FKs;
// hot paths use the read-only GetDefaultIssuer. Read-then-insert with
// ON CONFLICT DO NOTHING keeps concurrent first touches race-safe.
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
