package researchagent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/researchagent"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// scriptedCompletions plays back completion responses in order and captures
// the object-completion (extraction) request.
type scriptedCompletions struct {
	turns      []*openrouter.CompletionResponse
	turnIndex  int
	extraction openrouter.ObjectCompletionRequest
	extracted  string

	// lastTools records the tool definitions of the most recent turn, so the
	// wrap-up turn's tool-lessness is assertable.
	lastTools []openrouter.Tool
}

func (s *scriptedCompletions) GetCompletion(_ context.Context, req openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	if s.turnIndex >= len(s.turns) {
		return nil, fmt.Errorf("unexpected completion turn %d", s.turnIndex)
	}
	s.lastTools = req.Tools
	response := s.turns[s.turnIndex]
	s.turnIndex++
	return response, nil
}

func (s *scriptedCompletions) GetObjectCompletion(_ context.Context, req openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	s.extraction = req
	return &openrouter.CompletionResponse{Content: s.extracted}, nil
}

// echoTool is a stand-in research tool that records its calls.
type echoTool struct {
	name    string
	handler string
	calls   []string
}

func (e *echoTool) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "research",
		HandlerName: e.handler,
		Name:        e.name,
		Description: "test tool",
		InputSchema: []byte(`{"type": "object"}`),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (e *echoTool) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	// The runner must supply an auth context for the tools that authorize.
	if _, ok := contextvalues.GetAuthContext(ctx); !ok {
		return fmt.Errorf("missing auth context")
	}
	raw, err := io.ReadAll(payload)
	if err != nil {
		return fmt.Errorf("read payload: %w", err)
	}
	e.calls = append(e.calls, string(raw))
	_, err = fmt.Fprintf(wr, `{"echo": %q}`, string(raw))
	if err != nil {
		return fmt.Errorf("write result: %w", err)
	}
	return nil
}

func toolCallResponse(name, arguments string) *openrouter.CompletionResponse {
	return &openrouter.CompletionResponse{
		Content: "searching…",
		ToolCalls: []openrouter.ToolCall{{
			Index: 0,
			ID:    "call-1",
			Type:  "function",
			Function: openrouter.ToolCallFunction{
				Name:      name,
				Arguments: arguments,
			},
		}},
		Usage: openrouter.Usage{PromptTokens: 100, CompletionTokens: 20},
	}
}

func runInput() researchagent.RunInput {
	return researchagent.RunInput{
		OrgID:       "org-1",
		ProjectID:   uuid.New(),
		ReportID:    uuid.New(),
		TargetKind:  "stdio_command",
		TargetRaw:   "npx -y @scope/mcp-server",
		ArtifactRef: "npm:@scope/mcp-server",
		Evidence:    json.RawMessage(`{"identity": {"kind": "package"}}`),
	}
}

func TestRun(t *testing.T) {
	t.Parallel()

	search := &echoTool{name: "platform_web_search", handler: "web_search", calls: nil}
	fetch := &echoTool{name: "platform_fetch_page", handler: "fetch_page", calls: nil}

	completions := &scriptedCompletions{
		turns: []*openrouter.CompletionResponse{
			toolCallResponse("platform_web_search", `{"query": "somevendor mcp"}`),
			toolCallResponse("platform_fetch_page", `{"url": "https://example.com/a"}`),
			{Content: "Findings: nothing independent exists. https://example.com/a", Usage: openrouter.Usage{PromptTokens: 50, CompletionTokens: 30}},
		},
		extracted: `{
			"summary": "Little is known.",
			"coverage": {"level": "thin", "note": "one third-party mention"},
			"claims": [
				{"tier": "independently_reported", "text": "cited claim", "citations": [{"url": "https://example.com/a"}]},
				{"tier": "vendor_claim", "text": "uncited claim, must drop", "citations": []},
				{"tier": "observed", "text": "briefing restatement, silently dropped — the evidence panel shows it"}
			]
		}`,
	}

	runner := researchagent.New(completions, search, fetch)
	encoded, meta, err := runner.Run(t.Context(), runInput())
	require.NoError(t, err)

	require.Equal(t, 3, meta.Turns)
	require.Equal(t, 1, meta.Searches)
	require.Equal(t, 1, meta.Fetches)
	require.False(t, meta.TurnLimitReached)
	require.Equal(t, 1, meta.DroppedUncitedClaims)
	require.Equal(t, int64(250), meta.PromptTokens, "two tool turns at 100 plus the final at 50")

	var document researchagent.Document
	require.NoError(t, json.Unmarshal(encoded, &document))
	require.Equal(t, "Little is known.", document.Summary)
	require.Equal(t, researchagent.CoverageThin, document.Coverage.Level)
	require.Len(t, document.Claims, 1, "the uncited web claim and the observed restatement are both dropped")
	require.Equal(t, researchagent.TierIndependentlyReported, document.Claims[0].Tier)
	require.Equal(t, researchagent.Model, document.Run.Model)
	require.Equal(t, 1, document.Run.DroppedUncitedClaims)

	require.Len(t, search.calls, 1)
	require.Len(t, fetch.calls, 1)

	// The extraction prompt carries the transcript: briefing, agent prose,
	// and tool results.
	require.Contains(t, completions.extraction.Prompt, "npx -y @scope/mcp-server")
	require.Contains(t, completions.extraction.Prompt, "searching…")
	require.Contains(t, completions.extraction.Prompt, "https://example.com/a")
}

// The loop is bounded: a model that never stops calling tools gets one final
// tool-less wrap-up turn to state its findings, so the transcript never ends
// on a raw tool dump — a findings-less transcript is how a real run once
// extracted into a placeholder report.
func TestRun_TurnLimitForcesAWrapUp(t *testing.T) {
	t.Parallel()

	search := &echoTool{name: "platform_web_search", handler: "web_search", calls: nil}
	turns := make([]*openrouter.CompletionResponse, 0, 13)
	for range 12 {
		turns = append(turns, toolCallResponse("platform_web_search", `{"query": "again"}`))
	}
	turns = append(turns, &openrouter.CompletionResponse{
		Content: "FINAL FINDINGS: nothing independent surfaced.",
		Usage:   openrouter.Usage{PromptTokens: 10, CompletionTokens: 10},
	})
	completions := &scriptedCompletions{
		turns:     turns,
		extracted: `{"summary": "ran out", "coverage": {"level": "none"}, "claims": []}`,
	}

	runner := researchagent.New(completions, search)
	encoded, meta, err := runner.Run(t.Context(), runInput())
	require.NoError(t, err)
	require.True(t, meta.TurnLimitReached)
	require.Equal(t, 13, meta.Turns, "twelve budget turns plus the wrap-up")

	// The wrap-up turn carries no tools — it cannot keep researching.
	require.Nil(t, completions.lastTools)
	require.Contains(t, completions.extraction.Prompt, "FINAL FINDINGS")

	var document researchagent.Document
	require.NoError(t, json.Unmarshal(encoded, &document))
	require.True(t, document.Run.TurnLimitReached)
}

// A report keeps only its five most relevant claims: the extraction orders
// most-relevant-first, so the cap keeps the head of the list.
func TestRun_CapsClaims(t *testing.T) {
	t.Parallel()

	claims := make([]string, 0, 7)
	for i := range 7 {
		claims = append(claims, fmt.Sprintf(`{"tier": "independently_reported", "text": "claim %d", "citations": [{"url": "https://example.com/%d"}]}`, i, i))
	}
	completions := &scriptedCompletions{
		turns: []*openrouter.CompletionResponse{
			{Content: "done", Usage: openrouter.Usage{}},
		},
		extracted: fmt.Sprintf(`{"summary": "s", "coverage": {"level": "moderate"}, "claims": [%s]}`, strings.Join(claims, ",")),
	}

	runner := researchagent.New(completions)
	encoded, _, err := runner.Run(t.Context(), runInput())
	require.NoError(t, err)

	var document researchagent.Document
	require.NoError(t, json.Unmarshal(encoded, &document))
	require.Len(t, document.Claims, 5)
	require.Equal(t, "claim 0", document.Claims[0].Text, "the head of the list survives the cap")
}

// A degenerate extraction — the literal filler a schema-forced model emits
// when it has nothing — fails the run instead of rendering as a report.
func TestRun_RejectsDegenerateExtraction(t *testing.T) {
	t.Parallel()

	completions := &scriptedCompletions{
		turns: []*openrouter.CompletionResponse{
			{Content: "done", Usage: openrouter.Usage{}},
		},
		extracted: `{"summary": "placeholder", "coverage": {"level": "none"}, "claims": []}`,
	}

	runner := researchagent.New(completions)
	_, _, err := runner.Run(t.Context(), runInput())
	require.Error(t, err)
	require.Contains(t, err.Error(), "degenerate")
}

// An extraction that invents tiers or levels fails the run loudly rather
// than storing junk the panel would render as findings.
func TestRun_RejectsUnknownTier(t *testing.T) {
	t.Parallel()

	completions := &scriptedCompletions{
		turns: []*openrouter.CompletionResponse{
			{Content: "done", Usage: openrouter.Usage{}},
		},
		extracted: `{"summary": "s", "coverage": {"level": "thin"}, "claims": [{"tier": "verdict", "text": "bad"}]}`,
	}

	runner := researchagent.New(completions)
	_, _, err := runner.Run(t.Context(), runInput())
	require.Error(t, err)

	completions2 := &scriptedCompletions{
		turns: []*openrouter.CompletionResponse{
			{Content: "done", Usage: openrouter.Usage{}},
		},
		extracted: `{"summary": "s", "coverage": {"level": "certain"}, "claims": []}`,
	}
	runner2 := researchagent.New(completions2)
	_, _, err = runner2.Run(t.Context(), runInput())
	require.Error(t, err)
}

// A tool failure becomes the tool's result so the agent can adapt, never by
// itself a run failure — as long as some other tool call succeeded.
func TestRun_ToolErrorFeedsBack(t *testing.T) {
	t.Parallel()

	failing := &failingTool{}
	search := &echoTool{name: "platform_web_search", handler: "web_search", calls: nil}
	completions := &scriptedCompletions{
		turns: []*openrouter.CompletionResponse{
			toolCallResponse("platform_web_search", `{"query": "q"}`),
			toolCallResponse("platform_fetch_page", `{"url": "https://example.com"}`),
			{Content: "adapting", Usage: openrouter.Usage{}},
		},
		extracted: `{"summary": "s", "coverage": {"level": "none"}, "claims": []}`,
	}

	runner := researchagent.New(completions, failing, search)
	_, _, err := runner.Run(t.Context(), runInput())
	require.NoError(t, err)
	require.Contains(t, completions.extraction.Prompt, "tool error: fetch budget exhausted")
}

// A run where every tool call failed gathered nothing — it fails with the
// last tool error instead of extracting confident filler from a transcript
// of errors.
func TestRun_AllToolFailuresFailTheRun(t *testing.T) {
	t.Parallel()

	failing := &failingTool{}
	completions := &scriptedCompletions{
		turns: []*openrouter.CompletionResponse{
			toolCallResponse("platform_fetch_page", `{"url": "https://example.com"}`),
			toolCallResponse("platform_fetch_page", `{"url": "https://example.com/b"}`),
			{Content: "giving up", Usage: openrouter.Usage{}},
		},
		extracted: `{"summary": "must never be produced", "coverage": {"level": "none"}, "claims": []}`,
	}

	runner := researchagent.New(completions, failing)
	_, _, err := runner.Run(t.Context(), runInput())
	require.Error(t, err)
	require.Contains(t, err.Error(), "every research tool call failed")
	require.Contains(t, err.Error(), "fetch budget exhausted")
}

type failingTool struct{}

func (failingTool) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "research",
		HandlerName: "fetch_page",
		Name:        "platform_fetch_page",
		Description: "test tool",
		InputSchema: []byte(`{"type": "object"}`),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (failingTool) Call(_ context.Context, _ toolconfig.ToolCallEnv, _ io.Reader, _ io.Writer) error {
	return fmt.Errorf("fetch budget exhausted")
}
