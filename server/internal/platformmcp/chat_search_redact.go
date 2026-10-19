package platformmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// chatRedactMaxDepth bounds JSON nesting, including JSON encoded inside JSON
// strings such as tool-call arguments. Deeper values are withheld.
const chatRedactMaxDepth = 16

// messageText joins a message's redacted content, tool calls and stored
// payload. Each field is redacted whole before any truncation. changed
// reports that something was masked or withheld.
func (s *ChatSearchService) messageText(ctx context.Context, content string, toolCalls, raw []byte) (string, bool, error) {
	text, changed, err := s.redactText(ctx, content)
	if err != nil {
		return "", false, err
	}
	for _, field := range []struct {
		label string
		value []byte
	}{{"tool_calls", toolCalls}, {"content_raw", raw}} {
		if len(field.value) == 0 {
			continue
		}
		safe, hidden, err := s.redactText(ctx, string(field.value))
		if err != nil {
			return "", false, err
		}
		text += "\n" + field.label + ": " + safe
		changed = changed || hidden
	}
	return text, changed, nil
}

// redactText masks detected secrets in plain text or JSON.
func (s *ChatSearchService) redactText(ctx context.Context, text string) (string, bool, error) {
	changed := false
	safe, err := s.redactValue(ctx, text, 0, &changed)
	return safe, changed, err
}

func (s *ChatSearchService) redactValue(ctx context.Context, text string, depth int, changed *bool) (string, error) {
	if text == "" {
		return "", nil
	}
	if depth > chatRedactMaxDepth {
		*changed = true
		return "[WITHHELD]", nil
	}
	value, ok := decodeChatJSON(text)
	if !ok {
		return s.redactPlainText(ctx, text, changed)
	}
	safe, err := s.redactJSON(ctx, value, depth+1, changed)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(safe); err != nil {
		return "", fmt.Errorf("encode redacted chat json: %w", err)
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}

// decodeChatJSON decodes JSON objects and arrays with json.Number so large
// identifiers and exponents survive re-encoding unchanged.
func decodeChatJSON(text string) (any, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') || !json.Valid([]byte(trimmed)) {
		return nil, false
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return nil, false
	}
	return value, true
}

func (s *ChatSearchService) redactPlainText(ctx context.Context, text string, changed *bool) (string, error) {
	result, err := s.scanner.Scan(ctx, text)
	if err != nil || !result.Completed {
		return "", ErrUnavailable
	}
	masked := make([]bool, len(text))
	for _, finding := range result.Findings {
		if finding.StartPos < 0 || finding.EndPos > len(text) || finding.EndPos <= finding.StartPos {
			*changed = true
			return "[WITHHELD]", nil
		}
		for i := finding.StartPos; i < finding.EndPos; i++ {
			masked[i] = true
		}
	}
	if len(result.Findings) == 0 {
		return text, nil
	}
	*changed = true
	var out strings.Builder
	for i := 0; i < len(text); {
		if !masked[i] {
			out.WriteByte(text[i])
			i++
			continue
		}
		out.WriteString("[REDACTED]")
		for i < len(text) && masked[i] {
			i++
		}
	}
	return out.String(), nil
}

func (s *ChatSearchService) redactJSON(ctx context.Context, value any, depth int, changed *bool) (any, error) {
	if depth > chatRedactMaxDepth {
		*changed = true
		return "[WITHHELD]", nil
	}
	switch typed := value.(type) {
	case map[string]any:
		if chatAttachmentPart(typed) {
			*changed = true
			return "[WITHHELD_ATTACHMENT]", nil
		}
		sanitized := make(map[string]any, len(typed))
		for key, item := range typed {
			safeKey, err := s.redactPlainText(ctx, key, changed)
			if err != nil {
				return nil, err
			}
			if sensitiveChatKey(key, item) {
				*changed = true
				sanitized[safeKey] = "[REDACTED]"
				continue
			}
			safe, err := s.redactJSON(ctx, item, depth+1, changed)
			if err != nil {
				return nil, err
			}
			sanitized[safeKey] = safe
		}
		return sanitized, nil
	case []any:
		for i, item := range typed {
			safe, err := s.redactJSON(ctx, item, depth+1, changed)
			if err != nil {
				return nil, err
			}
			typed[i] = safe
		}
	case string:
		return s.redactValue(ctx, typed, depth, changed)
	}
	return value, nil
}

// sensitiveChatKey reports a credential-like key. Numeric token counters such
// as prompt_tokens are usage data, not credentials.
func sensitiveChatKey(key string, value any) bool {
	normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "", ".", "").Replace(key))
	if _, numeric := value.(json.Number); numeric {
		switch normalized {
		case "maxtokens", "maxcompletiontokens", "prompttokens", "completiontokens", "totaltokens", "inputtokens", "outputtokens", "cachedtokens", "cachereadinputtokens", "cachecreationinputtokens":
			return false
		}
	}
	for _, name := range []string{"password", "secret", "token", "authorization", "cookie", "apikey", "privatekey"} {
		if strings.Contains(normalized, name) {
			return true
		}
	}
	return false
}

// chatAttachmentPart reports an inline attachment content part. A type label
// alone can describe ordinary tool arguments, such as a file deletion, so a
// content body or locator key is required as well.
func chatAttachmentPart(value map[string]any) bool {
	kind, _ := value["type"].(string)
	var payloadKeys []string
	switch kind {
	case "image":
		payloadKeys = []string{"source", "data", "url"}
	case "image_url":
		payloadKeys = []string{"image_url"}
	case "input_image":
		payloadKeys = []string{"image_url", "file_id"}
	case "image_file":
		payloadKeys = []string{"image_file", "file_id"}
	case "file", "input_file":
		payloadKeys = []string{"file", "file_id", "file_data", "file_url"}
	case "document":
		payloadKeys = []string{"source"}
	case "audio":
		payloadKeys = []string{"source", "data", "url"}
	case "input_audio":
		payloadKeys = []string{"input_audio"}
	case "video":
		payloadKeys = []string{"source", "data", "video_url"}
	default:
		return false
	}
	for _, key := range payloadKeys {
		if _, exists := value[key]; exists {
			return true
		}
	}
	return false
}
