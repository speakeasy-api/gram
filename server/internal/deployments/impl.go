package deployments

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sourcegraph/conc/pool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/gen/deployments"
	srv "github.com/speakeasy-api/gram/gen/http/deployments/server"
	"github.com/speakeasy-api/gram/internal/assets"
	assetsRepo "github.com/speakeasy-api/gram/internal/assets/repo"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/deployments/repo"
	"github.com/speakeasy-api/gram/internal/inv"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/o11y"
	"github.com/speakeasy-api/gram/internal/oops"
	packages "github.com/speakeasy-api/gram/internal/packages"
	packagesRepo "github.com/speakeasy-api/gram/internal/packages/repo"
)

type Service struct {
	tracer       trace.Tracer
	logger       *slog.Logger
	db           *pgxpool.Pool
	repo         *repo.Queries
	auth         *auth.Auth
	assets       *assetsRepo.Queries
	packages     *packagesRepo.Queries
	assetStorage assets.BlobStore
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, assetStorage assets.BlobStore) *Service {
	return &Service{
		tracer:       otel.Tracer("github.com/speakeasy-api/gram/internal/deployments"),
		logger:       logger,
		db:           db,
		repo:         repo.New(db),
		auth:         auth.New(logger, db, sessions),
		assets:       assetsRepo.New(db),
		packages:     packagesRepo.New(db),
		assetStorage: assetStorage,
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

	dep, err := DescribeDeployment(ctx, s.logger, s.repo, ProjectID(*authCtx.ProjectID), DeploymentID(id))
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
		Openapiv3Assets: dep.Openapiv3Assets,
		Packages:        dep.Packages,
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

	dep, err := DescribeDeployment(ctx, s.logger, tx, ProjectID(*authCtx.ProjectID), DeploymentID(id))
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
		return nil, err
	}

	items := make([]*gen.DeploymentSummary, 0, len(rows))
	for _, r := range rows {
		items = append(items, &gen.DeploymentSummary{
			ID:         r.ID.String(),
			UserID:     r.UserID,
			CreatedAt:  r.CreatedAt.Time.Format(time.RFC3339),
			AssetCount: r.AssetCount,
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
		attribute.String("organization_id", authCtx.ActiveOrganizationID),
		attribute.String("project_id", projectID.String()),
		attribute.String("user_id", authCtx.UserID),
		attribute.String("session_id", *authCtx.SessionID),
	)

	if len(form.Openapiv3Assets) == 0 && len(form.Packages) == 0 {
		return nil, oops.E(oops.CodeInvalid, nil, "at least one asset or package is required")
	}

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
	for _, pkg := range resolved {
		newPackages = append(newPackages, upsertPackage(pkg))
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
		},
		newAssets, newPackages,
	)
	if err != nil {
		return nil, err
	}

	logger = logger.With(slog.String("deployment_id", newID.String()))
	span.SetAttributes(attribute.String("deployment_id", newID.String()))

	dep, err := DescribeDeployment(ctx, logger, tx, ProjectID(projectID), DeploymentID(newID))
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

	logger := s.logger.With(slog.String("project_id", projectID.String()))
	span.SetAttributes(attribute.String("project_id", projectID.String()))

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
	for _, pkg := range resolved {
		packagesToUpsert = append(packagesToUpsert, upsertPackage(pkg))
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

		logger = logger.With(slog.String("deployment_id", newID.String()))
		span.SetAttributes(attribute.String("deployment_id", newID.String()))

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

		logger = logger.With(slog.String("deployment_id", newID.String()))
		span.SetAttributes(attribute.String("deployment_id", newID.String()))

		cloneID = newID
	}

	dep, err := DescribeDeployment(ctx, logger, tx, ProjectID(projectID), DeploymentID(cloneID))
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

	return &gen.EvolveResult{Deployment: dep}, nil
}

type resolvedPackage struct {
	packageID uuid.UUID
	versionID uuid.UUID
}

func (s *Service) resolvePackages(ctx context.Context, tx *packagesRepo.Queries, requirements [][2]string) ([]resolvedPackage, error) {
	res := make([]resolvedPackage, 0, len(requirements))

	for _, p := range requirements {
		name, version := p[0], p[1]

		var packageID uuid.UUID
		var versionID uuid.UUID

		if version == "" {
			row, err := tx.PeekLatestPackageVersionByName(ctx, name)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, oops.E(oops.CodeBadRequest, err, "no versions found for package: "+name).Log(ctx, s.logger, slog.String("package_name", name))
			}
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "error getting latest package version").Log(ctx, s.logger, slog.String("package_name", name))
			}

			packageID = row.PackageID
			versionID = row.PackageVersionID
		} else {
			semver, err := packages.ParseSemver(version)
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
				Prerelease: conv.ToPGText(semver.Prerelease),
				Build:      conv.ToPGText(semver.Build),
			})
			if errors.Is(err, sql.ErrNoRows) {
				msg := fmt.Sprintf("package version not found: %s@%s", name, version)
				return nil, oops.E(oops.CodeBadRequest, err, msg).Log(ctx, s.logger, slog.String("package_name", name), slog.String("package_version", version))
			}
			if err != nil {
				return nil, oops.E(oops.CodeBadRequest, err, "error getting package by name and version").Log(ctx, s.logger, slog.String("package_name", name), slog.String("package_version", version))
			}

			packageID = row.PackageID
			versionID = row.PackageVersionID
		}

		if packageID == uuid.Nil || versionID == uuid.Nil {
			msg := fmt.Sprintf("could not resolve package version: %s@%s", name, conv.Default(version, "latest"))
			return nil, oops.E(oops.CodeBadRequest, nil, msg).Log(ctx, s.logger, slog.String("package_name", name), slog.String("package_version", conv.Default(version, "latest")))
		}

		res = append(res, resolvedPackage{
			packageID: packageID,
			versionID: versionID,
		})
	}

	return res, nil
}

func (s *Service) startDeployment(ctx context.Context, logger *slog.Logger, projectID uuid.UUID, deploymentID uuid.UUID, dep *gen.Deployment) (string, error) {
	span := trace.SpanFromContext(ctx)

	status := ""
	if err := s.processDeployment(ctx, dep); err != nil {
		if _, err := s.repo.TransitionDeployment(ctx, repo.TransitionDeploymentParams{
			DeploymentID: deploymentID,
			ProjectID:    projectID,
			Status:       "failed",
			Event:        "deployment:failed",
			Message:      err.Error(),
		}); err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "error transitioning deployment to error").Log(ctx, logger)
		}

		span.AddEvent("deployment_failed")
		status = "failed"
	} else {
		if _, err := s.repo.TransitionDeployment(ctx, repo.TransitionDeploymentParams{
			DeploymentID: deploymentID,
			ProjectID:    projectID,
			Status:       "completed",
			Event:        "deployment:completed",
			Message:      "Deployment completed",
		}); err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "error transitioning deployment to completed").Log(ctx, logger)
		}

		span.AddEvent("deployment_completed")
		status = "completed"
	}

	return status, nil
}

func (s *Service) processDeployment(ctx context.Context, deployment *gen.Deployment) error {
	logger := s.logger.With(
		slog.String("deployment_id", deployment.ID),
		slog.String("project_id", deployment.ProjectID),
	)

	deploymentID, err := uuid.Parse(deployment.ID)
	if err != nil {
		return oops.E(oops.CodeInvariantViolation, err, "error parsing deployment id").Log(ctx, logger)
	}

	projectID, err := uuid.Parse(deployment.ProjectID)
	if err != nil {
		return oops.E(oops.CodeInvariantViolation, err, "error parsing project id").Log(ctx, logger)
	}

	_, err = s.repo.TransitionDeployment(ctx, repo.TransitionDeploymentParams{
		DeploymentID: deploymentID,
		ProjectID:    projectID,
		Status:       "pending",
		Event:        "deployment:pending",
		Message:      "Deployment pending",
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error transitioning deployment to pending").Log(ctx, logger)
	}

	workers := pool.New().WithErrors().WithMaxGoroutines(2)
	for _, docInfo := range deployment.Openapiv3Assets {
		logger = s.logger.With(
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

		asset, err := s.assets.GetProjectAsset(ctx, assetsRepo.GetProjectAssetParams{
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
			dbtx, err := s.db.Begin(ctx)
			if err != nil {
				return oops.E(oops.CodeUnexpected, err, "error processing deployment").Log(ctx, logger)
			}
			defer o11y.NoLogDefer(func() error {
				return dbtx.Rollback(ctx)
			})

			tx := s.repo.WithTx(dbtx)

			processErr := s.processOpenAPIv3Document(ctx, logger, tx, openapiV3Task{
				projectID:    projectID,
				deploymentID: deploymentID,
				openapiDocID: openapiDocID,
				docInfo:      docInfo,
				docURL:       u,
			})

			if err := dbtx.Commit(ctx); err != nil {
				return oops.E(oops.CodeUnexpected, err, "error saving processed deployment").Log(ctx, logger)
			}

			if processErr == nil {
				trace.SpanFromContext(ctx).AddEvent("openapiv3_processed")
			}
			return processErr
		})
	}

	return workers.Wait()
}
