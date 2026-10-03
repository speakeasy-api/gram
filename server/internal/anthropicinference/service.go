package anthropicinference

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/policyflags"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const (
	// verdictBudget caps risk evaluation at nine seconds. Earlier request work
	// can shorten it to preserve checkpoint and response headroom.
	verdictBudget = 9 * time.Second
	// scanConcurrency bounds how many inputs of one transcript are evaluated
	// at once.
	scanConcurrency = 4
	// checkpointBudget bounds the marker write that follows scanning.
	checkpointBudget = 500 * time.Millisecond
	// responseBudget reserves time to serialize the verdict after checkpointing.
	responseBudget = 250 * time.Millisecond
	// requestBudget leaves 250ms before the documented 10-second upstream timeout.
	requestBudget = verdictBudget + checkpointBudget + responseBudget
	// titleScheduleTimeout bounds the goroutine that asks for a title, so a
	// degraded Temporal frontend cannot pin one goroutine per inference request.
	titleScheduleTimeout = 2 * time.Second
	// unavailableDenyReason is the fail-closed copy for a request that could
	// not be evaluated in full.
	unavailableDenyReason = "Speakeasy could not evaluate this request. Please try again."
)

var errPolicyDenied = errors.New("inference policy denied")

// inputScan is what one policy input's evaluation produced. An input the
// budget or an earlier denial cut off stays unevaluated.
type inputScan struct {
	evaluated bool
	complete  bool
	result    *risk.ScanResult
}

// The protocol has no acknowledgement flow; warn policies deny the call.
func denies(result *risk.ScanResult) bool {
	return result != nil && (result.Action == "block" || result.Action == "warn" || result.Action == "quarantine")
}

type scanner interface {
	ScanForInferenceEnforcement(context.Context, risk.RealtimeScanRequest) (*risk.InferenceScanOutcome, error)
}

// ChatTitleGenerator schedules async chat title generation.
type ChatTitleGenerator interface {
	ScheduleChatTitleGeneration(ctx context.Context, chatID, orgID, projectID string) error
}

type transcriptStore interface {
	// ResolveConversation maps the frame onto the stored chat that continues
	// it and reports how (a conversationOutcome* constant). Process calls it
	// once per request and carries the result on the frame for the other
	// methods.
	ResolveConversation(context.Context, Config, Frame) (uuid.UUID, string, error)
	ResolveActor(context.Context, Config, Frame) (string, error)
	Begin(context.Context, Config, Frame, string) (checkpointSession, error)
	// Save archives the frame's conversation messages that are not yet stored
	// and returns the index, within conversationMessages(frame.Messages), of
	// the first message it had not seen before.
	Save(context.Context, Config, Frame, string) (int, error)
}

// Service archives transcripts and runs the existing risk-policy scanner.
type Service struct {
	logger  *slog.Logger
	store   transcriptStore
	scanner scanner
	metrics *metrics
}

// NewService uses the shared chat writer so captured messages receive the same
// storage, metering, and asynchronous analysis as other imported conversations.
func NewService(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, writer *chat.ChatMessageWriter, scanner scanner, titles ChatTitleGenerator) *Service {
	return &Service{logger: logger, store: &postgresStore{db: db, writer: writer, titles: titles, logger: logger}, scanner: scanner, metrics: newMetrics(meterProvider, logger)}
}

// Process archives attempts independently of enforcement. Only a successfully
// evaluated checkpoint can exempt historical content from another scan.
func (s *Service) Process(ctx context.Context, config Config, frame Frame) (Verdict, error) {
	ctx = policyflags.WithRequestMemo(ctx)
	duration := verdictBudget
	if deadline, ok := ctx.Deadline(); ok {
		duration = min(duration, time.Until(deadline)-checkpointBudget-responseBudget)
	}
	budget, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	messages := conversationMessages(frame.Messages)
	if _, err := policyInputs(messages); err != nil {
		return Verdict{}, fmt.Errorf("decode inference transcript: %w", err)
	}
	conversation, outcome, err := s.store.ResolveConversation(budget, config, frame)
	if err != nil {
		return Verdict{}, fmt.Errorf("resolve inference conversation: %w", err)
	}
	frame.conversation, frame.conversationOutcome = conversation, outcome
	// A frame with no conversation messages (Anthropic's connection probe)
	// stores nothing, so it is not a conversation to count.
	if len(messages) > 0 {
		s.metrics.RecordConversation(ctx, frame, outcome)
		s.logger.DebugContext(ctx, "resolved inference conversation",
			attr.SlogOrganizationID(config.OrganizationID), attr.SlogProjectID(config.ProjectID.String()),
			attr.SlogChatID(conversation.String()), attr.SlogInferenceConversationOutcome(outcome),
			attr.SlogInferenceApplication(inferenceSource(frame.Source.Application)),
			attr.SlogInferenceHasSessionID(frame.SessionID != ""),
			attr.SlogInferenceMessageCount(len(messages)))
	}
	userID, err := s.store.ResolveActor(budget, config, frame)
	if err != nil {
		return Verdict{}, fmt.Errorf("resolve inference hook actor: %w", err)
	}
	_, err = s.store.Save(budget, config, frame, userID)
	if err != nil {
		return Verdict{}, fmt.Errorf("store inference transcript: %w", err)
	}
	session, err := s.store.Begin(budget, config, frame, userID)
	if err != nil {
		return Verdict{}, fmt.Errorf("begin inference checkpoint: %w", err)
	}
	accepted, err := session.Load(budget)
	if err != nil {
		return Verdict{}, fmt.Errorf("load inference checkpoint: %w", err)
	}
	hashes := transcriptHashes(messages)
	matched := acceptedPrefix(accepted, hashes)
	scanStart := min(matched, currentTurnStart(messages))
	inputs, err := policyInputs(messages[scanStart:])
	if err != nil {
		return Verdict{}, fmt.Errorf("decode inference transcript: %w", err)
	}
	priorInputs, err := policyInputs(messages[:scanStart])
	if err != nil {
		return Verdict{}, fmt.Errorf("decode inference transcript: %w", err)
	}
	scans := make([]inputScan, len(inputs))
	group, groupCtx := errgroup.WithContext(budget)
	group.SetLimit(scanConcurrency)
	for offset, input := range inputs {
		if groupCtx.Err() != nil {
			break
		}
		group.Go(func() error {
			// group.Go can block on a free slot past the budget, so the check
			// above may be stale by the time this runs.
			if groupCtx.Err() != nil {
				return nil
			}
			outcome, err := s.scanner.ScanForInferenceEnforcement(groupCtx, scanRequest(config, frame, userID, len(priorInputs)+offset, input))
			if err != nil {
				if groupCtx.Err() != nil {
					return nil
				}
				return fmt.Errorf("scan input %d: %w", len(priorInputs)+offset, err)
			}
			scans[offset] = inputScan{evaluated: true, complete: outcome != nil && outcome.Complete, result: nil}
			if outcome != nil {
				scans[offset].result = outcome.Result
			}
			if denies(scans[offset].result) {
				return errPolicyDenied
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil && !errors.Is(err, errPolicyDenied) {
		return Verdict{}, fmt.Errorf("evaluate inference policy: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Verdict{}, fmt.Errorf("inference policy deadline: %w", err)
	}
	for _, scan := range scans {
		if denies(scan.result) {
			return Verdict{
				Action:      "deny",
				DenyReason:  conv.Default(conv.PtrValOr(scan.result.UserMessage, ""), "This request was blocked by your organization's security policy."),
				ReferenceID: "",
			}, nil
		}
	}
	verdict := Verdict{Action: "allow", DenyReason: "", ReferenceID: ""}
	// Only the leading messages whose inputs all evaluated complete can be
	// accepted. Accepting them after a cut-off makes the redelivery
	// incremental instead of repeating the same scans into the same budget.
	cleanThrough := len(messages)
	for offset, scan := range scans {
		if !scan.evaluated || !scan.complete {
			cleanThrough = scanStart + inputs[offset].message
			break
		}
	}
	if slices.ContainsFunc(scans, func(scan inputScan) bool { return !scan.evaluated }) {
		// A failed evaluation must not silently allow inference, including when
		// Anthropic's administrator selected allow-on-webhook-failure.
		s.logger.WarnContext(ctx, "inference verdict budget exhausted",
			attr.SlogOrganizationID(config.OrganizationID), attr.SlogProjectID(config.ProjectID.String()),
			attr.SlogInferenceInputCount(len(inputs)), attr.SlogInferenceAcceptedMessages(cleanThrough))
		verdict = Verdict{Action: "deny", DenyReason: unavailableDenyReason, ReferenceID: ""}
	}
	if cleanThrough > matched || cleanThrough == len(messages) {
		// The scan budget may already be spent; the marker gets its own short
		// window so a slow write cannot push the verdict past the provider's.
		acceptCtx, cancelAccept := context.WithTimeout(ctx, checkpointBudget)
		defer cancelAccept()
		err := session.Accept(acceptCtx, hashes[:cleanThrough])
		switch {
		case err == nil:
		case ctx.Err() != nil:
			return Verdict{}, fmt.Errorf("accept inference checkpoint context: %w", ctx.Err())
		case errors.Is(err, errCheckpointConflict):
			// Another fully scanned delivery won. Keep its marker, without
			// turning this optimization into an artificial denial.
		case verdict.Action == "deny":
			// The verdict already fails closed; the marker only makes the
			// redelivery incremental.
			s.logger.WarnContext(ctx, "accept partial inference checkpoint", attr.SlogError(err))
		default:
			return Verdict{}, fmt.Errorf("accept inference checkpoint: %w", err)
		}
	}
	return verdict, nil
}

func scanRequest(config Config, frame Frame, userID string, index int, input policyInput) risk.RealtimeScanRequest {
	return risk.RealtimeScanRequest{
		Provenance: metering.RiskProvenance{
			OrganizationID:         config.OrganizationID,
			ProjectID:              config.ProjectID,
			RiskPolicyID:           uuid.Nil,
			RiskPolicyVersion:      0,
			PolicyLinkReason:       "",
			ChatID:                 uuid.Nil,
			ExternalConversationID: frame.SessionID,
			ChatMessageID:          uuid.Nil,
			ContentPartID:          uuid.Nil,
			MessageLinkReason:      "realtime_message_not_resolved",
			OperationID:            fmt.Sprintf("anthropic-inference:%s:%d", frame.RequestID, index),
			ExecutionPath:          "realtime_local",
			RequestID:              frame.RequestID,
			MessageType:            input.kind,
			HookSource:             inferenceSource(frame.Source.Application),
			UserID:                 userID,
			ToolCallID:             input.toolCallID,
			ToolName:               input.tool,
			Model:                  "",
			Provider:               "",
		},
		Text:        input.text,
		MessageType: input.kind,
		ToolName:    input.tool,
		ToolCallID:  input.toolCallID,
	}
}

type policyInput struct {
	kind       message.Type
	tool       string
	text       string
	toolCallID string
	// message is the index, within the messages given to policyInputs, of the
	// message this input came from.
	message int
}

// Preserve each block as an independent policy input so tool arguments stay
// valid JSON and content-specific policy scope expressions retain their meaning.
func policyInputs(messages []Message) ([]policyInput, error) {
	var inputs []policyInput
	for index, msg := range messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		blocks, err := knownBlocks(msg.Content)
		if err != nil {
			return nil, err
		}
		for _, block := range blocks {
			input := policyInput{kind: "", tool: "", text: "", toolCallID: "", message: index}
			switch block.Type {
			case "text":
				input.kind, input.text = message.User, block.Text
				if msg.Role == "assistant" {
					input.kind = message.Assistant
				}
			case "attachment":
				input.kind, input.text = message.PromptAttachment, block.Text
			case "tool_use":
				// Inference hooks use tool_name; the standard Messages API uses name.
				// Accept either so both documented protocol shapes are handled.
				input.kind, input.tool, input.text = message.ToolRequest, conv.Default(block.ToolName, block.Name), string(block.Input)
				input.toolCallID = block.ID
			case "tool_result":
				input.kind, input.tool, input.text = message.ToolResponse, block.ToolName, block.Content
				input.toolCallID = block.ToolUseID
			}
			if input.text == "" && input.tool == "" {
				continue
			}
			inputs = append(inputs, input)
		}
	}
	return inputs, nil
}

type postgresStore struct {
	db     *pgxpool.Pool
	writer *chat.ChatMessageWriter
	titles ChatTitleGenerator
	logger *slog.Logger
}

func (s *postgresStore) ResolveActor(ctx context.Context, config Config, frame Frame) (string, error) {
	// Resolve the configured project on every request, including connection tests,
	// so deleted projects and accidentally crossed organization bindings fail closed.
	_, err := projectsrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{
		ID:             config.ProjectID,
		OrganizationID: config.OrganizationID,
	})
	if err != nil {
		return "", fmt.Errorf("validate inference project: %w", err)
	}
	actor := frame.Actor
	if actor.Type == "user" && actor.EmailAddress != "" {
		users, err := usersrepo.New(s.db).GetConnectedUsersByEmails(ctx, usersrepo.GetConnectedUsersByEmailsParams{Emails: []string{conv.NormalizeEmail(actor.EmailAddress)}, OrganizationID: config.OrganizationID})
		if err != nil {
			return "", fmt.Errorf("resolve inference user: %w", err)
		}
		if len(users) == 1 {
			return users[0].ID, nil
		}
	}
	// Conversation identity includes the provider actor, so this fallback cannot
	// borrow the owner of another actor's conversation.
	chatID, err := s.conversation(ctx, config, frame)
	if err != nil {
		return "", err
	}
	conversation, err := chatrepo.New(s.db).GetChat(ctx, chatrepo.GetChatParams{ID: chatID, ProjectID: config.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("resolve last known inference user: %w", err)
	}
	return conversation.UserID.String, nil
}

func (s *postgresStore) Save(ctx context.Context, config Config, frame Frame, userID string) (int, error) {
	if len(frame.Messages) == 0 {
		return 0, nil
	}
	// The external user label is displayed in conversation views. Keep the
	// stable provider actor ID in conversationID, independently of this label.
	// Use the email when present so the conversation header matches its messages,
	// but pass null to the upsert when absent — letting COALESCE preserve a label
	// that was set by an earlier frame rather than overwriting it with the actor ID.
	externalUserIDLabel := conv.NormalizeEmail(frame.Actor.EmailAddress)
	now := time.Now().UTC()
	// Namespacing isolates these opaque, sometimes client-asserted session ids
	// from native hooks and Compliance imports. A frame without a usable
	// session is keyed by the chat already holding its transcript prefix when
	// there is one (see ResolveConversation), else by its request.
	candidateID, err := s.conversation(ctx, config, frame)
	if err != nil {
		return 0, err
	}
	externalChatID := "anthropic-inference:" + candidateID.String()
	conversation, err := chatrepo.New(s.db).UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
		ID:                candidateID,
		ProjectID:         config.ProjectID,
		OrganizationID:    config.OrganizationID,
		UserID:            conv.ToPGTextEmpty(userID),
		ExternalUserID:    conv.ToPGTextEmpty(externalUserIDLabel),
		ExternalChatID:    conv.ToPGText(externalChatID),
		Title:             conv.ToPGText(chat.DefaultInferenceChatTitle),
		CreatedAt:         conv.ToPGTimestamptz(now),
		UpdatedAt:         conv.ToPGTimestamptz(now),
		PreferStoredTitle: true,
	})
	if err != nil {
		return 0, fmt.Errorf("upsert inference conversation: %w", err)
	}
	chatID := conversation.ID
	// Only a chat this frame created can lack the key: an adopted chat was
	// matched on it and a chat continued by session was keyed when created.
	// The update keeps an existing key, so a redelivery is harmless.
	// Anonymous frames have no key to record.
	if key := actorKey(config, frame); key != nil && frame.conversationOutcome != conversationOutcomeAdoptedPrefix && frame.conversationOutcome != conversationOutcomeSession {
		if err := chatrepo.New(s.db).SetInferenceActorKey(ctx, chatrepo.SetInferenceActorKeyParams{
			InferenceActorKey: key, ID: chatID, ProjectID: config.ProjectID,
		}); err != nil {
			return 0, fmt.Errorf("record inference actor key: %w", err)
		}
	}
	// When this frame omits the actor email, use the label preserved on the
	// conversation (written by an earlier frame that had one) so new messages
	// stay consistent with the conversation header and existing messages.
	// Fall back to Actor.ID only when no label has been established yet.
	externalUserIDForMessages := externalUserIDLabel
	if externalUserIDForMessages == "" {
		chat, err := chatrepo.New(s.db).GetChat(ctx, chatrepo.GetChatParams{ID: chatID, ProjectID: config.ProjectID})
		if err != nil {
			return 0, fmt.Errorf("load inference conversation label: %w", err)
		}
		if chat.ExternalUserID.Valid {
			externalUserIDForMessages = chat.ExternalUserID.String
		} else {
			externalUserIDForMessages = frame.Actor.ID
		}
	}
	if err := chatrepo.New(s.db).UpdateInferenceMessageAttribution(ctx, chatrepo.UpdateInferenceMessageAttributionParams{
		ChatID: chatID, ProjectID: uuid.NullUUID{UUID: config.ProjectID, Valid: true}, ActorEmail: conv.ToPGTextEmpty(conv.NormalizeEmail(frame.Actor.EmailAddress)), UserID: conv.ToPGTextEmpty(userID), Source: inferenceSource(frame.Source.Application),
	}); err != nil {
		return 0, fmt.Errorf("refresh inference message attribution: %w", err)
	}

	messages := conversationMessages(frame.Messages)
	start, prev, err := s.alignFrame(ctx, config, chatID, messages)
	if err != nil {
		return 0, err
	}
	writes := make([]chat.ExternalMessageWrite, 0, max(0, len(messages)-start))
	parts := make(map[uuid.UUID][]chatrepo.CreateChatContentPartParams)
	for index := start; index < len(messages); index++ {
		msg := messages[index]
		// The chained identity makes concurrent deliveries and retries share the
		// same rows, while identical messages at different positions stay apart.
		hash := contentHash(msg)
		prev = chainHash(prev, hash)
		id := messageIdentityID(prev)
		rows, attachments, err := storageBlocks(msg, id)
		if err != nil {
			return 0, fmt.Errorf("decode inference storage blocks: %w", err)
		}
		for rowIndex, row := range rows {
			externalID := fmt.Sprintf("%s/block:%d", id, rowIndex)
			// Only the final row carries the counted ordinal. Partial writes can be
			// retried without counting an incompletely stored incoming message.
			if rowIndex == len(rows)-1 {
				externalID = id
			}
			messageID, err := uuid.NewV7()
			if err != nil {
				return 0, fmt.Errorf("generate inference message ID: %w", err)
			}
			createdAt := conv.ToPGTimestamptz(now.Add(time.Duration(len(writes)) * time.Microsecond))
			if rowIndex == len(rows)-1 && len(attachments) > 0 {
				contents := make([][]byte, len(attachments))
				for i, attachment := range attachments {
					contents[i] = []byte(attachment.Text)
				}
				urls, err := s.writer.WriteContentPartAssets(ctx, config.ProjectID, chatID, contents)
				if err != nil {
					return 0, fmt.Errorf("store inference attachments: %w", err)
				}
				for i, attachment := range attachments {
					metadata, err := json.Marshal(map[string]string{"display_path": attachment.FileName})
					if err != nil {
						return 0, fmt.Errorf("encode inference attachment metadata: %w", err)
					}
					parts[messageID] = append(parts[messageID], chatrepo.CreateChatContentPartParams{
						ChatID: chatID, ProjectID: config.ProjectID, Kind: message.PromptAttachment,
						ContentAssetUrl: urls[i], ExternalID: conv.ToPGText(fmt.Sprintf("%s/attachment:%d", id, i)),
						ParentChatMessageID: uuid.NullUUID{UUID: messageID, Valid: true}, Version: pgtype.Int4{Int32: 0, Valid: false},
						Source: conv.ToPGText(inferenceSource(frame.Source.Application)), Metadata: metadata,
						RiskAnalyzedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}, CreatedAt: createdAt,
					})
				}
			}
			writes = append(writes, chat.ExternalMessageWrite{
				Params: chatrepo.CreateExternalChatMessageParams{
					ID:                messageID,
					ChatID:            chatID,
					Role:              row.role,
					ProjectID:         config.ProjectID,
					Content:           row.content,
					ContentRaw:        msg.Content,
					ContentAssetUrl:   pgtype.Text{String: "", Valid: false},
					StorageError:      pgtype.Text{String: "", Valid: false},
					Model:             conv.ToPGTextEmpty(frame.Model),
					MessageID:         pgtype.Text{String: "", Valid: false},
					ToolCallID:        conv.ToPGTextEmpty(row.toolCallID),
					UserID:            conv.ToPGTextEmpty(userID),
					ExternalUserID:    conv.ToPGTextEmpty(externalUserIDForMessages),
					ExternalMessageID: conv.ToPGText(externalID),
					FinishReason:      pgtype.Text{String: "", Valid: false},
					ToolCalls:         row.toolCalls,
					PromptTokens:      0,
					CompletionTokens:  0,
					TotalTokens:       0,
					Origin:            conv.ToPGText("anthropic-inference"),
					UserAgent:         pgtype.Text{String: "", Valid: false},
					IpAddress:         pgtype.Text{String: "", Valid: false},
					Source:            conv.ToPGText(inferenceSource(frame.Source.Application)),
					ContentHash:       conv.Ternary(rowIndex == len(rows)-1, hash, nil),
					Generation:        0,
					CreatedAt:         createdAt,
				},
				BillingUserID:  userID,
				WorkloadSource: metering.WorkloadSourceHook,
				UserEmail:      frame.Actor.EmailAddress,
				Provider:       "anthropic",
				HookHostname:   "",
				AccountType:    "team",
				BillingMode:    "unknown",
			})
		}
	}
	if _, err := s.writer.WriteExternalWithContentParts(ctx, config.ProjectID, writes, parts); err != nil {
		return 0, fmt.Errorf("write inference messages: %w", err)
	}
	// PreferStoredTitle keeps a generated title, so scheduling stops once one
	// lands and an agent loop costs one start rather than one per model call.
	if len(writes) > 0 && chat.IsPlaceholderTitle(conversation.Title.String) {
		s.scheduleTitle(ctx, config, chatID)
	}
	return start, nil
}

// scheduleTitle asks the title generator to replace the inference placeholder.
// It runs off the request goroutine: the verdict is on a hard budget and a
// Temporal round trip must not spend any of it.
func (s *postgresStore) scheduleTitle(ctx context.Context, config Config, chatID uuid.UUID) {
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), titleScheduleTimeout)
		defer cancel()
		if err := s.titles.ScheduleChatTitleGeneration(ctx, chatID.String(), config.OrganizationID, config.ProjectID.String()); err != nil {
			s.logger.WarnContext(ctx, "failed to schedule inference conversation title generation",
				attr.SlogError(err),
				attr.SlogChatID(chatID.String()),
			)
		}
	}()
}

// alignFrame locates the incoming transcript within stored history. A frame
// that shares no message with stored history is continued by count: history
// stored before content hashing carries ordinals rather than hashes, and a
// client that rewrote every stored message in place is still sending the same
// conversation, so its history is not archived a second time.
func (s *postgresStore) alignFrame(ctx context.Context, config Config, chatID uuid.UUID, messages []Message) (int, []byte, error) {
	rows, err := chatrepo.New(s.db).ListInferenceMessageIdentities(ctx, chatrepo.ListInferenceMessageIdentitiesParams{
		ChatID: chatID, ProjectID: uuid.NullUUID{UUID: config.ProjectID, Valid: true}, RowLimit: alignmentAnchors,
	})
	if err != nil {
		return 0, nil, fmt.Errorf("list stored inference messages: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil, nil
	}
	stored := make([]messageIdentity, 0, len(rows))
	for _, row := range slices.Backward(rows) {
		identity := parseMessageIdentity(row.ExternalMessageID.String, row.ContentHash)
		if identity.content != nil {
			stored = append(stored, identity)
		}
	}
	if start, prev, ok := alignTranscript(stored, messages); ok {
		return start, prev, nil
	}
	storedCount, err := chatrepo.New(s.db).CountInferenceMessages(ctx, chatrepo.CountInferenceMessagesParams{ChatID: chatID, ProjectID: uuid.NullUUID{UUID: config.ProjectID, Valid: true}})
	if err != nil {
		return 0, nil, fmt.Errorf("count stored inference messages: %w", err)
	}
	return min(int(storedCount), len(messages)), nil, nil
}

// actorIdentity is the stable provider actor id a frame asserts, preferring
// the account id over the email, and whether it has one at all.
func actorIdentity(frame Frame) (string, bool) {
	actorID := conv.Default(frame.Actor.ID, conv.NormalizeEmail(frame.Actor.EmailAddress))
	return actorID, actorID != ""
}

// Include the actor because Claude Code session ids can be client asserted.
// Neither another actor nor another project can append to this conversation.
// A frame with no actor is request-scoped.
func conversationID(config Config, frame Frame) uuid.UUID {
	actorID, hasActor := actorIdentity(frame)
	sessionID := conv.Default(frame.SessionID, frame.RequestID)
	if !hasActor {
		sessionID = frame.RequestID
	}
	identity, _ := json.Marshal([]string{config.TenantID, frame.Actor.Type, actorID, sessionID})
	return uuid.NewSHA1(config.ProjectID, identity)
}

// actorKey fingerprints the provider actor a conversation belongs to: the
// same tuple conversationID hashes, minus the session. It is stored on the
// chat so a transcript prefix match can refuse another actor's conversation.
// A frame with no actor has no key: it can neither be adopted into nor
// adopt an archived conversation.
func actorKey(config Config, frame Frame) []byte {
	actorID, hasActor := actorIdentity(frame)
	if !hasActor {
		return nil
	}
	identity, _ := json.Marshal([]string{config.TenantID, frame.Actor.Type, actorID})
	sum := sha256.Sum256(identity)
	return sum[:]
}

// conversation returns the chat the frame resolved to, resolving it when the
// caller did not go through Process.
func (s *postgresStore) conversation(ctx context.Context, config Config, frame Frame) (uuid.UUID, error) {
	if frame.conversation != uuid.Nil {
		return frame.conversation, nil
	}
	id, _, err := s.ResolveConversation(ctx, config, frame)
	return id, err
}

// ResolveConversation maps a frame onto a stored chat. A frame with a session
// id continues the chat that id names, or starts it: a sessioned product
// that forks a conversation under a new session id must not be folded into
// the original, so content matching is reserved for frames that carry no
// session id at all (the protocol allows that, and some products send none).
// For those, the identities the frame's own newest prefixes would carry are
// looked up, and the actor's chat whose newest stored message is one of them
// is adopted. A chat whose history merely starts like the frame is never
// adopted, so two conversations sharing an opening stay apart, and when two
// of the actor's chats end in the same prefix none is chosen. Anything else
// is a new chat keyed as conversationID does.
func (s *postgresStore) ResolveConversation(ctx context.Context, config Config, frame Frame) (uuid.UUID, string, error) {
	candidate := conversationID(config, frame)
	queries := chatrepo.New(s.db)
	if frame.SessionID != "" {
		exists, err := queries.InferenceChatExists(ctx, chatrepo.InferenceChatExistsParams{ID: candidate, ProjectID: config.ProjectID})
		if err != nil {
			return uuid.Nil, "", fmt.Errorf("load inference conversation: %w", err)
		}
		if exists {
			return candidate, conversationOutcomeSession, nil
		}
		return candidate, conversationOutcomeNew, nil
	}
	messages := conversationMessages(frame.Messages)
	key := actorKey(config, frame)
	if len(messages) < 2 || key == nil {
		// A single message has no stored prefix to continue, and an anonymous
		// frame has no actor to continue it as.
		return candidate, conversationOutcomeNew, nil
	}
	// Newest prefix first: a continuation normally appends a few messages,
	// and the longest stored prefix is the chat to continue. One query takes
	// every candidate identity and returns the longest match.
	chains := prefixIdentities(messages)
	identities := make([]string, 0, alignmentAnchors)
	for k := len(messages); k >= max(1, len(messages)-alignmentAnchors+1); k-- {
		identities = append(identities, messageIdentityID(chains[k-1]))
	}
	rows, err := queries.FindInferenceChatsByNewestMessageIdentity(ctx, chatrepo.FindInferenceChatsByNewestMessageIdentityParams{
		ProjectID:          uuid.NullUUID{UUID: config.ProjectID, Valid: true},
		ExternalMessageIds: identities,
		InferenceActorKey:  key,
	})
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("find inference conversation by transcript prefix: %w", err)
	}
	// The first identity any chat ends in is the longest stored prefix. Two
	// chats ending in it are two conversations that so far read the same
	// (the same opening prompt twice), and neither may be guessed.
	for _, identity := range identities {
		var matches []uuid.UUID
		for _, row := range rows {
			if row.ExternalMessageID.String == identity {
				matches = append(matches, row.ChatID)
			}
		}
		switch len(matches) {
		case 0:
			continue
		case 1:
			return matches[0], conversationOutcomeAdoptedPrefix, nil
		default:
			return candidate, conversationOutcomeAmbiguousPrefix, nil
		}
	}
	return candidate, conversationOutcomeNew, nil
}

// productSource maps Anthropic's application names to product surfaces and
// reports whether the name is one it knows. In this protocol claude-code
// identifies the web product, not the local CLI. Unknown names pass through
// unchanged, so storage keeps whatever the client sent.
func productSource(application string) (string, bool) {
	switch source := strings.TrimSpace(application); source {
	case "claude-ai":
		return "claude-chat-web", true
	case "claude-code":
		return "claude-code-web", true
	case "claude-design":
		return source, true
	case "":
		return "anthropic-inference", true
	default:
		return source, false
	}
}

// inferenceSource is the product surface recorded on stored messages.
func inferenceSource(application string) string {
	source, _ := productSource(application)
	return source
}
