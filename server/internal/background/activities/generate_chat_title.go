package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type GenerateChatTitle struct {
	logger     *slog.Logger
	repo       *repo.Queries
	chatClient openrouter.CompletionClient
}

func NewGenerateChatTitle(logger *slog.Logger, db *pgxpool.Pool, chatClient openrouter.CompletionClient) *GenerateChatTitle {
	return &GenerateChatTitle{
		logger:     logger,
		repo:       repo.New(db),
		chatClient: chatClient,
	}
}

type GenerateChatTitleArgs struct {
	ChatID    string
	OrgID     string
	ProjectID string
}

const (
	defaultChatTitle = chat.DefaultChatTitle

	// titleModel is a small, fast model: a 3-6 word title needs no frontier
	// reasoning, and the default chat model runs titleCompletionTimeout out.
	titleModel = "google/gemini-3.1-flash-lite"
	// titleCompletionTimeout stays under the activity StartToCloseTimeout so a
	// slow completion is a skipped title, not an activity timeout plus retries.
	titleCompletionTimeout = 15 * time.Second

	// titleContextMessages and maxTitleMessageRunes bound the prompt: agent
	// transcripts carry multi-kilobyte turns, and only their opening lines say
	// what the turn is about.
	titleContextMessages = 6
	maxTitleMessageRunes = 600
	// maxGeneratedTitleRunes matches the limit on a manual rename.
	maxGeneratedTitleRunes = 200
)

func (g *GenerateChatTitle) Do(ctx context.Context, args GenerateChatTitleArgs) error {
	chatID, err := uuid.Parse(args.ChatID)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}

	projectID, err := uuid.Parse(args.ProjectID)
	if err != nil {
		return fmt.Errorf("invalid project ID: %w", err)
	}

	chatRow, err := g.repo.GetChat(ctx, repo.GetChatParams{ID: chatID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // chat was deleted or is not in this project, nothing to do
	}
	if err != nil {
		return fmt.Errorf("get chat: %w", err)
	}

	// A human renamed this chat — never overwrite a manually chosen title. This
	// is a fast-path check on the snapshot we just read; a rename that lands
	// *during* generation (below) is caught by the title_manually_set guard in
	// the final UpdateChatTitle write, which is the authoritative protection.
	if chatRow.TitleManuallySet {
		return nil
	}
	// A stand-in is at most MaxDerivedTitleRunes, so a longer title is already
	// a real name and the transcript need not be loaded.
	stored := chatRow.Title.String
	if !chat.IsPlaceholderTitle(stored) && utf8.RuneCountInString(stored) > chat.MaxDerivedTitleRunes {
		return nil
	}

	messages, err := g.repo.ListLatestGenerationChatMessages(ctx, repo.ListLatestGenerationChatMessagesParams{
		ChatID:    chatID,
		ProjectID: chatRow.ProjectID,
	})
	if err != nil {
		return fmt.Errorf("list chat messages: %w", err)
	}

	if !titleIsUpForGrabs(stored, messages) {
		return nil
	}

	contextStr := buildTitleContext(messages)
	if len(contextStr) < 20 {
		return nil // Not enough context yet — will retry on next completion.
	}

	title := g.generateTitle(ctx, args.OrgID, chatRow.ProjectID.String(), contextStr)
	if title == defaultChatTitle {
		return nil
	}

	// UpdateChatTitle only writes when title_manually_set is still false, so a
	// manual rename that raced this generation wins and is left untouched.
	err = g.repo.UpdateChatTitle(ctx, repo.UpdateChatTitleParams{
		ID:        chatID,
		ProjectID: projectID,
		Title:     conv.ToPGText(title),
	})
	if err != nil {
		return fmt.Errorf("update chat title: %w", err)
	}

	return nil
}

// titleIsUpForGrabs reports whether generation may name this chat: fixed
// placeholders and stand-ins derived from the chat's own messages are fair
// game; any other title was chosen by a source and stays.
func titleIsUpForGrabs(title string, messages []repo.ChatMessage) bool {
	if chat.IsPlaceholderTitle(title) {
		return true
	}
	contents := make([]string, 0, len(messages))
	for _, msg := range messages {
		contents = append(contents, msg.Content)
	}
	return chat.IsDerivedTitle(title, contents...)
}

// buildTitleContext concatenates the last few user/assistant messages into a
// single string suitable for LLM title generation, cutting each turn to
// maxTitleMessageRunes.
func buildTitleContext(messages []repo.ChatMessage) string {
	lines := make([]string, 0, titleContextMessages)
	for _, msg := range slices.Backward(messages) { // Start from the last message and work backwards to make sure we capture the most recent messages

		content := chat.StripLeadingEnvelopes(msg.Content)
		if (msg.Role != "user" && msg.Role != "assistant") || content == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s", msg.Role, truncateRunes(content, maxTitleMessageRunes)))
		if len(lines) >= titleContextMessages {
			break
		}
	}
	slices.Reverse(lines) // Restore chronological order for the model.
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// truncateRunes cuts s to maxRunes including the ellipsis that marks the cut.
func truncateRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	return conv.TruncateString(s, maxRunes-1) + "…"
}

func (g *GenerateChatTitle) generateTitle(ctx context.Context, orgID, projectID string, conversationContext string) string {
	titleCtx, cancel := context.WithTimeout(ctx, titleCompletionTimeout)
	defer cancel()

	systemPrompt := "Generate a concise title (3-6 words) for this conversation based on the messages below. " +
		"Return ONLY the title text, no quotes or explanation. " +
		"IMPORTANT: The title must directly relate to the content of the messages. " +
		"Do NOT expand, interpret, or replace abbreviations or acronyms — use the user's exact terminology. " +
		"If the conversation is a greeting, vague, or lacks a clear topic, return exactly: " + defaultChatTitle

	response, err := g.chatClient.GetCompletion(titleCtx, openrouter.CompletionRequest{
		MaxTokens: nil,
		OrgID:     orgID,
		ProjectID: projectID,
		ChatID:    uuid.Nil,
		Messages: []or.ChatMessages{
			openrouter.CreateMessageSystem(systemPrompt),
			openrouter.CreateMessageUser(conversationContext),
		},
		Tools:                     nil,
		ToolChoice:                nil,
		Temperature:               nil,
		Model:                     titleModel,
		Stream:                    false,
		UsageSource:               billing.ModelUsageSourceGram,
		KeyType:                   openrouter.KeyTypeInternal,
		KeySlot:                   "",
		UserID:                    "",
		ExternalUserID:            "",
		UserEmail:                 "",
		HTTPMetadata:              nil,
		APIKeyID:                  "",
		JSONSchema:                nil,
		Reasoning:                 &openrouter.Reasoning{Effort: "none", MaxTokens: nil, Exclude: nil, Enabled: nil},
		CacheControl:              nil,
		NormalizeOutboundMessages: false,
		WebSearch:                 nil,
		DisableResponseHealing:    false,
	})
	if err != nil {
		g.logger.WarnContext(ctx, "failed to generate chat title via OpenRouter", attr.SlogError(err))
		return defaultChatTitle
	}
	if response == nil || response.Message == nil {
		g.logger.WarnContext(ctx, "chat title completion returned no message")
		return defaultChatTitle
	}

	// Small models like to wrap a bare title in quotes despite the
	// instruction, and an ignored instruction can also return a sentence.
	title := strings.Trim(strings.TrimSpace(openrouter.GetText(*response.Message)), `"'`)
	if title == "" {
		return defaultChatTitle
	}

	return truncateRunes(title, maxGeneratedTitleRunes)
}
