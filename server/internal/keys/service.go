package keys

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	srv "github.com/speakeasy-api/gram/gen/http/keys/server"
	gen "github.com/speakeasy-api/gram/gen/keys"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/keys/repo"
	"github.com/speakeasy-api/gram/internal/sessions"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
)

type Service struct {
	logger   *slog.Logger
	db       *pgxpool.Pool
	repo     *repo.Queries
	sessions *sessions.Sessions
}

var _ gen.Service = &Service{}

func NewService(logger *slog.Logger, db *pgxpool.Pool) *Service {
	return &Service{logger: logger, db: db, repo: repo.New(db), sessions: sessions.New(logger)}
}

func Attach(mux goahttp.Muxer, service gen.Service) {
	endpoints := gen.NewEndpoints(service)
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) CreateKey(ctx context.Context, payload *gen.CreateKeyPayload) (*gen.Key, error) {
	session, ok := sessions.GetSessionValueFromContext(ctx)
	if !ok || session == nil {
		return nil, errors.New("session not found in context")
	}

	token, err := generateKey()
	if err != nil {
		return nil, err
	}

	createdKey, err := s.repo.CreateAPIKey(ctx, repo.CreateAPIKeyParams{
		OrganizationID:  session.ActiveOrganizationID,
		Name:            payload.Name,
		Token:           token,
		Scopes:          []string{string(APIKeyScopesReadConsumer), string(APIKeyScopesWriteConsumer)}, // these are the only default scopes for now
		CreatedByUserID: session.UserID,
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
	session, ok := sessions.GetSessionValueFromContext(ctx)
	if !ok || session == nil {
		return nil, errors.New("session not found in context")
	}

	keys, err := s.repo.ListAPIKeysByOrganization(ctx, session.ActiveOrganizationID)
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
	session, ok := sessions.GetSessionValueFromContext(ctx)
	if !ok || session == nil {
		return errors.New("session not found in context")
	}

	return s.repo.DeleteAPIKey(ctx, repo.DeleteAPIKeyParams{
		ID:             uuid.MustParse(payload.ID),
		OrganizationID: session.ActiveOrganizationID,
	})
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.sessions.SessionAuth(ctx, key)
}
