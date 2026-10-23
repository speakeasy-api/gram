package environments

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// ResolveReadableEnvironment loads an environment the caller selected by slug
// or ID and requires environment:read on it before returning. Selecting an
// environment — to execute a tool with it, or to bind it to a source or toolset
// the caller can invoke — hands its plaintext values to code a project writer
// controls, so project access alone must not authorize it.
//
// A selection that does not resolve is still authorized against the project
// before not-found is reported, so the response cannot be used as an existence
// oracle by callers without environment:read. Every returned error is an
// *oops.ShareableError.
func ResolveReadableEnvironment(ctx context.Context, logger *slog.Logger, engine *authz.Engine, db repo.DBTX, projectID uuid.UUID, selected toolconfig.SlugOrID) (repo.Environment, error) {
	if selected.IsEmpty() {
		return repo.Environment{}, oops.E(oops.CodeBadRequest, nil, "environment id or slug is required")
	}

	queries := repo.New(db)
	var environment repo.Environment
	var err error
	if selected.ID != uuid.Nil {
		environment, err = queries.GetEnvironmentByID(ctx, repo.GetEnvironmentByIDParams{
			ID:        selected.ID,
			ProjectID: projectID,
		})
	} else {
		environment, err = queries.GetEnvironmentBySlug(ctx, repo.GetEnvironmentBySlugParams{
			Slug:      strings.ToLower(selected.Slug),
			ProjectID: projectID,
		})
	}

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if authErr := engine.Require(ctx, authz.EnvironmentCheck(authz.ScopeEnvironmentRead, projectID.String(), projectID.String())); authErr != nil {
			return repo.Environment{}, authErr
		}
		return repo.Environment{}, oops.E(oops.CodeNotFound, err, "environment not found")
	case err != nil:
		return repo.Environment{}, oops.E(oops.CodeUnexpected, err, "failed to load environment").LogError(ctx, logger, attr.SlogProjectID(projectID.String()))
	}

	if err := engine.Require(ctx, authz.EnvironmentCheck(authz.ScopeEnvironmentRead, environment.ID.String(), projectID.String())); err != nil {
		return repo.Environment{}, err
	}

	return environment, nil
}
