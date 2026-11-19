package mcpfeedback

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// The length bounds of a report's fields, in characters. They are the bounds
// of the reference tool this one follows, and the storage columns carry the
// same ones: long enough for a few sentences, short enough that a report
// stays something a person reads in one pass.
const (
	// MaxContentLength bounds the report itself.
	MaxContentLength = 2000

	// MaxWorkaroundLength bounds what the agent did instead.
	MaxWorkaroundLength = 1000

	// MaxIntentionLength bounds what the agent was trying to do.
	MaxIntentionLength = 1000

	// MaxReportedToolNameLength bounds the name of the tool a report is about.
	MaxReportedToolNameLength = 100

	// MaxModelLength bounds the model and host product an agent names.
	MaxModelLength = 200
)

// Severity is the impact a problem had on the agent's task.
type Severity string

const (
	// SeverityUnspecified is a report whose agent gave no severity.
	SeverityUnspecified Severity = ""

	// SeverityBlocked means the agent could not complete its task.
	SeverityBlocked Severity = "blocked"

	// SeverityDegraded means the agent needed a slower or awkward path.
	SeverityDegraded Severity = "degraded"

	// SeverityCosmetic means the task worked but was confusing.
	SeverityCosmetic Severity = "cosmetic"
)

// ParseSeverity reads a severity an agent or an API caller sent. An empty
// value is SeverityUnspecified.
func ParseSeverity(value string) (Severity, bool) {
	switch severity := Severity(strings.ToLower(strings.TrimSpace(value))); severity {
	case SeverityUnspecified, SeverityBlocked, SeverityDegraded, SeverityCosmetic:
		return severity, true
	default:
		return SeverityUnspecified, false
	}
}

// Report is one piece of feedback as the agent wrote it, after validation.
type Report struct {
	// Content is the report itself. It is never empty.
	Content string

	// Severity is the impact on the agent's task.
	Severity Severity

	// Workaround is what the agent did instead.
	Workaround string

	// Intention is what the agent was trying to do.
	Intention string

	// ToolName is the tool the agent says the report is about. Nothing
	// checks that the server has a tool by that name.
	ToolName string

	// Model is the model and host product as the agent reported them.
	Model string
}

// InvalidReportError is why a report was refused. Its message is written for
// the agent that sent the report, which can correct it and send it again.
type InvalidReportError struct {
	// Message says what to change.
	Message string
}

func (e *InvalidReportError) Error() string {
	return "invalid feedback report: " + e.Message
}

// reportArguments is the tool's arguments as they arrive. Each field is a
// pointer so an absent field and an empty one can be told apart, and unknown
// fields are ignored: an agent that adds one should not lose its report.
type reportArguments struct {
	Content    *string `json:"content"`
	Severity   *string `json:"severity"`
	Workaround *string `json:"workaround"`
	Intention  *string `json:"intention"`
	Tool       *string `json:"tool"`
	Model      *string `json:"model"`
}

// ParseReport validates a feedback tool call's arguments. A failure is an
// [*InvalidReportError].
func ParseReport(arguments json.RawMessage) (Report, error) {
	var empty Report

	trimmed := bytes.TrimSpace(arguments)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return empty, &InvalidReportError{Message: "arguments must be an object with a content field"}
	}

	var args reportArguments
	if err := json.Unmarshal(trimmed, &args); err != nil {
		return empty, &InvalidReportError{Message: "every field must be a string"}
	}

	content, err := reportField("content", args.Content, MaxContentLength)
	if err != nil {
		return empty, err
	}
	if content == "" {
		return empty, &InvalidReportError{Message: "content is required"}
	}

	severity := SeverityUnspecified
	if args.Severity != nil {
		parsed, ok := ParseSeverity(*args.Severity)
		if !ok {
			return empty, &InvalidReportError{Message: fmt.Sprintf("severity must be %s, %s or %s", SeverityBlocked, SeverityDegraded, SeverityCosmetic)}
		}
		severity = parsed
	}

	workaround, err := reportField("workaround", args.Workaround, MaxWorkaroundLength)
	if err != nil {
		return empty, err
	}
	intention, err := reportField("intention", args.Intention, MaxIntentionLength)
	if err != nil {
		return empty, err
	}
	toolName, err := reportField("tool", args.Tool, MaxReportedToolNameLength)
	if err != nil {
		return empty, err
	}
	model, err := reportField("model", args.Model, MaxModelLength)
	if err != nil {
		return empty, err
	}

	return Report{
		Content:    content,
		Severity:   severity,
		Workaround: workaround,
		Intention:  intention,
		ToolName:   toolName,
		Model:      model,
	}, nil
}

// reportField cleans one free-text field and checks its length. An absent
// field is empty.
func reportField(name string, value *string, maxLength int) (string, error) {
	if value == nil {
		return "", nil
	}

	cleaned := cleanText(*value)
	if utf8.RuneCountInString(cleaned) > maxLength {
		return "", &InvalidReportError{Message: fmt.Sprintf("%s must be at most %d characters", name, maxLength)}
	}

	return cleaned, nil
}

// cleanText drops what a text column cannot hold or a reader should not get:
// invalid UTF-8, and control characters other than newlines and tabs. It then
// trims surrounding space.
func cleanText(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20 || r == 0x7f:
			return -1
		default:
			return r
		}
	}, strings.ToValidUTF8(value, ""))

	return strings.TrimSpace(cleaned)
}
