package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/auth/repo"
	"github.com/speakeasy-api/gram/internal/contextvalues"
)

var (
	ErrAuthCheckFailed    = errors.New("check failed")
	ErrAuthNoSession      = errors.New("no session found")
	ErrAuthInvalidOrgID   = errors.New("invalid session organization")
	ErrAuthInvalidProject = errors.New("invalid project")
	ErrAuthAccessDenied   = errors.New("access denied")
)

func checkProjectAccess(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, projectSlug string) (context.Context, error) {
	if projectSlug == "" {
		return ctx, fmt.Errorf("project access: %w: empty slug", ErrAuthInvalidProject)
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok {
		return ctx, fmt.Errorf("project access: %w", ErrAuthNoSession)
	}

	r := repo.New(db)
	row, err := r.CanAccessProject(ctx, repo.CanAccessProjectParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectSlug:    projectSlug,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ctx, fmt.Errorf("project access: %w", ErrAuthAccessDenied)
	case err != nil:
		logger.ErrorContext(ctx, "project access lookup error", slog.String("project_id", row.ID.String()), slog.String("org_id", authCtx.ActiveOrganizationID), slog.String("error", err.Error()))
		return ctx, fmt.Errorf("project access: %w: %w", ErrAuthCheckFailed, err)
	case row.Deleted:
		return ctx, fmt.Errorf("project access: %w", ErrAuthAccessDenied)
	}

	authCtx.ProjectID = &row.ID
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	return ctx, nil
}
