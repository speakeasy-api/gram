package intentpolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TranscriptFormat names a supported transcript encoding.
type TranscriptFormat string

const (
	// FormatMessages is a JSON document of Anthropic-style messages.
	FormatMessages TranscriptFormat = "messages"

	// FormatClaudeCode is Claude Code session JSONL.
	FormatClaudeCode TranscriptFormat = "claude-code"

	// FormatPi is Pi session JSONL.
	FormatPi TranscriptFormat = "pi"
)

// ErrEmptyTranscript means the transcript holds no conversational messages.
var ErrEmptyTranscript = errors.New("transcript has no messages")

// TranscriptToolCall is one tool invocation in an assistant message.
type TranscriptToolCall struct {
	// ID is the provider call id.
	ID string

	// Name is the raw tool name.
	Name string

	// Arguments is the argument object as JSON.
	Arguments json.RawMessage
}

// TranscriptMessage is one message ready to store.
type TranscriptMessage struct {
	// Role is user, assistant, or tool.
	Role string

	// Content is the message text or tool result text.
	Content string

	// ToolCalls holds an assistant message's calls.
	ToolCalls []TranscriptToolCall

	// ToolCallID links a tool result to its call.
	ToolCallID string

	// Timestamp is the recorded time, when the format carries one.
	Timestamp *time.Time
}

// Transcript is a parsed session.
type Transcript struct {
	// Format is the detected encoding.
	Format TranscriptFormat

	// Cwd is the recorded working directory, when present.
	Cwd string

	// Messages are in conversation order.
	Messages []TranscriptMessage

	// InvalidLines counts JSONL lines that were not valid JSON.
	InvalidLines int
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Arguments json.RawMessage `json:"arguments"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

type roleMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"toolCallId"`
}

// ParseTranscript detects the format and flattens the transcript into
// storable messages. Sidechain (subagent) records are skipped.
func ParseTranscript(raw []byte) (Transcript, error) {
	trimmed := bytes.TrimSpace(raw)
	if msgs, ok := parseMessagesDocument(trimmed); ok {
		var b transcriptBuilder
		for _, m := range msgs {
			if err := b.message(m.Role, m.Content, nil); err != nil {
				return Transcript{}, err
			}
		}
		return b.finish(FormatMessages, "", 0)
	}
	return parseJSONL(trimmed)
}

func parseMessagesDocument(raw []byte) ([]roleMessage, bool) {
	if len(raw) == 0 || (raw[0] != '[' && raw[0] != '{') {
		return nil, false
	}
	var msgs []roleMessage
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &msgs); err != nil {
			return nil, false
		}
	} else {
		var doc struct {
			Messages []roleMessage `json:"messages"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil || doc.Messages == nil {
			return nil, false
		}
		msgs = doc.Messages
	}
	for _, m := range msgs {
		if m.Role == "" || m.Content == nil {
			return nil, false
		}
	}
	return msgs, true
}

type jsonlRecord struct {
	Type        string       `json:"type"`
	ID          string       `json:"id"`
	Cwd         string       `json:"cwd"`
	Timestamp   string       `json:"timestamp"`
	IsSidechain bool         `json:"isSidechain"`
	Message     *roleMessage `json:"message"`
}

func parseJSONL(raw []byte) (Transcript, error) {
	var b transcriptBuilder
	format := FormatClaudeCode
	cwd := ""
	invalid := 0
	for line := range bytes.SplitSeq(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var rec jsonlRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			invalid++
			continue
		}
		if cwd == "" && rec.Cwd != "" {
			cwd = rec.Cwd
		}
		if rec.Type == "session" || rec.Type == "message" {
			format = FormatPi
		}
		if rec.IsSidechain || rec.Message == nil {
			continue
		}
		if rec.Type != "user" && rec.Type != "assistant" && rec.Type != "message" {
			continue
		}
		var ts *time.Time
		if t, err := time.Parse(time.RFC3339Nano, rec.Timestamp); err == nil {
			ts = &t
		}
		role := rec.Message.Role
		if role == "" {
			role = rec.Type
		}
		if role == "toolResult" {
			b.toolResult(rec.Message.ToolCallID, rec.Message.Content, ts)
			continue
		}
		if err := b.message(role, rec.Message.Content, ts); err != nil {
			return Transcript{}, err
		}
	}
	return b.finish(format, cwd, invalid)
}

type transcriptBuilder struct {
	messages []TranscriptMessage
}

func (b *transcriptBuilder) finish(format TranscriptFormat, cwd string, invalid int) (Transcript, error) {
	if len(b.messages) == 0 {
		return Transcript{}, ErrEmptyTranscript
	}
	return Transcript{Format: format, Cwd: cwd, Messages: b.messages, InvalidLines: invalid}, nil
}

func decodeBlocks(content json.RawMessage) ([]contentBlock, error) {
	content = bytes.TrimSpace(content)
	if len(content) == 0 || string(content) == "null" {
		return nil, nil
	}
	if content[0] == '"' {
		var s string
		if err := json.Unmarshal(content, &s); err != nil {
			return nil, fmt.Errorf("decode message text: %w", err)
		}
		return []contentBlock{{Type: "text", Text: s, ID: "", Name: "", Input: nil, Arguments: nil, ToolUseID: "", Content: nil}}, nil
	}
	if content[0] != '[' {
		return nil, nil
	}
	var blocks []contentBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return nil, fmt.Errorf("decode content blocks: %w", err)
	}
	return blocks, nil
}

func (b *transcriptBuilder) message(role string, content json.RawMessage, ts *time.Time) error {
	blocks, err := decodeBlocks(content)
	if err != nil {
		return err
	}
	switch role {
	case "assistant":
		var text []string
		var calls []TranscriptToolCall
		for i, block := range blocks {
			switch block.Type {
			case "text":
				if strings.TrimSpace(block.Text) != "" {
					text = append(text, block.Text)
				}
			case "tool_use", "toolCall":
				args := block.Input
				if block.Type == "toolCall" {
					args = block.Arguments
				}
				if len(bytes.TrimSpace(args)) == 0 || !json.Valid(args) {
					args = json.RawMessage(`{}`)
				}
				id := block.ID
				if id == "" {
					id = fmt.Sprintf("call_%d_%d", len(b.messages), i)
				}
				name := block.Name
				if name == "" {
					name = "unknown"
				}
				calls = append(calls, TranscriptToolCall{ID: id, Name: name, Arguments: args})
			}
		}
		if len(text) == 0 && len(calls) == 0 {
			return nil
		}
		b.messages = append(b.messages, TranscriptMessage{Role: "assistant", Content: strings.Join(text, "\n\n"), ToolCalls: calls, ToolCallID: "", Timestamp: ts})
	case "user":
		var text []string
		for _, block := range blocks {
			switch block.Type {
			case "text":
				if strings.TrimSpace(block.Text) != "" {
					text = append(text, block.Text)
				}
			case "image":
				text = append(text, "[image]")
			case "tool_result":
				b.toolResult(block.ToolUseID, block.Content, ts)
			}
		}
		if len(text) > 0 {
			b.messages = append(b.messages, TranscriptMessage{Role: "user", Content: strings.Join(text, "\n\n"), ToolCalls: nil, ToolCallID: "", Timestamp: ts})
		}
	}
	return nil
}

func (b *transcriptBuilder) toolResult(callID string, content json.RawMessage, ts *time.Time) {
	b.messages = append(b.messages, TranscriptMessage{Role: "tool", Content: flattenContent(content), ToolCalls: nil, ToolCallID: callID, Timestamp: ts})
}

func flattenContent(content json.RawMessage) string {
	content = bytes.TrimSpace(content)
	if len(content) == 0 || string(content) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return s
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(content, &blocks); err != nil {
		return string(content)
	}
	parts := make([]string, 0, len(blocks))
	for _, raw := range blocks {
		var block contentBlock
		if err := json.Unmarshal(raw, &block); err == nil && block.Type == "text" {
			parts = append(parts, block.Text)
			continue
		}
		if block.Type == "image" {
			parts = append(parts, "[image]")
			continue
		}
		parts = append(parts, string(raw))
	}
	return strings.Join(parts, "\n")
}
