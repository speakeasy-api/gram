package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/speakeasy-api/gram/internal/oops"
	"github.com/speakeasy-api/gram/internal/thirdparty/slack/client"

	"github.com/speakeasy-api/gram/internal/chat"
	"github.com/speakeasy-api/gram/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/internal/thirdparty/slack/types"
)

const slackSystemPrompt = `
You are a helpful assistant named "gram". Your responses will be later be posted to Slack, so format your final output nicely using Slack's rich text formatting rules. Remember this is not the same as Markdown syntax, YOU SHOULD NOT USE MARKDOWN SYNTAX. Some Reminders on how you should format your output:
- Use *asterisks* for bold text, _underscores_ for italic, and ~tildes~ for strikethrough.
- Use backticks for inline code.
- Use bullet points (- or •) for lists.
- Do NOT use Markdown syntax like **bold**, __underline__, or HTML tags.
`

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
	slackChatCompletionTimeout := 60 * time.Second
	authInfo, err := s.slackClient.GetAppAuthInfo(ctx, input.Event.TeamID)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "error getting app auth info").Log(ctx, s.logger)
	}

	systemPrompt := slackSystemPrompt

	previousConversationContext := ""
	if input.Event.Event.ThreadTs != "" {
		if replies, err := s.slackClient.GetConversationReplies(ctx, authInfo.AccessToken, client.SlackConversationInput{
			ChannelID: input.Event.Event.Channel,
			ThreadTS:  input.Event.Event.ThreadTs,
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
		Definition: openrouter.Tool{
			Type: "function",
			Function: &openrouter.FunctionDefinition{
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
			"SLACK_TOKEN": authInfo.AccessToken,
		},
		AgentTimeout: &slackChatCompletionTimeout,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "error getting chat response", slog.String("error", err.Error()))
		return "", fmt.Errorf("error getting chat response: %w", err)
	}

	// Check if chatResponse contains non-Slack formatting and ask the LLM to fix it
	if isNonSlackFormatting(chatResponse) {
		s.logger.InfoContext(ctx, "chat response contains non-Slack formatting (e.g., Markdown or HTML) trying again")
		retrySystemPrompt := slackSystemPrompt
		retryPrompt := fmt.Sprintf(
			"Please reformat the following message you previously provided using only Slack's rich text formatting rules (not Markdown or HTML):\n\n%s",
			chatResponse,
		)
		chatResponse, err = s.chatClient.AgentChat(ctx, authInfo.OrganizationID, authInfo.ProjectID, retryPrompt, chat.AgentChatOptions{
			SystemPrompt:            &retrySystemPrompt,
			ToolsetSlug:             nil,
			AgentTimeout:            &slackChatCompletionTimeout,
			AdditionalTools:         []chat.AgentTool{},
			AddedEnvironmentEntries: map[string]string{},
		})
		if err != nil {
			s.logger.ErrorContext(ctx, "error getting chat response", slog.String("error", err.Error()))
			return "", fmt.Errorf("error getting formatted chat response: %w", err)
		}
	}

	return chatResponse, nil
}

// isNonSlackFormatting checks if the input string contains formatting not compatible with Slack rich text (e.g., Markdown or HTML).
func isNonSlackFormatting(s string) bool {
	patterns := []string{
		`\*\*.+?\*\*`, // **bold** (Markdown)
		`__.+?__`,     // __bold__ (Markdown)
		`^# .+`,       // # heading (Markdown)
		"```.+?```",   // ```code block``` (Markdown)
		`<[^>]+>`,     // HTML tags
	}
	for _, pattern := range patterns {
		matched, err := regexp.MatchString(pattern, s)
		if err == nil && matched {
			return true
		}
	}
	return false
}
