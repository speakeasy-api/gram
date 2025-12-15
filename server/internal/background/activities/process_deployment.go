package activities

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"runtime"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sourcegraph/conc/pool"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/assets"
	assetsRepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	externalmcpRepo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/openapi"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	resourcesRepo "github.com/speakeasy-api/gram/server/internal/resources/repo"
	toolsRepo "github.com/speakeasy-api/gram/server/internal/tools/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type ProcessDeployment struct {
	logger         *slog.Logger
	tracer         trace.Tracer
	tracerProvider trace.TracerProvider
	metrics        *metrics
	db             *pgxpool.Pool
	features       feature.Provider
	repo           *repo.Queries
	assets         *assetsRepo.Queries
	tools          *toolsRepo.Queries
	resources      *resourcesRepo.Queries
	assetStorage   assets.BlobStore
	projects       *projectsRepo.Queries
	billingRepo    billing.Repository
	externalmcp    *externalmcpRepo.Queries
	registryClient *externalmcp.RegistryClient
}

func NewProcessDeployment(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	features feature.Provider,
	assetStorage assets.BlobStore,
	billingRepo billing.Repository,
) *ProcessDeployment {
	return &ProcessDeployment{
		logger:         logger,
		tracer:         tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities"),
		tracerProvider: tracerProvider,
		metrics:        newMetrics(newMeter(meterProvider), logger),
		db:             db,
		features:       features,
		repo:           repo.New(db),
		assets:         assetsRepo.New(db),
		assetStorage:   assetStorage,
		tools:          toolsRepo.New(db),
		resources:      resourcesRepo.New(db),
		projects:       projectsRepo.New(db),
		billingRepo:    billingRepo,
		externalmcp:    externalmcpRepo.New(db),
		registryClient: externalmcp.NewRegistryClient(logger),
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

	workers := pool.New().WithErrors().WithMaxGoroutines(max(2, runtime.GOMAXPROCS(0)))

	if err := p.doFunctions(ctx, workers, orgData.ID, projectID, deploymentID, orgData.Slug, orgData.ProjectSlug, deployment); err != nil {
		return err
	}

	if err := p.doOpenAPIv3(ctx, workers, projectID, deploymentID, orgData.Slug, orgData.ProjectSlug, deployment); err != nil {
		return err
	}

	if err := p.doExternalMCPs(ctx, workers, projectID, deploymentID); err != nil {
		return err
	}

	err = workers.Wait()
	if errors.Is(err, oops.ErrPermanent) {
		return temporal.NewApplicationErrorWithOptions("openapiv3 document was not processed successfully", "openapi_doc_error", temporal.ApplicationErrorOptions{
			NonRetryable: true,
			Cause:        err,
		})
	}

	if err != nil {
		return err
	}

	tools, err := p.tools.ListHttpTools(ctx, toolsRepo.ListHttpToolsParams{
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

	functionTools, err := p.tools.ListFunctionTools(ctx, toolsRepo.ListFunctionToolsParams{
		DeploymentID: uuid.NullUUID{UUID: deploymentID, Valid: true},
		ProjectID:    projectID,
		Cursor:       uuid.NullUUID{Valid: false, UUID: uuid.Nil},
		Limit:        1,
	})
	if err != nil {
		err = oops.E(oops.CodeUnexpected, err, "failed to read list of function tools in deployment").Log(ctx, p.logger)
		return temporal.NewApplicationErrorWithOptions("deployment function tools could not be verified", "deployment_error", temporal.ApplicationErrorOptions{
			NonRetryable: true,
			Cause:        err,
		})
	}

	functionResources, err := p.resources.ListFunctionResources(ctx, resourcesRepo.ListFunctionResourcesParams{
		DeploymentID: uuid.NullUUID{UUID: deploymentID, Valid: true},
		ProjectID:    projectID,
		Cursor:       uuid.NullUUID{Valid: false, UUID: uuid.Nil},
		Limit:        1,
	})
	if err != nil {
		err = oops.E(oops.CodeUnexpected, err, "failed to read list of function resources in deployment").Log(ctx, p.logger)
		return temporal.NewApplicationErrorWithOptions("deployment function resources could not be verified", "deployment_error", temporal.ApplicationErrorOptions{
			NonRetryable: true,
			Cause:        err,
		})
	}

	// Get external MCPs to check if deployment has any
	externalMCPs, err := p.repo.ListDeploymentExternalMCPs(ctx, deploymentID)
	if err != nil {
		err = oops.E(oops.CodeUnexpected, err, "failed to read list of external mcps in deployment").Log(ctx, p.logger)
		return temporal.NewApplicationErrorWithOptions("deployment external mcps could not be verified", "deployment_error", temporal.ApplicationErrorOptions{
			NonRetryable: true,
			Cause:        err,
		})
	}

	// If there were documents to process in this deployment but no tools were created then we consider this a failure.
	// External MCPs don't produce tools at deployment time - they're expanded at runtime.
	expectsTools := len(deployment.Openapiv3Assets) > 0 || len(deployment.FunctionsAssets) > 0
	hasTools := len(tools) > 0 || len(functionTools) > 0
	hasResources := len(functionResources) > 0
	hasExternalMCPs := len(externalMCPs) > 0
	if expectsTools && !hasTools && !hasResources && !hasExternalMCPs {
		err = oops.E(oops.CodeUnexpected, err, "no tools were created for deployment").Log(ctx, p.logger)
		return temporal.NewApplicationErrorWithOptions("empty deployment was not expected", "deployment_error", temporal.ApplicationErrorOptions{
			NonRetryable: true,
			Cause:        err,
		})
	}

	return nil
}

func (p *ProcessDeployment) doOpenAPIv3(
	ctx context.Context,
	pool *pool.ErrorPool,
	projectID uuid.UUID,
	deploymentID uuid.UUID,
	orgSlug string,
	projectSlug string,
	deployment *types.Deployment,
) error {
	parser := "speakeasy"

	for _, docInfo := range deployment.Openapiv3Assets {
		logger := p.logger.With(
			attr.SlogDeploymentID(deployment.ID),
			attr.SlogProjectID(deployment.ProjectID),
			attr.SlogDeploymentOpenAPIID(docInfo.ID),
			attr.SlogDeploymentOpenAPISlug(docInfo.Slug),
			attr.SlogAssetID(docInfo.AssetID),
			attr.SlogOrganizationSlug(orgSlug),
			attr.SlogProjectSlug(projectSlug),
		)

		openapiDocID, err := uuid.Parse(docInfo.ID)
		if err != nil {
			return oops.E(oops.CodeInvariantViolation, err, "error parsing openapi document id").Log(ctx, logger)
		}

		assetID, err := uuid.Parse(docInfo.AssetID)
		if err != nil {
			return oops.E(oops.CodeInvariantViolation, err, "error parsing openapit asset id").Log(ctx, logger)
		}

		asset, err := p.assets.GetProjectAsset(ctx, assetsRepo.GetProjectAssetParams{
			ID:        assetID,
			ProjectID: projectID,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "error reading openapi asset url").Log(ctx, logger)
		}

		u, err := url.Parse(asset.Url)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "error parsing openapi asset URL").Log(ctx, logger)
		}

		pool.Go(func() (err error) {
			ctx, span := p.tracer.Start(ctx, "openapiv3.extractTools", trace.WithAttributes(
				attr.DeploymentOpenAPIParser(parser),
				attr.OrganizationSlug(orgSlug),
				attr.ProjectID(deployment.ProjectID),
				attr.ProjectSlug(projectSlug),
				attr.DeploymentID(deployment.ID),
				attr.DeploymentOpenAPIID(docInfo.ID),
				attr.DeploymentOpenAPISlug(docInfo.Slug),
				attr.AssetID(docInfo.AssetID),
			))
			defer func() {
				if err != nil {
					span.SetStatus(codes.Error, err.Error())
				}
				span.End()
			}()

			start := time.Now()

			processor := openapi.NewToolExtractor(p.logger, p.tracerProvider, p.db, p.features, p.assetStorage)

			res, err := processor.Do(ctx, openapi.ToolExtractorTask{
				Parser:       parser,
				ProjectID:    projectID,
				DeploymentID: deploymentID,
				DocumentID:   openapiDocID,
				DocInfo:      docInfo,
				DocURL:       u,
				ProjectSlug:  projectSlug,
				OrgSlug:      orgSlug,
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

			docVersion := "-"
			if res != nil {
				docVersion = res.DocumentVersion
			}

			outcome := o11y.OutcomeFromError(err)
			p.metrics.RecordOpenAPIProcessed(ctx, parser, outcome, time.Since(start), docVersion)
			if res != nil && res.DocumentUpgrade != nil {
				p.metrics.RecordOpenAPIUpgrade(ctx, parser, *res.DocumentUpgrade, res.DocumentUpgradeDuration, docVersion)
			}

			return err
		})
	}

	return nil
}

func (p *ProcessDeployment) doFunctions(
	ctx context.Context,
	pool *pool.ErrorPool,
	orgID string,
	projectID uuid.UUID,
	deploymentID uuid.UUID,
	orgSlug string,
	projectSlug string,
	deployment *types.Deployment,
) error {
	for _, attachment := range deployment.FunctionsAssets {
		logger := p.logger.With(
			attr.SlogDeploymentID(deployment.ID),
			attr.SlogProjectID(deployment.ProjectID),
			attr.SlogDeploymentFunctionsID(attachment.ID),
			attr.SlogDeploymentFunctionsSlug(attachment.Slug),
			attr.SlogAssetID(attachment.AssetID),
			attr.SlogOrganizationSlug(orgSlug),
			attr.SlogProjectSlug(projectSlug),
		)

		attachmentID, err := uuid.Parse(attachment.ID)
		if err != nil {
			return oops.E(oops.CodeInvariantViolation, err, "error parsing functions attachment id").Log(ctx, logger)
		}

		assetID, err := uuid.Parse(attachment.AssetID)
		if err != nil {
			return oops.E(oops.CodeInvariantViolation, err, "error parsing functions asset id").Log(ctx, logger)
		}

		asset, err := p.assets.GetProjectAsset(ctx, assetsRepo.GetProjectAssetParams{
			ID:        assetID,
			ProjectID: projectID,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "error reading functions asset url").Log(ctx, logger)
		}

		u, err := url.Parse(asset.Url)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "error parsing functions asset URL").Log(ctx, logger)
		}

		pool.Go(func() (err error) {
			ctx, span := p.tracer.Start(ctx, "functions.extractTools", trace.WithAttributes(
				attr.OrganizationSlug(orgSlug),
				attr.ProjectID(deployment.ProjectID),
				attr.ProjectSlug(projectSlug),
				attr.DeploymentID(deployment.ID),
				attr.DeploymentFunctionsID(attachment.ID),
				attr.DeploymentFunctionsSlug(attachment.Slug),
				attr.AssetID(attachment.AssetID),
			))
			defer func() {
				if err != nil {
					span.SetStatus(codes.Error, err.Error())
				}
				span.End()
			}()

			start := time.Now()

			processor := functions.NewToolExtractor(p.logger, p.db, p.assetStorage)

			res, err := processor.Do(ctx, functions.ToolExtractorTask{
				ProjectID:    projectID,
				DeploymentID: deploymentID,
				AttachmentID: attachmentID,
				Attachment:   attachment,
				AssetURL:     u,
				ProjectSlug:  projectSlug,
				OrgSlug:      orgSlug,
				OnToolSkipped: func(err error) {
					var perr *functions.ProcessError
					switch {
					case errors.As(err, &perr):
						p.metrics.RecordFunctionsToolSkipped(ctx, perr.Reason())
					default:
						p.metrics.RecordFunctionsToolSkipped(ctx, "unexpected")
					}
				},
			})

			outcome := o11y.OutcomeFromError(err)
			if err == nil {
				p.metrics.RecordFunctionsProcessed(ctx, time.Since(start), outcome, "-", 0, attachment.Runtime)
			}

			if res != nil {
				p.metrics.RecordFunctionsProcessed(ctx, time.Since(start), outcome, res.ManifestVersion, res.NumTools, attachment.Runtime)
			}

			return err
		})
	}

	return nil
}

func (p *ProcessDeployment) doExternalMCPs(
	ctx context.Context,
	pool *pool.ErrorPool,
	projectID uuid.UUID,
	deploymentID uuid.UUID,
) error {
	externalMCPs, err := p.repo.ListDeploymentExternalMCPs(ctx, deploymentID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error listing external mcps for deployment").Log(ctx, p.logger)
	}

	for _, mcp := range externalMCPs {
		logger := p.logger.With(
			attr.SlogDeploymentID(deploymentID.String()),
			attr.SlogProjectID(projectID.String()),
			attr.SlogExternalMCPID(mcp.ID.String()),
			attr.SlogExternalMCPSlug(mcp.Slug),
		)

		pool.Go(func() error {
			logger.InfoContext(ctx, "processing external mcp",
				attr.SlogExternalMCPName(mcp.Name),
				attr.SlogMCPRegistryID(mcp.RegistryID.String()),
			)

			// Get registry details to make API call
			registry, err := p.externalmcp.GetMCPRegistryByID(ctx, mcp.RegistryID)
			if err != nil {
				return oops.E(oops.CodeUnexpected, err, "error getting registry for external mcp").Log(ctx, logger)
			}

			serverDetails, err := p.registryClient.GetServerDetails(ctx, externalmcp.Registry{
				ID:  registry.ID,
				URL: registry.Url,
			}, mcp.Name)
			if err != nil {
				return oops.E(oops.CodeUnexpected, err, "error fetching server details from registry").Log(ctx, logger)
			}

			logger.InfoContext(ctx, "fetched server details from registry",
				attr.SlogURL(serverDetails.RemoteURL),
			)

			// Attempt to connect to detect OAuth requirements
			var requiresOAuth bool
			_, err = externalmcp.ListToolsFromProxy(ctx, logger, serverDetails.RemoteURL, nil)
			if _, ok := externalmcp.IsAuthRequiredError(err); ok {
				requiresOAuth = true
				logger.InfoContext(ctx, "external MCP server requires OAuth",
					attr.SlogURL(serverDetails.RemoteURL),
				)
			} else if err != nil {
				// Log but don't fail - the server might be temporarily unavailable
				logger.WarnContext(ctx, "failed to probe external MCP server for auth requirements",
					attr.SlogURL(serverDetails.RemoteURL),
					attr.SlogError(err),
				)
			}

			// Create a proxy tool URN for this external MCP
			// Format: tools:externalmcp:<slug>:proxy
			toolURN := urn.Tool{
				Kind:   urn.ToolKindExternalMCP,
				Source: mcp.Slug,
				Name:   "proxy",
			}

			// Create the proxy tool definition with the remote URL and OAuth info
			_, err = p.externalmcp.CreateExternalMCPToolDefinition(ctx, externalmcpRepo.CreateExternalMCPToolDefinitionParams{
				ExternalMcpAttachmentID: mcp.ID,
				ToolUrn:                 toolURN.String(),
				RemoteUrl:               serverDetails.RemoteURL,
				RequiresOauth:           requiresOAuth,
			})
			if err != nil {
				return oops.E(oops.CodeUnexpected, err, "error creating external mcp tool definition").Log(ctx, logger)
			}

			logger.InfoContext(ctx, "created external mcp proxy tool",
				attr.SlogToolURN(toolURN.String()),
				attr.SlogOAuthRequired(requiresOAuth),
			)

			return nil
		})
	}

	return nil
}
