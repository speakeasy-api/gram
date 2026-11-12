package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/tooleval"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// toolEvalExempt lists external tools that have no eval case, each with the
// reason. Keep it empty unless a tool cannot be asked for in plain words.
var toolEvalExempt = map[string]string{}

// TestToolEvalCasesCoverEveryTool keeps the eval in step with the catalogue: a
// new tool needs a case, and a renamed or removed tool breaks the cases that
// name it.
func TestToolEvalCasesCoverEveryTool(t *testing.T) {
	t.Parallel()

	cases, err := tooleval.LoadCases()
	require.NoError(t, err)
	catalogue := toolEvalCatalogue(t, toolEvalRegistrar(t))
	byName := make(map[string]tooleval.Tool, len(catalogue))
	for _, tool := range catalogue {
		byName[tool.Name] = tool
	}

	covered := make(map[string]bool)
	negatives := 0
	for _, c := range cases {
		if c.Negative() {
			negatives++
			require.Empty(t, c.Args, "negative case %s expects no call, so it has no arguments", c.ID)
		}
		for _, name := range slices.Concat(c.Expect, c.Allow) {
			require.Contains(t, byName, name, "case %s names %s, which the external endpoint does not serve", c.ID, name)
		}
		for _, name := range c.Expect {
			covered[name] = true
			var schema struct {
				Properties map[string]json.RawMessage `json:"properties"`
			}
			require.NoError(t, json.Unmarshal(byName[name].InputSchema, &schema))
			for key := range c.Args {
				require.Contains(t, schema.Properties, key, "case %s expects argument %s, which %s does not take", c.ID, key, name)
			}
		}
		for _, tool := range catalogue {
			require.NotContains(t, c.Prompt, tool.Name, "case %s names a tool in its prompt, which gives the answer away", c.ID)
		}
	}

	var missing []string
	for _, tool := range catalogue {
		_, exempt := toolEvalExempt[tool.Name]
		if !covered[tool.Name] && !exempt {
			missing = append(missing, tool.Name)
		}
	}
	require.Empty(t, missing, "add a case to server/internal/platformmcp/tooleval/cases for each tool, or exempt it with a reason")
	require.GreaterOrEqual(t, negatives, 5, "negative cases check the agent does not reach for a tool it does not need")
	for name := range toolEvalExempt {
		require.Contains(t, byName, name, "exempt tool %s is not in the catalogue", name)
		require.False(t, covered[name], "exempt tool %s has a case; drop the exemption", name)
	}
}

// toolEvalPlaceholderPhrases are the wordings placeholder descriptions use.
// None appears in a live description.
var toolEvalPlaceholderPhrases = []string{
	"not switched on for your organization yet",
	"unavailable in this deployment",
	"unavailable on this server",
	"not available on this deployment",
	"not available yet",
}

var updateToolEvalCatalogue = flag.Bool("update-tooleval-catalogue", false, "rewrite tooleval/catalogue.json from the live catalogue")

// toolEvalCataloguePath is the committed snapshot of what clients are shown.
const toolEvalCataloguePath = "tooleval/catalogue.json"

// TestToolEvalCatalogueSnapshotIsCurrent keeps tooleval/catalogue.json equal to
// the live catalogue. The judged eval compares it with the base branch's copy
// to run only the cases a change could move, and reviewers read its diff to
// see exactly what an agent will be shown. Regenerate it with:
//
//	mise run test:server -run TestToolEvalCatalogueSnapshotIsCurrent ./internal/platformmcp/ -args -update-tooleval-catalogue
func TestToolEvalCatalogueSnapshotIsCurrent(t *testing.T) {
	t.Parallel()

	live, err := toolEvalSnapshot(t, toolEvalRegistrar(t)).Marshal()
	require.NoError(t, err)
	if *updateToolEvalCatalogue {
		require.NoError(t, os.WriteFile(toolEvalCataloguePath, live, 0o600))
		return
	}
	committed, err := os.ReadFile(toolEvalCataloguePath)
	require.NoError(t, err)
	// Compared as decoded JSON, since the repository's formatter rewrites the
	// file's layout.
	var want, got any
	require.NoError(t, json.Unmarshal(committed, &want))
	require.NoError(t, json.Unmarshal(live, &got))
	require.True(t, reflect.DeepEqual(want, got), "tooleval/catalogue.json is stale; regenerate it with -update-tooleval-catalogue (see this test's comment)")
}

// toolEvalProviderSchemaExempt lists tools whose input schema a model provider
// rejects, each with the reason. Anthropic rejects a request whose tool has
// oneOf, anyOf or allOf at the top of its input schema, so a client that lists
// these tools directly to a Claude model fails every turn, and the eval's
// direct surface leaves them out. Remove an entry once its schema is fixed.
var toolEvalProviderSchemaExempt = map[string]string{
	"change_risk_policy_audience": "top-level anyOf requires adding or removing at least one principal",
	"create_risk_policy":          "top-level oneOf selects a standard, prompt-based or preset policy",
	"create_risk_exclusion":       "top-level oneOf selects the match type: exact, rule_id, source or entity_type",
	"search_chats":                "top-level anyOf requires project_id or project_slug",
	"read_chat":                   "top-level anyOf requires project_id or project_slug",
	"search_team_memory":          "top-level oneOf requires exactly one of project_id or project_slug",
	"remember":                    "top-level oneOf requires exactly one of project_id or project_slug",
}

// TestPlatformToolSchemasFitModelProviders fails when a tool gains an input
// schema that a model provider rejects, or when an exempt tool is fixed.
func TestPlatformToolSchemasFitModelProviders(t *testing.T) {
	t.Parallel()

	problems := make(map[string]string)
	for _, tool := range toolEvalCatalogue(t, toolEvalRegistrar(t)) {
		if problem := tooleval.ProviderSchemaProblem(tool.InputSchema); problem != "" {
			problems[tool.Name] = problem
		}
	}
	for name, problem := range problems {
		require.Contains(t, toolEvalProviderSchemaExempt, name, "%s has a %s in its input schema, which Anthropic rejects; move the alternatives into properties", name, problem)
	}
	for name := range toolEvalProviderSchemaExempt {
		require.Contains(t, problems, name, "%s's schema is now accepted; remove its exemption", name)
	}
}

// TestToolEvalCatalogueIsLive keeps placeholders out of the eval. A tool whose
// dependency is missing registers a one-line placeholder with no input
// schema; scoring an agent against that says nothing about the real tool. A
// new dependency gate fails here until toolEvalRegistrar composes it. Some
// placeholders keep the live output type, so their wording is checked too.
func TestToolEvalCatalogueIsLive(t *testing.T) {
	t.Parallel()

	var placeholders []string
	for _, descriptor := range toolEvalRegistrar(t).For(AudienceExternal) {
		if descriptor.output == reflect.TypeFor[featureUnavailableResult]() || slices.ContainsFunc(toolEvalPlaceholderPhrases, func(phrase string) bool {
			return strings.Contains(descriptor.Description, phrase)
		}) {
			placeholders = append(placeholders, descriptor.Name)
		}
	}
	require.Empty(t, placeholders, "compose these tools' services in toolEvalRegistrar")
}

// TestToolEvalHarnessScoresScriptedAgent checks the agent loop, both
// surfaces and scoring with a scripted model that always picks the expected
// tool. On the progressive surface it finds the tool by exact name through the
// real search ranking first, so a tool search cannot find fails here.
func TestToolEvalHarnessScoresScriptedAgent(t *testing.T) {
	t.Parallel()

	cases, err := tooleval.LoadCases()
	require.NoError(t, err)
	registrar := toolEvalRegistrar(t)
	surfaces := []tooleval.Surface{toolEvalDirectSurface(t, registrar), toolEvalProgressiveSurface(t, registrar)}
	cfg := tooleval.Config{Models: []string{"anthropic/claude-sonnet-5.5"}, Runs: 1, Concurrency: 8}

	report, err := tooleval.Run(t.Context(), scriptedToolAgent{cases: cases}, cases, toolEvalCatalogue(t, registrar), surfaces, cfg)
	require.NoError(t, err)
	require.Len(t, report.Scores, 2)
	var misses []string
	for _, o := range report.Outcomes {
		if !o.Hit {
			misses = append(misses, fmt.Sprintf("%s/%s: selected %+v, err %v", o.Surface, o.Case.ID, o.Selected, o.Err))
		}
	}
	require.Empty(t, misses, "the scripted agent always picks an expected tool, so every miss is harness or search wiring")
	for _, score := range report.Scores {
		require.Zero(t, score.Errors, score.Surface)
		require.InDelta(t, 1.0, score.Selection, 1e-9, score.Surface)
		require.Equal(t, score.Negatives, score.NegativesRight, score.Surface)
	}
}

// scriptedToolAgent finds the case from the user's message and calls its
// first expected tool with the expected arguments, or answers in text for a
// negative case.
type scriptedToolAgent struct {
	cases []tooleval.Case
}

func (a scriptedToolAgent) GetCompletion(_ context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	encoded, err := json.Marshal(request.Messages)
	if err != nil {
		return nil, fmt.Errorf("encode messages: %w", err)
	}
	transcript := string(encoded)
	var current *tooleval.Case
	for i := range a.cases {
		prompt, err := json.Marshal(a.cases[i].Prompt)
		if err != nil {
			return nil, fmt.Errorf("encode prompt: %w", err)
		}
		if strings.Contains(transcript, strings.Trim(string(prompt), `"`)) {
			current = &a.cases[i]
			break
		}
	}
	if current == nil {
		return nil, errors.New("scripted agent: no case matches the user message")
	}
	if current.Negative() {
		return &openrouter.CompletionResponse{Content: "That is not something the Speakeasy tools do."}, nil
	}
	target := current.Expect[0]
	args, err := json.Marshal(current.Args)
	if err != nil {
		return nil, fmt.Errorf("encode arguments: %w", err)
	}
	lists := func(name string) bool {
		return slices.ContainsFunc(request.Tools, func(tool openrouter.Tool) bool { return tool.Function.Name == name })
	}
	call := func(name, arguments string) *openrouter.CompletionResponse {
		return &openrouter.CompletionResponse{ToolCalls: []openrouter.ToolCall{{ID: "call-" + name, Type: "function", Function: openrouter.ToolCallFunction{Name: name, Arguments: arguments}}}}
	}
	switch {
	case lists(target):
		return call(target, string(args)), nil
	case !strings.Contains(transcript, `"role":"tool"`):
		query, err := json.Marshal(map[string]string{"query": target})
		if err != nil {
			return nil, fmt.Errorf("encode query: %w", err)
		}
		return call(searchPlatformToolsName, string(query)), nil
	case strings.Contains(transcript, `\"name\":\"`+target+`\"`):
		wrapped, err := json.Marshal(map[string]json.RawMessage{"name": json.RawMessage(`"` + target + `"`), "arguments": args})
		if err != nil {
			return nil, fmt.Errorf("encode call: %w", err)
		}
		return call(callPlatformToolName, string(wrapped)), nil
	default:
		return &openrouter.CompletionResponse{Content: "search did not return " + target}, nil
	}
}

func (scriptedToolAgent) GetCompletionStream(context.Context, openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	return nil, errors.New("not scripted")
}

func (scriptedToolAgent) GetObjectCompletion(context.Context, openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	return nil, errors.New("not scripted")
}

func (scriptedToolAgent) CreateEmbeddings(context.Context, string, string, []string, ...openrouter.EmbeddingOption) ([][]float32, error) {
	return nil, errors.New("not scripted")
}

func (scriptedToolAgent) ResolveKey(context.Context, string, string, billing.ModelUsageSource, openrouter.KeyType) (openrouter.ResolvedKey, error) {
	return openrouter.ResolvedKey{Key: "scripted", Customer: false}, nil
}
