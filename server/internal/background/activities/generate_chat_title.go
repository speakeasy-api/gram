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
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/chattitle"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type GenerateChatTitle struct {
	logger     *slog.Logger
	repo       *repo.Queries
	chatClient openrouter.CompletionClient

	// transcriptShadow opens encrypted shadow copies of message bodies. Nil
	// reads plaintext for every project.
	transcriptShadow *transcriptstore.Store
	// transcriptShadowReadProjects are the projects whose titles are generated
	// from encrypted shadows instead of plaintext.
	transcriptShadowReadProjects map[uuid.UUID]struct{}
}

func NewGenerateChatTitle(logger *slog.Logger, db *pgxpool.Pool, chatClient openrouter.CompletionClient) *GenerateChatTitle {
	return &GenerateChatTitle{
		logger:                       logger,
		repo:                         repo.New(db),
		chatClient:                   chatClient,
		transcriptShadow:             nil,
		transcriptShadowReadProjects: nil,
	}
}

// WithTranscriptShadowReads makes title generation for the listed projects read
// message bodies from encrypted shadows instead of plaintext, as chat reads do
// on the server. An empty list or a nil store leaves every project on
// plaintext.
func (g *GenerateChatTitle) WithTranscriptShadowReads(store *transcriptstore.Store, projectIDs ...uuid.UUID) *GenerateChatTitle {
	g.transcriptShadow = store
	g.transcriptShadowReadProjects = make(map[uuid.UUID]struct{}, len(projectIDs))
	for _, projectID := range projectIDs {
		g.transcriptShadowReadProjects[projectID] = struct{}{}
	}
	return g
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

	// Decide from the loaded rows before opening any shadow: every later turn
	// of a titled chat schedules this activity, and it must not open the
	// transcript only to leave the title alone. A current shadow opens to the
	// same body as its row, so the decision is the same either way.
	if !titleIsUpForGrabs(stored, messages) {
		return nil
	}
	turns := titleContextTurns(messages)
	if len(formatTitleContext(turns)) < 20 {
		return nil // Not enough context yet — will retry on next completion.
	}

	// Only the turns sent to the model are opened.
	if err := g.openTranscriptShadows(ctx, chatRow.OrganizationID, chatRow.ProjectID, turns); err != nil {
		return err
	}
	contextStr := formatTitleContext(turns)

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

// openTranscriptShadows replaces message bodies with their encrypted shadows
// when the project reads through shadows, and leaves them untouched for every
// other project. A message with no shadow keeps its plaintext body, so chats
// written before capture began still get titles. A stale or unopenable shadow
// fails the activity: plaintext must not paper over broken capture or data.
// A stale shadow stays stale, so that failure is not retried. A shadow that
// does not decrypt fails with the same opaque error as a key outage, so it is
// retried like any other activity error until the workflow's attempts run out.
func (g *GenerateChatTitle) openTranscriptShadows(ctx context.Context, organizationID string, projectID uuid.UUID, messages []repo.ChatMessage) error {
	if g.transcriptShadow == nil || len(messages) == 0 {
		return nil
	}
	if _, enabled := g.transcriptShadowReadProjects[projectID]; !enabled {
		return nil
	}

	ids := make([]uuid.UUID, len(messages))
	for i, msg := range messages {
		ids[i] = msg.ID
	}
	opened, err := g.transcriptShadow.LoadMany(ctx, organizationID, projectID, ids)
	switch {
	case errors.Is(err, transcriptstore.ErrStale), errors.Is(err, transcriptstore.ErrInvalid):
		return temporal.NewNonRetryableApplicationError("open encrypted chat transcript", "chat_transcript_shadow_unreadable", err)
	case err != nil:
		return fmt.Errorf("open encrypted chat transcript: %w", err)
	}
	for i := range messages {
		message, ok := opened[messages[i].ID]
		if !ok {
			continue
		}
		messages[i].Content = message.Content
		messages[i].ContentRaw = message.ContentRaw
		messages[i].ToolCalls = message.ToolCalls
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
	// A cron chat captured before cron titles stored its envelope, which
	// reads under its job name; a generated title would lose that name.
	if _, ok := chattitle.StoredCronTitle(title); ok {
		return false
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
	return formatTitleContext(titleContextTurns(messages))
}

// titleContextTurns picks the last titleContextMessages user and assistant
// messages with content, in chronological order.
func titleContextTurns(messages []repo.ChatMessage) []repo.ChatMessage {
	turns := make([]repo.ChatMessage, 0, titleContextMessages)
	for _, msg := range slices.Backward(messages) { // Start from the last message and work backwards to make sure we capture the most recent messages
		if (msg.Role != "user" && msg.Role != "assistant") || chat.StripLeadingEnvelopes(msg.Content) == "" {
			continue
		}
		turns = append(turns, msg)
		if len(turns) >= titleContextMessages {
			break
		}
	}
	slices.Reverse(turns) // Restore chronological order for the model.
	return turns
}

// formatTitleContext renders turns as "role: content" lines, cutting each
// turn to maxTitleMessageRunes.
func formatTitleContext(turns []repo.ChatMessage) string {
	lines := make([]string, 0, len(turns))
	for _, msg := range turns {
		content := chat.StripLeadingEnvelopes(msg.Content)
		if content == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s", msg.Role, truncateRunes(content, maxTitleMessageRunes)))
	}
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
	if g.chatClient == nil {
		return defaultChatTitle
	}

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
