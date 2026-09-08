// Package chatanalysis implements the adminChatAnalysis service: the
// platform-admin surface over chat_analysis_settings, the per-(organization,
// judge) switches and daily budgets the chat analysis pipeline spends against
// (server/internal/chat/analysis).
package chatanalysis

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/admin_chat_analysis"
	srv "github.com/speakeasy-api/gram/server/gen/http/admin_chat_analysis/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/businessmemory"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *pgxpool.Pool
	auth     *auth.Auth
	audit    *audit.Logger
	signaler analysis.Signaler
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
	signaler analysis.Signaler,
) *Service {
	logger = logger.With(attr.SlogComponent("chatanalysis.api"))
	return &Service{
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/chatanalysis"),
		logger:   logger,
		db:       db,
		auth:     auth.New(logger, db, sessions, authzEngine),
		audit:    auditLogger,
		signaler: signaler,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// requirePlatformAdmin enforces the platform-admin flag and returns a logger
// tagged with the actor and the requested target organization. The target
// comes from the payload, never from the session's active organization.
func (s *Service) requirePlatformAdmin(ctx context.Context, organizationID string) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, logger, err := auth.RequirePlatformAdmin(ctx, s.logger)
	if err != nil {
		return nil, logger, err
	}
	if organizationID == "" {
		return nil, logger, oops.E(oops.CodeInvalid, nil, "organization_id is required")
	}

	logger = logger.With(attr.SlogOrganizationID(organizationID))
	if _, err := organizationsrepo.New(s.db).GetOrganizationMetadata(ctx, organizationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, logger, oops.E(oops.CodeNotFound, err, "organization not found")
		}
		return nil, logger, oops.E(oops.CodeUnexpected, err, "get organization metadata").LogError(ctx, logger)
	}

	return authCtx, logger, nil
}

func (s *Service) GetSettings(ctx context.Context, payload *gen.GetSettingsPayload) (*gen.ChatAnalysisSettings, error) {
	_, logger, err := s.requirePlatformAdmin(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}

	view, err := loadSettingsView(ctx, repo.New(s.db), payload.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get chat analysis settings").LogError(ctx, logger)
	}
	return view, nil
}

func (s *Service) UpsertWorkUnitsSettings(ctx context.Context, payload *gen.UpsertWorkUnitsSettingsPayload) (*gen.ChatAnalysisSettings, error) {
	return s.upsertJudgeSettings(ctx, payload.OrganizationID, analysis.WorkUnitsJudgeName, payload.WorkUnitsEnabled, payload.WorkUnitsDailyCap)
}

func (s *Service) UpsertBusinessMemorySettings(ctx context.Context, payload *gen.UpsertBusinessMemorySettingsPayload) (*gen.ChatAnalysisSettings, error) {
	return s.upsertJudgeSettings(ctx, payload.OrganizationID, businessmemory.JudgeName, payload.BusinessMemoryEnabled, payload.BusinessMemoryDailyCap)
}

func (s *Service) upsertJudgeSettings(ctx context.Context, organizationID string, judge string, enabled bool, dailyCap int) (*gen.ChatAnalysisSettings, error) {
	authCtx, logger, err := s.requirePlatformAdmin(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if dailyCap < 0 || dailyCap > MaxDailyCap {
		return nil, oops.E(oops.CodeInvalid, nil, "daily cap must be between 0 and %d", MaxDailyCap)
	}

	view, err := UpsertSettings(
		ctx, s.db, s.audit, organizationID, judge, enabled, dailyCap,
		urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), authCtx.Email,
	)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "upsert chat analysis settings").LogError(ctx, logger)
	}
	return settingsView(view), nil
}

// TriggerAnalysis wakes the chat analysis coordinator of every live project in
// the named organization. It moves no queue state itself: the coordinator's
// pass still enqueues, reserves against the daily budget, and applies the
// inactivity window — this only replaces waiting for a chat write or the
// periodic sweep to deliver the same signal.
func (s *Service) TriggerAnalysis(ctx context.Context, payload *gen.TriggerAnalysisPayload) (*gen.TriggerAnalysisResult, error) {
	_, logger, err := s.requirePlatformAdmin(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}

	projectsSignaled, err := TriggerOrganization(ctx, s.db, s.signaler, payload.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "trigger chat analysis").LogError(ctx, logger)
	}

	return &gen.TriggerAnalysisResult{ProjectsSignaled: projectsSignaled}, nil
}

func loadSettingsView(ctx context.Context, queries *repo.Queries, organizationID string) (*gen.ChatAnalysisSettings, error) {
	view, err := loadSettings(ctx, queries, organizationID)
	if err != nil {
		return nil, err
	}
	return settingsView(view), nil
}

func settingsView(view Settings) *gen.ChatAnalysisSettings {
	return &gen.ChatAnalysisSettings{
		OrganizationID: view.OrganizationID, WorkUnitsEnabled: view.WorkUnitsEnabled,
		WorkUnitsDailyCap: view.WorkUnitsDailyCap, BusinessMemoryEnabled: view.BusinessMemoryEnabled,
		BusinessMemoryDailyCap: view.BusinessMemoryDailyCap, IsDefault: view.IsDefault,
	}
}
