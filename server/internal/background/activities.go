package background

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/internal/assets"
	"github.com/speakeasy-api/gram/internal/background/activities"
	"github.com/speakeasy-api/gram/internal/chat"
	slack_client "github.com/speakeasy-api/gram/internal/thirdparty/slack/client"
	"github.com/speakeasy-api/gram/internal/thirdparty/slack/types"
)

type Activities struct {
	processDeployment      *activities.ProcessDeployment
	transitionDeployment   *activities.TransitionDeployment
	getSlackProjectContext *activities.GetSlackProjectContext
	postSlackMessage       *activities.PostSlackMessage
	slackChatCompletion    *activities.SlackChatCompletion
}

func NewActivities(logger *slog.Logger, db *pgxpool.Pool, assetStorage assets.BlobStore, slackClient *slack_client.SlackClient, chatClient *chat.ChatClient) *Activities {
	return &Activities{
		processDeployment:      activities.NewProcessDeployment(logger, db, assetStorage),
		transitionDeployment:   activities.NewTransitionDeployment(logger, db),
		getSlackProjectContext: activities.NewSlackProjectContextActivity(logger, db, slackClient),
		postSlackMessage:       activities.NewPostSlackMessageActivity(logger, slackClient),
		slackChatCompletion:    activities.NewSlackChatCompletionActivity(logger, slackClient, chatClient),
	}
}

func (a *Activities) TransitionDeployment(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID, status string) (*activities.TransitionDeploymentResult, error) {
	return a.transitionDeployment.Do(ctx, projectID, deploymentID, status)
}

func (a *Activities) ProcessDeployment(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID) error {
	return a.processDeployment.Do(ctx, projectID, deploymentID)
}

func (a *Activities) GetSlackProjectContext(ctx context.Context, event types.SlackEvent) (*activities.SlackProjectContextResponse, error) {
	return a.getSlackProjectContext.Do(ctx, event)
}

func (a *Activities) PostSlackMessage(ctx context.Context, input activities.PostSlackMessageInput) error {
	return a.postSlackMessage.Do(ctx, input)
}

func (a *Activities) SlackChatCompletion(ctx context.Context, input activities.SlackChatCompletionInput) (string, error) {
	return a.slackChatCompletion.Do(ctx, input)
}
