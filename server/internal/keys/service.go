package keys

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/gen/http/keys/server"
	gen "github.com/speakeasy-api/gram/gen/keys"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/keys/repo"
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

func (s *Service) CreateKey(ctx context.Context, payload *gen.CreateKeyPayload) (*gen.Key, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, errors.New("auth not found in context")
	}

	token, err := generateKey()
	if err != nil {
		return nil, err
	}

	createdKey, err := s.repo.CreateAPIKey(ctx, repo.CreateAPIKeyParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Name:            payload.Name,
		Token:           token,
		Scopes:          []string{string(APIKeyScopesConsumer)}, // this is the only default scopes for now
		CreatedByUserID: authCtx.UserID,
	})

	if err != nil {
		return nil, err
	}

	return &gen.Key{
		ID:             createdKey.ID.String(),
		Name:           createdKey.Name,
		OrganizationID: createdKey.OrganizationID,
		ProjectID:      conv.FromNullableUUID(createdKey.ProjectID),
		Token:          createdKey.Token,
		Scopes:         createdKey.Scopes,
		CreatedAt:      createdKey.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      createdKey.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func (s *Service) ListKeys(ctx context.Context, payload *gen.ListKeysPayload) (*gen.ListKeysResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, errors.New("session not found in context")
	}

	keys, err := s.repo.ListAPIKeysByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	var result []*gen.Key
	for _, key := range keys {
		result = append(result, &gen.Key{
			ID:             key.ID.String(),
			Name:           key.Name,
			OrganizationID: key.OrganizationID,
			ProjectID:      conv.FromNullableUUID(key.ProjectID),
			Token:          key.Token,
			Scopes:         key.Scopes,
			CreatedAt:      key.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:      key.UpdatedAt.Time.Format(time.RFC3339),
		})
	}

	return &gen.ListKeysResult{Keys: result}, nil
}

func (s *Service) RevokeKey(ctx context.Context, payload *gen.RevokeKeyPayload) (err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return errors.New("auth not found in context")
	}

	return s.repo.DeleteAPIKey(ctx, repo.DeleteAPIKeyParams{
		ID:             uuid.MustParse(payload.ID),
		OrganizationID: authCtx.ActiveOrganizationID,
	})
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
