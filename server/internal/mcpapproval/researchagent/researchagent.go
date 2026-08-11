// Package researchagent runs the MCP approval workflow's research agent: a
// bounded tool-calling loop over the research web tools, followed by a
// schema-held extraction pass that turns the agent's cited prose into the
// structured report stored on mcp_research_reports.
//
// The agent gathers and cites; it never adjudicates. Its entire input surface
// beyond the deterministic briefing is untrusted web content, and the system
// prompt pins that posture: fetched content is data, never instructions, and
// a page that tries to manipulate the review is itself a finding.
//
// The loop runs in-process rather than on the assistants runtime: the tools
// are plain executors from platformtools/research, so a research run is a
// sequence of completions and local tool calls with nothing to host. The
// assistants runtime remains the right home if research ever becomes a
// long-lived conversation rather than a one-shot run.
package researchagent

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

//go:embed prompt.txt
var systemPrompt string

// PromptVersion identifies the research prompt a run used, stored on the
// report so reports stay distinguishable across prompt changes. Bump on any
// change to prompt.txt or the extraction instructions.
const PromptVersion = "3"

// Model is the completion model the research loop runs on.
const Model = "anthropic/claude-sonnet-5"

// extractionModel turns the transcript into the structured report. A model
// with native structured outputs: on routes without them, OpenRouter's
// schema shim plus its response-healing layer can "repair" slightly
// malformed output into schema-valid placeholder filler — a real run had
// its entire report stuffed into the summary string that way.
const extractionModel = "openai/gpt-5.4-mini"

// maxTurns bounds the agent loop. Hitting it does not fail the run — the
// report is extracted from the transcript as it stands and says so.
const maxTurns = 12

// maxTranscriptChars bounds the transcript handed to the extraction pass.
const maxTranscriptChars = 200_000

// extractionSystemPrompt holds the extraction pass to the report's honesty
// rules; the schema enforces the shape, this enforces the semantics.
const extractionSystemPrompt = `You convert a security-research transcript into a structured report about one MCP server (named in the transcript's briefing). Extract only what the transcript establishes — invent nothing, soften nothing. The report is at most 5 claims, the most decision-relevant first: pick what an administrator deciding on this server most needs to know, and leave the rest out. Never restate facts from the deterministic briefing — the reader already sees those separately; report only what the WEB research established. Include only claims that bear on that server or its vendor: the transcript will also contain other companies' material — security-product marketing, competitor pages — and what those companies say about their own products is irrelevant and must be left out entirely; such a source earns a claim only for what it states about the server under review. Keep the two provenance tiers strictly separate: "independently_reported" only for what third parties wrote about the server or its vendor, "vendor_claim" only for what the SERVER'S OWN vendor says about itself — never another company's self-description. Every claim must carry the URL(s) the transcript cites for it; if the transcript gives no URL for a statement, leave that statement out. The coverage level describes how much INDEPENDENT material exists — vendor material does not count toward it. The summary is a neutral overview, never a recommendation.`

// CompletionProvider is the completion surface the runner needs.
// *openrouter.ChatClient and *chat.Client satisfy it.
type CompletionProvider interface {
	GetCompletion(ctx context.Context, req openrouter.CompletionRequest) (*openrouter.CompletionResponse, error)
	GetObjectCompletion(ctx context.Context, req openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error)
}

// Runner executes research runs.
type Runner struct {
	completions CompletionProvider
	tools       []core.PlatformToolExecutor
}

// New builds a runner over the supplied completion provider and tool
// executors — in production, the two research web tools.
func New(completions CompletionProvider, tools ...core.PlatformToolExecutor) *Runner {
	return &Runner{completions: completions, tools: tools}
}

// RunInput identifies the run and carries the agent's briefing.
type RunInput struct {
	OrgID     string
	ProjectID uuid.UUID

	// ReportID keys the run: it scopes the fetch tool's per-run budget and
	// names the run in errors.
	ReportID uuid.UUID

	// TargetKind, TargetRaw, and ArtifactRef identify the server under
	// review, as the request records them.
	TargetKind  string
	TargetRaw   string
	ArtifactRef string

	// Evidence is the request's current deterministic evidence document,
	// included verbatim in the briefing so the agent builds on it instead of
	// re-deriving it.
	Evidence json.RawMessage
}

// Run executes one research run: the agent loop, then the extraction pass.
// It returns the encoded report document and its run metadata.
func (r *Runner) Run(ctx context.Context, input RunInput) (json.RawMessage, RunMeta, error) {
	meta := RunMeta{
		Model:                Model,
		PromptVersion:        PromptVersion,
		PromptTokens:         0,
		CompletionTokens:     0,
		Searches:             0,
		Fetches:              0,
		Turns:                0,
		TurnLimitReached:     false,
		DroppedUncitedClaims: 0,
	}

	// The search tool authorizes against the auth context, which a
	// server-initiated run must supply itself.
	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID:  input.OrgID,
		UserID:                "",
		ExternalUserID:        "",
		APIKeyID:              "",
		APIKeyName:            "",
		OrgWidePluginHooksKey: false,
		SessionID:             nil,
		ProjectID:             &input.ProjectID,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               false,
	})

	transcript := &strings.Builder{}
	briefing := r.briefing(input)
	fmt.Fprintf(transcript, "BRIEFING:\n%s\n", briefing)

	messages := []or.ChatMessages{
		or.CreateChatMessagesSystem(or.ChatSystemMessage{
			Role:    or.ChatSystemMessageRoleSystem,
			Content: or.CreateChatSystemMessageContentStr(systemPrompt),
			Name:    nil,
		}),
		or.CreateChatMessagesUser(or.ChatUserMessage{
			Role:    or.ChatUserMessageRoleUser,
			Content: or.CreateChatUserMessageContentStr(briefing),
			Name:    nil,
		}),
	}

	temperature := 0.2
	toolSuccesses := 0
	lastToolError := ""
	wrappingUp := false
	for {
		if meta.Turns == maxTurns && !wrappingUp {
			// The budget is gone while the agent is still gathering. Without
			// a wrap-up the transcript ends on a raw tool dump with no
			// findings in it, and the extraction pass has nothing of the
			// agent's to extract — that is how a run of real research once
			// produced a placeholder report. One final tool-less turn makes
			// the agent state what it found.
			meta.TurnLimitReached = true
			wrappingUp = true
			messages = append(messages, or.CreateChatMessagesUser(or.ChatUserMessage{
				Role:    or.ChatUserMessageRoleUser,
				Content: or.CreateChatUserMessageContentStr("Your research budget is exhausted. Stop researching and write your findings now as plain prose, with the URL after every web-sourced claim and uncertainty stated plainly."),
				Name:    nil,
			}))
		}
		meta.Turns++

		// The wrap-up turn carries no tools: its one job is stating findings.
		tools := r.toolDefinitions()
		if wrappingUp {
			tools = nil
		}

		response, err := r.completions.GetCompletion(ctx, openrouter.CompletionRequest{
			OrgID:          input.OrgID,
			ProjectID:      input.ProjectID.String(),
			Messages:       messages,
			Tools:          tools,
			Temperature:    &temperature,
			Model:          Model,
			Stream:         false,
			UsageSource:    billing.ModelUsageSourceMCPResearch,
			ChatID:         uuid.Nil,
			UserID:         "",
			ExternalUserID: "",
			UserEmail:      "",
			HTTPMetadata:   nil,
			APIKeyID:       "",
			KeyType:        openrouter.KeyTypeChat,
			KeySlot:        "",
			JSONSchema:     nil,
			Reasoning:      nil,
			CacheControl:   nil,

			NormalizeOutboundMessages: false,
			WebSearch:                 nil,
			DisableResponseHealing:    false,
		})
		if err != nil {
			return nil, meta, fmt.Errorf("research turn %d: %w", meta.Turns, err)
		}

		meta.PromptTokens += int64(response.Usage.PromptTokens)
		meta.CompletionTokens += int64(response.Usage.CompletionTokens)

		if response.Content != "" {
			fmt.Fprintf(transcript, "\nAGENT:\n%s\n", response.Content)
		}

		if wrappingUp || len(response.ToolCalls) == 0 {
			break
		}

		messages = append(messages, assistantToolCallMessage(response))
		for _, call := range response.ToolCalls {
			result, ok := r.executeTool(ctx, input.ReportID, call, &meta)
			if ok {
				toolSuccesses++
			} else {
				lastToolError = result
			}
			fmt.Fprintf(transcript, "\nTOOL %s(%s):\n%s\n", call.Function.Name, call.Function.Arguments, result)
			messages = append(messages, or.CreateChatMessagesTool(or.ChatToolMessage{
				Role:       or.ChatToolMessageRoleTool,
				Content:    or.CreateChatToolMessageContentStr(result),
				ToolCallID: call.ID,
			}))
		}
	}

	// A run whose tools never once succeeded gathered nothing: extracting a
	// report from a transcript of errors produces confident-looking filler.
	// Fail honestly instead — the report row carries the reason and the
	// admin re-runs once the cause is fixed.
	if toolSuccesses == 0 && lastToolError != "" {
		return nil, meta, fmt.Errorf("every research tool call failed; last failure: %s", lastToolError)
	}

	document, err := r.extract(ctx, input, transcript.String(), &meta)
	if err != nil {
		return nil, meta, err
	}

	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, meta, fmt.Errorf("encode research report: %w", err)
	}

	return encoded, meta, nil
}

// briefing renders the agent's starting context: the server under review and
// the deterministic evidence already gathered.
func (r *Runner) briefing(input RunInput) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "Server under review: %s\n", input.TargetRaw)
	fmt.Fprintf(b, "Reference kind: %s\n", input.TargetKind)
	if input.ArtifactRef != "" {
		fmt.Fprintf(b, "Resolved artifact: %s\n", input.ArtifactRef)
	}

	evidence := strings.TrimSpace(string(input.Evidence))
	if evidence == "" || evidence == "{}" || evidence == "null" {
		b.WriteString("\nDeterministic evidence: none gathered — treat every deterministic signal as unknown.\n")
	} else {
		fmt.Fprintf(b, "\nDeterministic evidence already gathered (JSON):\n%s\n", evidence)
	}

	b.WriteString("\nResearch this server's vendor and public track record, then report your cited findings.")

	return b.String()
}

// toolDefinitions renders the executors' descriptors as completion tools.
func (r *Runner) toolDefinitions() []openrouter.Tool {
	tools := make([]openrouter.Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		descriptor := tool.Descriptor()
		tools = append(tools, openrouter.Tool{
			Type: "function",
			Function: &openrouter.FunctionDefinition{
				Name:        descriptor.Name,
				Description: descriptor.Description,
				Parameters:  json.RawMessage(descriptor.InputSchema),
			},
			CacheControl: nil,
		})
	}

	return tools
}

// executeTool runs one tool call, reporting whether it succeeded. A failing
// tool returns its error text as the tool result so the agent can adapt —
// mirroring how MCP surfaces tool errors to models — rather than failing the
// whole run.
func (r *Runner) executeTool(ctx context.Context, reportID uuid.UUID, call openrouter.ToolCall, meta *RunMeta) (string, bool) {
	for _, tool := range r.tools {
		descriptor := tool.Descriptor()
		if descriptor.Name != call.Function.Name {
			continue
		}

		switch descriptor.HandlerName {
		case "web_search":
			meta.Searches++
		case "fetch_page":
			meta.Fetches++
		}

		var out bytes.Buffer
		env := toolconfig.ToolCallEnv{
			SystemEnv:  nil,
			UserConfig: nil,
			OAuthToken: "",
			GramEmail:  "",
			// The report id keys the fetch tool's per-run budget.
			GramChatID: reportID.String(),
			MCPClient:  toolconfig.MCPClientIdentity{Name: "", Version: "", OAuthClientID: ""},
		}
		if err := tool.Call(ctx, env, strings.NewReader(call.Function.Arguments), &out); err != nil {
			return fmt.Sprintf("tool error: %s", err.Error()), false
		}

		return out.String(), true
	}

	return fmt.Sprintf("unknown tool %q", call.Function.Name), false
}

// extract turns the transcript into the structured report document.
func (r *Runner) extract(ctx context.Context, input RunInput, transcript string, meta *RunMeta) (*Document, error) {
	if len(transcript) > maxTranscriptChars {
		transcript = transcript[:maxTranscriptChars]
	}

	var schema map[string]any
	if err := json.Unmarshal(extractionSchema, &schema); err != nil {
		return nil, fmt.Errorf("decode extraction schema: %w", err)
	}

	strict := true
	temperature := 0.0
	response, err := r.completions.GetObjectCompletion(ctx, openrouter.ObjectCompletionRequest{
		OrgID:        input.OrgID,
		ProjectID:    input.ProjectID.String(),
		Model:        extractionModel,
		SystemPrompt: extractionSystemPrompt,
		Prompt:       transcript,
		Temperature:  &temperature,
		UsageSource:  billing.ModelUsageSourceMCPResearch,
		UserID:       "",

		ExternalUserID: "",
		UserEmail:      "",
		HTTPMetadata:   nil,
		JSONSchema: &or.ChatJSONSchemaConfig{
			Name:        "mcp_research_report",
			Description: nil,
			Schema:      schema,
			Strict:      optionalnullable.From(&strict),
		},
		KeyType:   openrouter.KeyTypeChat,
		KeySlot:   "",
		Reasoning: nil,
		// Healed output is worse than failed output here: validation must
		// see the malformed original, not schema-valid filler.
		DisableResponseHealing: true,
	})
	if err != nil {
		return nil, fmt.Errorf("extract research report: %w", err)
	}

	meta.PromptTokens += int64(response.Usage.PromptTokens)
	meta.CompletionTokens += int64(response.Usage.CompletionTokens)

	document := &Document{
		Summary:  "",
		Coverage: Coverage{Level: "", Note: ""},
		Claims:   nil,
		Run: RunMeta{
			Model:                "",
			PromptVersion:        "",
			PromptTokens:         0,
			CompletionTokens:     0,
			Searches:             0,
			Fetches:              0,
			Turns:                0,
			TurnLimitReached:     false,
			DroppedUncitedClaims: 0,
		},
	}
	if err := json.Unmarshal([]byte(response.Content), document); err != nil {
		return nil, fmt.Errorf("decode extracted report: %w", err)
	}

	dropped, err := document.validate()
	if err != nil {
		return nil, fmt.Errorf("validate extracted report: %w", err)
	}
	meta.DroppedUncitedClaims = dropped
	document.Run = *meta

	return document, nil
}

// assistantToolCallMessage rebuilds the assistant turn for the message
// history, tool calls included.
func assistantToolCallMessage(response *openrouter.CompletionResponse) or.ChatMessages {
	calls := make([]or.ChatToolCall, 0, len(response.ToolCalls))
	for _, call := range response.ToolCalls {
		calls = append(calls, or.ChatToolCall{
			ID:   call.ID,
			Type: or.ChatToolCallType(call.Type),
			Function: or.ChatToolCallFunction{
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			},
		})
	}

	message := or.ChatAssistantMessage{
		Role:             or.ChatAssistantMessageRoleAssistant,
		Content:          nil,
		Name:             nil,
		ToolCalls:        calls,
		Refusal:          nil,
		Reasoning:        nil,
		ReasoningDetails: nil,
		Images:           nil,
		Audio:            nil,
	}
	if response.Content != "" {
		content := or.CreateChatAssistantMessageContentStr(response.Content)
		message.Content = optionalnullable.From(&content)
	}

	return or.CreateChatMessagesAssistant(message)
}
