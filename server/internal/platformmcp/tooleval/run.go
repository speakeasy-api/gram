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
	// Unfinished is an agent that ran out of turns or time without a tool
	// call or a final answer. It is a miss even on a negative case.
	Unfinished bool

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

	Turns        int
	PromptTokens int
	// CachedTokens is the part of PromptTokens read from the provider's
	// prompt cache.
	CachedTokens     int
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
	// The first case of each model and surface runs before the rest, so it
	// writes the prompt cache for the tool list and instructions that every
	// later case reads. Started together, they would all miss and each write.
	outcomes := make([]Outcome, len(jobs))
	warmed := make(map[string]bool)
	var first, rest []int
	for i, j := range jobs {
		key := j.model + "/" + j.surface.Name
		if warmed[key] {
			rest = append(rest, i)
			continue
		}
		warmed[key] = true
		first = append(first, i)
	}
	for _, batch := range [][]int{first, rest} {
		group, groupCtx := errgroup.WithContext(ctx)
		group.SetLimit(cfg.Concurrency)
		for _, i := range batch {
			j := jobs[i]
			group.Go(func() error {
				outcomes[i] = attempt(groupCtx, client, j.model, j.surface, j.run, j.c, tools, validators)
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			return nil, fmt.Errorf("run cases: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
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
	outcome := Outcome{Model: model, Surface: surface.Name, Run: run, Case: c, Selected: nil, Reads: nil, Answer: "", Unfinished: false, Hit: false, OnPath: false, SchemaValid: false, SchemaError: "", ArgsMatch: false, ConfirmedFirst: false, Turns: 0, PromptTokens: 0, CachedTokens: 0, CompletionTokens: 0, CostUSD: 0, Latency: 0, Err: nil}

	listed := make([]openrouter.Tool, 0, len(surface.Tools))
	for _, tool := range surface.Tools {
		listed = append(listed, openrouter.Tool{Type: "function", Function: &openrouter.FunctionDefinition{Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema}, CacheControl: nil})
	}
	if strings.HasPrefix(model, "anthropic/") && len(listed) > 0 {
		// The request-level breakpoint below caches the end of each case's
		// own conversation, which no other case shares. A breakpoint on the
		// last tool caches the tool list, the bulk of every request, across
		// cases.
		listed[len(listed)-1].CacheControl = &or.ChatContentCacheControl{Type: or.ChatContentCacheControlTypeEphemeral, TTL: nil}
	}
	messages := []or.ChatMessages{systemMessage(model, SystemPrompt(surface)), openrouter.CreateMessageUser(c.Prompt)}

turns:
	for turn := range maxAgentTurns {
		outcome.Turns = turn + 1
		response, err := completeWithRetry(ctx, client, completionRequest(model, messages, listed))
		if err != nil {
			// A provider that kept rate limiting is an error, not a miss,
			// even when the waiting ran the case out of time.
			if errors.Is(err, openrouter.ErrRateLimited) {
				outcome.Err = fmt.Errorf("case %s turn %d: %w", c.ID, turn, err)
				break
			}
			// An agent that runs out of time has not reached a tool, like one
			// that runs out of turns: a miss, not an error that drops out of
			// the rates. A cancelled run is still an error.
			if errors.Is(ctx.Err(), context.DeadlineExceeded) && parent.Err() == nil {
				break
			}
			outcome.Err = fmt.Errorf("case %s turn %d: %w", c.ID, turn, err)
			break
		}
		outcome.PromptTokens += response.Usage.PromptTokens
		if response.Usage.PromptTokensDetails != nil {
			outcome.CachedTokens += response.Usage.PromptTokensDetails.CachedTokens
		}
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
	if outcome.Err == nil && outcome.Selected == nil && outcome.Answer == "" {
		outcome.Unfinished = true
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
		o.Hit = o.Case.Negative() && !o.Unfinished
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

// systemMessage is the system prompt. For Anthropic models it ends in a cache
// breakpoint, because the server instructions it carries are the same for
// every case on a surface.
func systemMessage(model, prompt string) or.ChatMessages {
	if !strings.HasPrefix(model, "anthropic/") {
		return openrouter.CreateMessageSystem(prompt)
	}
	return or.CreateChatMessagesSystem(or.ChatSystemMessage{
		ConfigurationUpdate: nil,
		Role:                or.ChatSystemMessageRoleSystem,
		Content: or.CreateChatSystemMessageContentArrayOfChatContentText([]or.ChatContentText{{
			PromptCacheBreakpoint: nil,
			Type:                  or.ChatContentTextTypeText,
			Text:                  prompt,
			CacheControl:          &or.ChatContentCacheControl{Type: or.ChatContentCacheControlTypeEphemeral, TTL: nil},
		}}),
		Name: nil,
	})
}

// rateLimitRetries is how many times one completion is retried after a rate
// limit, waiting rateLimitWait and then twice as long each time, up to
// rateLimitMaxWait. A pinned provider rate limits a run with many cases in
// flight.
const (
	rateLimitRetries = 6
	rateLimitWait    = 2 * time.Second
	rateLimitMaxWait = 30 * time.Second
)

func completeWithRetry(ctx context.Context, client openrouter.CompletionClient, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	wait := rateLimitWait
	for try := 0; ; try++ {
		response, err := client.GetCompletion(ctx, request)
		if err == nil {
			return response, nil
		}
		if !errors.Is(err, openrouter.ErrRateLimited) || try == rateLimitRetries {
			return nil, fmt.Errorf("get completion: %w", err)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("get completion: %w", err)
		case <-timer.C:
		}
		wait = min(2*wait, rateLimitMaxWait)
	}
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
