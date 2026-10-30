// Package chatredact masks secrets in captured chat content before it leaves
// the server: in tool search results, recall digests and generated summaries.
// Every caller that turns transcript text into something another person can
// read goes through the same Redactor so their masking cannot drift apart.
package chatredact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
)

// ErrScanFailed reports that the secret scanner did not complete, so the text
// cannot be shown safely. Callers fail closed.
var ErrScanFailed = errors.New("chat redaction scan failed")

// Scanner detects secrets in plain text.
type Scanner interface {
	Scan(ctx context.Context, content string) (scanners.Result, error)
}

// Redactor masks detected secrets in chat text and JSON payloads.
type Redactor struct {
	scanner Scanner
}

// New returns a Redactor backed by the gitleaks secret scanner.
func New() *Redactor {
	return &Redactor{scanner: gitleaks.NewScanner()}
}

// NewWithScanner returns a Redactor backed by scanner.
func NewWithScanner(scanner Scanner) *Redactor {
	return &Redactor{scanner: scanner}
}

// maxDepth bounds JSON nesting, including JSON encoded inside JSON
// strings such as tool-call arguments. Deeper values are withheld.
const maxDepth = 16

// Message joins a message's redacted content, tool calls and stored
// payload. Each field is redacted whole before any truncation. changed
// reports that something was masked or withheld.
func (r *Redactor) Message(ctx context.Context, content string, toolCalls, raw []byte) (string, bool, error) {
	text, changed, err := r.Text(ctx, content)
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
		safe, hidden, err := r.Text(ctx, string(field.value))
		if err != nil {
			return "", false, err
		}
		text += "\n" + field.label + ": " + safe
		changed = changed || hidden
	}
	return text, changed, nil
}

// Text masks detected secrets in plain text or JSON.
func (r *Redactor) Text(ctx context.Context, text string) (string, bool, error) {
	changed := false
	safe, err := r.redactValue(ctx, text, 0, &changed)
	return safe, changed, err
}

func (r *Redactor) redactValue(ctx context.Context, text string, depth int, changed *bool) (string, error) {
	if text == "" {
		return "", nil
	}
	if depth > maxDepth {
		*changed = true
		return "[WITHHELD]", nil
	}
	value, ok := decodeJSON(text)
	if !ok {
		return r.redactPlainText(ctx, text, changed)
	}
	safe, err := r.redactJSON(ctx, value, depth+1, changed)
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

// decodeJSON decodes JSON objects and arrays with json.Number so large
// identifiers and exponents survive re-encoding unchanged.
func decodeJSON(text string) (any, bool) {
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

func (r *Redactor) redactPlainText(ctx context.Context, text string, changed *bool) (string, error) {
	result, err := r.scanner.Scan(ctx, text)
	if err != nil || !result.Completed {
		return "", ErrScanFailed
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

func (r *Redactor) redactJSON(ctx context.Context, value any, depth int, changed *bool) (any, error) {
	if depth > maxDepth {
		*changed = true
		return "[WITHHELD]", nil
	}
	switch typed := value.(type) {
	case map[string]any:
		if attachmentPart(typed) {
			*changed = true
			return "[WITHHELD_ATTACHMENT]", nil
		}
		sanitized := make(map[string]any, len(typed))
		for key, item := range typed {
			safeKey, err := r.redactPlainText(ctx, key, changed)
			if err != nil {
				return nil, err
			}
			if sensitiveKey(key, item) {
				*changed = true
				sanitized[safeKey] = "[REDACTED]"
				continue
			}
			safe, err := r.redactJSON(ctx, item, depth+1, changed)
			if err != nil {
				return nil, err
			}
			sanitized[safeKey] = safe
		}
		return sanitized, nil
	case []any:
		for i, item := range typed {
			safe, err := r.redactJSON(ctx, item, depth+1, changed)
			if err != nil {
				return nil, err
			}
			typed[i] = safe
		}
	case string:
		return r.redactValue(ctx, typed, depth, changed)
	}
	return value, nil
}

// sensitiveKey reports a credential-like key. Numeric token counters such
// as prompt_tokens are usage data, not credentials.
func sensitiveKey(key string, value any) bool {
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

// attachmentPart reports an inline attachment content part. A type label
// alone can describe ordinary tool arguments, such as a file deletion, so a
// content body or locator key is required as well.
func attachmentPart(value map[string]any) bool {
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
