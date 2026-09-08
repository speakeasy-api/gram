package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

// ErrTypeGitHubRepoConflict tags the non-retryable Temporal application error
// emitted when a project's computed GitHub repo is already connected to a
// different project (plugin_github_connections_installation_repo_key). Most
// often a stale row from a soft-deleted project that freed its slug for
// reuse. Retrying can't fix this — it needs a human to clean up the stale
// connection — so the workflow logs it once at warn and moves on instead of
// burning retries every tick.
const ErrTypeGitHubRepoConflict = "PluginGitHubRepoConflict"

type PluginPublishClient interface {
	PublishProject(ctx context.Context, input plugins.PublishProjectInput) (*plugins.PublishProjectResult, error)
}

type PluginPublisher struct {
	logger    *slog.Logger
	db        *pgxpool.Pool
	publisher PluginPublishClient
}

func NewPluginPublisher(logger *slog.Logger, db *pgxpool.Pool, publisher PluginPublishClient) *PluginPublisher {
	return &PluginPublisher{
		logger:    logger.With(attr.SlogComponent("plugin-publisher")),
		db:        db,
		publisher: publisher,
	}
}

type ListPluginPublishCandidatesInput struct {
	AfterProjectID *uuid.UUID
	Limit          int32
}

type PluginPublishCandidate struct {
	ProjectID       uuid.UUID
	CreatedByUserID string
}

type ListPluginPublishCandidatesResult struct {
	Candidates []PluginPublishCandidate
}

func (p *PluginPublisher) ListCandidates(ctx context.Context, input ListPluginPublishCandidatesInput) (*ListPluginPublishCandidatesResult, error) {
	if p.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 100
	}

	var after uuid.UUID
	if input.AfterProjectID != nil {
		after = *input.AfterProjectID
	}

	// Heal keys minted under a placeholder creator before listing, so the
	// subsequent actor lookup sees a real users.id and already-published
	// hooks/MCP keys start authenticating without a republish.
	repaired, err := keysrepo.New(p.db).RepairOrphanedAPIKeyCreators(ctx)
	if err != nil {
		return nil, fmt.Errorf("repair orphaned api key creators: %w", err)
	}
	if repaired > 0 {
		p.logger.InfoContext(ctx, "repaired orphaned api key creators")
	}

	rows, err := pluginsrepo.New(p.db).ListPluginPublishCandidates(ctx, pluginsrepo.ListPluginPublishCandidatesParams{
		AfterProjectID: after,
		ResultLimit:    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list plugin publish candidates: %w", err)
	}

	candidates := make([]PluginPublishCandidate, 0, len(rows))
	for _, row := range rows {
		if row.CreatedByUserID == "" || row.CreatedByUserID == "system" {
			p.logger.WarnContext(ctx, "plugin publish candidate has no real actor",
				attr.SlogProjectID(row.ProjectID.String()),
				attr.SlogUserID(row.CreatedByUserID),
			)
		}
		candidates = append(candidates, PluginPublishCandidate{
			ProjectID:       row.ProjectID,
			CreatedByUserID: row.CreatedByUserID,
		})
	}

	return &ListPluginPublishCandidatesResult{Candidates: candidates}, nil
}

func (p *PluginPublisher) PublishProject(ctx context.Context, input plugins.PublishProjectInput) (*plugins.PublishProjectResult, error) {
	if p.publisher == nil {
		return nil, fmt.Errorf("plugin publisher is not configured")
	}

	// Publishing runs from a Temporal activity, so nothing in the context
	// identifies a request. Say so, rather than letting the audit log record
	// this as a surface we failed to classify.
	ctx = contextvalues.SetActingSurface(ctx, string(audit.SurfaceSystem))

	result, err := p.publisher.PublishProject(ctx, input)
	if err != nil {
		if errors.Is(err, plugins.ErrGitHubRepoConflict) {
			// err.Error() on an *oops.ShareableError only returns the static
			// public message ("persist plugin api keys") — String() carries
			// the cause chain with the actual repo/owner that conflicted,
			// which is what an operator needs to act on this.
			detail := err.Error()
			if se, ok := err.(interface{ String() string }); ok {
				detail = se.String()
			}
			return nil, temporal.NewNonRetryableApplicationError(detail, ErrTypeGitHubRepoConflict, err)
		}
		return nil, fmt.Errorf("publish plugin project: %w", err)
	}
	return result, nil
}
