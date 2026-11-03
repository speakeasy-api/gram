package hooks

// Claude transcript intake. Claude's unified Chat/Cowork mode runs plugin
// hooks only inside the Cowork VM, where the per-event hooks cannot be relied
// on to describe the whole session. The Cowork distribution of the Speakeasy
// hooks binary therefore registers only the Stop hook and, each time it
// fires, uploads the transcript lines written since its last successful
// upload. This endpoint stores those lines as the session's chat history.
//
// Every stored row is keyed by the transcript entry's uuid, so re-sent lines
// (a retry after a failed upload, or a fresh VM that starts from offset zero)
// are idempotent.

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metering"
)

const (
	// ClaudeTranscriptPath receives transcript uploads from the hooks binary.
	ClaudeTranscriptPath = "/hooks/claude-transcript"

	claudeTranscriptSchemaV1 = "claude.transcript.v1"
	// claudeTranscriptOrigin marks rows stored from an uploaded transcript.
	claudeTranscriptOrigin = "claude-transcript"
	// claudeTranscriptIDPrefix namespaces transcript entry uuids in
	// external_message_id so they cannot collide with other import sources.
	claudeTranscriptIDPrefix = "claude-transcript:"

	// claudeTranscriptMaxBodyBytes bounds the request body as sent, which is
	// usually gzip-compressed.
	claudeTranscriptMaxBodyBytes = 10 << 20
	// claudeTranscriptMaxDecodedBytes bounds the decompressed body so a small
	// compressed request cannot expand without limit.
	claudeTranscriptMaxDecodedBytes = 32 << 20
	claudeTranscriptMaxEntries      = 20000
	claudeTranscriptPersistTimeout  = 30 * time.Second
)

// claudeTranscriptUpload is the request body: a contiguous run of raw
// transcript JSONL lines plus the session identity they belong to.
type claudeTranscriptUpload struct {
	SchemaVersion string `json:"schema_version"`
	SessionID     string `json:"session_id"`
	// Adapter is the hook source slug: "cowork" when the binary detected the
	// Cowork runtime, otherwise "claude".
	Adapter   string `json:"adapter"`
	Cwd       string `json:"cwd"`
	Hostname  string `json:"hostname"`
	UserEmail string `json:"user_email"`
	// OffsetStart and OffsetEnd are the byte range of the transcript file the
	// entries were read from. Diagnostic only.
	OffsetStart int64             `json:"offset_start"`
	OffsetEnd   int64             `json:"offset_end"`
	Entries     []json.RawMessage `json:"entries"`
}

type claudeTranscriptResult struct {
	Stored int64 `json:"stored"`
}

// AttachClaudeTranscript mounts the transcript intake endpoint. It
// authenticates with the hooks API key the same way the AHP endpoint does.
func AttachClaudeTranscript(mux goahttp.Muxer, s *Service) {
	mux.Handle(http.MethodPost, ClaudeTranscriptPath, s.ahpAuthenticate(http.HandlerFunc(s.serveClaudeTranscript)).ServeHTTP)
}

func (s *Service) serveClaudeTranscript(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	upload, status, err := readClaudeTranscriptUpload(w, r)
	if err != nil {
		s.logger.WarnContext(ctx, "rejected claude transcript upload",
			attr.SlogEvent("hooks_claude_transcript_rejected"),
			attr.SlogError(err),
			attr.SlogProjectID(authCtx.ProjectID.String()),
		)
		http.Error(w, err.Error(), status)
		return
	}

	// The binary hangs up once its own budget runs out; a detached context
	// lets a write that already started finish, so the retry the binary makes
	// on its next Stop finds the rows and stores nothing twice.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), claudeTranscriptPersistTimeout)
	defer cancel()
	stored, err := s.persistClaudeTranscript(persistCtx, authCtx, upload)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errChatProjectMismatch) {
			status = http.StatusConflict
		}
		s.logHookPersistFailure(ctx, "claude transcript", err,
			attr.SlogProjectID(authCtx.ProjectID.String()),
			attr.SlogGenAIConversationID(upload.SessionID),
		)
		http.Error(w, http.StatusText(status), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(claudeTranscriptResult{Stored: stored})
}

// readClaudeTranscriptUpload decodes and validates the request body. The
// returned status is the HTTP status to answer with when err is non-nil.
func readClaudeTranscriptUpload(w http.ResponseWriter, r *http.Request) (*claudeTranscriptUpload, int, error) {
	var body io.Reader = http.MaxBytesReader(w, r.Body, claudeTranscriptMaxBodyBytes)
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Content-Encoding")), "gzip") {
		gz, err := gzip.NewReader(body)
		if err != nil {
			return nil, statusForBodyError(err), fmt.Errorf("open gzip body: %w", err)
		}
		defer func() { _ = gz.Close() }()
		body = gz
	}
	raw, err := io.ReadAll(io.LimitReader(body, claudeTranscriptMaxDecodedBytes+1))
	if err != nil {
		return nil, statusForBodyError(err), fmt.Errorf("read body: %w", err)
	}
	if len(raw) > claudeTranscriptMaxDecodedBytes {
		return nil, http.StatusRequestEntityTooLarge, errors.New("decoded body too large")
	}

	var upload claudeTranscriptUpload
	if err := json.Unmarshal(raw, &upload); err != nil {
		return nil, http.StatusBadRequest, fmt.Errorf("decode body: %w", err)
	}
	upload.SessionID = strings.TrimSpace(upload.SessionID)
	switch {
	case strings.TrimSpace(upload.SchemaVersion) != claudeTranscriptSchemaV1:
		return nil, http.StatusBadRequest, errors.New("unsupported schema_version")
	case upload.SessionID == "":
		return nil, http.StatusBadRequest, errors.New("session_id is required")
	case len(upload.Entries) > claudeTranscriptMaxEntries:
		return nil, http.StatusRequestEntityTooLarge, errors.New("too many entries")
	}
	return &upload, 0, nil
}

func statusForBodyError(err error) int {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

// persistClaudeTranscript stores the upload's conversation entries in the
// session's chat and returns how many rows were newly stored.
func (s *Service) persistClaudeTranscript(ctx context.Context, authCtx *contextvalues.AuthContext, upload *claudeTranscriptUpload) (int64, error) {
	projectID := *authCtx.ProjectID
	// Identity and surface resolution reuse the canonical ingest path, so a
	// transcript session is attributed exactly as a hook-captured one would be.
	payload := claudeTranscriptPayload(upload)
	actor := s.resolveCanonicalActor(ctx, payload, authCtx)
	metadata := s.canonicalSessionMetadata(ctx, payload, authCtx, actor)

	enabled, err := s.sessionCaptureEnabled(ctx, &metadata, projectID)
	if err != nil || !enabled {
		return 0, err
	}

	rows := claudeTranscriptRows(upload.Entries, time.Now().UTC())
	if len(rows) == 0 {
		return 0, nil
	}

	hookSource := conv.Default(s.claudeSessionSurface(ctx, &metadata), agentVariantCowork)
	chatID := sessionIDToUUID(upload.SessionID)
	if err := s.ensureHookChat(ctx, s.repo, &metadata, chatID, projectID, claudeTranscriptTitle(rows, hookSource)); err != nil {
		return 0, err
	}

	writes := make([]chat.ExternalMessageWrite, 0, len(rows))
	assistantTurn := false
	for _, row := range rows {
		assistantTurn = assistantTurn || row.role == "assistant"
		writes = append(writes, chat.ExternalMessageWrite{
			Params: chatRepo.CreateExternalChatMessageParams{
				ID:        uuid.Nil,
				ChatID:    chatID,
				Role:      row.role,
				ProjectID: projectID,
				Content:   row.content,
				// Like native hook rows, only the flattened text is stored.
				ContentRaw:        nil,
				ContentAssetUrl:   conv.ToPGTextEmpty(""),
				StorageError:      conv.ToPGTextEmpty(""),
				Model:             conv.ToPGTextEmpty(row.model),
				MessageID:         conv.ToPGTextEmpty(row.messageID),
				ToolCallID:        conv.ToPGTextEmpty(row.toolCallID),
				UserID:            conv.ToPGTextEmpty(metadata.UserID),
				ExternalUserID:    conv.ToPGTextEmpty(metadata.UserEmail),
				ExternalMessageID: conv.ToPGText(row.externalID),
				FinishReason:      conv.ToPGTextEmpty(row.finishReason),
				ToolCalls:         row.toolCalls,
				// Claude repeats a message's usage on every line the message is
				// split across, so per-row token counts would overcount.
				PromptTokens:     0,
				CompletionTokens: 0,
				TotalTokens:      0,
				Origin:           conv.ToPGText(claudeTranscriptOrigin),
				UserAgent:        conv.ToPGTextEmpty(""),
				IpAddress:        conv.ToPGTextEmpty(""),
				Source:           conv.ToPGTextEmpty(hookSource),
				ContentHash:      nil,
				Generation:       0,
				CreatedAt:        conv.ToPGTimestamptz(row.createdAt),
			},
			BillingUserID:     metadata.UserID,
			WorkloadSource:    metering.WorkloadSourceHook,
			UserEmail:         metadata.UserEmail,
			Provider:          metadata.Provider,
			InferenceProvider: "",
			HookHostname:      metadata.Hostname,
			AccountType:       metadata.AccountType,
			BillingMode:       metadata.BillingMode,
		})
	}

	stored, err := s.writer.WriteExternalWithContentParts(ctx, projectID, writes, nil)
	if err != nil {
		return stored, fmt.Errorf("write claude transcript messages: %w", err)
	}
	if stored > 0 && assistantTurn {
		s.scheduleCanonicalChatTitle(ctx, authCtx, chatID, hookSource)
	}
	return stored, nil
}

// claudeTranscriptPayload projects the upload onto a canonical ingest payload
// so actor, session metadata, and surface resolution can be shared with the
// hook ingest path. It is never ingested as an event.
func claudeTranscriptPayload(upload *claudeTranscriptUpload) *gen.IngestPayload {
	adapter := "claude"
	if strings.EqualFold(strings.TrimSpace(upload.Adapter), agentVariantCowork) {
		adapter = agentVariantCowork
	}
	return &gen.IngestPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		DeviceAgentToken: nil,
		Replayed:         nil,
		SchemaVersion:    hookIngestSchemaV1,
		IdempotencyKey:   nil,
		Source: &gen.HookIngestSource{
			Adapter:        adapter,
			AdapterVersion: nil,
			RawEventName:   nil,
			Hostname:       conv.PtrEmpty(strings.TrimSpace(upload.Hostname)),
			UserEmail:      conv.PtrEmpty(strings.TrimSpace(upload.UserEmail)),
		},
		Session: &gen.HookIngestSession{
			ID:     &upload.SessionID,
			TurnID: nil,
			Cwd:    conv.PtrEmpty(strings.TrimSpace(upload.Cwd)),
			Model:  nil,
		},
		Event: &gen.HookIngestEvent{Type: "session.updated", OccurredAt: nil},
		Data:  nil,
		Raw:   nil,
	}
}

// claudeTranscriptTitle seeds a new chat with the first user text, or the
// surface placeholder when the upload carries none. Title generation replaces
// it once an assistant turn is stored.
func claudeTranscriptTitle(rows []claudeTranscriptRow, hookSource string) string {
	for _, row := range rows {
		if row.role == "user" && strings.TrimSpace(row.content) != "" {
			return chat.DerivedTitle(row.content)
		}
	}
	return canonicalPlaceholderTitle(hookSource)
}

// claudeTranscriptLine is the subset of a Claude Code transcript entry the
// intake reads. Unknown fields and entry types are ignored.
type claudeTranscriptLine struct {
	Type             string `json:"type"`
	UUID             string `json:"uuid"`
	Timestamp        string `json:"timestamp"`
	IsSidechain      bool   `json:"isSidechain"`
	IsMeta           bool   `json:"isMeta"`
	IsCompactSummary bool   `json:"isCompactSummary"`
	Message          struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type claudeTranscriptBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

type claudeTranscriptToolCall struct {
	ID       string                       `json:"id"`
	Type     string                       `json:"type"`
	Function claudeTranscriptToolFunction `json:"function"`
}

type claudeTranscriptToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// claudeTranscriptRow is one chat row derived from a transcript entry.
type claudeTranscriptRow struct {
	externalID   string
	role         string
	content      string
	toolCallID   string
	toolCalls    []byte
	finishReason string
	model        string
	messageID    string
	createdAt    time.Time
}

// claudeTranscriptRows converts transcript entries to chat rows. Only user and
// assistant entries on the main conversation become rows; thinking blocks,
// meta and compaction-summary entries, sidechains (subagents), and every
// bookkeeping entry type are skipped. A mixed entry splits into one row per
// role the conversation readers understand: user text, assistant text, an
// assistant tool call, or a tool result. Undecodable entries are skipped.
func claudeTranscriptRows(entries []json.RawMessage, now time.Time) []claudeTranscriptRow {
	var rows []claudeTranscriptRow
	for _, raw := range entries {
		var line claudeTranscriptLine
		if err := json.Unmarshal(raw, &line); err != nil {
			continue
		}
		uuidStr := strings.TrimSpace(line.UUID)
		if uuidStr == "" || line.IsSidechain || line.IsMeta || line.IsCompactSummary {
			continue
		}
		if line.Type != "user" && line.Type != "assistant" {
			continue
		}
		blocks, ok := claudeTranscriptBlocks(line.Message.Content)
		if !ok {
			continue
		}
		entryRows := claudeTranscriptEntryRows(line, blocks)
		createdAt := now
		if ts, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(line.Timestamp)); err == nil {
			createdAt = ts.UTC()
		}
		base := claudeTranscriptIDPrefix + uuidStr
		for i := range entryRows {
			row := &entryRows[i]
			row.externalID = base
			if len(entryRows) > 1 {
				row.externalID = fmt.Sprintf("%s/block:%d", base, i)
			}
			// Rows split from one entry keep their order at the entry's time.
			row.createdAt = createdAt.Add(time.Duration(i) * time.Microsecond)
		}
		rows = append(rows, entryRows...)
	}
	return rows
}

// claudeTranscriptBlocks decodes message content, which is either a plain
// string or an array of typed blocks.
func claudeTranscriptBlocks(content json.RawMessage) ([]claudeTranscriptBlock, bool) {
	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		return []claudeTranscriptBlock{{Type: "text", Text: text, ID: "", Name: "", Input: nil, ToolUseID: "", Content: nil}}, true
	}
	var blocks []claudeTranscriptBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return nil, false
	}
	return blocks, true
}

func claudeTranscriptEntryRows(line claudeTranscriptLine, blocks []claudeTranscriptBlock) []claudeTranscriptRow {
	var rows []claudeTranscriptRow
	var texts []string
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if strings.TrimSpace(block.Text) != "" {
				texts = append(texts, block.Text)
			}
		case "tool_use":
			if line.Type != "assistant" {
				continue
			}
			calls, err := json.Marshal([]claudeTranscriptToolCall{{
				ID:   block.ID,
				Type: "function",
				Function: claudeTranscriptToolFunction{
					Name:      block.Name,
					Arguments: string(conv.DefaultSlice(block.Input, json.RawMessage("{}"))),
				},
			}})
			if err != nil {
				continue
			}
			row := claudeTranscriptBaseRow(line, "assistant")
			row.toolCalls = calls
			row.finishReason = "tool_calls"
			rows = append(rows, row)
		case "tool_result":
			if line.Type != "user" {
				continue
			}
			content := claudeTranscriptToolResultText(block.Content)
			if strings.TrimSpace(content) == "" {
				continue
			}
			row := claudeTranscriptBaseRow(line, "tool")
			row.content = content
			row.toolCallID = block.ToolUseID
			rows = append(rows, row)
		}
	}
	if len(texts) > 0 {
		row := claudeTranscriptBaseRow(line, line.Type)
		row.content = strings.Join(texts, "\n\n")
		// Text precedes the tool traffic it introduces.
		rows = append([]claudeTranscriptRow{row}, rows...)
	}
	return rows
}

func claudeTranscriptBaseRow(line claudeTranscriptLine, role string) claudeTranscriptRow {
	row := claudeTranscriptRow{
		externalID:   "",
		role:         role,
		content:      "",
		toolCallID:   "",
		toolCalls:    nil,
		finishReason: "",
		model:        "",
		messageID:    "",
		createdAt:    time.Time{},
	}
	if line.Type == "assistant" {
		row.model = strings.TrimSpace(line.Message.Model)
		row.messageID = strings.TrimSpace(line.Message.ID)
	}
	return row
}

// claudeTranscriptToolResultText flattens a tool result's content, which is a
// plain string or an array of blocks whose text blocks carry the output.
func claudeTranscriptToolResultText(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		return text
	}
	var blocks []claudeTranscriptBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return ""
	}
	texts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			texts = append(texts, block.Text)
		}
	}
	return strings.Join(texts, "\n")
}
