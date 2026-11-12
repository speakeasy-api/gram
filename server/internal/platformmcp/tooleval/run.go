package tooleval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	// maxAgentTurns bounds model calls per case. An agent that has not reached
	// a Platform tool by then counts as reaching none.
	maxAgentTurns = 8

	// caseTimeout bounds one case's whole agent loop. Running out of it
	// counts as reaching no tool.
	caseTimeout = 3 * time.Minute

	// stubResult answers an orientation read, so the agent moves on to the tool
	// that serves the request. Tools never run in this eval.
	stubResult = `{"eval_note":"This read succeeded. The resources it returned are the ones listed in your instructions; continue with the user's request."}`
)

// The eval bills to fixed placeholder IDs, like the memory eval.
var (
	evalOrgID     = uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://speakeasy.com/evals/platform-tools/org")).String()
	evalProjectID = uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://speakeasy.com/evals/platform-tools/project")).String()
)

// Tool is one tool as a client lists it.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	ReadOnly    bool            `json:"read_only"`
	// Annotations are the tool's MCP hints, as a client lists them.
	Annotations json.RawMessage `json:"annotations,omitempty"`
}

// Call is a Platform tool the agent chose and the arguments it sent.
type Call struct {
	Tool      string
	Arguments json.RawMessage
}

// Dispatch is the surface's answer to one model tool call. A discovery call
// returns Output; a call that reaches a Platform tool returns Selected.
type Dispatch struct {
	Output   string
	Selected *Call
}

// Surface is one way a client sees the Platform MCP.
type Surface struct {
	Name         string
	Instructions string
	Tools        []Tool
	Dispatch     func(ctx context.Context, name string, arguments json.RawMessage) (Dispatch, error)

	// Omitted are catalogue tools the surface cannot list, such as tools
	// whose schema a model provider rejects. A case whose every expected tool
	// is omitted is skipped on this surface rather than scored as a miss.
	Omitted []string
}

// skips reports whether a case cannot be answered on the surface.
func (s Surface) skips(c Case) bool {
	if c.Negative() || len(s.Omitted) == 0 {
		return false
	}
	for _, tool := range c.Expect {
		if !slices.Contains(s.Omitted, tool) {
			return false
		}
	}
	return true
}

// Config is what one eval run varies.
type Config struct {
	Models      []string
	Runs        int
	Concurrency int
}

// Validate rejects a config the run cannot use.
func (c Config) Validate() error {
	if len(c.Models) == 0 {
		return errors.New("at least one model is required")
	}
	if c.Runs < 1 || c.Concurrency < 1 {
		return errors.New("runs and concurrency must be positive")
	}
	for i, model := range c.Models {
		switch {
		case !openrouter.IsModelAllowed(model):
			return fmt.Errorf("model %q is not allowed", model)
		case slices.Contains(c.Models[:i], model):
			return fmt.Errorf("model %q is listed twice", model)
		}
	}
	return nil
}

// Outcome is one agent's attempt at one case.
type Outcome struct {
	Model   string
	Surface string
	Run     int
	Case    Case

	// Selected is the first Platform tool call that was not an orientation read,
	// or nil when the agent answered without one.
	Selected *Call
	// Reads are the orientation reads the agent made first.
	Reads []string
	// Answer is the agent's final text when it stopped without a Platform
	// tool, such as a refusal or a question back to the user.
	Answer string

	// Hit is an expected tool reached first; OnPath is one of the case's
	// allowed first steps reached first instead.
	Hit         bool
	OnPath      bool
	SchemaValid bool
	SchemaError string
	ArgsMatch   bool
	// ConfirmedFirst is a write sent with confirmed true on its first call,
	// skipping the preview every write tool asks for.
	ConfirmedFirst bool

	Turns            int
	PromptTokens     int
	CompletionTokens int
	CostUSD          float64
	Latency          time.Duration
	Err              error
}

// Run asks every case on every surface with every model, Runs times.
func Run(ctx context.Context, client openrouter.CompletionClient, cases []Case, catalog []Tool, surfaces []Surface, cfg Config) (*Report, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	tools := make(map[string]Tool, len(catalog))
	for _, tool := range catalog {
		tools[tool.Name] = tool
	}
	validators, err := compileSchemas(catalog)
	if err != nil {
		return nil, err
	}

	type job struct {
		model   string
		surface Surface
		run     int
		c       Case
	}
	var jobs []job
	skipped := make(map[string][]string)
	for _, surface := range surfaces {
		for _, c := range cases {
			if surface.skips(c) {
				skipped[surface.Name] = append(skipped[surface.Name], c.ID)
			}
		}
	}
	for _, model := range cfg.Models {
		for _, surface := range surfaces {
			for run := range cfg.Runs {
				for _, c := range cases {
					if surface.skips(c) {
						continue
					}
					jobs = append(jobs, job{model: model, surface: surface, run: run, c: c})
				}
			}
		}
	}
	outcomes := make([]Outcome, len(jobs))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(cfg.Concurrency)
	for i, j := range jobs {
		group.Go(func() error {
			outcomes[i] = attempt(groupCtx, client, j.model, j.surface, j.run, j.c, tools, validators)
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, fmt.Errorf("run cases: %w", err)
	}
	if len(outcomes) > 0 && !slices.ContainsFunc(outcomes, func(o Outcome) bool { return o.Err == nil }) {
		return nil, fmt.Errorf("every case errored, first: %w", outcomes[0].Err)
	}
	report := newReport(cfg, surfaces, outcomes)
	for _, surface := range surfaces {
		if len(surface.Omitted) > 0 {
			report.Notes = append(report.Notes, fmt.Sprintf("The %s surface leaves out %d tools whose input schema a model provider rejects: %s. It skips the %d cases that expect only those tools: %s.",
				surface.Name, len(surface.Omitted), strings.Join(surface.Omitted, ", "), len(skipped[surface.Name]), strings.Join(skipped[surface.Name], ", ")))
		}
	}
	return report, nil
}

func compileSchemas(catalog []Tool) (map[string]*jsonschema.Resolved, error) {
	validators := make(map[string]*jsonschema.Resolved, len(catalog))
	for _, tool := range catalog {
		var schema jsonschema.Schema
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			return nil, fmt.Errorf("decode %s input schema: %w", tool.Name, err)
		}
		resolved, err := schema.Resolve(nil)
		if err != nil {
			return nil, fmt.Errorf("resolve %s input schema: %w", tool.Name, err)
		}
		validators[tool.Name] = resolved
	}
	return validators, nil
}

// SystemPrompt is what the agent reads before the user's request.
func SystemPrompt(surface Surface) string {
	return "You are an AI assistant connected to the Speakeasy Platform MCP server. Use its tools to do what the user asks. " +
		"When a request is ambiguous, unsafe, or not something these tools do, say so instead of calling a tool.\n\n" +
		Environment + "\n\n" +
		"Server instructions:\n" + surface.Instructions
}

func attempt(parent context.Context, client openrouter.CompletionClient, model string, surface Surface, run int, c Case, tools map[string]Tool, validators map[string]*jsonschema.Resolved) Outcome {
	ctx, cancel := context.WithTimeout(parent, caseTimeout)
	defer cancel()
	started := time.Now()
	outcome := Outcome{Model: model, Surface: surface.Name, Run: run, Case: c}

	listed := make([]openrouter.Tool, 0, len(surface.Tools))
	for _, tool := range surface.Tools {
		listed = append(listed, openrouter.Tool{Type: "function", Function: &openrouter.FunctionDefinition{Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema}, CacheControl: nil})
	}
	messages := []or.ChatMessages{openrouter.CreateMessageSystem(SystemPrompt(surface)), openrouter.CreateMessageUser(c.Prompt)}

turns:
	for turn := range maxAgentTurns {
		outcome.Turns = turn + 1
		response, err := client.GetCompletion(ctx, completionRequest(model, messages, listed))
		if err != nil {
			// An agent that runs out of time has not reached a tool, like one
			// that runs out of turns: a miss, not an error that drops out of
			// the rates. A cancelled run is still an error.
			if errors.Is(ctx.Err(), context.DeadlineExceeded) && parent.Err() == nil {
				outcome.Answer = fmt.Sprintf("(no tool within %s)", caseTimeout)
				break
			}
			outcome.Err = fmt.Errorf("case %s turn %d: %w", c.ID, turn, err)
			break
		}
		outcome.PromptTokens += response.Usage.PromptTokens
		outcome.CompletionTokens += response.Usage.CompletionTokens
		if response.Usage.Cost != nil {
			outcome.CostUSD += *response.Usage.Cost
		}
		if len(response.ToolCalls) == 0 {
			outcome.Answer = response.Content
			break
		}
		messages = append(messages, assistantToolCallMessage(response))
		for _, call := range response.ToolCalls {
			dispatch, err := surface.Dispatch(ctx, call.Function.Name, json.RawMessage(call.Function.Arguments))
			if err != nil {
				outcome.Err = fmt.Errorf("case %s dispatch %s: %w", c.ID, call.Function.Name, err)
				break turns
			}
			output := dispatch.Output
			if dispatch.Selected != nil {
				if !c.orients(dispatch.Selected.Tool) {
					outcome.Selected = dispatch.Selected
					break turns
				}
				outcome.Reads = append(outcome.Reads, dispatch.Selected.Tool)
				output = stubResult
			}
			messages = append(messages, or.CreateChatMessagesTool(or.ChatToolMessage{
				Role:       or.ChatToolMessageRoleTool,
				Content:    or.CreateChatToolMessageContentStr(output),
				ToolCallID: call.ID,
			}))
		}
	}
	outcome.Latency = time.Since(started)
	score(&outcome, tools, validators)
	return outcome
}

// score fills in the hit and argument checks from the selected call.
func score(o *Outcome, tools map[string]Tool, validators map[string]*jsonschema.Resolved) {
	if o.Err != nil {
		return
	}
	if o.Selected == nil {
		o.Hit = o.Case.Negative()
		return
	}
	o.Hit = slices.Contains(o.Case.Expect, o.Selected.Tool)
	o.OnPath = o.Case.onPath(o.Selected.Tool)
	var arguments map[string]any
	if err := json.Unmarshal(nonEmpty(o.Selected.Arguments), &arguments); err != nil {
		o.SchemaError = "arguments are not a JSON object"
		return
	}
	// Any write sent confirmed on its first call skipped the preview,
	// whether or not it was the right tool.
	if tool, ok := tools[o.Selected.Tool]; ok && !tool.ReadOnly {
		confirmed, _ := arguments["confirmed"].(bool)
		o.ConfirmedFirst = confirmed
	}
	if !o.Hit {
		return
	}
	if validator, ok := validators[o.Selected.Tool]; ok {
		if err := validator.Validate(arguments); err != nil {
			o.SchemaError = err.Error()
		} else {
			o.SchemaValid = true
		}
	}
	o.ArgsMatch = argsMatch(o.Case.Args, nonEmpty(o.Selected.Arguments))
}

func nonEmpty(arguments json.RawMessage) json.RawMessage {
	if strings.TrimSpace(string(arguments)) == "" {
		return json.RawMessage(`{}`)
	}
	return arguments
}

func completionRequest(model string, messages []or.ChatMessages, tools []openrouter.Tool) openrouter.CompletionRequest {
	var cache *or.AnthropicCacheControlDirective
	if strings.HasPrefix(model, "anthropic/") {
		// The direct surface lists every tool, tens of thousands of tokens
		// that repeat on every turn and every case, so cache them.
		cache = &or.AnthropicCacheControlDirective{Type: or.AnthropicCacheControlDirectiveTypeEphemeral, TTL: nil}
	}
	return openrouter.CompletionRequest{
		OrgID:                     evalOrgID,
		Messages:                  messages,
		ProjectID:                 evalProjectID,
		Tools:                     tools,
		Temperature:               nil,
		Model:                     model,
		Stream:                    false,
		MaxTokens:                 nil,
		ToolChoice:                nil,
		UsageSource:               "",
		ChatID:                    uuid.Nil,
		UserID:                    "",
		ExternalUserID:            "",
		UserEmail:                 "",
		HTTPMetadata:              nil,
		APIKeyID:                  "",
		KeyType:                   openrouter.KeyTypeInternal,
		KeySlot:                   "",
		JSONSchema:                nil,
		Reasoning:                 nil,
		CacheControl:              cache,
		NormalizeOutboundMessages: false,
		WebSearch:                 nil,
		DisableResponseHealing:    false,
	}
}

func assistantToolCallMessage(response *openrouter.CompletionResponse) or.ChatMessages {
	calls := make([]or.ChatToolCall, 0, len(response.ToolCalls))
	for _, call := range response.ToolCalls {
		calls = append(calls, or.ChatToolCall{
			ID:       call.ID,
			Type:     or.ChatToolCallType(call.Type),
			Function: or.ChatToolCallFunction{Name: call.Function.Name, Arguments: call.Function.Arguments},
		})
	}
	message := or.ChatAssistantMessage{
		Model:            nil,
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
