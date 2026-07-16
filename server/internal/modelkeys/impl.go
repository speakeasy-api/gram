package modelkeys

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/model_keys/server"
	gen "github.com/speakeasy-api/gram/server/gen/model_keys"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/modelkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	db          *pgxpool.Pool
	auth        *auth.Auth
	authz       *authz.Engine
	enc         *encryption.Client
	provisioner openrouter.Provisioner
	features    *productfeatures.Client
	audit       *audit.Logger
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	enc *encryption.Client,
	provisioner openrouter.Provisioner,
	features *productfeatures.Client,
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("modelkeys"))

	return &Service{
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/modelkeys"),
		logger:      logger,
		db:          db,
		auth:        auth.New(logger, db, sessions, authzEngine),
		authz:       authzEngine,
		enc:         enc,
		provisioner: provisioner,
		features:    features,
		audit:       auditLogger,
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

func modelProviderKeySnapshot(row repo.ModelProviderKey) *audit.ModelProviderKeySnapshot {
	return &audit.ModelProviderKeySnapshot{
		ProjectID: row.ProjectID,
		Slot:      row.Slot,
		Provider:  row.Provider,
		Enabled:   row.Enabled,
	}
}

func (s *Service) ListKeys(ctx context.Context, _ *gen.ListKeysPayload) (*gen.ListKeysResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListKeysByProject(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list model provider keys").LogError(ctx, s.logger)
	}

	return &gen.ListKeysResult{Keys: mv.BuildModelProviderKeyListView(rows)}, nil
}

func (s *Service) UpsertKey(ctx context.Context, payload *gen.UpsertKeyPayload) (*types.ModelProviderKey, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	enabled, err := s.features.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureCustomModelKeys)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check custom model keys feature").LogError(ctx, logger)
	}
	if !enabled {
		return nil, oops.E(oops.CodeForbidden, nil, "custom model keys are not enabled for this organization")
	}

	slot := strings.ToLower(strings.TrimSpace(payload.Slot))
	if !slices.Contains(ValidSlots(), slot) {
		return nil, oops.E(oops.CodeInvalid, nil, "unsupported slot: %s", slot)
	}

	provider := strings.ToLower(strings.TrimSpace(payload.Provider))
	if provider != ProviderOpenRouter {
		return nil, oops.E(oops.CodeInvalid, nil, "unsupported model provider: %s", provider)
	}

	apiKey := strings.TrimSpace(payload.APIKey)
	if apiKey == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "api_key is required")
	}

	if _, _, err := s.provisioner.GetKeyUsage(ctx, apiKey); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "the model provider rejected the API key").LogError(ctx, logger)
	}

	encrypted, err := s.enc.Encrypt([]byte(apiKey))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encrypt model provider key").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin model provider key transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)

	replaced, err := queries.SoftDeleteKeyBySlot(ctx, repo.SoftDeleteKeyBySlotParams{
		ProjectID: *authCtx.ProjectID,
		Slot:      slot,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "replace model provider key").LogError(ctx, logger)
	}

	var snapshotBefore *audit.ModelProviderKeySnapshot
	if len(replaced) > 0 {
		snapshotBefore = modelProviderKeySnapshot(replaced[0])
	}

	row, err := queries.InsertKey(ctx, repo.InsertKeyParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       *authCtx.ProjectID,
		Slot:            slot,
		Provider:        provider,
		ApiKeyEncrypted: encrypted,
		Enabled:         payload.Enabled,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "model provider key was updated concurrently, retry").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "save model provider key").LogError(ctx, logger)
	}

	if err := s.audit.LogModelProviderKeyUpsert(ctx, dbtx, audit.LogModelProviderKeyUpsertEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		KeyURN:           urn.NewModelProviderKey(row.ID),
		Slot:             row.Slot,
		SnapshotBefore:   snapshotBefore,
		SnapshotAfter:    modelProviderKeySnapshot(row),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log model provider key upsert").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit model provider key transaction").LogError(ctx, logger)
	}

	return mv.BuildModelProviderKeyView(row), nil
}

func (s *Service) SetKeyEnabled(ctx context.Context, payload *gen.SetKeyEnabledPayload) (*types.ModelProviderKey, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	keyID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid key id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin model provider key transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	existing, err := queries.GetKeyByIDForUpdate(ctx, repo.GetKeyByIDForUpdateParams{
		ID:        keyID,
		ProjectID: *authCtx.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "model provider key not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get model provider key").LogError(ctx, logger)
	}

	if payload.Enabled {
		enabled, err := s.features.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureCustomModelKeys)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "check custom model keys feature").LogError(ctx, logger)
		}
		if !enabled {
			return nil, oops.E(oops.CodeForbidden, nil, "custom model keys are not enabled for this organization")
		}
	}

	if existing.Enabled == payload.Enabled {
		return mv.BuildModelProviderKeyView(existing), nil
	}

	row, err := queries.SetKeyEnabled(ctx, repo.SetKeyEnabledParams{
		Enabled:   payload.Enabled,
		ID:        keyID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set model provider key enabled state").LogError(ctx, logger)
	}

	if err := s.audit.LogModelProviderKeyUpsert(ctx, dbtx, audit.LogModelProviderKeyUpsertEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		KeyURN:           urn.NewModelProviderKey(row.ID),
		Slot:             row.Slot,
		SnapshotBefore:   modelProviderKeySnapshot(existing),
		SnapshotAfter:    modelProviderKeySnapshot(row),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log model provider key upsert").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit model provider key transaction").LogError(ctx, logger)
	}

	return mv.BuildModelProviderKeyView(row), nil
}

func (s *Service) DeleteKey(ctx context.Context, payload *gen.DeleteKeyPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	keyID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid key id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin model provider key transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	deleted, err := repo.New(dbtx).SoftDeleteKeyByID(ctx, repo.SoftDeleteKeyByIDParams{
		ID:        keyID,
		ProjectID: *authCtx.ProjectID,
	})
	switch {
	// Absent or already-deleted keys make retried deletes an idempotent no-op
	// with no additional audit event.
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "delete model provider key").LogError(ctx, logger)
	}

	if err := s.audit.LogModelProviderKeyDelete(ctx, dbtx, audit.LogModelProviderKeyDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		KeyURN:           urn.NewModelProviderKey(deleted.ID),
		Slot:             deleted.Slot,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log model provider key deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit model provider key transaction").LogError(ctx, logger)
	}

	return nil
}
