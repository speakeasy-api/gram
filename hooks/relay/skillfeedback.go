package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
	"github.com/speakeasy-api/gram/hooks/sdk/models/operations"
)

// skillFeedbackBudget bounds one feedback submission end to end. The agent is
// blocked on the tool call while this runs, so the budget stays short.
const skillFeedbackBudget = 15 * time.Second

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type skillFeedbackInput struct {
	Skill   string `json:"skill" jsonschema:"Canonical name of the skill that was used."`
	Outcome string `json:"outcome" jsonschema:"How the skill affected the task."`
	Note    string `json:"note,omitempty" jsonschema:"Optional concise context about the outcome."`
}

// skillFeedbackInputSchema mirrors the assistant-side skill feedback tool
// schema so agents see identical constraints on every surface.
func skillFeedbackInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[skillFeedbackInput](nil)
	if err != nil {
		panic(fmt.Errorf("build skill feedback schema: %w", err))
	}
	minSkill, maxSkill, maxNote := 1, 64, 4000
	schema.Properties["skill"].MinLength = &minSkill
	schema.Properties["skill"].MaxLength = &maxSkill
	schema.Properties["skill"].Pattern = skillNamePattern.String()
	schema.Properties["outcome"].Enum = []any{
		string(components.OutcomeHelped),
		string(components.OutcomePartiallyHelped),
		string(components.OutcomeDidNotHelp),
		string(components.OutcomeMisleading),
		string(components.OutcomeHarmful),
	}
	schema.Properties["note"].MaxLength = &maxNote
	return schema
}

// RunSkillFeedbackMCP serves the speakeasy-skill-feedback MCP server over
// stdio. Generated plugin packages point their .mcp.json at this subcommand
// through the bootstrap script, so the agent-facing feedback tool authenticates
// with the same credential chain as hook ingestion instead of a key baked into
// the MCP config.
func RunSkillFeedbackMCP(ctx context.Context, cfg Config) error {
	if cfg.ConfigError != "" {
		return fmt.Errorf("load plugin config: %s", cfg.ConfigError)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "speakeasy-skill-feedback", Version: BinaryVersion}, &mcp.ServerOptions{
		// Initialize-time instructions are the discovery nudge: harnesses
		// surface them in model context even before the tool schema loads.
		Instructions: "After materially relying on a distributed skill while completing a task, call skill_feedback with that skill's name and how it affected the task. One call per skill per task.",
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "skill_feedback",
		Description: "Record feedback only after materially relying on a skill while completing a task.",
		InputSchema: skillFeedbackInputSchema(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input skillFeedbackInput) (*mcp.CallToolResult, any, error) {
		if err := submitSkillFeedback(ctx, cfg, input); err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: `{"recorded":true}`}}}, nil, nil
	})

	// Harnesses terminate stdio servers by closing stdin, which can surface
	// as an EOF error when it races a pending read — that is a clean shutdown.
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("serve skill feedback mcp: %w", err)
	}
	return nil
}

func submitSkillFeedback(ctx context.Context, cfg Config, input skillFeedbackInput) error {
	switch {
	case len(input.Skill) > 64 || !skillNamePattern.MatchString(input.Skill):
		return errors.New("skill must be a canonical 1-64 character skill name")
	case utf8.RuneCountInString(input.Note) > 4000:
		return errors.New("note must be at most 4000 Unicode characters")
	}
	outcome := components.Outcome(input.Outcome)
	switch outcome {
	case components.OutcomeHelped, components.OutcomePartiallyHelped, components.OutcomeDidNotHelp, components.OutcomeMisleading, components.OutcomeHarmful:
	default:
		return errors.New("invalid feedback outcome")
	}

	c, ok := resolveAuth(cfg)
	if !ok {
		return errors.New("no hooks credential on this machine; sign in with the plugin's speakeasy-hooks login command")
	}

	ctx, cancel := context.WithTimeout(ctx, skillFeedbackBudget)
	defer cancel()

	body := components.SkillFeedbackPayload{
		SchemaVersion: components.SkillFeedbackPayloadSchemaVersionHookSkillFeedbackV1,
		Skill:         input.Skill,
		Outcome:       outcome,
		Note:          nil,
	}
	if input.Note != "" {
		body.Note = &input.Note
	}
	request := operations.SkillFeedbackRequest{GramKey: nil, GramProject: nil, Body: body}
	security := &operations.SkillFeedbackSecurity{
		ApikeyHeaderGramKey:          &c.APIKey,
		ProjectSlugHeaderGramProject: nil,
	}
	if c.Project != "" {
		security.ProjectSlugHeaderGramProject = &c.Project
	}

	cl := newClient(c.ServerURL)
	var response *operations.SkillFeedbackResponse
	var err error
	for attempt := 0; ; attempt++ {
		response, err = cl.sdk.Hooks.SkillFeedback(ctx, request, security)
		if err == nil {
			break
		}
		if interpretError(err).statusCode != 0 || attempt >= 2 || ctx.Err() != nil {
			return fmt.Errorf("record skill feedback: %w", err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("record skill feedback: %w", err)
		case <-time.After(time.Duration(attempt+1) * 250 * time.Millisecond):
		}
	}
	if response == nil || response.StatusCode != http.StatusNoContent {
		return errors.New("unexpected skill feedback response status")
	}
	return nil
}
