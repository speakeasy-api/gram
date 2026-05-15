package aiintegrations

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/ai_integrations"
	srv "github.com/speakeasy-api/gram/server/gen/http/ai_integrations/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
	authz  *authz.Engine
	audit  *audit.Logger
	client *Client
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	client *Client,
) *Service {
	logger = logger.With(attr.SlogComponent("aiintegrations.api"))
	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/aiintegrations"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions, authzEngine),
		authz:  authzEngine,
		audit:  auditLogger,
		client: client,
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

func (s *Service) GetConfig(ctx context.Context, payload *gen.GetConfigPayload) (*gen.AIIntegrationConfig, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	provider, err := NormalizeProvider(payload.Provider)
	if err != nil {
		return nil, err
	}

	cfg, row, err := s.client.LoadForOrgAndProviderRow(ctx, authCtx.ActiveOrganizationID, provider)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return emptyView(authCtx.ActiveOrganizationID, provider), nil
	}
	return buildView(cfg, row.ID), nil
}

func (s *Service) UpsertConfig(ctx context.Context, payload *gen.UpsertConfigPayload) (*gen.AIIntegrationConfig, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	provider, err := NormalizeProvider(payload.Provider)
	if err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogUserID(authCtx.UserID))

	before, beforeRow, err := s.client.LoadForOrgAndProviderRow(ctx, authCtx.ActiveOrganizationID, provider)
	if err != nil {
		return nil, err
	}
	apiKey := strings.TrimSpace(payload.APIKey)
	if apiKey == "" {
		if beforeRow == nil || before.APIKey == "" {
			return nil, oops.E(oops.CodeInvalid, nil, "api_key is required")
		}
		apiKey = before.APIKey
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	cfg, row, err := s.client.UpsertWithTx(ctx, dbtx, authCtx.ActiveOrganizationID, provider, apiKey, payload.Enabled)
	if err != nil {
		return nil, err
	}

	var beforeSnap *audit.AIIntegrationSnapshot
	if beforeRow != nil {
		snap := snapshotFromConfig(before)
		beforeSnap = &snap
	}
	afterSnap := snapshotFromConfig(cfg)

	if err := s.audit.LogAIIntegrationUpsert(ctx, dbtx, audit.LogAIIntegrationUpsertEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        cfg.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		ConfigURN:        urn.NewAIIntegrationConfig(row.ID),
		SnapshotBefore:   beforeSnap,
		SnapshotAfter:    &afterSnap,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log ai integration upsert").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit ai integration upsert").Log(ctx, logger)
	}

	return buildView(cfg, row.ID), nil
}

func (s *Service) DeleteConfig(ctx context.Context, payload *gen.DeleteConfigPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	provider, err := NormalizeProvider(payload.Provider)
	if err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogUserID(authCtx.UserID))

	cfg, row, err := s.client.LoadForOrgAndProviderRow(ctx, authCtx.ActiveOrganizationID, provider)
	if err != nil {
		return err
	}
	if row == nil {
		return nil
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := s.client.SoftDeleteWithTx(ctx, dbtx, authCtx.ActiveOrganizationID, provider); err != nil {
		return err
	}

	if err := s.audit.LogAIIntegrationDelete(ctx, dbtx, audit.LogAIIntegrationDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        cfg.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		ConfigURN:        urn.NewAIIntegrationConfig(row.ID),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log ai integration delete").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit ai integration delete").Log(ctx, logger)
	}

	return nil
}

func snapshotFromConfig(cfg Config) audit.AIIntegrationSnapshot {
	return audit.AIIntegrationSnapshot{
		Provider:  cfg.Provider,
		ProjectID: cfg.ProjectID,
		Enabled:   cfg.Enabled,
		HasAPIKey: cfg.APIKey != "",
	}
}

func emptyView(orgID, provider string) *gen.AIIntegrationConfig {
	return &gen.AIIntegrationConfig{
		ID:             nil,
		OrganizationID: orgID,
		Provider:       provider,
		ProjectID:      nil,
		Enabled:        false,
		HasAPIKey:      false,
		LastPolledAt:   nil,
		CreatedAt:      nil,
		UpdatedAt:      nil,
	}
}

func buildView(cfg Config, idValue uuid.UUID) *gen.AIIntegrationConfig {
	id := idValue.String()
	projectID := cfg.ProjectID.String()
	lastPolledAt := cfg.LastPolledAt.Format(time.RFC3339)
	createdAt := cfg.CreatedAt.Format(time.RFC3339)
	updatedAt := cfg.UpdatedAt.Format(time.RFC3339)
	return &gen.AIIntegrationConfig{
		ID:             &id,
		OrganizationID: cfg.OrganizationID,
		Provider:       cfg.Provider,
		ProjectID:      &projectID,
		Enabled:        cfg.Enabled,
		HasAPIKey:      cfg.APIKey != "",
		LastPolledAt:   &lastPolledAt,
		CreatedAt:      &createdAt,
		UpdatedAt:      &updatedAt,
	}
}
