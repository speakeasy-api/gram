package intentpolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/google/uuid"

	intentpolicyv1 "github.com/speakeasy-api/gram/infra/gen/gram/intentpolicy/v1"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

const (
	// MaxExportedContentBytes caps one exported message's text. Pub/Sub
	// rejects messages over 10 MB, so content and arguments together stay well
	// under it.
	MaxExportedContentBytes = 4 << 20 // 4 MiB

	// MaxExportedArgumentsBytes caps one exported tool call's arguments.
	MaxExportedArgumentsBytes = 1 << 20 // 1 MiB

	// MaxExportedArgumentsTotalBytes caps all of one message's exported
	// arguments together; later calls get what remains of it.
	MaxExportedArgumentsTotalBytes = 4 << 20 // 4 MiB
)

// transcriptNamespace scopes exported transcript message identities.
var transcriptNamespace = uuid.MustParse("1b2e7c55-8f0a-4d3b-9e61-4a7f2c9d0e13")

// Redactor masks secrets in text or JSON before it leaves the server.
type Redactor interface {
	Text(ctx context.Context, text string) (string, bool, error)
}

// TranscriptMessageID is the identity of one message as exported by one analysis.
func TranscriptMessageID(analysisID, chatID uuid.UUID, generation int32, messageID uuid.UUID) uuid.UUID {
	key := analysisID.String() + "\x00" + chatID.String() + "\x00" + strconv.FormatInt(int64(generation), 10) + "\x00" + messageID.String()
	return uuid.NewSHA1(transcriptNamespace, []byte(key))
}

// TranscriptRecords projects the messages of one analyzed chat generation into
// their exported form, masking secrets with redactor. A message the redactor
// fails on is exported without content or arguments and marked withheld, so a
// scan failure never leaks unmasked text.
func TranscriptRecords(ctx context.Context, redactor Redactor, rc RecordContext, chatID uuid.UUID, rows []MessageRow) ([]*intentpolicyv1.TranscriptMessage, error) {
	actor := intentpolicyv1.TranscriptMessage_Actor_builder{UserId: new(rc.UserID)}.Build()
	if rc.UserEmail != "" {
		actor.SetUserEmail(rc.UserEmail)
	}

	out := make([]*intentpolicyv1.TranscriptMessage, 0, len(rows))
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("export transcript: %w", err)
		}
		m := intentpolicyv1.TranscriptMessage_builder{
			Id:             new(TranscriptMessageID(rc.AnalysisID, chatID, rc.Generation, row.ID).String()),
			AnalysisId:     new(rc.AnalysisID.String()),
			OrganizationId: new(rc.OrganizationID),
			ProjectId:      new(rc.ProjectID.String()),
			Actor:          actor,
			ChatId:         new(chatID.String()),
			Generation:     new(rc.Generation),
			MessageId:      new(row.ID.String()),
			Sequence:       new(row.Seq),
			Role:           new(row.Role),
			ContentBytes:   new(int64(len(row.Content))),
			CreatedAt:      new(formatTime(row.CreatedAt)),
			ExportedAt:     new(formatTime(rc.EvaluatedAt)),
		}.Build()
		if row.ToolCallID != "" {
			m.SetToolCallId(row.ToolCallID)
		}
		if row.Model != "" {
			m.SetModel(row.Model)
		}

		calls, err := parseStoredCalls(row)
		if err != nil {
			return nil, err
		}
		redacted, withheld, err := fillContent(ctx, redactor, m, row.Content, calls)
		if err != nil {
			return nil, err
		}
		m.SetRedacted(redacted)
		m.SetWithheld(withheld)
		out = append(out, m)
	}
	return out, nil
}

func parseStoredCalls(row MessageRow) ([]storedToolCall, error) {
	if len(row.ToolCalls) == 0 {
		return nil, nil
	}
	var calls []storedToolCall
	if err := json.Unmarshal(row.ToolCalls, &calls); err != nil {
		return nil, fmt.Errorf("parse tool calls of message %s: %w", row.ID, err)
	}
	return calls, nil
}

// fillContent sets the masked content and tool calls on m. It returns whether
// anything was masked and whether the message was withheld after a failed scan;
// only context cancellation is returned as an error.
func fillContent(ctx context.Context, redactor Redactor, m *intentpolicyv1.TranscriptMessage, content string, calls []storedToolCall) (redacted, withheld bool, err error) {
	safeContent, changed, scanErr := redactor.Text(ctx, content)
	if scanErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, false, fmt.Errorf("export transcript: %w", ctxErr)
		}
		withheld = true
	}
	redacted = changed

	exported := make([]*intentpolicyv1.TranscriptMessage_ToolCall, 0, len(calls))
	budget := MaxExportedArgumentsTotalBytes
	for _, call := range calls {
		server, function, _ := toolref.AttributeTool(call.Function.Name)
		tc := intentpolicyv1.TranscriptMessage_ToolCall_builder{
			ToolCallId:     new(call.ID),
			ToolName:       new(call.Function.Name),
			McpServer:      new(server),
			McpFunction:    new(function),
			ArgumentsBytes: new(int64(len(call.Function.Arguments))),
		}.Build()
		if !withheld {
			safeArgs, changed, scanErr := redactor.Text(ctx, call.Function.Arguments)
			if scanErr != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return false, false, fmt.Errorf("export transcript: %w", ctxErr)
				}
				withheld = true
			} else {
				redacted = redacted || changed
				args, truncated := truncateUTF8(safeArgs, min(MaxExportedArgumentsBytes, budget))
				budget -= len(args)
				tc.SetArguments(args)
				tc.SetArgumentsTruncated(truncated)
			}
		}
		exported = append(exported, tc)
	}

	if withheld {
		// A failed scan withholds the whole message, including any arguments
		// already masked, so partial content never ships.
		for _, tc := range exported {
			tc.ClearArguments()
			tc.ClearArgumentsTruncated()
		}
		m.SetToolCalls(exported)
		return false, true, nil
	}
	text, truncated := truncateUTF8(safeContent, MaxExportedContentBytes)
	m.SetContent(text)
	m.SetContentTruncated(truncated)
	m.SetToolCalls(exported)
	return redacted, false, nil
}

// truncateUTF8 cuts s to at most limit bytes on a rune boundary.
func truncateUTF8(s string, limit int) (string, bool) {
	if limit < 0 {
		limit = 0
	}
	if len(s) <= limit {
		return s, false
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}
