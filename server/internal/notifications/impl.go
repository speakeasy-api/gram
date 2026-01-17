package notifications

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/notifications"
	srv "github.com/speakeasy-api/gram/server/gen/http/notifications/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/notifications/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	auth   *auth.Auth
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	logger = logger.With(attr.SlogComponent("notifications"))

	return &Service{
		tracer: otel.Tracer("github.com/speakeasy-api/gram/server/internal/notifications"),
		logger: logger,
		db:     db,
		repo:   repo.New(db),
		auth:   auth.New(logger, db, sessions),
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

func (s *Service) ListNotifications(ctx context.Context, payload *gen.ListNotificationsPayload) (*gen.ListNotificationsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	var notifications []repo.Notification
	var err error

	if payload.Cursor != nil {
		cursorID, parseErr := uuid.Parse(*payload.Cursor)
		if parseErr != nil {
			return nil, oops.E(oops.CodeBadRequest, parseErr, "invalid cursor").Log(ctx, s.logger)
		}
		notifications, err = s.repo.ListNotificationsByCursor(ctx, repo.ListNotificationsByCursorParams{
			ProjectID:  *authCtx.ProjectID,
			Cursor:     cursorID,
			Archived:   payload.Archived,
			LimitCount: payload.Limit,
		})
	} else {
		notifications, err = s.repo.ListNotifications(ctx, repo.ListNotificationsParams{
			ProjectID:  *authCtx.ProjectID,
			Archived:   payload.Archived,
			LimitCount: payload.Limit,
		})
	}

	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list notifications").Log(ctx, s.logger)
	}

	result := make([]*gen.Notification, len(notifications))
	for i, n := range notifications {
		result[i] = toGenNotification(n)
	}

	var nextCursor *string
	if len(notifications) == int(payload.Limit) {
		lastID := notifications[len(notifications)-1].ID.String()
		nextCursor = &lastID
	}

	return &gen.ListNotificationsResult{
		Notifications: result,
		NextCursor:    nextCursor,
	}, nil
}

func (s *Service) CreateNotification(ctx context.Context, payload *gen.CreateNotificationPayload) (*gen.Notification, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	var actorUserID pgtype.Text
	if authCtx.UserID != "" {
		actorUserID = pgtype.Text{String: authCtx.UserID, Valid: true}
	}

	notification, err := s.repo.CreateNotification(ctx, repo.CreateNotificationParams{
		ProjectID:    *authCtx.ProjectID,
		Type:         string(payload.Type),
		Level:        string(payload.Level),
		Title:        payload.Title,
		Message:      conv.PtrToPGText(payload.Message),
		ActorUserID:  actorUserID,
		ResourceType: conv.PtrToPGText(payload.ResourceType),
		ResourceID:   conv.PtrToPGText(payload.ResourceID),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create notification").Log(ctx, s.logger)
	}

	return toGenNotification(notification), nil
}

func (s *Service) ArchiveNotification(ctx context.Context, payload *gen.ArchiveNotificationPayload) (*gen.Notification, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	notificationID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid notification ID").Log(ctx, s.logger)
	}

	notification, err := s.repo.ArchiveNotification(ctx, repo.ArchiveNotificationParams{
		ID:        notificationID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "notification not found").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to archive notification").Log(ctx, s.logger)
	}

	return toGenNotification(notification), nil
}

func (s *Service) GetUnreadCount(ctx context.Context, payload *gen.GetUnreadCountPayload) (*gen.UnreadCountResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	var count int32
	var err error

	if payload.Since != nil {
		sinceTime, parseErr := time.Parse(time.RFC3339, *payload.Since)
		if parseErr != nil {
			return nil, oops.E(oops.CodeBadRequest, parseErr, "invalid since timestamp").Log(ctx, s.logger)
		}
		count, err = s.repo.CountNotificationsSince(ctx, repo.CountNotificationsSinceParams{
			ProjectID: *authCtx.ProjectID,
			Since:     pgtype.Timestamptz{Time: sinceTime, Valid: true, InfinityModifier: 0},
		})
	} else {
		count, err = s.repo.CountAllNotifications(ctx, *authCtx.ProjectID)
	}

	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to count notifications").Log(ctx, s.logger)
	}

	return &gen.UnreadCountResult{
		Count: count,
	}, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func toGenNotification(n repo.Notification) *gen.Notification {
	result := &gen.Notification{
		ID:           n.ID.String(),
		ProjectID:    n.ProjectID.String(),
		Type:         gen.NotificationType(n.Type),
		Level:        gen.NotificationLevel(n.Level),
		Title:        n.Title,
		Message:      nil,
		ActorUserID:  nil,
		ResourceType: nil,
		ResourceID:   nil,
		ArchivedAt:   nil,
		CreatedAt:    n.CreatedAt.Time.Format(time.RFC3339),
	}

	if n.Message.Valid {
		result.Message = &n.Message.String
	}
	if n.ActorUserID.Valid {
		result.ActorUserID = &n.ActorUserID.String
	}
	if n.ResourceType.Valid {
		result.ResourceType = &n.ResourceType.String
	}
	if n.ResourceID.Valid {
		result.ResourceID = &n.ResourceID.String
	}
	if n.ArchivedAt.Valid {
		archivedAt := n.ArchivedAt.Time.Format(time.RFC3339)
		result.ArchivedAt = &archivedAt
	}

	return result
}
