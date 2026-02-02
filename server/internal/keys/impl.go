package keys

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
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
	organizations_repo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	project_repo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

const keyPrefix = "gram"

type Service struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	db          *pgxpool.Pool
	repo        *repo.Queries
	auth        *auth.Auth
	projectRepo *project_repo.Queries
	orgsRepo    *organizations_repo.Queries
	keyPrefix   string
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
		tracer:      otel.Tracer("github.com/speakeasy-api/gram/server/internal/keys"),
		logger:      logger,
		db:          db,
		repo:        repo.New(db),
		auth:        auth.New(logger, db, sessions),
		projectRepo: project_repo.New(db),
		orgsRepo:    organizations_repo.New(db),
		keyPrefix:   fullKeyPrefix,
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

	scopes := map[string]struct{}{}
	for _, rawscope := range payload.Scopes {
		scope, ok := auth.APIKeyScopes[rawscope]
		if !ok || scope == auth.APIKeyScopeInvalid {
			return nil, oops.E(oops.CodeBadRequest, nil, "invalid api key scope: %s", scope).Log(ctx, s.logger)
		}

		scopes[scope.String()] = struct{}{}
	}

	if len(scopes) == 0 {
		scopes = map[string]struct{}{
			auth.APIKeyScopeConsumer.String(): {},
		}
	}

	finalScopes := slices.Sorted(maps.Keys(scopes))

	createdKey, err := s.repo.CreateAPIKey(ctx, repo.CreateAPIKeyParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Name:            payload.Name,
		KeyHash:         keyHash,
		KeyPrefix:       s.keyPrefix + token[:5],
		Scopes:          finalScopes,
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
		LastAccessedAt:  nil,
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
		var lastAccessedAt *string
		if key.LastAccessedAt.Valid {
			formatted := key.LastAccessedAt.Time.Truncate(time.Minute).Format(time.RFC3339)
			lastAccessedAt = &formatted
		}

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
			LastAccessedAt:  lastAccessedAt,
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

func (s *Service) VerifyKey(
	ctx context.Context,
	payload *gen.VerifyKeyPayload,
) (*gen.ValidateKeyResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	orgID := authCtx.ActiveOrganizationID
	orgMeta, err := s.orgsRepo.GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch organization: %w", err)
	}

	rawProjects, err := s.projectRepo.ListProjectsByOrganization(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch projects: %w", err)
	}

	result := &gen.ValidateKeyResult{
		Organization: parseOrg(orgMeta),
		Projects:     parseProjects(rawProjects),
		Scopes:       authCtx.APIKeyScopes,
	}

	return result, nil
}

func parseOrg(orgMeta organizations_repo.OrganizationMetadatum) *gen.ValidateKeyOrganization {
	return &gen.ValidateKeyOrganization{
		ID:   orgMeta.ID,
		Name: orgMeta.Name,
		Slug: orgMeta.Slug,
	}
}

func parseProjects(rawProjects []project_repo.Project) []*gen.ValidateKeyProject {
	projects := make([]*gen.ValidateKeyProject, len(rawProjects))
	for idx, p := range rawProjects {
		projects[idx] = &gen.ValidateKeyProject{
			ID:   p.ID.String(),
			Name: p.Name,
			Slug: p.Slug,
		}
	}

	return projects
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) generateToken() (string, error) {
	const randomKeyLength = 64
	randomBytes := make([]byte, randomKeyLength/2) // there are 2 hex chars per byte, we can guarantee output of 64 chars this way
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", fmt.Errorf("generate random token bytes: %w", err)
	}
	return hex.EncodeToString(randomBytes), nil
}
