package activities

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
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
	gramAppID, err := uuid.Parse(input.Event.GramAppID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid gram app ID on event").Log(ctx, s.logger)
	}
	authInfo, err := s.slackClient.GetAppAuthInfoByID(ctx, gramAppID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error getting app auth info").Log(ctx, s.logger)
	}

	err = s.slackClient.PostMessage(ctx, authInfo.AccessToken, input.PostInput)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error posting slack message").Log(ctx, s.logger)
	}

	return nil
}
