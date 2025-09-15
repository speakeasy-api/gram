package deployments

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
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
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	packagesRepo "github.com/speakeasy-api/gram/server/internal/packages/repo"
	"github.com/speakeasy-api/gram/server/internal/packages/semver"
	"go.temporal.io/sdk/client"
)

type Service struct {
	logger       *slog.Logger
	tracer       trace.Tracer
	db           *pgxpool.Pool
	repo         *repo.Queries
	auth         *auth.Auth
	assets       *assetsRepo.Queries
	packages     *packagesRepo.Queries
	assetStorage assets.BlobStore
	temporal     client.Client
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, temporal client.Client, sessions *sessions.Manager, assetStorage assets.BlobStore) *Service {
	logger = logger.With(attr.SlogComponent("deployments"))
	tracer := tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/deployments")

	return &Service{
		logger:       logger,
		tracer:       tracer,
		db:           db,
		repo:         repo.New(db),
		auth:         auth.New(logger, db, sessions),
		assets:       assetsRepo.New(db),
		packages:     packagesRepo.New(db),
		assetStorage: assetStorage,
		temporal:     temporal,
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
		ID:              dep.ID,
		CreatedAt:       dep.CreatedAt,
		OrganizationID:  dep.OrganizationID,
		ProjectID:       dep.ProjectID,
		UserID:          dep.UserID,
		IdempotencyKey:  dep.IdempotencyKey,
		Status:          dep.Status,
		ExternalID:      dep.ExternalID,
		ExternalURL:     dep.ExternalURL,
		GithubRepo:      dep.GithubRepo,
		GithubPr:        dep.GithubPr,
		GithubSha:       dep.GithubSha,
		ClonedFrom:      dep.ClonedFrom,
		Openapiv3Assets: dep.Openapiv3Assets,
		Packages:        dep.Packages,
		ToolCount:       dep.ToolCount,
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

	var cursor uuid.NullUUID
	if form.Cursor != nil {
		c, err := uuid.Parse(*form.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
		}

		cursor = uuid.NullUUID{UUID: c, Valid: true}
	}

	rows, err := s.repo.GetDeploymentLogs(ctx, repo.GetDeploymentLogsParams{
		DeploymentID: id,
		ProjectID:    *authCtx.ProjectID,
		Cursor:       cursor,
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
		items = append(items, &gen.DeploymentLogEvent{
			ID:        r.ID.String(),
			Event:     r.Event,
			Message:   r.Message,
			CreatedAt: r.CreatedAt.Time.Format(time.RFC3339),
		})
	}

	var nextCursor *string
	limit := 50
	if len(items) >= limit+1 {
		nextCursor = conv.Ptr(items[limit].ID)
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
			ID:         r.ID.String(),
			UserID:     r.UserID,
			Status:     r.Status,
			CreatedAt:  r.CreatedAt.Time.Format(time.RFC3339),
			AssetCount: r.AssetCount,
			ToolCount:  r.ToolCount,
		})
	}

	var nextCursor *string
	limit := 50
	if len(items) >= limit+1 {
		nextCursor = conv.Ptr(items[limit].ID)
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

	projectID := *authCtx.ProjectID

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attr.OrganizationID(authCtx.ActiveOrganizationID),
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

	newAssets := make([]upsertOpenAPIv3, 0, len(form.Openapiv3Assets))
	for _, add := range form.Openapiv3Assets {
		assetID, err := uuid.Parse(add.AssetID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "error parsing asset id").Log(ctx, s.logger)
		}

		newAssets = append(newAssets, upsertOpenAPIv3{
			assetID: assetID,
			name:    add.Name,
			slug:    string(add.Slug),
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

	if len(newPackages) == 0 && len(newAssets) == 0 {
		return nil, oops.E(oops.CodeInvalid, nil, "at least one asset or package is required").Log(ctx, logger)
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
		newAssets, newPackages,
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
		s, err := s.startDeployment(ctx, logger, projectID, newID, dep)
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

	projectID := *authCtx.ProjectID

	logger := s.logger.With(attr.SlogProjectID(projectID.String()))
	span.SetAttributes(attr.ProjectID(projectID.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing deployments").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	tx := s.repo.WithTx(dbtx)
	pkgTx := s.packages.WithTx(dbtx)

	assetsToUpsert := make([]upsertOpenAPIv3, 0, len(form.UpsertOpenapiv3Assets))
	for _, add := range form.UpsertOpenapiv3Assets {
		assetID, err := uuid.Parse(add.AssetID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "error parsing asset id").Log(ctx, s.logger)
		}

		assetsToUpsert = append(assetsToUpsert, upsertOpenAPIv3{
			assetID: assetID,
			name:    add.Name,
			slug:    string(add.Slug),
		})
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

	excludeOpenapiv3Assets := make([]uuid.UUID, 0, len(form.ExcludeOpenapiv3Assets))
	for _, assetID := range form.ExcludeOpenapiv3Assets {
		id, err := uuid.Parse(assetID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "error parsing deployment asset id to exclude").Log(ctx, s.logger)
		}
		excludeOpenapiv3Assets = append(excludeOpenapiv3Assets, id)
	}

	excludePackages := make([]uuid.UUID, 0, len(form.ExcludePackages))
	for _, pkgID := range form.ExcludePackages {
		id, err := uuid.Parse(pkgID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "error parsing deployment package id to exclude").Log(ctx, s.logger)
		}
		excludePackages = append(excludePackages, id)
	}

	if len(packagesToUpsert) == 0 && len(assetsToUpsert) == 0 && len(excludeOpenapiv3Assets) == 0 && len(excludePackages) == 0 {
		return nil, oops.E(oops.CodeInvalid, nil, "at least one asset or package to upsert or exclude is required").Log(ctx, logger)
	}

	var cloneID uuid.UUID

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
			assetsToUpsert,
			packagesToUpsert,
		)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error initializing deployment").Log(ctx, logger)
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
		newID, err := cloneDeployment(
			ctx, s.tracer, logger, tx,
			ProjectID(projectID), DeploymentID(latestDeploymentID),
			assetsToUpsert,
			packagesToUpsert,
			excludeOpenapiv3Assets,
			excludePackages,
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
		s, err := s.startDeployment(ctx, logger, projectID, cloneID, dep)
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

	return &gen.EvolveResult{Deployment: dep}, nil
}

func (s *Service) Redeploy(ctx context.Context, payload *gen.RedeployPayload) (*gen.RedeployResult, error) {
	span := trace.SpanFromContext(ctx)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if payload.DeploymentID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "deployment id is required").Log(ctx, s.logger)
	}

	projectID := *authCtx.ProjectID

	logger := s.logger.With(attr.SlogProjectID(projectID.String()))
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
		return nil, oops.E(oops.CodeBadRequest, err, "invalid deployment id").Log(ctx, logger)
	}

	newID, err := cloneDeployment(
		ctx, s.tracer, logger, tx,
		ProjectID(projectID), DeploymentID(deploymentID),
		[]upsertOpenAPIv3{},
		[]upsertPackage{},
		[]uuid.UUID{},
		[]uuid.UUID{},
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
		s, err := s.startDeployment(ctx, logger, projectID, newID, dep)
		if err != nil {
			return nil, err
		}

		status = s
		if status == "" {
			return nil, oops.E(oops.CodeInvariantViolation, nil, "unable to resolve deployment status").Log(ctx, logger)
		}

		dep.Status = status
	}

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

func (s *Service) startDeployment(ctx context.Context, logger *slog.Logger, projectID uuid.UUID, deploymentID uuid.UUID, dep *types.Deployment) (string, error) {
	wr, err := background.ExecuteProcessDeploymentWorkflow(ctx, s.temporal, background.ProcessDeploymentWorkflowParams{
		ProjectID:      projectID,
		DeploymentID:   deploymentID,
		IdempotencyKey: conv.PtrValOr(dep.IdempotencyKey, ""),
	})
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "error starting deployment").Log(ctx, logger)
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
