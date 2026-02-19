package deployments

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	temporalSDK "go.temporal.io/sdk/temporal"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/deployments"
	srv "github.com/speakeasy-api/gram/server/gen/http/deployments/server"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/assets"
	assetsRepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	externalmcpRepo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	packagesRepo "github.com/speakeasy-api/gram/server/internal/packages/repo"
	"github.com/speakeasy-api/gram/server/internal/packages/semver"
	"github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

type Service struct {
	logger         *slog.Logger
	tracer         trace.Tracer
	db             *pgxpool.Pool
	repo           *repo.Queries
	externalmcp    *externalmcpRepo.Queries
	registryClient *externalmcp.RegistryClient
	auth           *auth.Auth
	assets         *assetsRepo.Queries
	packages       *packagesRepo.Queries
	assetStorage   assets.BlobStore
	temporalEnv    *temporal.Environment
	posthog        *posthog.Posthog
	siteURL        *url.URL
}

var _ gen.Service = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	temporalEnv *temporal.Environment,
	sessions *sessions.Manager,
	assetStorage assets.BlobStore,
	posthog *posthog.Posthog,
	siteURL *url.URL,
	mcpRegistryClient *externalmcp.RegistryClient,
) *Service {
	logger = logger.With(attr.SlogComponent("deployments"))
	tracer := tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/deployments")

	return &Service{
		logger:         logger,
		tracer:         tracer,
		db:             db,
		repo:           repo.New(db),
		externalmcp:    externalmcpRepo.New(db),
		auth:           auth.New(logger, db, sessions),
		assets:         assetsRepo.New(db),
		packages:       packagesRepo.New(db),
		assetStorage:   assetStorage,
		temporalEnv:    temporalEnv,
		posthog:        posthog,
		siteURL:        siteURL,
		registryClient: mcpRegistryClient,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) GetDeployment(ctx context.Context, form *gen.GetDeploymentPayload) (res *gen.GetDeploymentResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	id, err := uuid.Parse(form.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "error parsing deployment id").Log(ctx, s.logger)
	}

	dep, err := mv.DescribeDeployment(ctx, s.logger, s.repo, mv.ProjectID(*authCtx.ProjectID), mv.DeploymentID(id))
	if err != nil {
		return nil, err
	}

	if dep == nil {
		return nil, oops.C(oops.CodeNotFound)
	}

	return &gen.GetDeploymentResult{
		ID:                   dep.ID,
		CreatedAt:            dep.CreatedAt,
		OrganizationID:       dep.OrganizationID,
		ProjectID:            dep.ProjectID,
		UserID:               dep.UserID,
		IdempotencyKey:       dep.IdempotencyKey,
		Status:               dep.Status,
		ExternalID:           dep.ExternalID,
		ExternalURL:          dep.ExternalURL,
		GithubRepo:           dep.GithubRepo,
		GithubPr:             dep.GithubPr,
		GithubSha:            dep.GithubSha,
		ClonedFrom:           dep.ClonedFrom,
		Packages:             dep.Packages,
		Openapiv3Assets:      dep.Openapiv3Assets,
		Openapiv3ToolCount:   dep.Openapiv3ToolCount,
		FunctionsToolCount:   dep.FunctionsToolCount,
		ExternalMcpToolCount: dep.ExternalMcpToolCount,
		FunctionsAssets:      dep.FunctionsAssets,
		ExternalMcps:         dep.ExternalMcps,
	}, nil
}

func (s *Service) GetDeploymentLogs(ctx context.Context, form *gen.GetDeploymentLogsPayload) (res *gen.GetDeploymentLogsResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	id, err := uuid.Parse(form.DeploymentID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "bad deployment id").Log(ctx, s.logger)
	}

	var cursorSeq pgtype.Int8
	if form.Cursor != nil && *form.Cursor != "" {
		seq, _, err := decodeCursor(*form.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
		}
		cursorSeq = pgtype.Int8{Int64: seq, Valid: true}
	}

	rows, err := s.repo.GetDeploymentLogs(ctx, repo.GetDeploymentLogsParams{
		DeploymentID: id,
		ProjectID:    *authCtx.ProjectID,
		CursorSeq:    cursorSeq,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error getting deployment logs").Log(ctx, s.logger)
	}

	status := "unknown"
	if len(rows) > 0 {
		status = rows[0].Status
	}

	items := make([]*gen.DeploymentLogEvent, 0, len(rows))
	for _, r := range rows {
		var attachmentID *string
		if r.AttachmentID.Valid {
			attachmentID = new(r.AttachmentID.UUID.String())
		}
		items = append(items, &gen.DeploymentLogEvent{
			ID:             r.ID.String(),
			Event:          r.Event,
			Message:        r.Message,
			AttachmentID:   attachmentID,
			AttachmentType: conv.FromPGText[string](r.AttachmentType),
			CreatedAt:      r.CreatedAt.Time.Format(time.RFC3339),
		})
	}

	var nextCursor *string
	limit := 50
	if len(rows) >= limit+1 {
		nextCursor = new(encodeCursor(rows[limit].Seq, rows[limit].ID))
		items = items[:limit]
	}

	return &gen.GetDeploymentLogsResult{
		Events:     items,
		Status:     status,
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) GetLatestDeployment(ctx context.Context, _ *gen.GetLatestDeploymentPayload) (res *gen.GetLatestDeploymentResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing deployments").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	tx := s.repo.WithTx(dbtx)

	id, err := tx.GetLatestDeploymentID(ctx, *authCtx.ProjectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &gen.GetLatestDeploymentResult{
				Deployment: nil,
			}, nil
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error getting latest deployment id").Log(ctx, s.logger)
	}

	if id == uuid.Nil {
		return &gen.GetLatestDeploymentResult{
			Deployment: nil,
		}, nil
	}

	dep, err := mv.DescribeDeployment(ctx, s.logger, tx, mv.ProjectID(*authCtx.ProjectID), mv.DeploymentID(id))
	if err != nil {
		return nil, err
	}

	return &gen.GetLatestDeploymentResult{
		Deployment: dep,
	}, nil
}

func (s *Service) GetActiveDeployment(ctx context.Context, _ *gen.GetActiveDeploymentPayload) (res *gen.GetActiveDeploymentResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing deployments").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	tx := s.repo.WithTx(dbtx)

	id, err := tx.GetActiveDeploymentID(ctx, *authCtx.ProjectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &gen.GetActiveDeploymentResult{
				Deployment: nil,
			}, nil
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error getting active deployment id").Log(ctx, s.logger)
	}

	if id == uuid.Nil {
		return &gen.GetActiveDeploymentResult{
			Deployment: nil,
		}, nil
	}

	dep, err := mv.DescribeDeployment(ctx, s.logger, tx, mv.ProjectID(*authCtx.ProjectID), mv.DeploymentID(id))
	if err != nil {
		return nil, err
	}

	return &gen.GetActiveDeploymentResult{
		Deployment: dep,
	}, nil
}

func (s *Service) ListDeployments(ctx context.Context, form *gen.ListDeploymentsPayload) (res *gen.ListDeploymentResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	var cursor uuid.NullUUID
	if form.Cursor != nil {
		c, err := uuid.Parse(*form.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
		}

		cursor = uuid.NullUUID{UUID: c, Valid: true}
	}

	rows, err := s.repo.ListDeployments(ctx, repo.ListDeploymentsParams{
		ProjectID: *authCtx.ProjectID,
		Cursor:    cursor,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing deployments").Log(ctx, s.logger)
	}

	items := make([]*gen.DeploymentSummary, 0, len(rows))
	for _, r := range rows {
		items = append(items, &gen.DeploymentSummary{
			ID:                    r.ID.String(),
			UserID:                r.UserID,
			Status:                r.Status,
			CreatedAt:             r.CreatedAt.Time.Format(time.RFC3339),
			Openapiv3AssetCount:   r.Openapiv3AssetCount,
			Openapiv3ToolCount:    r.Openapiv3ToolCount,
			FunctionsAssetCount:   r.FunctionsAssetCount,
			FunctionsToolCount:    r.FunctionsToolCount,
			ExternalMcpAssetCount: r.ExternalMcpAssetCount,
			ExternalMcpToolCount:  r.ExternalMcpToolCount,
		})
	}

	var nextCursor *string
	limit := 50
	if len(items) >= limit+1 {
		nextCursor = new(items[limit].ID)
		items = items[:limit]
	}

	return &gen.ListDeploymentResult{
		NextCursor: nextCursor,
		Items:      items,
	}, nil
}

func (s *Service) CreateDeployment(ctx context.Context, form *gen.CreateDeploymentPayload) (*gen.CreateDeploymentResult, error) {
	logger := s.logger
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	organizationID := authCtx.ActiveOrganizationID
	projectID := *authCtx.ProjectID

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attr.OrganizationID(organizationID),
		attr.ProjectID(projectID.String()),
		attr.UserID(authCtx.UserID),
		attr.SessionID(conv.PtrValOr(authCtx.SessionID, "")),
	)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing deployments").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	tx := s.repo.WithTx(dbtx)

	newOpenAPIAssets := make([]upsertOpenAPIv3, 0, len(form.Openapiv3Assets))
	for _, add := range form.Openapiv3Assets {
		assetID, err := uuid.Parse(add.AssetID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "error parsing openapi asset id").Log(ctx, s.logger)
		}

		newOpenAPIAssets = append(newOpenAPIAssets, upsertOpenAPIv3{
			assetID: assetID,
			name:    add.Name,
			slug:    string(add.Slug),
		})
	}

	newFunctions := make([]upsertFunctions, 0, len(form.Functions))
	for _, add := range form.Functions {
		assetID, err := uuid.Parse(add.AssetID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "error parsing functions asset id").Log(ctx, s.logger)
		}

		newFunctions = append(newFunctions, upsertFunctions{
			assetID: assetID,
			name:    add.Name,
			slug:    string(add.Slug),
			runtime: add.Runtime,
		})
	}

	pkgInputs := make([][2]string, 0, len(form.Packages))
	for _, add := range form.Packages {
		pkgInputs = append(pkgInputs, [2]string{add.Name, conv.PtrValOr(add.Version, "")})
	}

	resolved, err := s.resolvePackages(ctx, s.packages.WithTx(dbtx), pkgInputs)
	if err != nil {
		return nil, err
	}

	newPackages := make([]upsertPackage, 0, len(resolved))
	for i, pkg := range resolved {
		if err := validatePackageInclusion(ctx, logger, projectID, pkgInputs[i], pkg); err != nil {
			return nil, err
		}

		newPackages = append(newPackages, upsertPackage{
			packageID: pkg.packageID,
			versionID: pkg.versionID,
		})
	}

	newExternalMCPs := make([]upsertExternalMCP, 0, len(form.ExternalMcps))
	for _, add := range form.ExternalMcps {
		registryID, err := uuid.Parse(add.RegistryID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "error parsing external mcp registry id").Log(ctx, s.logger)
		}

		newExternalMCPs = append(newExternalMCPs, upsertExternalMCP{
			registryID:              registryID,
			name:                    add.Name,
			slug:                    string(add.Slug),
			registryServerSpecifier: add.RegistryServerSpecifier,
		})
	}

	if len(newPackages) == 0 && len(newOpenAPIAssets) == 0 && len(newFunctions) == 0 && len(newExternalMCPs) == 0 {
		return nil, oops.E(oops.CodeInvalid, nil, "at least one openapi document, functions file, package, or external mcp is required").Log(ctx, logger)
	}

	newID, err := createDeployment(
		ctx, s.tracer, logger, tx,
		IdempotencyKey(&form.IdempotencyKey),
		deploymentFields{
			projectID:      projectID,
			userID:         authCtx.UserID,
			organizationID: authCtx.ActiveOrganizationID,
			externalID:     conv.PtrValOr(form.ExternalID, ""),
			externalURL:    conv.PtrValOr(form.ExternalURL, ""),
			githubRepo:     conv.PtrValOr(form.GithubRepo, ""),
			githubPr:       conv.PtrValOr(form.GithubPr, ""),
			githubSha:      conv.PtrValOr(form.GithubSha, ""),
		},
		newOpenAPIAssets,
		newFunctions,
		newPackages,
		newExternalMCPs,
	)
	if err != nil {
		return nil, err
	}

	logger = logger.With(attr.SlogDeploymentID(newID.String()))
	span.SetAttributes(attr.DeploymentID(newID.String()))

	dep, err := mv.DescribeDeployment(ctx, logger, tx, mv.ProjectID(projectID), mv.DeploymentID(newID))
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving deployment").Log(ctx, logger)
	}

	status := dep.Status
	if status == "created" {
		s, err := s.startDeployment(ctx, logger, projectID, newID, dep, form.NonBlocking != nil && *form.NonBlocking)
		if err != nil {
			return nil, err
		}

		status = s
		if status == "" {
			return nil, oops.E(oops.CodeInvariantViolation, nil, "error resolving deployment status").Log(ctx, logger)
		}

		dep.Status = status
	}

	// Re-read the deployment to get the latest status, tool count etc
	dep, err = mv.DescribeDeployment(ctx, logger, s.repo, mv.ProjectID(projectID), mv.DeploymentID(newID))
	if err != nil {
		return nil, err
	}

	s.captureDeploymentProcessedEvent(ctx, logger, authCtx.OrganizationSlug, *authCtx.ProjectSlug, "create", dep, nil)

	return &gen.CreateDeploymentResult{
		Deployment: dep,
	}, nil
}

func (s *Service) Evolve(ctx context.Context, form *gen.EvolvePayload) (*gen.EvolveResult, error) {
	span := trace.SpanFromContext(ctx)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	organizationID := authCtx.ActiveOrganizationID
	projectID := *authCtx.ProjectID

	logger := s.logger.With(attr.SlogProjectID(projectID.String()), attr.SlogOrganizationID(organizationID))
	span.SetAttributes(attr.ProjectID(projectID.String()), attr.OrganizationID(organizationID))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing deployments").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	tx := s.repo.WithTx(dbtx)
	pkgTx := s.packages.WithTx(dbtx)

	openapiv3ToUpsert := make([]upsertOpenAPIv3, 0, len(form.UpsertOpenapiv3Assets))
	for _, add := range form.UpsertOpenapiv3Assets {
		assetID, err := uuid.Parse(add.AssetID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "error parsing openapiv3 asset id to upsert").Log(ctx, s.logger)
		}

		openapiv3ToUpsert = append(openapiv3ToUpsert, upsertOpenAPIv3{
			assetID: assetID,
			name:    add.Name,
			slug:    string(add.Slug),
		})
	}

	openapiv3ToExclude := make([]uuid.UUID, 0, len(form.ExcludeOpenapiv3Assets))
	for _, assetID := range form.ExcludeOpenapiv3Assets {
		id, err := uuid.Parse(assetID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "error parsing openapiv3 asset id to exclude").Log(ctx, s.logger)
		}
		openapiv3ToExclude = append(openapiv3ToExclude, id)
	}

	functionsToUpsert := make([]upsertFunctions, 0, len(form.UpsertFunctions))
	for _, add := range form.UpsertFunctions {
		assetID, err := uuid.Parse(add.AssetID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "error parsing functions asset id to upsert").Log(ctx, s.logger)
		}

		functionsToUpsert = append(functionsToUpsert, upsertFunctions{
			assetID: assetID,
			name:    add.Name,
			slug:    string(add.Slug),
			runtime: add.Runtime,
		})
	}

	functionsToExclude := make([]uuid.UUID, 0, len(form.ExcludeFunctions))
	for _, assetID := range form.ExcludeFunctions {
		id, err := uuid.Parse(assetID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "error parsing functions asset id to exclude").Log(ctx, s.logger)
		}
		functionsToExclude = append(functionsToExclude, id)
	}

	pkgInputs := make([][2]string, 0, len(form.UpsertPackages))
	for _, add := range form.UpsertPackages {
		pkgInputs = append(pkgInputs, [2]string{add.Name, conv.PtrValOr(add.Version, "")})
	}

	resolved, err := s.resolvePackages(ctx, pkgTx, pkgInputs)
	if err != nil {
		return nil, err
	}

	packagesToUpsert := make([]upsertPackage, 0, len(resolved))
	for i, pkg := range resolved {
		if err := validatePackageInclusion(ctx, logger, projectID, pkgInputs[i], pkg); err != nil {
			return nil, err
		}

		packagesToUpsert = append(packagesToUpsert, upsertPackage{
			packageID: pkg.packageID,
			versionID: pkg.versionID,
		})
	}

	packagesToExclude := make([]uuid.UUID, 0, len(form.ExcludePackages))
	for _, pkgID := range form.ExcludePackages {
		id, err := uuid.Parse(pkgID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "error parsing package id to exclude").Log(ctx, s.logger)
		}
		packagesToExclude = append(packagesToExclude, id)
	}

	externalMCPsToUpsert := make([]upsertExternalMCP, 0, len(form.UpsertExternalMcps))
	for _, add := range form.UpsertExternalMcps {
		registryID, err := uuid.Parse(add.RegistryID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "error parsing external mcp registry id to upsert").Log(ctx, s.logger)
		}

		externalMCPsToUpsert = append(externalMCPsToUpsert, upsertExternalMCP{
			registryID:              registryID,
			name:                    add.Name,
			slug:                    string(add.Slug),
			registryServerSpecifier: add.RegistryServerSpecifier,
		})
	}

	println("\n\n\nto upsert: ", len(externalMCPsToUpsert), "\n\n\n")

	externalMCPsToExclude := make([]string, 0, len(form.ExcludeExternalMcps))
	externalMCPsToExclude = append(externalMCPsToExclude, form.ExcludeExternalMcps...)

	packagesChanged := len(packagesToUpsert) > 0 || len(packagesToExclude) > 0
	openapiChanged := len(openapiv3ToUpsert) > 0 || len(openapiv3ToExclude) > 0
	functionsChanged := len(functionsToUpsert) > 0 || len(functionsToExclude) > 0
	externalMCPsChanged := len(externalMCPsToUpsert) > 0 || len(externalMCPsToExclude) > 0

	if !packagesChanged && !openapiChanged && !functionsChanged && !externalMCPsChanged {
		return nil, oops.E(oops.CodeInvalid, nil, "at least one asset, package, or external mcp to upsert or exclude is required").Log(ctx, logger)
	}

	var cloneID uuid.UUID
	var previousDeployment *types.Deployment

	latestDeploymentID, err := tx.GetLatestDeploymentID(ctx, projectID)
	switch {
	// 1️⃣ Project has no deployments, we need to create an initial one instead of cloning
	case errors.Is(err, sql.ErrNoRows), latestDeploymentID == uuid.Nil:
		newID, err := createDeployment(
			ctx, s.tracer, logger, tx,
			IdempotencyKey(nil),
			deploymentFields{
				projectID:      projectID,
				userID:         authCtx.UserID,
				organizationID: authCtx.ActiveOrganizationID,
				externalID:     "",
				externalURL:    "",
				githubRepo:     "",
				githubPr:       "",
				githubSha:      "",
			},
			openapiv3ToUpsert,
			functionsToUpsert,
			packagesToUpsert,
			externalMCPsToUpsert,
		)
		if err != nil {
			return nil, err
		}

		ierr := inv.Check("initial deployment state", "deployment id cannot be nil", newID != uuid.Nil)
		if ierr != nil {
			return nil, oops.E(oops.CodeInvariantViolation, ierr, "error cloning deployment").Log(ctx, logger)
		}

		logger = logger.With(attr.SlogDeploymentID(newID.String()))
		span.SetAttributes(attr.DeploymentID(newID.String()))

		cloneID = newID
	// 2️⃣ Something went wrong querying for the latest deployment
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error getting latest deployment").Log(ctx, logger)
	// 3️⃣ We found a latest deployment, we need to clone it
	default:
		previousDeployment, err = mv.DescribeDeployment(ctx, logger, tx, mv.ProjectID(projectID), mv.DeploymentID(latestDeploymentID))
		if err != nil {
			return nil, err
		}

		newID, err := cloneDeployment(
			ctx, s.tracer, logger, tx,
			ProjectID(projectID), DeploymentID(latestDeploymentID),
			openapiv3ToUpsert,
			functionsToUpsert,
			packagesToUpsert,
			externalMCPsToUpsert,
			openapiv3ToExclude,
			functionsToExclude,
			packagesToExclude,
			externalMCPsToExclude,
		)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error cloning deployment").Log(ctx, logger)
		}

		ierr := inv.Check("cloned deployment state", "deployment id cannot be nil", newID != uuid.Nil)
		if ierr != nil {
			return nil, oops.E(oops.CodeInvariantViolation, ierr, "error cloning deployment").Log(ctx, logger)
		}

		logger = logger.With(attr.SlogDeploymentID(newID.String()))
		span.SetAttributes(attr.DeploymentID(newID.String()))

		cloneID = newID
	}

	dep, err := mv.DescribeDeployment(ctx, logger, tx, mv.ProjectID(projectID), mv.DeploymentID(cloneID))
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving deployment").Log(ctx, logger)
	}

	status := dep.Status
	if status == "created" {
		s, err := s.startDeployment(ctx, logger, projectID, cloneID, dep, form.NonBlocking != nil && *form.NonBlocking)
		if err != nil {
			return nil, err
		}

		status = s
		if status == "" {
			return nil, oops.E(oops.CodeInvariantViolation, nil, "unable to resolve deployment status").Log(ctx, logger)
		}

		dep.Status = status
	}

	// Re-read the deployment to get the latest status, tool count etc
	dep, err = mv.DescribeDeployment(ctx, logger, s.repo, mv.ProjectID(projectID), mv.DeploymentID(cloneID))
	if err != nil {
		return nil, err
	}

	s.captureDeploymentProcessedEvent(ctx, logger, authCtx.OrganizationSlug, *authCtx.ProjectSlug, "evolve", dep, previousDeployment)

	return &gen.EvolveResult{Deployment: dep}, nil
}

func (s *Service) Redeploy(ctx context.Context, payload *gen.RedeployPayload) (*gen.RedeployResult, error) {
	span := trace.SpanFromContext(ctx)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if payload.DeploymentID == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "deployment id is required").Log(ctx, s.logger)
	}

	organizationID := authCtx.ActiveOrganizationID
	projectID := *authCtx.ProjectID

	logger := s.logger.With(attr.SlogProjectID(projectID.String()), attr.SlogOrganizationID(organizationID))
	span.SetAttributes(attr.ProjectID(projectID.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing deployments").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	tx := s.repo.WithTx(dbtx)

	deploymentID, err := uuid.Parse(payload.DeploymentID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid deployment id").Log(ctx, logger)
	}

	newID, err := cloneDeployment(
		ctx, s.tracer, logger, tx,
		ProjectID(projectID), DeploymentID(deploymentID),
		[]upsertOpenAPIv3{},
		[]upsertFunctions{},
		[]upsertPackage{},
		[]upsertExternalMCP{},
		[]uuid.UUID{},
		[]uuid.UUID{},
		[]uuid.UUID{},
		[]string{},
	)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error cloning deployment").Log(ctx, logger)
	}

	ierr := inv.Check("cloned deployment state", "deployment id cannot be nil", newID != uuid.Nil)
	if ierr != nil {
		return nil, oops.E(oops.CodeInvariantViolation, ierr, "error cloning deployment").Log(ctx, logger)
	}

	logger = logger.With(attr.SlogDeploymentID(newID.String()))
	span.SetAttributes(attr.DeploymentID(newID.String()))

	dep, err := mv.DescribeDeployment(ctx, logger, tx, mv.ProjectID(projectID), mv.DeploymentID(newID))
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving deployment").Log(ctx, logger)
	}

	status := dep.Status
	if status == "created" {
		s, err := s.startDeployment(ctx, logger, projectID, newID, dep, false)
		if err != nil {
			return nil, err
		}

		status = s
		if status == "" {
			return nil, oops.E(oops.CodeInvariantViolation, nil, "unable to resolve deployment status").Log(ctx, logger)
		}

		dep.Status = status
	}

	s.captureDeploymentProcessedEvent(ctx, logger, authCtx.OrganizationSlug, *authCtx.ProjectSlug, "redeploy", dep, nil)

	return &gen.RedeployResult{Deployment: dep}, nil
}

type resolvedPackage struct {
	packageID uuid.UUID
	projectID uuid.UUID
	versionID uuid.UUID
}

func (s *Service) resolvePackages(ctx context.Context, tx *packagesRepo.Queries, requirements [][2]string) ([]resolvedPackage, error) {
	res := make([]resolvedPackage, 0, len(requirements))

	for _, p := range requirements {
		name, version := p[0], p[1]

		var packageID uuid.UUID
		var projectID uuid.UUID
		var versionID uuid.UUID

		if version == "" {
			row, err := tx.PeekLatestPackageVersionByName(ctx, name)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, oops.E(oops.CodeBadRequest, err, "no versions found for package: %s", name).Log(ctx, s.logger, attr.SlogPackageName(name))
			}
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "error getting latest package version").Log(ctx, s.logger, attr.SlogPackageName(name))
			}

			packageID = row.PackageID
			projectID = row.ProjectID
			versionID = row.PackageVersionID
		} else {
			semver, err := semver.ParseSemver(version)
			if err != nil {
				return nil, oops.E(oops.CodeBadRequest, err, "error parsing semver").Log(ctx, s.logger)
			}

			if err := inv.Check("semver", "semver must be valid", semver.Valid); err != nil {
				return nil, oops.E(oops.CodeInvariantViolation, err, "package version incorrectly parsed").Log(ctx, s.logger)
			}

			row, err := tx.PeekPackageByNameAndVersion(ctx, packagesRepo.PeekPackageByNameAndVersionParams{
				Name:       name,
				Major:      semver.Major,
				Minor:      semver.Minor,
				Patch:      semver.Patch,
				Prerelease: conv.ToPGTextEmpty(semver.Prerelease),
				Build:      conv.ToPGTextEmpty(semver.Build),
			})
			if errors.Is(err, sql.ErrNoRows) {
				return nil, oops.E(oops.CodeBadRequest, err, "package version not found: %s@%s", name, version).Log(ctx, s.logger, attr.SlogPackageName(name), attr.SlogPackageVersion(version))
			}
			if err != nil {
				return nil, oops.E(oops.CodeBadRequest, err, "error getting package by name and version").Log(ctx, s.logger, attr.SlogPackageName(name), attr.SlogPackageVersion(version))
			}

			packageID = row.PackageID
			projectID = row.ProjectID
			versionID = row.PackageVersionID
		}

		if packageID == uuid.Nil || versionID == uuid.Nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "could not resolve package version: %s@%s", name, conv.Default(version, "latest")).Log(ctx, s.logger, attr.SlogPackageName(name), attr.SlogPackageVersion(conv.Default(version, "latest")))
		}

		res = append(res, resolvedPackage{
			packageID: packageID,
			projectID: projectID,
			versionID: versionID,
		})
	}

	return res, nil
}

func (s *Service) startDeployment(ctx context.Context, logger *slog.Logger, projectID uuid.UUID, deploymentID uuid.UUID, dep *types.Deployment, nonBlocking bool) (string, error) {
	defer func() {
		logger.InfoContext(ctx, "starting project-scoped functions reaper")
		_, err := background.ExecuteProjectFunctionsReaperWorkflow(ctx, s.temporalEnv, projectID)
		if err != nil && !temporalSDK.IsWorkflowExecutionAlreadyStartedError(err) {
			logger.ErrorContext(
				ctx, "failed to start project-scoped functions reaper workflow",
				attr.SlogError(err),
			)
		}
	}()

	wr, err := background.ExecuteProcessDeploymentWorkflow(ctx, s.temporalEnv, background.ProcessDeploymentWorkflowParams{
		ProjectID:      projectID,
		DeploymentID:   deploymentID,
		IdempotencyKey: conv.PtrValOr(dep.IdempotencyKey, ""),
	})
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "error starting deployment").Log(ctx, logger)
	}

	if nonBlocking {
		return dep.Status, nil
	}

	var res background.ProcessDeploymentWorkflowResult
	if err := wr.Get(ctx, &res); err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "error getting deployment status").Log(ctx, logger)
	}

	logger.InfoContext(ctx, "processed deployment", attr.SlogDeploymentID(deploymentID.String()), attr.SlogDeploymentStatus(res.Status), attr.SlogProjectID(projectID.String()))

	return res.Status, nil
}

// validatePackageInclusion validates that a package can be added to a project
// through a deployment. It checks the package data is well formed and guards
// against adding a package to its own project causing a circular dependency.
func validatePackageInclusion(ctx context.Context, logger *slog.Logger, targetProjectID uuid.UUID, requirement [2]string, resolved resolvedPackage) error {
	if err := inv.Check(
		"resolved package state",
		"package id cannot be nil", resolved.packageID != uuid.Nil,
		"version id cannot be nil", resolved.versionID != uuid.Nil,
	); err != nil {
		return oops.E(oops.CodeInvariantViolation, err, "error resolving package: %s@%s", requirement[0], requirement[1]).Log(ctx, logger)
	}
	if resolved.projectID == targetProjectID {
		return oops.E(oops.CodeInvalid, nil, "cannot add package to its own project: %s", requirement[0]).Log(ctx, logger)
	}

	return nil
}

func (s *Service) captureDeploymentProcessedEvent(
	ctx context.Context,
	logger *slog.Logger,
	organizationSlug string,
	projectSlug string,
	deploymentType string,
	dep *types.Deployment,
	previousDeployment *types.Deployment,
) {
	prevDeploymentHasFunctions := previousDeployment != nil && len(previousDeployment.FunctionsAssets) > 0
	firstDeploymentWithFunctions := !prevDeploymentHasFunctions && len(dep.FunctionsAssets) > 0

	properties := map[string]any{
		"deployment_id":                   dep.ID,
		"project_id":                      dep.ProjectID,
		"organization_id":                 dep.OrganizationID,
		"organization_slug":               organizationSlug,
		"project_slug":                    projectSlug,
		"deployment_type":                 deploymentType,
		"status":                          dep.Status,
		"openapiv3_tool_count":            dep.Openapiv3ToolCount,
		"functions_tool_count":            dep.FunctionsToolCount,
		"functions_asset_count":           len(dep.FunctionsAssets),
		"openapiv3_asset_count":           len(dep.Openapiv3Assets),
		"first_deployment_with_functions": firstDeploymentWithFunctions,
		"logs_url":                        s.siteURL.JoinPath(organizationSlug, projectSlug, "deployments", dep.ID).String(),
	}

	if previousDeployment != nil {
		properties["previous_status"] = previousDeployment.Status
		properties["previous_openapiv3_tool_count"] = previousDeployment.Openapiv3ToolCount
		properties["previous_functions_tool_count"] = previousDeployment.FunctionsToolCount
		properties["previous_functions_asset_count"] = len(previousDeployment.FunctionsAssets)
		properties["previous_openapiv3_asset_count"] = len(previousDeployment.Openapiv3Assets)
	}

	if err := s.posthog.CaptureEvent(ctx, "deployment_processed", dep.ID, properties); err != nil {
		logger.ErrorContext(ctx, "error capturing deployment_processed event", attr.SlogError(err))
	}
}
