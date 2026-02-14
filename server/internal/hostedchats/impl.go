package hostedchats

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/hostedchats/server"
	gen "github.com/speakeasy-api/gram/server/gen/hostedchats"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hostedchats/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

type Service struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	db          *pgxpool.Pool
	repo        *repo.Queries
	projectRepo *projectsrepo.Queries
	sessions    *sessions.Manager
	auth        *auth.Auth
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	logger = logger.With(attr.SlogComponent("hostedchats"))

	return &Service{
		tracer:      otel.Tracer("github.com/speakeasy-api/gram/server/internal/hostedchats"),
		logger:      logger,
		db:          db,
		repo:        repo.New(db),
		projectRepo: projectsrepo.New(db),
		sessions:    sessions,
		auth:        auth.New(logger, db, sessions),
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

func (s *Service) Create(ctx context.Context, payload *gen.CreatePayload) (*gen.HostedChatResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.auth.CheckProjectAccess(ctx, s.logger, *authCtx.ProjectID); err != nil {
		return nil, err
	}

	slug := conv.PtrValOr(payload.Slug, "")
	if slug == "" {
		slug = conv.ToSlug(payload.Name)
	}
	if slug == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "unable to generate a valid slug from the provided name").Log(ctx, s.logger)
	}

	row, err := s.repo.CreateHostedChat(ctx, repo.CreateHostedChatParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       *authCtx.ProjectID,
		CreatedByUserID: authCtx.UserID,
		Name:            payload.Name,
		Slug:            slug,
		McpSlug:         conv.PtrToPGText(payload.McpSlug),
		SystemPrompt:    conv.PtrToPGText(payload.SystemPrompt),
		WelcomeTitle:    conv.PtrToPGText(payload.WelcomeTitle),
		WelcomeSubtitle: conv.PtrToPGText(payload.WelcomeSubtitle),
		ThemeColorScheme: conv.Default(conv.PtrValOr(payload.ThemeColorScheme, ""), "system"),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create hosted chat").Log(ctx, s.logger)
	}

	return &gen.HostedChatResult{
		HostedChat: toHostedChat(&row),
	}, nil
}

func (s *Service) Get(ctx context.Context, payload *gen.GetPayload) (*gen.HostedChatResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.auth.CheckProjectAccess(ctx, s.logger, *authCtx.ProjectID); err != nil {
		return nil, err
	}

	row, err := s.repo.GetHostedChatBySlug(ctx, repo.GetHostedChatBySlugParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      payload.Slug,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get hosted chat by slug").Log(ctx, s.logger)
	}

	return &gen.HostedChatResult{
		HostedChat: toHostedChat(&row),
	}, nil
}

func (s *Service) List(ctx context.Context, payload *gen.ListPayload) (*gen.ListHostedChatsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.auth.CheckProjectAccess(ctx, s.logger, *authCtx.ProjectID); err != nil {
		return nil, err
	}

	rows, err := s.repo.ListHostedChatsByProject(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list hosted chats").Log(ctx, s.logger)
	}

	chats := make([]*gen.HostedChat, 0, len(rows))
	for _, row := range rows {
		chats = append(chats, toHostedChat(&row))
	}

	return &gen.ListHostedChatsResult{
		HostedChats: chats,
	}, nil
}

func (s *Service) Update(ctx context.Context, payload *gen.UpdatePayload) (*gen.HostedChatResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.auth.CheckProjectAccess(ctx, s.logger, *authCtx.ProjectID); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid hosted chat id").Log(ctx, s.logger)
	}

	row, err := s.repo.UpdateHostedChat(ctx, repo.UpdateHostedChatParams{
		ID:               id,
		ProjectID:        *authCtx.ProjectID,
		Name:             conv.PtrToPGText(payload.Name),
		McpSlug:          conv.PtrToPGText(payload.McpSlug),
		SystemPrompt:     conv.PtrToPGText(payload.SystemPrompt),
		WelcomeTitle:     conv.PtrToPGText(payload.WelcomeTitle),
		WelcomeSubtitle:  conv.PtrToPGText(payload.WelcomeSubtitle),
		ThemeColorScheme: conv.PtrToPGText(payload.ThemeColorScheme),
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "update hosted chat").Log(ctx, s.logger)
	}

	return &gen.HostedChatResult{
		HostedChat: toHostedChat(&row),
	}, nil
}

func (s *Service) Delete(ctx context.Context, payload *gen.DeletePayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.auth.CheckProjectAccess(ctx, s.logger, *authCtx.ProjectID); err != nil {
		return err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid hosted chat id").Log(ctx, s.logger)
	}

	if err := s.repo.DeleteHostedChat(ctx, repo.DeleteHostedChatParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete hosted chat").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) GetPublic(ctx context.Context, payload *gen.GetPublicPayload) (*gen.HostedChatPublicResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	row, err := s.repo.GetHostedChatPublicBySlug(ctx, payload.ChatSlug)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get hosted chat public").Log(ctx, s.logger)
	}

	// Verify user belongs to the org that owns this hosted chat
	if row.OrganizationID != authCtx.ActiveOrganizationID {
		return nil, oops.C(oops.CodeForbidden)
	}

	return &gen.HostedChatPublicResult{
		HostedChat:  toHostedChatFromPublic(&row),
		ProjectSlug: conv.Ptr(row.ProjectSlug),
	}, nil
}

func toHostedChat(row *repo.HostedChat) *gen.HostedChat {
	return &gen.HostedChat{
		ID:               row.ID.String(),
		OrganizationID:   row.OrganizationID,
		ProjectID:        row.ProjectID.String(),
		Name:             row.Name,
		Slug:             row.Slug,
		McpSlug:          conv.FromPGText(row.McpSlug),
		SystemPrompt:     conv.FromPGText(row.SystemPrompt),
		WelcomeTitle:     conv.FromPGText(row.WelcomeTitle),
		WelcomeSubtitle:  conv.FromPGText(row.WelcomeSubtitle),
		ThemeColorScheme: row.ThemeColorScheme,
		CreatedAt:        row.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:        row.UpdatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func toHostedChatFromPublic(row *repo.GetHostedChatPublicBySlugRow) *gen.HostedChat {
	return &gen.HostedChat{
		ID:               row.ID.String(),
		OrganizationID:   row.OrganizationID,
		ProjectID:        row.ProjectID.String(),
		Name:             row.Name,
		Slug:             row.Slug,
		McpSlug:          conv.FromPGText(row.McpSlug),
		SystemPrompt:     conv.FromPGText(row.SystemPrompt),
		WelcomeTitle:     conv.FromPGText(row.WelcomeTitle),
		WelcomeSubtitle:  conv.FromPGText(row.WelcomeSubtitle),
		ThemeColorScheme: row.ThemeColorScheme,
		CreatedAt:        row.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:        row.UpdatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
	}
}
