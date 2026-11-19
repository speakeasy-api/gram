// Package mcpfeedback is the feedback tool an MCP server can offer the agents
// that use it: what the tool looks like, what a report must look like, where
// reports are stored, and how each one leaves as a webhook event and an
// exported log record.
//
// The gateway owns the tool. It lists it next to the server's own tools and
// answers calls to it itself, so the tool works the same on a hosted server
// and on one Speakeasy proxies, and a server offers it by switching it on.
package mcpfeedback

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpfeedback/repo"
)

const (
	// DefaultToolName is the name the tool is listed under until an admin
	// chooses another.
	DefaultToolName = "submit_feedback"

	// ToolTitle is the tool's display name.
	ToolTitle = "Submit feedback"

	// MaxToolNameLength bounds a custom tool name. MCP clients commonly
	// reject names over 64 characters.
	MaxToolNameLength = 64

	// MaxToolDescriptionLength bounds a custom description. Every session
	// that lists the server's tools pays for it in context, and the default
	// is about a third of this.
	MaxToolDescriptionLength = 2000
)

// DefaultToolDescription is what agents read about the tool until an admin
// writes their own. It asks for the kind of report a server's owner can act
// on, and tells the agent what happens to it.
const DefaultToolDescription = "Report a concrete problem or friction with this MCP server or its tools for the team that runs it to review: " +
	"an incorrect result, a confusing error, a documentation gap, or a missing tool. " +
	"Positive feedback about what works well is welcome too. " +
	"The team reads these directly, so keep each field brief and plain. " +
	"Write in neutral, professional language, and describe the user's frustration rather than quoting it. " +
	"Returns a receipt only; no follow-up will reach this session. " +
	"Submit only real observations, one per call. " +
	"Each submission is recorded with the signed-in user's identity when there is one. " +
	"Never include credentials, secrets, or data returned by tools."

// reportSchema is the JSON Schema of a report. It is a struct rather than a
// map so the properties keep the order an agent should read them in.
type reportSchema struct {
	Type       string                 `json:"type"`
	Properties reportSchemaProperties `json:"properties"`
	Required   []string               `json:"required"`
}

type reportSchemaProperties struct {
	Content    reportSchemaProperty `json:"content"`
	Severity   reportSchemaProperty `json:"severity"`
	Workaround reportSchemaProperty `json:"workaround"`
	Intention  reportSchemaProperty `json:"intention"`
	Tool       reportSchemaProperty `json:"tool"`
	Model      reportSchemaProperty `json:"model"`
}

type reportSchemaProperty struct {
	Type        string   `json:"type"`
	MinLength   int      `json:"minLength,omitempty"`
	MaxLength   int      `json:"maxLength,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Description string   `json:"description"`
}

// toolInputSchema is built from the limits ParseReport enforces, so what the
// tool advertises and what it accepts cannot drift apart.
var toolInputSchema = mustMarshalReportSchema()

func mustMarshalReportSchema() json.RawMessage {
	schema := reportSchema{
		Type: "object",
		Properties: reportSchemaProperties{
			Content: reportSchemaProperty{
				Type:        "string",
				MinLength:   1,
				MaxLength:   MaxContentLength,
				Enum:        nil,
				Description: "A short note the team can understand in one read. Lead with the problem or question in one plain sentence, say what you expected, and include only the names or IDs needed to reproduce it.",
			},
			Severity: reportSchemaProperty{
				Type:        "string",
				MinLength:   0,
				MaxLength:   0,
				Enum:        []string{string(SeverityBlocked), string(SeverityDegraded), string(SeverityCosmetic)},
				Description: "Impact on your task: blocked means you could not complete it, degraded means you needed a slower or awkward path, cosmetic means it worked but was confusing",
			},
			Workaround: reportSchemaProperty{
				Type:        "string",
				MinLength:   0,
				MaxLength:   MaxWorkaroundLength,
				Enum:        nil,
				Description: "What worked instead, in a sentence or two. Omit if you found none",
			},
			Intention: reportSchemaProperty{
				Type:        "string",
				MinLength:   0,
				MaxLength:   MaxIntentionLength,
				Enum:        nil,
				Description: "What you were trying to accomplish when this came up, in one short sentence",
			},
			Tool: reportSchemaProperty{
				Type:        "string",
				MinLength:   0,
				MaxLength:   MaxReportedToolNameLength,
				Enum:        nil,
				Description: "The affected tool's exact name, if a single tool is involved",
			},
			Model: reportSchemaProperty{
				Type:        "string",
				MinLength:   0,
				MaxLength:   MaxModelLength,
				Enum:        nil,
				Description: "Your model and host product, if known",
			},
		},
		Required: []string{"content"},
	}

	encoded, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Errorf("marshal feedback tool input schema: %w", err))
	}

	return encoded
}

// toolNamePattern is the set of names every MCP client accepts for a tool.
var toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Tool is the feedback tool as one MCP server lists it.
type Tool struct {
	// Name is the name agents call the tool by.
	Name string

	// Title is the tool's display name.
	Title string

	// Description is what agents read to decide when and how to call it.
	Description string

	// InputSchema is the JSON Schema of a report.
	InputSchema json.RawMessage
}

// Settings is what an admin chose for one server's feedback tool.
type Settings struct {
	// Enabled reports whether the server offers the tool.
	Enabled bool

	// ToolName replaces DefaultToolName when it is not empty.
	ToolName string

	// ToolDescription replaces DefaultToolDescription when it is not empty.
	ToolDescription string
}

// SettingsFromConfig reads the settings stored for a server.
func SettingsFromConfig(config repo.McpServerFeedbackConfig) Settings {
	return Settings{
		Enabled:         config.Enabled,
		ToolName:        conv.FromPGTextOrEmpty[string](config.ToolName),
		ToolDescription: conv.FromPGTextOrEmpty[string](config.ToolDescription),
	}
}

// Tool is the tool these settings list, with the defaults filled in for
// whatever the admin left alone.
func (s Settings) Tool() Tool {
	return Tool{
		Name:        conv.Default(s.ToolName, DefaultToolName),
		Title:       ToolTitle,
		Description: conv.Default(s.ToolDescription, DefaultToolDescription),
		InputSchema: toolInputSchema,
	}
}

// Validate reports the first reason these settings cannot be stored, worded
// for the admin who entered them.
func (s Settings) Validate() error {
	if s.ToolName != "" {
		if utf8.RuneCountInString(s.ToolName) > MaxToolNameLength {
			return fmt.Errorf("tool name must be at most %d characters", MaxToolNameLength)
		}
		if !toolNamePattern.MatchString(s.ToolName) {
			return errors.New("tool name may only contain letters, digits, underscores and hyphens")
		}
	}

	if utf8.RuneCountInString(s.ToolDescription) > MaxToolDescriptionLength {
		return fmt.Errorf("tool description must be at most %d characters", MaxToolDescriptionLength)
	}

	return nil
}
