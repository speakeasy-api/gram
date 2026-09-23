package anthropicinference

import (
	"context"
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

type transcriptStore interface {
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
}

// NewService uses the shared chat writer so captured messages receive the same
// storage, metering, and asynchronous analysis as other imported conversations.
func NewService(logger *slog.Logger, db *pgxpool.Pool, writer *chat.ChatMessageWriter, scanner scanner) *Service {
	return &Service{logger: logger, store: &postgresStore{db: db, writer: writer}, scanner: scanner}
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
	userID, err := s.store.ResolveActor(budget, config, frame)
	if err != nil {
		return Verdict{}, fmt.Errorf("resolve inference hook actor: %w", err)
	}
	messages := conversationMessages(frame.Messages)
	if _, err := policyInputs(messages); err != nil {
		return Verdict{}, fmt.Errorf("decode inference transcript: %w", err)
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
			// Admission can block until a slot opens after cancellation.
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
	conversation, err := chatrepo.New(s.db).GetChat(ctx, chatrepo.GetChatParams{ID: conversationID(config, frame), ProjectID: config.ProjectID})
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
	// from native hooks and Compliance imports. Null sessions are request-local.
	candidateID := conversationID(config, frame)
	externalChatID := "anthropic-inference:" + candidateID.String()
	chatID, err := chatrepo.New(s.db).UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
		ID:                candidateID,
		ProjectID:         config.ProjectID,
		OrganizationID:    config.OrganizationID,
		UserID:            conv.ToPGTextEmpty(userID),
		ExternalUserID:    conv.ToPGTextEmpty(externalUserIDLabel),
		ExternalChatID:    conv.ToPGText(externalChatID),
		Title:             conv.ToPGText("Claude inference conversation"),
		CreatedAt:         conv.ToPGTimestamptz(now),
		UpdatedAt:         conv.ToPGTimestamptz(now),
		PreferStoredTitle: true,
	})
	if err != nil {
		return 0, fmt.Errorf("upsert inference conversation: %w", err)
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
	return start, nil
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

// Include the actor because Claude Code session ids can be client asserted.
// Neither another actor nor another project can append to this conversation.
func conversationID(config Config, frame Frame) uuid.UUID {
	actorID := conv.Default(frame.Actor.ID, conv.NormalizeEmail(frame.Actor.EmailAddress))
	sessionID := conv.Default(frame.SessionID, frame.RequestID)
	if actorID == "" {
		sessionID = frame.RequestID
	}
	identity, _ := json.Marshal([]string{config.TenantID, frame.Actor.Type, actorID, sessionID})
	return uuid.NewSHA1(config.ProjectID, identity)
}

// inferenceSource maps Anthropic's application names to product surfaces. In
// this protocol claude-code identifies the web product, not the local CLI.
func inferenceSource(application string) string {
	switch source := strings.TrimSpace(application); source {
	case "claude-ai":
		return "claude-chat-web"
	case "claude-code":
		return "claude-code-web"
	case "":
		return "anthropic-inference"
	default:
		return source
	}
}
