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
	"github.com/speakeasy-api/gram/internal/sessions"
)

var (
	ErrAuthCheckFailed    = errors.New("check failed")
	ErrAuthNoSession      = errors.New("no session found")
	ErrAuthInvalidOrgID   = errors.New("invalid session organization")
	ErrAuthInvalidProject = errors.New("invalid project")
	ErrAuthAccessDenied   = errors.New("access denied")
)

type ProjectAccess struct {
	ProjectID      uuid.UUID
	OrganizationID string
	UserID         string
}

func EnsureProjectAccess(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, projectSlug string) (*ProjectAccess, error) {
	if projectSlug == "" {
		return nil, fmt.Errorf("project access: %w: empty slug", ErrAuthInvalidProject)
	}

	session, ok := sessions.GetSessionValueFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("project access: %w", ErrAuthNoSession)
	}

	r := repo.New(db)
	row, err := r.CanAccessProject(ctx, repo.CanAccessProjectParams{
		OrganizationID: session.ActiveOrganizationID,
		ProjectSlug:    projectSlug,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, fmt.Errorf("project access: %w", ErrAuthAccessDenied)
	case err != nil:
		logger.ErrorContext(ctx, "project access lookup error", slog.String("project_id", row.ID.String()), slog.String("org_id", session.ActiveOrganizationID), slog.String("error", err.Error()))
		return nil, fmt.Errorf("project access: %w: %w", ErrAuthCheckFailed, err)
	case row.Deleted:
		return nil, fmt.Errorf("project access: %w", ErrAuthAccessDenied)
	}

	return &ProjectAccess{OrganizationID: session.ActiveOrganizationID, ProjectID: row.ID, UserID: session.UserID}, nil
}
