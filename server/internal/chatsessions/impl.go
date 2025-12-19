package chatsessions

import (
	"context"
	"log/slog"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/chat_sessions"
	srv "github.com/speakeasy-api/gram/server/gen/http/chat_sessions/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type Service struct {
	tracer              trace.Tracer
	logger              *slog.Logger
	db                  *pgxpool.Pool
	auth                *auth.Auth
	chatSessionsManager *chatsessions.Manager
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, chatSessionsManager *chatsessions.Manager) *Service {
	logger = logger.With(attr.SlogComponent("chat_sessions"))

	return &Service{
		tracer:              otel.Tracer("github.com/speakeasy-api/gram/server/internal/chatsessions"),
		logger:              logger,
		db:                  db,
		auth:                auth.New(logger, db, sessions),
		chatSessionsManager: chatSessionsManager,
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

func (s *Service) APIKeyAuth(ctx context.Context, key string, scheme *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, scheme)
}

func (s *Service) ProjectSlugAuth(ctx context.Context, slug string, scheme *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, slug, scheme)
}

func (s *Service) Create(ctx context.Context, p *gen.CreatePayload) (*gen.CreateResult, error) {
	ctx, span := s.tracer.Start(ctx, "chatsessions.create")
	defer span.End()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized).Log(ctx, s.logger)
	}
	if authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized).Log(ctx, s.logger)
	}

	claims := chatsessions.ChatSessionClaims{
		OrgID:            authCtx.ActiveOrganizationID,
		ProjectID:        authCtx.ProjectID.String(),
		OrganizationSlug: authCtx.OrganizationSlug,
		ProjectSlug:      *authCtx.ProjectSlug,
		UserIdentifier:   p.UserIdentifier,
		RegisteredClaims: jwt.RegisteredClaims{}, //nolint:exhaustruct // to be populated by chatSessionsManager
	}

	token, _, err := s.chatSessionsManager.GenerateToken(
		ctx,
		claims,
		p.EmbedOrigin,
		p.ExpiresAfter, // Min/max validated by Goa
	)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to generate chat session token").Log(ctx, s.logger)
	}

	result := &gen.CreateResult{
		ClientToken:    token,
		ExpiresAfter:   p.ExpiresAfter,
		Status:         "active",
		UserIdentifier: p.UserIdentifier,
		EmbedOrigin:    p.EmbedOrigin,
	}

	s.logger.InfoContext(ctx, "created chat session",
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogProjectID(authCtx.ProjectID.String()),
	)

	return result, nil
}

func (s *Service) Revoke(ctx context.Context, p *gen.RevokePayload) error {
	ctx, span := s.tracer.Start(ctx, "chatsessions.revoke")
	defer span.End()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeUnauthorized).Log(ctx, s.logger)
	}
	if authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized).Log(ctx, s.logger)
	}

	// Validate the token and extract JTI
	claims, err := s.chatSessionsManager.ValidateToken(ctx, p.Token)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid token").Log(ctx, s.logger)
	}

	// Verify the token belongs to the same org/project
	if claims.OrgID != authCtx.ActiveOrganizationID || claims.ProjectID != authCtx.ProjectID.String() {
		return oops.E(oops.CodeForbidden, nil, "token does not belong to this project").Log(ctx, s.logger)
	}

	// Revoke the token
	err = s.chatSessionsManager.RevokeToken(ctx, claims.ID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to revoke token").Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "revoked chat session",
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogProjectID(authCtx.ProjectID.String()),
	)

	return nil
}
