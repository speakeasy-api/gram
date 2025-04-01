package deployments

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/sessions"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/gen/deployments"
	srv "github.com/speakeasy-api/gram/gen/http/deployments/server"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/deployments/repo"
	"github.com/speakeasy-api/gram/internal/must"
)

type Service struct {
	logger   *slog.Logger
	db       *pgxpool.Pool
	repo     *repo.Queries
	sessions *sessions.Sessions
}

var _ gen.Service = &Service{}

func NewService(logger *slog.Logger, db *pgxpool.Pool) *Service {
	return &Service{logger: logger, db: db, repo: repo.New(db), sessions: sessions.New()}
}

func Attach(mux goahttp.Muxer, service gen.Service) {
	endpoints := gen.NewEndpoints(service)
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) GetDeployment(ctx context.Context, form *gen.GetDeploymentPayload) (res *gen.GetDeploymentResult, err error) {
	id, err := uuid.Parse(form.ID)
	if err != nil {
		return nil, err
	}

	deployment, err := s.repo.GetDeployment(ctx, id)
	if err != nil {
		return nil, err
	}

	return &gen.GetDeploymentResult{
		ID:                deployment.ID.String(),
		CreatedAt:         deployment.CreatedAt.Time.Format(time.RFC3339),
		OrganizationID:    deployment.OrganizationID.String(),
		ProjectID:         deployment.ProjectID.String(),
		UserID:            deployment.UserID.String,
		ExternalID:        conv.FromPGText(deployment.ExternalID),
		ExternalURL:       conv.FromPGText(deployment.ExternalUrl),
		Openapiv3AssetIds: []string{},
	}, nil
}

func (s *Service) ListDeployments(context.Context, *gen.ListDeploymentsPayload) (res *gen.ListDeploymentResult, err error) {
	return &gen.ListDeploymentResult{}, nil
}

func (s *Service) CreateDeployment(ctx context.Context, form *gen.CreateDeploymentPayload) (*gen.CreateDeploymentResult, error) {
	session, ok := sessions.GetSessionValueFromContext(ctx)
	if !ok || session == nil {
		return nil, errors.New("session not found in context")
	}

	deployment, err := s.repo.CreateDeployment(ctx, repo.CreateDeploymentParams{
		OrganizationID: must.Value(uuid.NewV7()),
		ProjectID:      must.Value(uuid.NewV7()),
		UserID:         pgtype.Text{String: session.UserID, Valid: true},
		ExternalID:     pgtype.Text{String: *form.ExternalID, Valid: true},
		ExternalUrl:    pgtype.Text{String: *form.ExternalURL, Valid: true},
	})
	if err != nil {
		return nil, err
	}

	return &gen.CreateDeploymentResult{
		Deployment: &gen.Deployment{
			ID:                deployment.ID.String(),
			CreatedAt:         deployment.CreatedAt.Time.Format(time.RFC3339),
			OrganizationID:    deployment.OrganizationID.String(),
			ProjectID:         deployment.ProjectID.String(),
			UserID:            deployment.UserID.String,
			ExternalID:        conv.FromPGText(deployment.ExternalID),
			ExternalURL:       conv.FromPGText(deployment.ExternalUrl),
			Openapiv3AssetIds: []string{},
		},
	}, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.sessions.SessionAuth(ctx, key)
}
