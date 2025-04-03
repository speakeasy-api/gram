package deployments

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/gen/deployments"
	srv "github.com/speakeasy-api/gram/gen/http/deployments/server"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/deployments/repo"
)

type Service struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	auth   *auth.Auth
}

var _ gen.Service = &Service{}

func NewService(logger *slog.Logger, db *pgxpool.Pool, redisClient *redis.Client) *Service {
	return &Service{logger: logger, db: db, repo: repo.New(db), auth: auth.New(logger, db, redisClient)}
}

func Attach(mux goahttp.Muxer, service gen.Service) {
	endpoints := gen.NewEndpoints(service)
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) GetDeployment(ctx context.Context, form *gen.GetDeploymentPayload) (res *gen.GetDeploymentResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		s.logger.ErrorContext(ctx, "failed to check project access")
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
		s.logger.ErrorContext(ctx, "failed to check project access")
		return nil, errors.New("authorization check failed")
	}

	var cursor uuid.NullUUID
	if form.Cursor != nil {
		c, err := uuid.Parse(*form.Cursor)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to parse cursor", slog.String("error", err.Error()))
			return nil, errors.New("invalid cursor")
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
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		s.logger.ErrorContext(ctx, "failed to check auth access")
		return nil, errors.New("authorization check failed")
	}

	if len(form.Openapiv3Assets) == 0 {
		return nil, errors.New("at least one asset is required")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to begin database transaction", slog.String("error", err.Error()))
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer dbtx.Rollback(ctx)

	tx := s.repo.WithTx(dbtx)

	_, err = tx.CreateDeployment(ctx, repo.CreateDeploymentParams{
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
		s.logger.ErrorContext(ctx, "failed to create new deployment", slog.String("error", err.Error()))
		return nil, fmt.Errorf("error saving deployment")
	}

	deployment, err := tx.GetDeploymentByIdempotencyKey(ctx, repo.GetDeploymentByIdempotencyKeyParams{
		IdempotencyKey: form.IdempotencyKey,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to read deployment", slog.String("error", err.Error()))
		return nil, fmt.Errorf("error reading deployment")
	}

	deploymentAssets := []*gen.OpenAPIv3DeploymentAsset{}
	for _, a := range form.Openapiv3Assets {
		assetID, err := uuid.Parse(a.AssetID)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to parse asset ID", slog.String("error", err.Error()))
			return nil, fmt.Errorf("error parsing asset ID")
		}

		dasset, err := tx.AddDeploymentOpenAPIv3Asset(ctx, repo.AddDeploymentOpenAPIv3AssetParams{
			DeploymentID: deployment.ID,
			AssetID:      assetID,
			Name:         a.Name,
			Slug:         a.Slug,
		})
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to add openapiv3 asset to deployment", slog.String("error", err.Error()))
			return nil, fmt.Errorf("error adding openapi v3 asset to deployment")
		}

		deploymentAssets = append(deploymentAssets, &gen.OpenAPIv3DeploymentAsset{
			ID:      dasset.ID.String(),
			AssetID: dasset.AssetID.String(),
			Name:    dasset.Name,
			Slug:    dasset.Slug,
		})
	}

	stat, err := tx.MarkDeploymentCreated(ctx, repo.MarkDeploymentCreatedParams{
		DeploymentID: deployment.ID,
		ProjectID:    *authCtx.ProjectID,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to mark deployment as created", slog.String("error", err.Error()))
		return nil, fmt.Errorf("error logging deployment creation")
	}

	if err := dbtx.Commit(ctx); err != nil {
		s.logger.ErrorContext(ctx, "failed to commit database transaction", slog.String("error", err.Error()))
		return nil, fmt.Errorf("error saving deployment")
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
		Openapiv3Assets: deploymentAssets,
	}

	if err := s.processDeployment(ctx, dep); err != nil {
		s.logger.ErrorContext(ctx, "failed to process deployment", slog.String("error", err.Error()))
		return nil, fmt.Errorf("error processing deployment")
	}

	return &gen.CreateDeploymentResult{
		Deployment: dep,
	}, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) processDeployment(ctx context.Context, deployment *gen.Deployment) error {
	_, _ = ctx, deployment
	return nil
}
