package activities

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/slack/types"
)

type PostSlackMessage struct {
	slackClient *client.SlackClient
	logger      *slog.Logger
}

type PostSlackMessageInput struct {
	PostInput client.SlackPostMessageInput
	Event     types.SlackEvent
}

func NewPostSlackMessageActivity(logger *slog.Logger, client *client.SlackClient) *PostSlackMessage {
	return &PostSlackMessage{
		slackClient: client,
		logger:      logger,
	}
}

func (s *PostSlackMessage) Do(ctx context.Context, input PostSlackMessageInput) error {
	authInfo, err := s.slackClient.GetAppAuthInfo(ctx, input.Event.TeamID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error getting app auth info").Log(ctx, s.logger)
	}

	err = s.slackClient.PostMessage(ctx, authInfo.AccessToken, input.PostInput)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error posting slack message").Log(ctx, s.logger)
	}

	return nil
}
