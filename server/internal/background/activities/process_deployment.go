package activities

import (
	"context"
	"log/slog"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sourcegraph/conc/pool"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/internal/assets"
	assetsRepo "github.com/speakeasy-api/gram/internal/assets/repo"
	"github.com/speakeasy-api/gram/internal/deployments/repo"
	"github.com/speakeasy-api/gram/internal/mv"
	"github.com/speakeasy-api/gram/internal/oops"
	"github.com/speakeasy-api/gram/internal/openapi"
)

type ProcessDeployment struct {
	logger       *slog.Logger
	db           *pgxpool.Pool
	repo         *repo.Queries
	assets       *assetsRepo.Queries
	assetStorage assets.BlobStore
}

func NewProcessDeployment(logger *slog.Logger, db *pgxpool.Pool, assetStorage assets.BlobStore) *ProcessDeployment {
	return &ProcessDeployment{
		db:           db,
		repo:         repo.New(db),
		assets:       assetsRepo.New(db),
		assetStorage: assetStorage,
		logger:       logger,
	}
}

func (p *ProcessDeployment) Do(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID) error {
	deployment, err := mv.DescribeDeployment(ctx, p.logger, p.repo, mv.ProjectID(projectID), mv.DeploymentID(deploymentID))
	if err != nil {
		return err
	}

	logger := p.logger.With(
		slog.String("deployment_id", deployment.ID),
		slog.String("project_id", deployment.ProjectID),
	)

	workers := pool.New().WithErrors().WithMaxGoroutines(2)
	for _, docInfo := range deployment.Openapiv3Assets {
		logger = p.logger.With(
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
				return oops.E(oops.CodeUnexpected, processErr, "openapiv3 document was not processed successfully").Log(ctx, logger)
			}

			return nil
		})
	}

	return workers.Wait()
}
