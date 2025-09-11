package functions

import (
	"context"
	"errors"
	"log/slog"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	slogmulti "github.com/samber/slog-multi"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/deployments/events"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type ToolExtractorTask struct {
	ProjectID    uuid.UUID
	DeploymentID uuid.UUID
	AttachmentID uuid.UUID
	Attachment   *types.DeploymentFunctions
	AssetURL     *url.URL
	ProjectSlug  string
	OrgSlug      string

	OnOperationSkipped func(err error)
}

type ToolExtractorResult struct{}

type ToolExtractor struct {
	logger       *slog.Logger
	db           *pgxpool.Pool
	assetStorage assets.BlobStore
}

func NewToolExtractor(logger *slog.Logger, db *pgxpool.Pool, assetStorage assets.BlobStore) *ToolExtractor {
	return &ToolExtractor{
		logger:       logger,
		db:           db,
		assetStorage: assetStorage,
	}
}

func (p *ToolExtractor) Do(
	ctx context.Context,
	task ToolExtractorTask,
) (*ToolExtractorResult, error) {
	assetURL := task.AssetURL
	projectID := task.ProjectID
	deploymentID := task.DeploymentID
	attachementID := task.AttachmentID
	attachement := task.Attachment
	if err := inv.Check("functions tool extractor task",
		"asset url set", assetURL != nil,
		"project id set", projectID != uuid.Nil,
		"deployment id set", deploymentID != uuid.Nil,
		"functions attachment id set", attachementID != uuid.Nil,
		"functions attachement info set", attachement != nil && attachement.Name != "" && attachement.Slug != "",
	); err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, oops.Permanent(err), "unable to verify functions attachement").Log(ctx, p.logger)
	}

	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error opening database transaction").Log(ctx, p.logger)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	tx := repo.New(dbtx)
	_ = tx

	slogArgs := []any{
		attr.SlogProjectID(projectID.String()),
		attr.SlogDeploymentFunctionsName(attachement.Name),
		attr.SlogDeploymentFunctionsSlug(string(attachement.Slug)),
		attr.SlogDeploymentID(deploymentID.String()),
		attr.SlogDeploymentFunctionsID(attachementID.String()),
		attr.SlogProjectSlug(task.ProjectSlug),
		attr.SlogOrganizationSlug(task.OrgSlug),
	}

	eventsHandler := events.NewLogHandler()
	logger := slog.New(slogmulti.Fanout(
		p.logger.Handler(),
		eventsHandler,
	)).With(slogArgs...)

	defer func() {
		if _, err := eventsHandler.Flush(ctx, p.db); err != nil {
			p.logger.ErrorContext(
				ctx,
				"failed to flush deployment events",
				attr.SlogError(err),
				attr.SlogProjectID(projectID.String()),
				attr.SlogDeploymentID(deploymentID.String()),
				attr.SlogDeploymentFunctionsID(attachementID.String()),
			)
		}
	}()

	_ = logger

	return nil, errors.New("not implemented")
}
