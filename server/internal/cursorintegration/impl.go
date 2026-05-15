package cursorintegration

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/cursor_integration"
	srv "github.com/speakeasy-api/gram/server/gen/http/cursor_integration/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/cursorintegration/repo"
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
	logger = logger.With(attr.SlogComponent("cursorintegration.api"))
	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/cursorintegration"),
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

func (s *Service) GetConfig(ctx context.Context, _ *gen.GetConfigPayload) (*gen.CursorIntegrationConfig, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if authCtx.ProjectID == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "missing project context")
	}
	projectID := *authCtx.ProjectID
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	cfg, row, err := s.client.LoadForProjectRow(ctx, authCtx.ActiveOrganizationID, projectID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return emptyView(authCtx.ActiveOrganizationID, projectID.String()), nil
	}
	return buildView(cfg, *row), nil
}

func (s *Service) UpsertConfig(ctx context.Context, payload *gen.UpsertConfigPayload) (*gen.CursorIntegrationConfig, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if authCtx.ProjectID == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "missing project context")
	}
	projectID := *authCtx.ProjectID
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogProjectID(projectID.String()), attr.SlogUserID(authCtx.UserID))

	before, beforeRow, err := s.client.LoadForProjectRow(ctx, authCtx.ActiveOrganizationID, projectID)
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

	cfg, row, err := s.client.UpsertWithTx(ctx, dbtx, authCtx.ActiveOrganizationID, projectID, apiKey, payload.Enabled)
	if err != nil {
		return nil, err
	}

	var beforeSnap *audit.CursorIntegrationSnapshot
	if beforeRow != nil {
		snap := snapshotFromConfig(before)
		beforeSnap = &snap
	}
	afterSnap := snapshotFromConfig(cfg)

	if err := s.audit.LogCursorIntegrationUpsert(ctx, dbtx, audit.LogCursorIntegrationUpsertEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        projectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		ConfigURN:        urn.NewCursorIntegrationConfig(row.ID),
		SnapshotBefore:   beforeSnap,
		SnapshotAfter:    &afterSnap,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log cursor integration upsert").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit cursor integration upsert").Log(ctx, logger)
	}

	return buildView(cfg, *row), nil
}

func (s *Service) DeleteConfig(ctx context.Context, _ *gen.DeleteConfigPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if authCtx.ProjectID == nil {
		return oops.E(oops.CodeBadRequest, nil, "missing project context")
	}
	projectID := *authCtx.ProjectID
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogProjectID(projectID.String()), attr.SlogUserID(authCtx.UserID))

	_, row, err := s.client.LoadForProjectRow(ctx, authCtx.ActiveOrganizationID, projectID)
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

	if err := s.client.SoftDeleteWithTx(ctx, dbtx, authCtx.ActiveOrganizationID, projectID); err != nil {
		return err
	}

	if err := s.audit.LogCursorIntegrationDelete(ctx, dbtx, audit.LogCursorIntegrationDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        projectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		ConfigURN:        urn.NewCursorIntegrationConfig(row.ID),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log cursor integration delete").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit cursor integration delete").Log(ctx, logger)
	}

	return nil
}

func snapshotFromConfig(cfg Config) audit.CursorIntegrationSnapshot {
	return audit.CursorIntegrationSnapshot{
		Enabled:   cfg.Enabled,
		HasAPIKey: cfg.APIKey != "",
	}
}

func emptyView(orgID, projectID string) *gen.CursorIntegrationConfig {
	return &gen.CursorIntegrationConfig{
		ID:             nil,
		OrganizationID: orgID,
		ProjectID:      projectID,
		Enabled:        false,
		HasAPIKey:      false,
		LastPolledAt:   nil,
		CreatedAt:      nil,
		UpdatedAt:      nil,
	}
}

func buildView(cfg Config, row repo.CursorIntegrationConfig) *gen.CursorIntegrationConfig {
	id := row.ID.String()
	lastPolledAt := row.LastPolledAt.Time.Format(time.RFC3339)
	createdAt := row.CreatedAt.Time.Format(time.RFC3339)
	updatedAt := row.UpdatedAt.Time.Format(time.RFC3339)
	return &gen.CursorIntegrationConfig{
		ID:             &id,
		OrganizationID: cfg.OrganizationID,
		ProjectID:      cfg.ProjectID.String(),
		Enabled:        cfg.Enabled,
		HasAPIKey:      cfg.APIKey != "",
		LastPolledAt:   &lastPolledAt,
		CreatedAt:      &createdAt,
		UpdatedAt:      &updatedAt,
	}
}
