package keys

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/keys/server"
	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const keyPrefix = "gram"

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	db        *pgxpool.Pool
	repo      *repo.Queries
	auth      *auth.Auth
	keyPrefix string
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, env string) *Service {
	logger = logger.With(attr.SlogComponent("keys"))

	var keyEnv string
	switch env {
	case "local":
		keyEnv = "local"
	case "dev":
		keyEnv = "test"
	case "prod":
		keyEnv = "live"
	default:
		keyEnv = "local"
	}
	fullKeyPrefix := fmt.Sprintf("%s_%s_", keyPrefix, keyEnv)
	return &Service{
		tracer:    otel.Tracer("github.com/speakeasy-api/gram/server/internal/keys"),
		logger:    logger,
		db:        db,
		repo:      repo.New(db),
		auth:      auth.New(logger, db, sessions),
		keyPrefix: fullKeyPrefix,
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

func (s *Service) CreateKey(ctx context.Context, payload *gen.CreateKeyPayload) (*gen.Key, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	token, err := s.generateToken()
	if err != nil {
		return nil, err
	}

	fullKey := s.keyPrefix + token

	keyHash, err := auth.GetAPIKeyHash(fullKey)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error hashing api key").Log(ctx, s.logger)
	}

	var projectID uuid.NullUUID
	if authCtx.ProjectID != nil {
		projectID = uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}
	} else {
		projectID = uuid.NullUUID{UUID: uuid.UUID{}, Valid: false}
	}

	createdKey, err := s.repo.CreateAPIKey(ctx, repo.CreateAPIKeyParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Name:            payload.Name,
		KeyHash:         keyHash,
		KeyPrefix:       s.keyPrefix + token[:5],
		Scopes:          []string{string(APIKeyScopesConsumer)}, // this is the only default scopes for now
		CreatedByUserID: authCtx.UserID,
		ProjectID:       projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating api key").Log(ctx, s.logger)
	}

	return &gen.Key{
		ID:              createdKey.ID.String(),
		Name:            createdKey.Name,
		OrganizationID:  createdKey.OrganizationID,
		ProjectID:       conv.FromNullableUUID(createdKey.ProjectID),
		Key:             &fullKey,
		KeyPrefix:       createdKey.KeyPrefix,
		Scopes:          createdKey.Scopes,
		CreatedByUserID: createdKey.CreatedByUserID,
		CreatedAt:       createdKey.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:       createdKey.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func (s *Service) ListKeys(ctx context.Context, payload *gen.ListKeysPayload) (*gen.ListKeysResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	keys, err := s.repo.ListAPIKeysByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing api keys").Log(ctx, s.logger)
	}

	var result []*gen.Key
	for _, key := range keys {
		result = append(result, &gen.Key{
			ID:              key.ID.String(),
			Name:            key.Name,
			OrganizationID:  key.OrganizationID,
			ProjectID:       conv.FromNullableUUID(key.ProjectID),
			Key:             nil,
			KeyPrefix:       key.KeyPrefix,
			Scopes:          key.Scopes,
			CreatedByUserID: key.CreatedByUserID,
			CreatedAt:       key.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:       key.UpdatedAt.Time.Format(time.RFC3339),
		})
	}

	return &gen.ListKeysResult{Keys: result}, nil
}

func (s *Service) RevokeKey(ctx context.Context, payload *gen.RevokeKeyPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	keyID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid key ID format")
	}

	err = s.repo.DeleteAPIKey(ctx, repo.DeleteAPIKeyParams{
		ID:             keyID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return oops.E(oops.CodeUnexpected, err, "error revoking api key").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
