package keys

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/keys/server"
	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizations_repo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	project_repo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	db          *pgxpool.Pool
	repo        *repo.Queries
	auth        *auth.Auth
	authz       *authz.Engine
	projectRepo *project_repo.Queries
	orgsRepo    *organizations_repo.Queries
	audit       *audit.Logger
	keyPrefix   string
}

var _ gen.Service = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	env string,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("keys"))

	fullKeyPrefix := auth.APIKeyPrefix(env)
	return &Service{
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/keys"),
		logger:      logger,
		db:          db,
		repo:        repo.New(db),
		auth:        auth.New(logger, db, sessions, authzEngine),
		authz:       authzEngine,
		projectRepo: project_repo.New(db),
		orgsRepo:    organizations_repo.New(db),
		audit:       auditLogger,
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

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) CreateKey(ctx context.Context, payload *gen.CreateKeyPayload) (*gen.Key, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
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

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing api keys").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	kr := s.repo.WithTx(dbtx)

	createdKey, err := kr.CreateAPIKey(ctx, repo.CreateAPIKeyParams{
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

	if err := s.audit.LogKeyCreate(ctx, dbtx, audit.LogKeyCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        projectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		KeyURN:           urn.NewAPIKey(createdKey.ID),
		KeyName:          payload.Name,
		Scopes:           finalScopes,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error adding api key creation audit log").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving api key creation").Log(ctx, s.logger)
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
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
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
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error accessing api keys").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	kr := s.repo.WithTx(dbtx)

	keyID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid key ID format")
	}

	deleted, err := kr.DeleteAPIKey(ctx, repo.DeleteAPIKeyParams{
		ID:             keyID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "error revoking api key").Log(ctx, s.logger)
	}

	if err := s.audit.LogKeyRevoke(ctx, dbtx, audit.LogKeyRevokeEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        deleted.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		KeyURN:           urn.NewAPIKey(keyID),
		KeyName:          deleted.Name,
		Scopes:           deleted.Scopes,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error adding api key revocation audit log").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error saving api key revocation").Log(ctx, s.logger)
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

func (s *Service) generateToken() (string, error) {
	const randomKeyLength = 64
	randomBytes := make([]byte, randomKeyLength/2) // there are 2 hex chars per byte, we can guarantee output of 64 chars this way
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", fmt.Errorf("generate random token bytes: %w", err)
	}
	return hex.EncodeToString(randomBytes), nil
}
