package chatmessage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/speakeasy-api/gram/server/internal/agentevents"
	"github.com/speakeasy-api/gram/server/internal/agentevents/eventsink"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksRepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

type Writer interface {
	Write(ctx context.Context, projectID uuid.UUID, params []chatRepo.CreateChatMessageParams) (int64, error)
}

type ProductFeaturesClient interface {
	IsFeatureEnabled(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

type TitleGenerator interface {
	ScheduleChatTitleGeneration(ctx context.Context, chatID, orgID, projectID string) error
}

type Config struct {
	Writer          Writer
	ProductFeatures ProductFeaturesClient
	DB              hooksRepo.DBTX
	TitleGenerator  TitleGenerator
}

type Sink[T any] struct {
	Writer          Writer
	ProductFeatures ProductFeaturesClient
	DB              hooksRepo.DBTX
	TitleGenerator  TitleGenerator
}

func New[T any](config Config) *Sink[T] {
	return &Sink[T]{
		Writer:          config.Writer,
		ProductFeatures: config.ProductFeatures,
		DB:              config.DB,
		TitleGenerator:  config.TitleGenerator,
	}
}

func Installer[T any](config Config) agentevents.SinkInstaller[T] {
	return agentevents.SinkInstallerFunc[T](func(agent *agentevents.Agent[T]) error {
		return agent.Use(New[T](config))
	})
}

func (s *Sink[T]) Write(ctx context.Context, ev agentevents.Event[T]) error {
	if s == nil || s.Writer == nil || s.ProductFeatures == nil {
		return nil
	}

	if ev.Context.ConversationID == "" {
		return nil
	}

	projectID, err := uuid.Parse(ev.Context.ProjectID)
	if err != nil {
		return fmt.Errorf("invalid project ID for agent conversation: %w", err)
	}

	enabled, err := s.ProductFeatures.IsFeatureEnabled(ctx, ev.Context.OrgID, productfeatures.FeatureSessionCapture)
	if err != nil {
		return fmt.Errorf("check session_capture feature flag: %w", err)
	}

	if !enabled {
		return nil
	}

	defaultTitle := activities.DefaultCursorChatTitle

	chatIDSource := ev.Context.ChatID
	if chatIDSource == "" {
		chatIDSource = ev.Context.ConversationID
	}
	chatID, err := uuid.Parse(chatIDSource)
	if err != nil {
		chatID = uuid.NewSHA1(uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8"), []byte(chatIDSource))
	}
	eventType, _, err := ev.EventType()
	if err != nil {
		return err
	}

	messages, err := eventsink.BuildChatMessages(ev, chatID)
	if err != nil {
		return err
	}

	for _, message := range messages {
		if err := s.writeMessage(ctx, ev, chatID, projectID, message, defaultTitle); err != nil {
			return err
		}
	}

	if eventType == types.AssistantResponseComplete && len(messages) > 0 && s.TitleGenerator != nil {
		if err := s.TitleGenerator.ScheduleChatTitleGeneration(
			context.WithoutCancel(ctx),
			chatID.String(),
			ev.Context.OrgID,
			ev.Context.ProjectID,
		); err != nil {
			return fmt.Errorf("schedule chat title generation for agent event: %w", err)
		}
	}
	return nil
}

func (s *Sink[T]) writeMessage(ctx context.Context, ev agentevents.Event[T], chatID uuid.UUID, projectID uuid.UUID, message chatRepo.CreateChatMessageParams, defaultTitle string) error {
	_, err := s.Writer.Write(ctx, projectID, []chatRepo.CreateChatMessageParams{message})
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgerrcode.ForeignKeyViolation {
		return fmt.Errorf("insert chat message: %w", err)
	}
	if s.DB == nil {
		return fmt.Errorf("upsert chat after FK violation: nil db")
	}

	repo := hooksRepo.New(s.DB)
	_, upsertErr := repo.UpsertClaudeCodeSession(ctx, hooksRepo.UpsertClaudeCodeSessionParams{
		ID:             chatID,
		ProjectID:      projectID,
		OrganizationID: ev.Context.OrgID,
		UserID:         conv.ToPGTextEmpty(ev.Context.UserID),
		ExternalUserID: conv.ToPGTextEmpty(ev.Context.UserEmail),
		Title:          conv.ToPGText(defaultTitle),
	})
	if upsertErr != nil {
		return fmt.Errorf("upsert claude code session after FK violation: %w", upsertErr)
	}

	if _, err = s.Writer.Write(ctx, projectID, []chatRepo.CreateChatMessageParams{message}); err != nil {
		return fmt.Errorf("insert chat message after creating chat: %w", err)
	}
	return nil
}
