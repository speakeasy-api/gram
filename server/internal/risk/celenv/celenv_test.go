package celenv_test

import (
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
)

func sortSpans(s []celenv.Span) {
	sort.Slice(s, func(i, j int) bool {
		if s[i].Target != s[j].Target {
			return s[i].Target < s[j].Target
		}
		return s[i].Start < s[j].Start
	})
}

func userMsg(content string) celenv.Message {
	return celenv.Message{Type: "user_message", Content: content, Tools: nil}
}

func toolReq(tools ...celenv.Tool) celenv.Message {
	return celenv.Message{Type: "tool_request", Content: "", Tools: tools}
}

func toolResp(output string) celenv.Message {
	return celenv.Message{Type: "tool_response", Content: output, Tools: nil}
}

// --- Scopes (boolean) --------------------------------------------------------

func TestScope_AutoScopedBodies(t *testing.T) {
	t.Parallel()
	eng, err := celenv.New()
	require.NoError(t, err)

	prg, err := eng.Compile(`prompt.includes("password")`)
	require.NoError(t, err)

	// prompt is non-empty only on user messages, so no explicit type check needed.
	in, err := eng.EvalScope(prg, userMsg("my PASSWORD is hunter2"))
	require.NoError(t, err)
	require.True(t, in)

	out, err := eng.EvalScope(prg, celenv.Message{Type: "assistant_message", Content: "password", Tools: nil})
	require.NoError(t, err)
	require.False(t, out)
}

func TestScope_ToolServer(t *testing.T) {
	t.Parallel()
	eng, err := celenv.New()
	require.NoError(t, err)

	prg, err := eng.Compile(`tools.exists(t, t.server.eq("shell"))`)
	require.NoError(t, err)

	in, err := eng.EvalScope(prg, toolReq(
		celenv.Tool{Name: "shell:run", Server: "shell", Function: "run", Args: "{}"},
	))
	require.NoError(t, err)
	require.True(t, in)
}

func TestScope_RejectsNonBool(t *testing.T) {
	t.Parallel()
	eng, err := celenv.New()
	require.NoError(t, err)

	_, err = eng.Compile(`content`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must evaluate to bool")
}

// The tool struct is tightened: an unknown field is a COMPILE error, not a
// silent runtime miss.
func TestCompile_RejectsUnknownToolField(t *testing.T) {
	t.Parallel()
	eng, err := celenv.New()
	require.NoError(t, err)

	_, err = eng.Compile(`tools.exists(t, t.functionn.match("bash"))`)
	require.Error(t, err)

	// the real fields still compile
	_, err = eng.Compile(`tools.exists(t, t.function.match("bash"))`)
	require.NoError(t, err)
}

func TestDetection_Glob(t *testing.T) {
	t.Parallel()
	eng, err := celenv.New()
	require.NoError(t, err)

	prg, err := eng.Compile(`tools.exists(t, t.function.glob("*_exec"))`)
	require.NoError(t, err)

	spans, matched, err := eng.EvalDetection(prg, toolReq(
		celenv.Tool{Name: "win:ps", Server: "win", Function: "powershell_exec", Args: "{}"},
	))
	require.NoError(t, err)
	require.True(t, matched)
	require.Len(t, spans, 1)
	require.Equal(t, "powershell_exec", spans[0].Value)
}

// --- Detection: correlated tools ---------------------------------------------

// Headline: a two-condition rule on the SAME tool yields one finding with two
// attributed spans.
func TestDetection_CorrelatedTwoSpans(t *testing.T) {
	t.Parallel()
	eng, err := celenv.New()
	require.NoError(t, err)

	prg, err := eng.Compile(`tools.exists(t, t.function.match("bash") && t.args.get("command").match("DROP TABLE"))`)
	require.NoError(t, err)

	spans, matched, err := eng.EvalDetection(prg, toolReq(
		celenv.Tool{Name: "shell:run", Server: "shell", Function: "run_bash_command", Args: `{"command":"DROP TABLE users"}`},
	))
	require.NoError(t, err)
	require.True(t, matched)
	require.Len(t, spans, 2)

	sortSpans(spans)
	require.Equal(t, "tool.args", spans[0].Target)
	require.Equal(t, "command", spans[0].Path)
	require.Equal(t, "DROP TABLE", spans[0].Value)
	require.Equal(t, "shell:run", spans[0].ToolCallID)
	require.Equal(t, "tool.function", spans[1].Target)
	require.Equal(t, "bash", spans[1].Value)
}

// Correlation: conditions split across DIFFERENT tools must NOT fire (this is the
// behavior change from the old uncorrelated aggregate).
func TestDetection_CorrelationDoesNotCrossTools(t *testing.T) {
	t.Parallel()
	eng, err := celenv.New()
	require.NoError(t, err)

	prg, err := eng.Compile(`tools.exists(t, t.function.match("bash") && t.args.get("command").match("DROP TABLE"))`)
	require.NoError(t, err)

	spans, matched, err := eng.EvalDetection(prg, toolReq(
		// bash tool, but harmless command
		celenv.Tool{Name: "shell:run", Server: "shell", Function: "run_bash_command", Args: `{"command":"ls"}`},
		// dangerous command, but not a bash tool
		celenv.Tool{Name: "db:query", Server: "db", Function: "query", Args: `{"command":"DROP TABLE users"}`},
	))
	require.NoError(t, err)
	require.False(t, matched)
	require.Empty(t, spans)
}

func TestDetection_JSONPathNested(t *testing.T) {
	t.Parallel()
	eng, err := celenv.New()
	require.NoError(t, err)

	prg, err := eng.Compile(`tools.exists(t, t.args.get("payload.sql").includes("delete"))`)
	require.NoError(t, err)

	spans, matched, err := eng.EvalDetection(prg, toolReq(
		celenv.Tool{Name: "db:exec", Server: "db", Function: "exec", Args: `{"payload":{"sql":"DELETE FROM users"}}`},
	))
	require.NoError(t, err)
	require.True(t, matched)
	require.Len(t, spans, 1)
	require.Equal(t, "payload.sql", spans[0].Path)
	require.Equal(t, "DELETE", spans[0].Value)
}

func TestDetection_JSONPathBracketSyntaxNormalized(t *testing.T) {
	t.Parallel()
	eng, err := celenv.New()
	require.NoError(t, err)

	prg, err := eng.Compile(`tools.exists(t, t.args.get("$.items[0].name").eq("danger"))`)
	require.NoError(t, err)

	_, matched, err := eng.EvalDetection(prg, toolReq(
		celenv.Tool{Name: "x:y", Server: "x", Function: "y", Args: `{"items":[{"name":"danger"}]}`},
	))
	require.NoError(t, err)
	require.True(t, matched)
}

func TestDetection_MissingPathNoMatch(t *testing.T) {
	t.Parallel()
	eng, err := celenv.New()
	require.NoError(t, err)

	prg, err := eng.Compile(`tools.exists(t, t.args.get("nope").present())`)
	require.NoError(t, err)

	_, matched, err := eng.EvalDetection(prg, toolReq(
		celenv.Tool{Name: "x:y", Server: "x", Function: "y", Args: `{"command":"ls"}`},
	))
	require.NoError(t, err)
	require.False(t, matched)
}

// --- Detection: tool outputs -------------------------------------------------

func TestDetection_OutputJSON(t *testing.T) {
	t.Parallel()
	eng, err := celenv.New()
	require.NoError(t, err)

	prg, err := eng.Compile(`output.get("error").present()`)
	require.NoError(t, err)

	spans, matched, err := eng.EvalDetection(prg, toolResp(`{"error":"permission denied"}`))
	require.NoError(t, err)
	require.True(t, matched)
	require.Len(t, spans, 1)
	require.Equal(t, "output", spans[0].Target)
	require.Equal(t, "error", spans[0].Path)
	require.Equal(t, "permission denied", spans[0].Value)
}

func TestDetection_OutputEmptyOnNonResponse(t *testing.T) {
	t.Parallel()
	eng, err := celenv.New()
	require.NoError(t, err)

	prg, err := eng.Compile(`output.present()`)
	require.NoError(t, err)

	_, matched, err := eng.EvalDetection(prg, userMsg(`{"error":"x"}`)) // user msg, not a response
	require.NoError(t, err)
	require.False(t, matched)
}

// --- Detection: content + multiplicity --------------------------------------

func TestDetection_ContentMultipleOccurrences(t *testing.T) {
	t.Parallel()
	eng, err := celenv.New()
	require.NoError(t, err)

	prg, err := eng.Compile(`content.match("secret")`)
	require.NoError(t, err)

	spans, matched, err := eng.EvalDetection(prg, userMsg("the secret is a secret"))
	require.NoError(t, err)
	require.True(t, matched)
	require.Len(t, spans, 2)
	require.Equal(t, 4, spans[0].Start)
	require.Equal(t, 16, spans[1].Start)
}

// Across all tools (any-tool detection without correlation): a span per match.
func TestDetection_AcrossAllTools(t *testing.T) {
	t.Parallel()
	eng, err := celenv.New()
	require.NoError(t, err)

	// filter (not exists) visits every tool, so every offending tool is captured.
	prg, err := eng.Compile(`tools.filter(t, t.args.get("command").match("rm -rf")).size() > 0`)
	require.NoError(t, err)

	spans, matched, err := eng.EvalDetection(prg, toolReq(
		celenv.Tool{Name: "shell:a", Server: "shell", Function: "a", Args: `{"command":"rm -rf /tmp"}`},
		celenv.Tool{Name: "shell:b", Server: "shell", Function: "b", Args: `{"command":"ls"}`},
		celenv.Tool{Name: "shell:c", Server: "shell", Function: "c", Args: `{"command":"rm -rf /var"}`},
	))
	require.NoError(t, err)
	require.True(t, matched)
	require.Len(t, spans, 2)
	require.ElementsMatch(t, []string{"shell:a", "shell:c"}, []string{spans[0].ToolCallID, spans[1].ToolCallID})
}

// --- Concurrency -------------------------------------------------------------

func TestConcurrentEval(t *testing.T) {
	t.Parallel()
	eng, err := celenv.New()
	require.NoError(t, err)

	prg, err := eng.Compile(`content.match("secret")`)
	require.NoError(t, err)

	const n = 200
	type outcome struct {
		err  error
		got  int
		want int
	}
	results := make(chan outcome, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			content, want := "nothing", 0
			if i%2 == 0 {
				content, want = "a secret and a secret", 2
			}
			spans, _, err := eng.EvalDetection(prg, userMsg(content))
			results <- outcome{err: err, got: len(spans), want: want}
		}(i)
	}
	wg.Wait()
	close(results)

	for r := range results {
		require.NoError(t, r.err)
		require.Equal(t, r.want, r.got)
	}
}

// --- Schema ------------------------------------------------------------------

func TestDescribe(t *testing.T) {
	t.Parallel()
	s := celenv.Describe()
	require.NotEmpty(t, s.Variables)
	require.NotEmpty(t, s.Functions)

	vars := make(map[string]bool)
	for _, v := range s.Variables {
		vars[v.Name] = true
	}
	for _, want := range []string{"type", "content", "prompt", "assistant", "output", "tools"} {
		require.True(t, vars[want], "missing variable %q", want)
	}

	fns := make(map[string]bool)
	for _, f := range s.Functions {
		fns[f.Name] = true
	}
	require.True(t, fns["get"])
	require.True(t, fns["match"])
}
