package deployments

import (
	"context"
	"database/sql"
	"errors"
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
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/oops"
)

type Service struct {
	tracer       trace.Tracer
	logger       *slog.Logger
	db           *pgxpool.Pool
	repo         *repo.Queries
	auth         *auth.Auth
	assets       *assetsRepo.Queries
	assetStorage assets.BlobStore
}

var _ gen.Service = &Service{}

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, assetStorage assets.BlobStore) *Service {
	return &Service{
		tracer:       otel.Tracer("github.com/speakeasy-api/gram/internal/deployments"),
		logger:       logger,
		db:           db,
		repo:         repo.New(db),
		auth:         auth.New(logger, db, sessions),
		assets:       assetsRepo.New(db),
		assetStorage: assetStorage,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
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
		return nil, errors.New("authorization check failed")
	}

	id, err := uuid.Parse(form.ID)
	if err != nil {
		return nil, err
	}

	rows, err := s.repo.GetDeploymentWithAssets(ctx, repo.GetDeploymentWithAssetsParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, errors.New("deployment not found")
	case err != nil:
		return nil, err
	}

	deployment := rows[0].Deployment
	assets := make([]*gen.OpenAPIv3DeploymentAsset, 0, len(rows))
	for _, r := range rows {
		assets = append(assets, &gen.OpenAPIv3DeploymentAsset{
			ID:      r.DeploymentsOpenapiv3Asset.ID.String(),
			AssetID: r.DeploymentsOpenapiv3Asset.AssetID.String(),
			Name:    r.DeploymentsOpenapiv3Asset.Name,
			Slug:    r.DeploymentsOpenapiv3Asset.Slug,
		})
	}

	return &gen.GetDeploymentResult{
		ID:              deployment.ID.String(),
		CreatedAt:       deployment.CreatedAt.Time.Format(time.RFC3339),
		OrganizationID:  deployment.OrganizationID,
		ProjectID:       deployment.ProjectID.String(),
		UserID:          deployment.UserID,
		IdempotencyKey:  conv.Ptr(deployment.IdempotencyKey),
		Status:          rows[0].Status,
		ExternalID:      conv.FromPGText(deployment.ExternalID),
		ExternalURL:     conv.FromPGText(deployment.ExternalUrl),
		GithubRepo:      conv.FromPGText(deployment.GithubRepo),
		GithubPr:        conv.FromPGText(deployment.GithubPr),
		GithubSha:       conv.FromPGText(deployment.GithubSha),
		Openapiv3Assets: assets,
	}, nil
}

func (s *Service) ListDeployments(ctx context.Context, form *gen.ListDeploymentsPayload) (res *gen.ListDeploymentResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, errors.New("authorization check failed")
	}

	var cursor uuid.NullUUID
	if form.Cursor != nil {
		c, err := uuid.Parse(*form.Cursor)
		if err != nil {
			return nil, oops.E(err, "invalid cursor", "failed to parse cursor").Log(ctx, s.logger)
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
		return nil, errors.New("authorization check failed")
	}

	projectID := *authCtx.ProjectID

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("organization_id", authCtx.ActiveOrganizationID),
		attribute.String("project_id", projectID.String()),
		attribute.String("user_id", authCtx.UserID),
		attribute.String("session_id", *authCtx.SessionID),
	)

	if len(form.Openapiv3Assets) == 0 {
		return nil, errors.New("at least one asset is required")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(err, "database error", "failed to begin database transaction").Log(ctx, logger)
	}
	defer dbtx.Rollback(ctx)

	tx := s.repo.WithTx(dbtx)

	cmd, err := tx.CreateDeployment(ctx, repo.CreateDeploymentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		UserID:         authCtx.UserID,
		ExternalID:     conv.PtrToPGText(form.ExternalID),
		ExternalUrl:    conv.PtrToPGText(form.ExternalURL),
		GithubRepo:     conv.PtrToPGText(form.GithubRepo),
		GithubPr:       conv.PtrToPGText(form.GithubPr),
		IdempotencyKey: form.IdempotencyKey,
	})
	if err != nil {
		return nil, oops.E(err, "unexpected database error", "failed to create new deployment").Log(ctx, logger)
	}

	created := cmd.RowsAffected() > 0

	row, err := tx.GetDeploymentByIdempotencyKey(ctx, repo.GetDeploymentByIdempotencyKeyParams{
		IdempotencyKey: form.IdempotencyKey,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(err, "unexpected database error", "failed to read deployment").Log(ctx, logger)
	}

	deployment := row.Deployment
	logger = logger.With(slog.String("deployment_id", deployment.ID.String()))
	deploymentAssets := []*gen.OpenAPIv3DeploymentAsset{}
	var status string

	span.SetAttributes(
		attribute.String("deployment_id", deployment.ID.String()),
	)

	if created {
		stat, err := tx.TransitionDeployment(ctx, repo.TransitionDeploymentParams{
			DeploymentID: deployment.ID,
			ProjectID:    *authCtx.ProjectID,
			Status:       "created",
			Event:        "deployment:created",
			Message:      "Deployment created",
		})
		if err != nil {
			return nil, oops.E(err, "error logging deployment creation", "failed to mark deployment as created").Log(ctx, logger)
		}
		span.AddEvent("deployment_created")

		status = stat.Status
	} else {
		status = row.Status
	}

	deploymentAssets, err = s.addOpenAPIv3Documents(ctx, tx, deployment.ID, form.Openapiv3Assets)
	if err != nil {
		return nil, oops.E(err, "error adding openapi v3 assets to deployment", "failed to add openapi v3 assets to deployment").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(err, "error saving deployment", "failed to commit database transaction").Log(ctx, logger)
	}

	dep := &gen.Deployment{
		ID:              deployment.ID.String(),
		CreatedAt:       deployment.CreatedAt.Time.Format(time.RFC3339),
		OrganizationID:  deployment.OrganizationID,
		ProjectID:       deployment.ProjectID.String(),
		UserID:          deployment.UserID,
		Status:          status,
		ExternalID:      conv.FromPGText(deployment.ExternalID),
		ExternalURL:     conv.FromPGText(deployment.ExternalUrl),
		GithubSha:       conv.FromPGText(deployment.GithubSha),
		GithubPr:        conv.FromPGText(deployment.GithubPr),
		IdempotencyKey:  conv.Ptr(deployment.IdempotencyKey),
		Openapiv3Assets: deploymentAssets,
	}

	if status == "created" {
		status, err = s.startDeployment(ctx, logger, projectID, deployment.ID, dep)
		if err != nil {
			return nil, err
		}
		if status == "" {
			return nil, errors.New("unable to resolve deployment status")
		}

		dep.Status = status
	}

	return &gen.CreateDeploymentResult{
		Deployment: dep,
	}, nil
}

func (s *Service) addOpenAPIv3Documents(ctx context.Context, tx *repo.Queries, deploymentID uuid.UUID, assets []*gen.OpenAPIv3DeploymentAssetForm) ([]*gen.OpenAPIv3DeploymentAsset, error) {
	span := trace.SpanFromContext(ctx)
	logger := s.logger.With(slog.String("deployment_id", deploymentID.String()))

	deploymentAssets := []*gen.OpenAPIv3DeploymentAsset{}
	for _, a := range assets {
		assetID, err := uuid.Parse(a.AssetID)
		if err != nil {
			return nil, oops.E(err, "error parsing asset ID", "failed to parse asset ID").Log(ctx, logger)
		}

		dasset, err := tx.AddDeploymentOpenAPIv3Asset(ctx, repo.AddDeploymentOpenAPIv3AssetParams{
			DeploymentID: deploymentID,
			AssetID:      assetID,
			Name:         a.Name,
			Slug:         a.Slug,
		})
		if err != nil {
			return nil, oops.E(err, "error adding openapi v3 asset to deployment", "failed to add openapi v3 asset to deployment").Log(ctx, logger)
		}

		span.AddEvent("deployment_asset_added")

		deploymentAssets = append(deploymentAssets, &gen.OpenAPIv3DeploymentAsset{
			ID:      dasset.ID.String(),
			AssetID: dasset.AssetID.String(),
			Name:    dasset.Name,
			Slug:    dasset.Slug,
		})
	}

	return deploymentAssets, nil
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
			return "", oops.E(err, "error transitioning deployment to error", "failed to transition deployment to error").Log(ctx, logger)
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
			return "", oops.E(err, "error transitioning deployment to completed", "failed to transition deployment to completed").Log(ctx, logger)
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
		return oops.E(err, "error parsing deployment id", "failed to parse deployment id").Log(ctx, logger)
	}

	projectID, err := uuid.Parse(deployment.ProjectID)
	if err != nil {
		return oops.E(err, "error parsing project id", "failed to parse project id").Log(ctx, logger)
	}

	_, err = s.repo.TransitionDeployment(ctx, repo.TransitionDeploymentParams{
		DeploymentID: deploymentID,
		ProjectID:    projectID,
		Status:       "pending",
		Event:        "deployment:pending",
		Message:      "Deployment pending",
	})
	if err != nil {
		return oops.E(err, "error logging deployment event", "failed to mark deployment as created").Log(ctx, logger)
	}

	workers := pool.New().WithErrors().WithMaxGoroutines(2)
	for _, docInfo := range deployment.Openapiv3Assets {
		logger = s.logger.With(
			slog.String("openapi_id", docInfo.ID),
			slog.String("asset_id", docInfo.AssetID),
		)

		openapiDocID, err := uuid.Parse(docInfo.ID)
		if err != nil {
			return oops.E(err, "error parsing openapi document id", "failed to parse openapi document id").Log(ctx, logger)
		}

		assetID, err := uuid.Parse(docInfo.AssetID)
		if err != nil {
			return oops.E(err, "error parsing asset id", "failed to parse asset id").Log(ctx, logger)
		}

		asset, err := s.assets.GetProjectAsset(ctx, assetsRepo.GetProjectAssetParams{
			ID:        assetID,
			ProjectID: projectID,
		})
		if err != nil {
			return oops.E(err, "error getting asset", "failed to get asset").Log(ctx, logger)
		}

		u, err := url.Parse(asset.Url)
		if err != nil {
			return oops.E(err, "error parsing asset URL", "failed to parse asset URL").Log(ctx, logger)
		}

		workers.Go(func() error {
			dbtx, err := s.db.Begin(ctx)
			if err != nil {
				return oops.E(err, "unexpected database error", "failed to begin database transaction").Log(ctx, logger)
			}
			defer dbtx.Rollback(ctx)

			tx := s.repo.WithTx(dbtx)

			processErr := s.processOpenAPIv3Document(ctx, logger, tx, openapiV3Task{
				projectID:    projectID,
				deploymentID: deploymentID,
				openapiDocID: openapiDocID,
				docInfo:      docInfo,
				docURL:       u,
			})

			if err := dbtx.Commit(ctx); err != nil {
				return oops.E(err, "unexpected database error", "failed to commit database transaction").Log(ctx, logger)
			}

			if processErr == nil {
				trace.SpanFromContext(ctx).AddEvent("openapiv3_processed")
			}
			return processErr
		})
	}

	return workers.Wait()
}

func (s *Service) AddOpenAPIv3Source(ctx context.Context, form *gen.AddOpenAPIv3SourcePayload) (*gen.AddOpenAPIv3SourceResult, error) {
	span := trace.SpanFromContext(ctx)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, errors.New("authorization check failed")
	}

	projectID := *authCtx.ProjectID

	assetID, err := uuid.Parse(form.AssetID)
	if err != nil {
		return nil, oops.E(err, "error parsing asset id", "failed to parse asset id").Log(ctx, s.logger)
	}

	logger := s.logger.With(
		slog.String("project_id", projectID.String()),
	)
	span.SetAttributes(
		attribute.String("project_id", projectID.String()),
	)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(err, "database error", "failed to begin database transaction").Log(ctx, logger)
	}
	defer dbtx.Rollback(ctx)

	tx := s.repo.WithTx(dbtx)

	var cloneID uuid.UUID

	latestDeploymentID, err := tx.GetLatestDeploymentID(ctx, projectID)
	switch {
	// 1️⃣ Project has no deployments, we need to create an initial one instead of cloning
	case errors.Is(err, sql.ErrNoRows), latestDeploymentID == uuid.Nil:
		key := uuid.New().String()
		_, err := tx.CreateDeployment(ctx, repo.CreateDeploymentParams{
			ProjectID:      projectID,
			UserID:         authCtx.UserID,
			OrganizationID: authCtx.ActiveOrganizationID,
			IdempotencyKey: key,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("no deployment created")
		}
		if err != nil {
			return nil, oops.E(err, "error creating initial deployment", "failed to create initial deployment").Log(ctx, logger)
		}

		d, err := tx.GetDeploymentByIdempotencyKey(ctx, repo.GetDeploymentByIdempotencyKeyParams{
			IdempotencyKey: key,
			ProjectID:      projectID,
		})
		if err != nil {
			return nil, oops.E(err, "error reading initial deployment", "failed to read laatest deployment").Log(ctx, logger)
		}

		logger = s.logger.With(
			slog.String("deployment_id", d.Deployment.ID.String()),
		)
		span.SetAttributes(
			attribute.String("deployment_id", d.Deployment.ID.String()),
		)

		_, err = tx.AddDeploymentOpenAPIv3Asset(ctx, repo.AddDeploymentOpenAPIv3AssetParams{
			DeploymentID: d.Deployment.ID,
			AssetID:      assetID,
			Name:         form.Name,
			Slug:         form.Slug,
		})
		if err != nil {
			return nil, oops.E(err, "error adding deployment openapi v3 asset", "failed to add deployment openapi v3 asset").Log(ctx, logger)
		}

		cloneID = d.Deployment.ID

		span.AddEvent("initial_deployment_created")
	// 2️⃣ Something went wrong querying for the latest deployment
	case err != nil:
		return nil, oops.E(err, "error getting latest deployment", "failed to get latest deployment").Log(ctx, logger)
	// 3️⃣ We found a latest deployment, we need to clone it
	default:
		newID, err := tx.CloneDeployment(ctx, repo.CloneDeploymentParams{
			ID:        latestDeploymentID,
			ProjectID: projectID,
		})
		if err != nil {
			return nil, oops.E(err, "error cloning deployment", "failed to clone deployment").Log(ctx, logger)
		}

		logger = s.logger.With(
			slog.String("deployment_id", newID.String()),
		)
		span.SetAttributes(
			attribute.String("deployment_id", newID.String()),
		)

		_, err = tx.CloneDeploymentOpenAPIv3Assets(ctx, repo.CloneDeploymentOpenAPIv3AssetsParams{
			OriginalDeploymentID: latestDeploymentID,
			CloneDeploymentID:    newID,
		})
		if err != nil {
			return nil, oops.E(err, "error cloning deployment openapi v3 assets", "failed to clone deployment openapi v3 assets").Log(ctx, logger)
		}

		_, err = tx.AddDeploymentOpenAPIv3Asset(ctx, repo.AddDeploymentOpenAPIv3AssetParams{
			DeploymentID: newID,
			AssetID:      assetID,
			Name:         form.Name,
			Slug:         form.Slug,
		})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, oops.E(err, "error adding deployment openapi v3 asset", "failed to add deployment openapi v3 asset").Log(ctx, logger)
		}

		cloneID = newID

		span.AddEvent("deployment_cloned")
	}

	stat, err := tx.TransitionDeployment(ctx, repo.TransitionDeploymentParams{
		DeploymentID: cloneID,
		ProjectID:    projectID,
		Status:       "created",
		Event:        "deployment:created",
		Message:      "Deployment created",
	})
	if err != nil {
		return nil, oops.E(err, "error logging deployment creation", "failed to mark deployment as created").Log(ctx, logger)
	}

	rows, err := tx.GetDeploymentWithAssets(ctx, repo.GetDeploymentWithAssetsParams{
		ID:        cloneID,
		ProjectID: projectID,
	})
	if err != nil {
		return nil, oops.E(err, "error getting deployment with assets", "failed to get deployment with assets").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(err, "error saving deployment", "failed to commit database transaction").Log(ctx, logger)
	}

	deployment := rows[0].Deployment
	assets := make([]*gen.OpenAPIv3DeploymentAsset, 0, len(rows))
	for _, r := range rows {
		assets = append(assets, &gen.OpenAPIv3DeploymentAsset{
			ID:      r.DeploymentsOpenapiv3Asset.ID.String(),
			AssetID: r.DeploymentsOpenapiv3Asset.AssetID.String(),
			Name:    r.DeploymentsOpenapiv3Asset.Name,
			Slug:    r.DeploymentsOpenapiv3Asset.Slug,
		})
	}

	dep := &gen.Deployment{
		ID:              deployment.ID.String(),
		CreatedAt:       deployment.CreatedAt.Time.Format(time.RFC3339),
		OrganizationID:  deployment.OrganizationID,
		ProjectID:       deployment.ProjectID.String(),
		UserID:          deployment.UserID,
		Status:          stat.Status,
		ExternalID:      conv.FromPGText(deployment.ExternalID),
		ExternalURL:     conv.FromPGText(deployment.ExternalUrl),
		GithubSha:       conv.FromPGText(deployment.GithubSha),
		GithubPr:        conv.FromPGText(deployment.GithubPr),
		IdempotencyKey:  conv.Ptr(deployment.IdempotencyKey),
		Openapiv3Assets: assets,
	}

	status, err := s.startDeployment(ctx, logger, projectID, deployment.ID, dep)
	if err != nil {
		return nil, err
	}
	if status == "" {
		return nil, errors.New("unable to resolve deployment status")
	}

	dep.Status = status

	return &gen.AddOpenAPIv3SourceResult{
		Deployment: dep,
	}, nil
}
