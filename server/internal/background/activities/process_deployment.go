package activities

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sourcegraph/conc/pool"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/assets"
	assetsRepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/openapi"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	toolsRepo "github.com/speakeasy-api/gram/server/internal/tools/repo"
)

type ProcessDeployment struct {
	logger       *slog.Logger
	tracer       trace.Tracer
	metrics      *metrics
	db           *pgxpool.Pool
	features     feature.Provider
	repo         *repo.Queries
	assets       *assetsRepo.Queries
	tools        *toolsRepo.Queries
	assetStorage assets.BlobStore
	projects     *projectsRepo.Queries
}

func NewProcessDeployment(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	features feature.Provider,
	assetStorage assets.BlobStore,
) *ProcessDeployment {
	return &ProcessDeployment{
		logger:       logger,
		tracer:       tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities"),
		metrics:      newMetrics(newMeter(meterProvider), logger),
		db:           db,
		features:     features,
		repo:         repo.New(db),
		assets:       assetsRepo.New(db),
		assetStorage: assetStorage,
		tools:        toolsRepo.New(db),
		projects:     projectsRepo.New(db),
	}
}

func (p *ProcessDeployment) Do(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID) error {
	deployment, err := mv.DescribeDeployment(ctx, p.logger, p.repo, mv.ProjectID(projectID), mv.DeploymentID(deploymentID))
	if err != nil {
		return err
	}

	orgData, err := p.projects.GetProjectWithOrganizationMetadata(ctx, uuid.MustParse(deployment.ProjectID))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error loading organization metadata").Log(ctx, p.logger)
	}

	f := conv.Default[feature.Provider](p.features, &feature.InMemory{})
	useSpeakeasy, err := f.IsFlagEnabled(ctx, feature.FlagSpeakeasyOpenAPIParserV0, projectID.String())
	if err != nil {
		useSpeakeasy = false
		p.logger.ErrorContext(
			ctx, "error checking openapi parser feature flag for organization",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgData.ID),
			attr.SlogOrganizationSlug(orgData.Slug),
			attr.SlogProjectID(projectID.String()),
			attr.SlogProjectSlug(orgData.ProjectSlug),
		)
	}

	parser := "libopenapi"
	if useSpeakeasy {
		parser = "speakeasy"
	}

	workers := pool.New().WithErrors().WithMaxGoroutines(2)
	perm := &atomic.Bool{}
	for _, docInfo := range deployment.Openapiv3Assets {
		logger := p.logger.With(
			attr.SlogDeploymentID(deployment.ID),
			attr.SlogProjectID(deployment.ProjectID),
			attr.SlogDeploymentOpenAPIID(docInfo.ID),
			attr.SlogAssetID(docInfo.AssetID),
			attr.SlogOrganizationSlug(orgData.Slug),
			attr.SlogProjectSlug(orgData.ProjectSlug),
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
			var processErr error
			var res *openapi.ToolExtractorResult

			ctx, span := p.tracer.Start(ctx, "openapiv3.extractTools", trace.WithAttributes(
				attr.DeploymentOpenAPIParser(parser),
				attr.OrganizationID(orgData.ID),
				attr.OrganizationSlug(orgData.Slug),
				attr.ProjectID(deployment.ProjectID),
				attr.ProjectSlug(orgData.ProjectSlug),
				attr.DeploymentID(deployment.ID),
				attr.DeploymentOpenAPIID(docInfo.ID),
				attr.AssetID(docInfo.AssetID),
			))
			defer func() {
				if processErr != nil {
					span.SetStatus(codes.Error, processErr.Error())
				}
				span.End()
			}()

			start := time.Now()

			processor := openapi.NewToolExtractor(p.logger, p.db, p.features, p.assetStorage)

			res, processErr = processor.Do(ctx, openapi.ToolExtractorTask{
				Parser: parser,

				ProjectID:    projectID,
				DeploymentID: deploymentID,
				DocumentID:   openapiDocID,
				DocInfo:      docInfo,
				DocURL:       u,
				ProjectSlug:  orgData.ProjectSlug,
				OrgSlug:      orgData.Slug,
				OnOperationSkipped: func(err error) {
					var perr *openapi.ProcessError
					switch {
					case errors.As(err, &perr):
						p.metrics.RecordOpenAPIOperationSkipped(ctx, perr.Reason())
					default:
						p.metrics.RecordOpenAPIOperationSkipped(ctx, "unexpected")
					}
				},
			})

			if processErr != nil {
				var se *oops.ShareableError
				if errors.As(processErr, &se) && !se.AsGoa().Temporary {
					perm.Store(true)
				}
			}

			docVersion := "-"
			if res != nil {
				docVersion = res.DocumentVersion
			}

			p.metrics.RecordOpenAPIProcessed(ctx, parser, o11y.OutcomeFromError(processErr), time.Since(start), docVersion)
			if res != nil && res.DocumentUpgrade != nil {
				p.metrics.RecordOpenAPIUpgrade(ctx, parser, *res.DocumentUpgrade, res.DocumentUpgradeDuration, docVersion)
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
		Limit:        1,
	})
	if err != nil {
		err = oops.E(oops.CodeUnexpected, err, "failed to read list of tools in deployment").Log(ctx, p.logger)
		return temporal.NewApplicationErrorWithOptions("deployment tools could not be verified", "deployment_error", temporal.ApplicationErrorOptions{
			NonRetryable: true,
			Cause:        err,
		})
	}

	// If there were documents to process in this deployment but no tools were created then we consider this a failure.
	expectsTools := len(deployment.Openapiv3Assets) > 0
	if expectsTools && len(tools) == 0 {
		err = oops.E(oops.CodeUnexpected, err, "no tools were created for deployment").Log(ctx, p.logger)
		return temporal.NewApplicationErrorWithOptions("empty deployment was not expected", "deployment_error", temporal.ApplicationErrorOptions{
			NonRetryable: true,
			Cause:        err,
		})
	}

	return nil
}
