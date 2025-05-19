package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/speakeasy-api/gram/internal/oops"
	"github.com/speakeasy-api/gram/internal/thirdparty/slack/client"

	"github.com/speakeasy-api/gram/internal/chat"
	"github.com/speakeasy-api/gram/internal/thirdparty/slack/types"
)

type SlackChatCompletion struct {
	slackClient *client.SlackClient
	logger      *slog.Logger
	chatClient  *chat.ChatClient
}

type SlackChatCompletionInput struct {
	Event       types.SlackEvent
	Prompt      string
	ToolsetSlug string
}

func NewSlackChatCompletionActivity(logger *slog.Logger, client *client.SlackClient, chatClient *chat.ChatClient) *SlackChatCompletion {
	return &SlackChatCompletion{
		slackClient: client,
		logger:      logger,
		chatClient:  chatClient,
	}
}

func (s *SlackChatCompletion) Do(ctx context.Context, input SlackChatCompletionInput) (string, error) {
	authInfo, err := s.slackClient.GetAppAuthInfo(ctx, input.Event.TeamID)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "error getting app auth info").Log(ctx, s.logger)
	}

	systemPrompt := `
	You are a helpful assistant named "gram". Your responses will be later be posted to Slack, so format your final output nicely using Slack's rich text formatting rules. Remember this is not the same as Markdown formatting. Some Reminders on how you should format your output:
	- Use *asterisks* for bold text, _underscores_ for italic, and ~tildes~ for strikethrough.
	- Use backticks for inline code.
	- Use bullet points (- or •) for lists.
	- Do NOT use Markdown syntax like **bold**, __underline__, or HTML tags.
	`

	previousConversationContext := ""
	if input.Event.Event.ThreadTs != "" {
		if replies, err := s.slackClient.GetConversationReplies(ctx, authInfo.AccessToken, client.SlackConversationInput{
			ChannelID: input.Event.Event.Channel,
			ThreadTS:  input.Event.Event.EventTs,
			Limit:     nil,
		}); err != nil {
			s.logger.ErrorContext(ctx, "error getting conversation replies", slog.String("error", err.Error()))
		} else {
			for _, reply := range replies.Messages {
				previousConversationContext += fmt.Sprintf("%s: %s\n\n", reply.User, reply.Text)
			}
		}
	}

	if previousConversationContext != "" {
		systemPrompt += "\n\nHere is the previous conversation context for reference:\n" + previousConversationContext
	}

	currentDatetimeParams := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
	currentDatetimeParamsJSON, err := json.Marshal(currentDatetimeParams)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to marshal currentDatetimeParams", slog.String("error", err.Error()))
		return "sorry, I cannot process that message right now", nil
	}

	currentDatetimeTool := chat.AgentTool{
		Definition: chat.Tool{
			Type: "function",
			Function: &chat.FunctionDefinition{
				Name:        "get_current_datetime",
				Description: "Returns the current date and time in ISO 8601 format.",
				Parameters:  json.RawMessage(currentDatetimeParamsJSON),
			},
		},
		Executor: func(ctx context.Context, rawArgs string) (string, error) {
			return time.Now().Format(time.RFC3339), nil
		},
	}

	chatResponse, err := s.chatClient.AgentChat(ctx, authInfo.OrganizationID, authInfo.ProjectID, input.Prompt, chat.AgentChatOptions{
		SystemPrompt:    &systemPrompt,
		ToolsetSlug:     &input.ToolsetSlug,
		AdditionalTools: []chat.AgentTool{currentDatetimeTool},
		AddedEnvironmentEntries: map[string]string{
			"SLACK_SLACK_BOT_TOKEN": authInfo.AccessToken,
		},
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "error getting chat response", slog.String("error", err.Error()))
		return "", err
	}

	return chatResponse, nil
}
