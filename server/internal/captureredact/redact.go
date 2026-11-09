// Package captureredact removes recognized credentials from captured content
// before it is stored or exported, independently of risk policies.
package captureredact

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
)

const (
	// Replacement hides the entire value when scanning cannot finish safely.
	Replacement = "[redacted]"

	// maxNesting bounds JSON embedded repeatedly within strings and value trees.
	// Ordinary nested JSON containers do not consume this recursion budget.
	maxNesting = 32

	// genericAPIKeyRule is a heuristic over arbitrary assignments. Capture
	// redaction uses credential-specific rules to avoid deleting ordinary IDs.
	genericAPIKeyRule = "secret.generic_api_key"
)

// Scanner detects credential spans in the complete input, without truncation.
type Scanner interface {
	Scan(context.Context, string) (scanners.Result, error)
}

// Redactor reuses a credential scanner across captures. It is safe for concurrent
// calls when its Scanner is safe for concurrent calls.
type Redactor struct {
	// scanner reports byte offsets into the original text.
	scanner Scanner
}

// Report describes removed credentials without retaining matched content.
type Report struct {
	// Findings contains one location per validated detector match, before overlaps
	// are merged for replacement. Its length is the detection count.
	Findings []Finding `json:"findings"`

	// Incomplete means some content was fully redacted because it could not be
	// inspected safely. Findings may then describe only part of the capture.
	Incomplete bool `json:"incomplete"`
}

// Finding locates a detector match without storing its raw value or fingerprint.
type Finding struct {
	// RuleID is the credential-specific detector's stable rule identifier.
	RuleID string `json:"rule_id"`

	// StartPos is an inclusive byte offset within the decoded string surface.
	StartPos int `json:"start_pos"`

	// EndPos is an exclusive byte offset within the decoded string surface.
	EndPos int `json:"end_pos"`

	// Path identifies the scanned string by traversal order as $strings[N]. It
	// contains no caller-controlled object key, and is not a JSON pointer.
	Path string `json:"path"`
}

// New creates a redactor with a warm, concurrency-safe Gitleaks scanner. An
// initialization failure leaves it closed: subsequent text is fully redacted.
func New() *Redactor {
	scanner := gitleaks.NewScanner()
	if err := scanner.Prime(); err != nil {
		return &Redactor{scanner: nil}
	}
	return NewWithScanner(scanner)
}

// NewWithScanner creates a redactor with an injected scanner. A nil scanner
// redacts every nonempty text value.
func NewWithScanner(scanner Scanner) *Redactor {
	return &Redactor{scanner: scanner}
}

var shared = sync.OnceValue(New)

// RedactsRule reports whether capture redaction removes matches from a detector
// rule. Generic API-key heuristics remain available to asynchronous risk scans.
func RedactsRule(ruleID string) bool {
	return ruleID != genericAPIKeyRule
}

// RedactText removes recognized credentials using the shared warm scanner.
func RedactText(ctx context.Context, text string) string {
	return shared().RedactText(ctx, text)
}

// RedactJSON removes credentials from JSON using the shared warm scanner.
func RedactJSON(ctx context.Context, data []byte) []byte {
	return shared().RedactJSON(ctx, data)
}

// RedactJSONWithReport removes credentials and returns content-free metadata.
func RedactJSONWithReport(ctx context.Context, data []byte) ([]byte, Report) {
	return shared().RedactJSONWithReport(ctx, data)
}

// RedactValue copies and redacts a JSON-like value using the shared warm scanner.
func RedactValue(ctx context.Context, value any) any {
	return shared().RedactValue(ctx, value)
}

// RedactText removes credential-specific Gitleaks matches. It scans the complete
// text and decodes JSON string escapes when the text is a JSON document. An
// error, incomplete scan or invalid span replaces the whole text. Generic
// API-key heuristics are excluded; encoded binary content is not decoded.
func (r *Redactor) RedactText(ctx context.Context, text string) string {
	op := newOperation(r, text)
	return op.redactText(ctx, text, 0, nil, nil)
}

// RedactJSON copies JSON and redacts decoded string values and keys, including
// JSON documents embedded in strings. Unchanged bytes, numeric lexemes, object
// order and whitespace are preserved. Malformed JSON becomes "[redacted]".
func (r *Redactor) RedactJSON(ctx context.Context, data []byte) []byte {
	if data == nil {
		return nil
	}
	op := newOperation(r, string(data))
	return op.redactJSON(ctx, data, 0, nil, nil)
}

// RedactJSONWithReport returns a redacted copy plus safe detector locations.
// Location offsets index decoded string surfaces, not the JSON byte stream.
func (r *Redactor) RedactJSONWithReport(ctx context.Context, data []byte) ([]byte, Report) {
	op := newOperation(r, string(data))
	if data == nil {
		return nil, op.report
	}
	redacted := op.redactJSON(ctx, data, 0, nil, nil)
	return redacted, op.report
}

func (r *Redactor) redactJSON(ctx context.Context, data []byte, depth int) []byte {
	op := newOperation(r, string(data))
	return op.redactJSON(ctx, data, depth, nil, nil)
}

// RedactValue copies JSON-like maps, slices, strings, byte slices and raw JSON.
// Primitive numeric and boolean values keep their types. Unsupported values are
// replaced rather than passing potentially secret-bearing structures through.
func (r *Redactor) RedactValue(ctx context.Context, value any) any {
	return r.redactValue(ctx, value, 0)
}

func (r *Redactor) redactValue(ctx context.Context, value any, depth int) any {
	if depth > maxNesting || ctx.Err() != nil {
		return Replacement
	}
	switch value := value.(type) {
	case nil, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return value
	case string:
		return r.RedactText(ctx, value)
	case json.RawMessage:
		return json.RawMessage(r.RedactJSON(ctx, value))
	case []byte:
		if value == nil {
			return []byte(nil)
		}
		if json.Valid(value) {
			return r.RedactJSON(ctx, value)
		}
		return []byte(r.RedactText(ctx, string(value)))
	case map[string]any:
		if value == nil {
			return map[string]any(nil)
		}
		output := make(map[string]any, len(value))
		for key, item := range value {
			output[r.RedactText(ctx, key)] = r.redactValue(ctx, item, depth+1)
		}
		return output
	case map[string]string:
		if value == nil {
			return map[string]string(nil)
		}
		output := make(map[string]string, len(value))
		for key, item := range value {
			output[r.RedactText(ctx, key)] = r.RedactText(ctx, item)
		}
		return output
	case []any:
		if value == nil {
			return []any(nil)
		}
		output := make([]any, len(value))
		for i, item := range value {
			output[i] = r.redactValue(ctx, item, depth+1)
		}
		return output
	case []string:
		if value == nil {
			return []string(nil)
		}
		output := make([]string, len(value))
		for i, item := range value {
			output[i] = r.RedactText(ctx, item)
		}
		return output
	default:
		return Replacement
	}
}
