package agent

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	srv "github.com/speakeasy-api/gram/server/gen/http/agent/server"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/marketplace"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	db        *pgxpool.Pool
	repo      *repo.Queries
	auth      *auth.Auth
	authz     *authz.Engine
	serverURL string
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	serverURL string,
) *Service {
	logger = logger.With(attr.SlogComponent("agent"))
	return &Service{
		tracer:    tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/agent"),
		logger:    logger,
		db:        db,
		repo:      repo.New(db),
		auth:      auth.New(logger, db, sessions, authzEngine),
		authz:     authzEngine,
		serverURL: serverURL,
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

// GetPlugins returns every plugin in the published projects of the caller's
// org. Per-principal assignment scoping is intentionally disabled for now (see
// DNO-239): every org member receives every published-project plugin. The
// supplied email is still validated and will drive principal resolution again
// (user:/role: lookups) once RBAC-backed assignment management ships.
func (s *Service) GetPlugins(ctx context.Context, payload *gen.GetPluginsPayload) (*gen.GetPluginsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Validate the caller sent a well-formed email even though it does not yet
	// scope the result, so the request contract stays stable for DNO-239.
	email := conv.NormalizeEmail(payload.Email)
	if _, err := urn.ParsePrincipal(string(urn.PrincipalTypeEmail) + ":" + email); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid email")
	}

	// Best-effort: record that this user's device agent polled, so the dashboard
	// can show who is actively running it. Never fail the sync if the write fails
	// (mirrors api_keys.last_accessed_at). The query's ON CONFLICT guard caps
	// writes to at most once per minute per (org, email).
	if err := s.repo.UpsertDeviceAgentSync(ctx, repo.UpsertDeviceAgentSyncParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Email:          email,
	}); err != nil {
		s.logger.WarnContext(ctx, "failed to record device agent sync",
			attr.SlogError(err),
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		)
	}

	rows, err := s.repo.GetAgentPluginSet(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving agent plugin set").LogError(ctx, s.logger)
	}

	base := strings.TrimRight(s.serverURL, "/")
	marketplaceURL := func(token string) string {
		return base + marketplace.RoutePrefix + token + ".git"
	}

	return mv.BuildAgentPluginsView(rows, marketplaceURL), nil
}

// ListSyncedUsers returns the emails seen polling agent.getPlugins for the
// caller's org, most recently active first, for the dashboard's device-agent
// users view. Org admins only; attribution is by the email the agent reports on
// each sync (the org-scoped API key is shared across the fleet).
func (s *Service) ListSyncedUsers(ctx context.Context, _ *gen.ListSyncedUsersPayload) (*gen.ListSyncedUsersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(authCtx.ActiveOrganizationID),
		attr.UserID(authCtx.UserID),
	)

	rows, err := s.repo.ListDeviceAgentSyncs(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing device agent syncs").LogError(ctx, s.logger)
	}

	users := make([]*gen.SyncedAgentUser, 0, len(rows))
	for _, r := range rows {
		users = append(users, &gen.SyncedAgentUser{
			Email:       r.Email,
			FirstSeenAt: r.FirstSeenAt.Time.Format(time.RFC3339),
			LastSeenAt:  r.LastSeenAt.Time.Format(time.RFC3339),
		})
	}

	return &gen.ListSyncedUsersResult{Users: users}, nil
}
