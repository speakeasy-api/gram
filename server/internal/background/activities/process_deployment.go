package activities

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sourcegraph/conc/pool"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/internal/assets"
	assetsRepo "github.com/speakeasy-api/gram/internal/assets/repo"
	"github.com/speakeasy-api/gram/internal/deployments/repo"
	"github.com/speakeasy-api/gram/internal/mv"
	"github.com/speakeasy-api/gram/internal/oops"
	"github.com/speakeasy-api/gram/internal/openapi"
	toolsRepo "github.com/speakeasy-api/gram/internal/tools/repo"
)

type ProcessDeployment struct {
	logger       *slog.Logger
	db           *pgxpool.Pool
	repo         *repo.Queries
	assets       *assetsRepo.Queries
	tools        *toolsRepo.Queries
	assetStorage assets.BlobStore
}

func NewProcessDeployment(logger *slog.Logger, db *pgxpool.Pool, assetStorage assets.BlobStore) *ProcessDeployment {
	return &ProcessDeployment{
		db:           db,
		repo:         repo.New(db),
		assets:       assetsRepo.New(db),
		assetStorage: assetStorage,
		tools:        toolsRepo.New(db),
		logger:       logger,
	}
}

func (p *ProcessDeployment) Do(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID) error {
	deployment, err := mv.DescribeDeployment(ctx, p.logger, p.repo, mv.ProjectID(projectID), mv.DeploymentID(deploymentID))
	if err != nil {
		return err
	}

	workers := pool.New().WithErrors().WithMaxGoroutines(2)
	perm := &atomic.Bool{}
	for _, docInfo := range deployment.Openapiv3Assets {
		logger := p.logger.With(
			slog.String("deployment_id", deployment.ID),
			slog.String("project_id", deployment.ProjectID),
			slog.String("openapi_id", docInfo.ID),
			slog.String("asset_id", docInfo.AssetID),
		)

		openapiDocID, err := uuid.Parse(docInfo.ID)
		if err != nil {
			return oops.E(oops.CodeInvariantViolation, err, "error parsing openapi document id").Log(ctx, logger)
		}

		assetID, err := uuid.Parse(docInfo.AssetID)
		if err != nil {
			return oops.E(oops.CodeInvariantViolation, err, "error parsing asset id").Log(ctx, logger)
		}

		asset, err := p.assets.GetProjectAsset(ctx, assetsRepo.GetProjectAssetParams{
			ID:        assetID,
			ProjectID: projectID,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "error getting asset").Log(ctx, logger)
		}

		u, err := url.Parse(asset.Url)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "error parsing asset URL").Log(ctx, logger)
		}

		workers.Go(func() error {
			processor := openapi.NewToolExtractor(p.logger, p.db, p.assetStorage)

			processErr := processor.Do(ctx, openapi.ToolExtractorTask{
				ProjectID:    projectID,
				DeploymentID: deploymentID,
				DocumentID:   openapiDocID,
				DocInfo:      docInfo,
				DocURL:       u,
			})

			if processErr == nil {
				trace.SpanFromContext(ctx).AddEvent("openapiv3_processed")
			}

			if processErr != nil {
				var se *oops.ShareableError
				if errors.As(processErr, &se) && !se.AsGoa().Temporary {
					perm.Store(true)
				}
			}

			return processErr
		})
	}

	err = workers.Wait()
	if perm.Load() {
		return temporal.NewApplicationErrorWithOptions("openapiv3 document was not processed successfully", "openapi_doc_error", temporal.ApplicationErrorOptions{
			NonRetryable: true,
			Cause:        err,
		})
	}

	if err != nil {
		return err
	}

	tools, err := p.tools.ListTools(ctx, toolsRepo.ListToolsParams{
		DeploymentID: uuid.NullUUID{UUID: deploymentID, Valid: true},
		ProjectID:    projectID,
		Cursor:       uuid.NullUUID{Valid: false, UUID: uuid.Nil},
	})
	if err != nil || len(tools) == 0 {
		err = oops.E(oops.CodeUnexpected, err, "no tools were created for deployment").Log(ctx, p.logger)
		return temporal.NewApplicationErrorWithOptions("openapiv3 document was not processed successfully", "openapi_doc_error", temporal.ApplicationErrorOptions{
			NonRetryable: true,
			Cause:        err,
		})
	}

	return nil
}
