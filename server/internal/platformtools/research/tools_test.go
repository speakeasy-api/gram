package research_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/platformtools/research"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// fakeCompletions records the request and returns canned annotations, the way
// OpenRouter's web-search plugin delivers results.
type fakeCompletions struct {
	request     openrouter.CompletionRequest
	annotations []openrouter.ResponseAnnotation
	usage       openrouter.Usage
	err         error
}

func (f *fakeCompletions) GetCompletion(_ context.Context, req openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	f.request = req
	if f.err != nil {
		return nil, f.err
	}

	return &openrouter.CompletionResponse{
		Annotations: f.annotations,
		Usage:       f.usage,
		Content:     "prose the tool must discard",
	}, nil
}

func authedContext(t *testing.T) context.Context {
	t.Helper()

	projectID := uuid.New()
	return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-1",
		ProjectID:            &projectID,
	})
}

func runSearch(t *testing.T, ctx context.Context, tool *research.WebSearch, input string) (map[string]any, error) {
	t.Helper()

	var out bytes.Buffer
	if err := tool.Call(ctx, toolconfig.ToolCallEnv{GramChatID: "report-default"}, strings.NewReader(input), &out); err != nil {
		return nil, fmt.Errorf("call web search: %w", err)
	}

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &decoded))

	return decoded, nil
}

func TestWebSearch(t *testing.T) {
	t.Parallel()

	completions := &fakeCompletions{
		annotations: []openrouter.ResponseAnnotation{
			{Type: "url_citation", URLCitation: &openrouter.ResponseURLCitation{
				URL: "https://somevendor.io/security", Title: "Security at SomeVendor", Content: "We publish a trust center …",
			}},
			// Non-citation annotations and citation-less entries are skipped,
			// never returned as empty results.
			{Type: "file_citation", URLCitation: nil},
			{Type: "url_citation", URLCitation: &openrouter.ResponseURLCitation{URL: "", Title: "broken", Content: ""}},
			{Type: "url_citation", URLCitation: &openrouter.ResponseURLCitation{
				URL: "https://example.com/review", Title: "Independent review", Content: "…",
			}},
		},
	}
	tool := research.NewWebSearchTool(research.NewSearchClient(completions), research.NewURLMenu())

	decoded, err := runSearch(t, authedContext(t), tool, `{"query": "is somevendor real"}`)
	require.NoError(t, err)

	results, ok := decoded["results"].([]any)
	require.True(t, ok)
	require.Len(t, results, 2)
	first, ok := results[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "https://somevendor.io/security", first["url"])
	require.Equal(t, "Security at SomeVendor", first["title"])
	require.Equal(t, "We publish a trust center …", first["snippet"])

	// The completion ran on the chat key with the research attribution and
	// the web plugin — the properties spend accounting depends on.
	require.Equal(t, openrouter.KeyTypeChat, completions.request.KeyType)
	require.Equal(t, "mcp-research", string(completions.request.UsageSource))
	require.NotNil(t, completions.request.WebSearch)
	require.Equal(t, 5, completions.request.WebSearch.MaxResults)
	require.Equal(t, "org-1", completions.request.OrgID)
	// The search route rejects a disabled reasoning setting with a 400, so
	// the override must stay unset.
	require.Nil(t, completions.request.Reasoning)
}

// A search is a billed completion the tool runs on the caller's behalf, so
// the caller can collect what its searches cost. Draining forgets it: this
// tool outlives every run that uses it.
func TestWebSearch_ReportsWhatItsSearchesCost(t *testing.T) {
	t.Parallel()

	completions := &fakeCompletions{
		annotations: []openrouter.ResponseAnnotation{
			{Type: "url_citation", URLCitation: &openrouter.ResponseURLCitation{
				URL: "https://example.com/a", Title: "a", Content: "…",
			}},
		},
		usage: openrouter.Usage{PromptTokens: 1200, CompletionTokens: 340},
	}
	tool := research.NewWebSearchTool(research.NewSearchClient(completions), research.NewURLMenu())

	var out bytes.Buffer
	env := toolconfig.ToolCallEnv{GramChatID: "report-1"}
	require.NoError(t, tool.Call(authedContext(t), env, strings.NewReader(`{"query": "one"}`), &out))
	out.Reset()
	require.NoError(t, tool.Call(authedContext(t), env, strings.NewReader(`{"query": "two"}`), &out))

	prompt, completion := tool.DrainUsage("report-1")
	require.Equal(t, int64(2400), prompt, "both searches count")
	require.Equal(t, int64(680), completion)

	prompt, completion = tool.DrainUsage("report-1")
	require.Zero(t, prompt, "drained usage is not counted twice")
	require.Zero(t, completion)

	prompt, completion = tool.DrainUsage("another-report")
	require.Zero(t, prompt, "and one run never collects another's spend")
	require.Zero(t, completion)
}

// Searches are billed completions, and the loop that issues them chooses its
// next query from the last one's results — so a seeded result chain can spend
// without limit unless the tool bounds itself, exactly as the fetch tool does.
func TestWebSearch_BoundsSearchesPerRun(t *testing.T) {
	t.Parallel()

	completions := &fakeCompletions{
		annotations: []openrouter.ResponseAnnotation{
			{Type: "url_citation", URLCitation: &openrouter.ResponseURLCitation{
				URL: "https://example.com/a", Title: "a", Content: "…",
			}},
		},
	}
	tool := research.NewWebSearchTool(research.NewSearchClient(completions), research.NewURLMenu())
	ctx := authedContext(t)

	var lastErr error
	runs := 0
	for range 20 {
		var out bytes.Buffer
		err := tool.Call(ctx, toolconfig.ToolCallEnv{GramChatID: "report-1"}, strings.NewReader(`{"query": "vendor"}`), &out)
		if err != nil {
			lastErr = err
			break
		}
		runs++
	}

	require.Equal(t, 15, runs, "the run gets its budget and no more")
	require.Error(t, lastErr)
	require.Contains(t, lastErr.Error(), "search budget")

	// The budget is per run, so another run is unaffected by this one.
	var out bytes.Buffer
	require.NoError(t, tool.Call(ctx, toolconfig.ToolCallEnv{GramChatID: "report-2"}, strings.NewReader(`{"query": "vendor"}`), &out))
}

func TestWebSearch_ClampsMaxResults(t *testing.T) {
	t.Parallel()

	completions := &fakeCompletions{}
	tool := research.NewWebSearchTool(research.NewSearchClient(completions), research.NewURLMenu())

	_, err := runSearch(t, authedContext(t), tool, `{"query": "q", "max_results": 50}`)
	require.NoError(t, err)
	require.Equal(t, 10, completions.request.WebSearch.MaxResults)
}

func TestWebSearch_RequiresQuery(t *testing.T) {
	t.Parallel()

	tool := research.NewWebSearchTool(research.NewSearchClient(&fakeCompletions{}), research.NewURLMenu())

	_, err := runSearch(t, authedContext(t), tool, `{"query": "  "}`)
	require.Error(t, err)
}

func TestWebSearch_RequiresAuthContext(t *testing.T) {
	t.Parallel()

	tool := research.NewWebSearchTool(research.NewSearchClient(&fakeCompletions{}), research.NewURLMenu())

	_, err := runSearch(t, t.Context(), tool, `{"query": "q"}`)
	require.Error(t, err)
}

// fetchToolAllowing builds a fetch tool whose menu already contains pageURL
// for each run id, standing in for the search results, harvested links, or
// briefing seeds that make a URL fetchable in production.
func fetchToolAllowing(client *http.Client, pageURL string, runIDs ...string) *research.FetchPage {
	menu := research.NewURLMenu()
	for _, runID := range runIDs {
		menu.Allow(runID, pageURL)
	}
	return research.NewFetchPageTool(research.ConfigureFetchClient(client), menu)
}

func runFetch(t *testing.T, tool *research.FetchPage, chatID, input string) (map[string]any, error) {
	t.Helper()

	var out bytes.Buffer
	if err := tool.Call(t.Context(), toolconfig.ToolCallEnv{GramChatID: chatID}, strings.NewReader(input), &out); err != nil {
		return nil, fmt.Errorf("call fetch page: %w", err)
	}

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &decoded))

	return decoded, nil
}

func TestFetchPage_ExtractsReadableText(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>SomeVendor</title><style>.x{color:red}</style></head>
			<body><script>alert("never this")</script>
			<h1>Security</h1><p>We take security seriously.</p><p>Contact us.</p></body></html>`))
	}))
	t.Cleanup(server.Close)

	tool := fetchToolAllowing(server.Client(), server.URL, "chat-1")
	decoded, err := runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, server.URL))
	require.NoError(t, err)

	content, ok := decoded["content"].(string)
	require.True(t, ok)
	require.Contains(t, content, "Security")
	require.Contains(t, content, "We take security seriously.")
	require.NotContains(t, content, "alert(")
	require.NotContains(t, content, "color:red")
	require.NotContains(t, decoded, "truncated")
}

// The extractor's whole justification is surviving real-world markup, so the
// cases here are the shapes that break naive extraction: tables whose cells
// would otherwise concatenate, deep nesting, skipped subtrees nested inside
// kept ones (and inside each other), entity-encoded text, and documents that
// are simply broken mid-tag.
func TestFetchPage_ExtractsStructuredAndMalformedHTML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     string
		contains []string
		excludes []string
	}{
		{
			name: "table cells stay separated",
			body: `<table><caption>Pricing</caption>
				<tr><th>Plan</th><th>Cost</th></tr>
				<tr><td>Free</td><td>Zero</td></tr>
				<tr><td>Team</td><td><ul><li>Seat billing</li><li>SSO included</li></ul></td></tr>
			</table>`,
			contains: []string{"Pricing", "Plan", "Cost", "Free", "Zero", "Seat billing", "SSO included"},
			excludes: []string{"PlanCost", "FreeZero", "billingSSO"},
		},
		{
			name: "deeply nested blocks keep their text",
			body: `<div><section><article><div><div><blockquote><div>
				<p>Buried statement.</p>
				</div></blockquote></div></div></article></section></div>`,
			contains: []string{"Buried statement."},
		},
		{
			name: "skipped subtrees inside kept ones drop cleanly",
			body: `<div>Before diagram.
				<svg><title>chart title</title><svg><text>inner svg text</text></svg><text>outer svg text</text></svg>
				<template><p>template body</p></template>
				After diagram.</div>`,
			contains: []string{"Before diagram.", "After diagram."},
			excludes: []string{"chart title", "inner svg text", "outer svg text", "template body"},
		},
		{
			name:     "entities decode",
			body:     `<p>Fetch &amp; extract &lt;safely&gt; &quot;quoted&quot;</p>`,
			contains: []string{`Fetch & extract <safely> "quoted"`},
		},
		{
			name:     "unclosed tags degrade to the text already seen",
			body:     `<div><p>Readable up to the break.<li>Also readable.<table><tr><td>Last cell`,
			contains: []string{"Readable up to the break.", "Also readable.", "Last cell"},
		},
		{
			// Foreign-content breakout: browsers pop an unclosed svg when a
			// block-level HTML tag opens, so a broken icon in a header must
			// not swallow the rest of the page.
			name: "an unclosed svg does not swallow the page",
			body: `<header><svg viewBox="0 0 24 24"><path d="M0 0h24"/><text>icon label</text>
				<p>Content after the broken icon.</p><div>And the rest of the page.</div></header>`,
			contains: []string{"Content after the broken icon.", "And the rest of the page."},
			excludes: []string{"icon label"},
		},
		{
			// foreignObject is an HTML integration point: block elements
			// inside it are legal svg content and must not trigger the
			// unclosed-svg breakout, or the svg nodes after the subtree
			// leak into the extracted text.
			name: "foreignObject does not break out of its svg",
			body: `<div>Before chart.
				<svg><foreignObject><div><p>embedded html</p></div></foreignObject><text>axis label</text></svg>
				After chart.</div>`,
			contains: []string{"Before chart.", "After chart."},
			excludes: []string{"axis label"},
		},
		{
			// An unclosed foreignObject must die with the svg that owns it:
			// here the inner svg closes over its dangling foreignObject, so
			// the block tag that follows still pops the outer unclosed svg
			// instead of the stale foreignObject suppressing the breakout.
			name: "an unclosed foreignObject dies with its svg",
			body: `<div>Before chart.
				<svg><svg><foreignObject><span>embedded html</span></svg>
				<p>After the break.</p></div>`,
			contains: []string{"Before chart.", "After the break."},
		},
		{
			// An unclosed template genuinely captures the rest of the
			// document in a browser, so staying dark is the faithful read.
			name:     "an unclosed template stays inert",
			body:     `<p>Visible lead.</p><template><p>inert body`,
			contains: []string{"Visible lead."},
			excludes: []string{"inert body"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte(`<html><body>` + tt.body + `</body></html>`))
			}))
			t.Cleanup(server.Close)

			tool := fetchToolAllowing(server.Client(), server.URL, "chat-1")
			decoded, err := runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, server.URL))
			require.NoError(t, err)

			content, ok := decoded["content"].(string)
			require.True(t, ok)
			for _, want := range tt.contains {
				require.Contains(t, content, want)
			}
			for _, unwanted := range tt.excludes {
				require.NotContains(t, content, unwanted)
			}
		})
	}
}

func TestFetchPage_TruncatesLongPages(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(bytes.Repeat([]byte("lengthy page content "), 4000))
	}))
	t.Cleanup(server.Close)

	tool := fetchToolAllowing(server.Client(), server.URL, "chat-1")
	decoded, err := runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, server.URL))
	require.NoError(t, err)

	require.Equal(t, true, decoded["truncated"])
	content, ok := decoded["content"].(string)
	require.True(t, ok)
	require.LessOrEqual(t, len(content), 41_000)
}

// The byte cap is the bound that matters against a hostile server: the
// character clip only applies to what was already read, so a page that streams
// forever is stopped by this and nothing else. The test above hits the
// character clip long before 2 MiB, which left this path unexercised.
func TestFetchPage_StopsReadingAtTheByteCap(t *testing.T) {
	t.Parallel()

	// Well past 2 MiB, and marked so the prefix is identifiable: the tool
	// must return the head and stop, not read to the end.
	const chunk = "somevendor security notes filler "
	served := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		body := bytes.Repeat([]byte(chunk), 200_000)
		served = len(body)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	tool := fetchToolAllowing(server.Client(), server.URL, "chat-1")
	decoded, err := runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, server.URL))
	require.NoError(t, err, "an oversized page truncates rather than failing: a partial page is still material")

	require.Greater(t, served, 2<<20, "the fixture must exceed the byte cap for this to test it")
	require.Equal(t, true, decoded["truncated"])
	content, ok := decoded["content"].(string)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(content, "somevendor security notes"), "what is returned is the head of the page")
	require.LessOrEqual(t, len(content), 41_000, "and the character clip still applies on top")
}

// The https rule has to survive the redirect chain: the site under review
// chooses where it sends the fetcher, and a hop to plaintext would hand back
// content anyone on the path could have written.
func TestFetchPage_RefusesARedirectOffHTTPS(t *testing.T) {
	t.Parallel()

	plaintext := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("content an attacker on the path chose"))
	}))
	t.Cleanup(plaintext.Close)

	redirector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plaintext.URL, http.StatusFound)
	}))
	t.Cleanup(redirector.Close)

	tool := fetchToolAllowing(redirector.Client(), redirector.URL, "chat-1")
	_, err := runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, redirector.URL))
	require.Error(t, err)
	require.Contains(t, err.Error(), "every hop must stay on https")
}

// A call that cannot say which run it belongs to would share one bucket with
// every other such call, so the budget it is subject to would not be a
// per-run budget at all.
func TestResearchTools_RefuseACallWithNoRun(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("page"))
	}))
	t.Cleanup(server.Close)

	fetch := research.NewFetchPageTool(research.ConfigureFetchClient(server.Client()), research.NewURLMenu())
	var out bytes.Buffer
	err := fetch.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(fmt.Sprintf(`{"url": %q}`, server.URL)), &out)
	require.Error(t, err)
	require.Contains(t, err.Error(), "identify its run")

	search := research.NewWebSearchTool(research.NewSearchClient(&fakeCompletions{}), research.NewURLMenu())
	out.Reset()
	err = search.Call(authedContext(t), toolconfig.ToolCallEnv{}, strings.NewReader(`{"query": "vendor"}`), &out)
	require.Error(t, err)
	require.Contains(t, err.Error(), "identify its run")
}

func TestFetchPage_RefusesBinaryContent(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 0x50, 0x4e, 0x47})
	}))
	t.Cleanup(server.Close)

	tool := fetchToolAllowing(server.Client(), server.URL, "chat-1")
	_, err := runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, server.URL))
	require.Error(t, err)
	require.Contains(t, err.Error(), "image/png")
}

func TestFetchPage_RefusesNonHTTPSchemes(t *testing.T) {
	t.Parallel()

	tool := research.NewFetchPageTool(research.ConfigureFetchClient(&http.Client{}), research.NewURLMenu())

	_, err := runFetch(t, tool, "chat-1", `{"url": "ftp://host/file"}`)
	require.Error(t, err)

	_, err = runFetch(t, tool, "chat-1", `{"url": "not a url"}`)
	require.Error(t, err)

	// Plaintext http included: this tool follows links about a party under
	// review and what it returns becomes evidence, so anyone on the path must
	// not get to choose what that evidence says.
	_, err = runFetch(t, tool, "chat-1", `{"url": "http://vendor.example.com/readme"}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "only https pages are fetchable")
}

// A body nobody described is sniffed rather than waved through: an absent
// Content-Type was being read as permission, so a binary arrived as bytes the
// agent would try to quote.
func TestFetchPage_SniffsAnUndeclaredBody(t *testing.T) {
	t.Parallel()

	binary := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header()["Content-Type"] = nil
		_, _ = w.Write([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01, 0x02, 0x03})
	}))
	t.Cleanup(binary.Close)

	tool := fetchToolAllowing(binary.Client(), binary.URL, "chat-1")
	_, err := runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, binary.URL))
	require.Error(t, err)
	require.Contains(t, err.Error(), "declared no content type")

	// Text with no declared type is still fetchable: plenty of small sites
	// omit the header, and refusing them would lose real material.
	text := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header()["Content-Type"] = nil
		_, _ = w.Write([]byte("SomeVendor security notes\n\nno independent coverage"))
	}))
	t.Cleanup(text.Close)

	tool = fetchToolAllowing(text.Client(), text.URL, "chat-2")
	decoded, err := runFetch(t, tool, "chat-2", fmt.Sprintf(`{"url": %q}`, text.URL))
	require.NoError(t, err)
	require.Contains(t, decoded["content"], "no independent coverage")
}

// A non-HTML body is returned as served. Collapsing whitespace is for markup,
// where it is layout; in JSON it is structure the agent may be reading.
func TestFetchPage_KeepsNonHTMLBodiesAsServed(t *testing.T) {
	t.Parallel()

	body := "{\n  \"name\": \"somevendor-mcp\",\n  \"version\": \"1.2.3\"\n}"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	tool := fetchToolAllowing(server.Client(), server.URL, "chat-1")
	decoded, err := runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, server.URL))
	require.NoError(t, err)
	require.Equal(t, body, decoded["content"])
}

func TestFetchPage_SurfacesErrorStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	tool := fetchToolAllowing(server.Client(), server.URL, "chat-1")
	_, err := runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, server.URL))
	require.Error(t, err)
	require.Contains(t, err.Error(), "404")
}

func TestFetchPage_BoundsRedirects(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path+"x", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	tool := fetchToolAllowing(server.Client(), server.URL, "chat-1")
	_, err := runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, server.URL))
	require.Error(t, err)
	require.Contains(t, err.Error(), "redirects")
}

// The menu is the exfiltration control: a URL the model composed — rather
// than selected from what trusted code observed — must not be fetchable no
// matter how it is written.
func TestFetchPage_RefusesAURLOutsideTheMenu(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	tool := research.NewFetchPageTool(research.ConfigureFetchClient(server.Client()), research.NewURLMenu())
	_, err := runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, server.URL))
	require.Error(t, err)
	require.Contains(t, err.Error(), "not among this run's observed sources")
}

// One run's menu never unlocks another run's fetches: menus are the per-run
// record of what that run's own searches and pages presented.
func TestFetchPage_MenuIsPerRun(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	tool := fetchToolAllowing(server.Client(), server.URL, "chat-1")

	_, err := runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, server.URL))
	require.NoError(t, err)

	_, err = runFetch(t, tool, "chat-2", fmt.Sprintf(`{"url": %q}`, server.URL))
	require.Error(t, err)
	require.Contains(t, err.Error(), "not among this run's observed sources")
}

// A search result makes its URL fetchable — the search tool is a trusted
// writer to the same menu the fetch tool checks.
func TestWebSearch_ResultsUnlockFetches(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("found via search"))
	}))
	t.Cleanup(server.Close)

	completions := &fakeCompletions{
		annotations: []openrouter.ResponseAnnotation{
			{Type: "url_citation", URLCitation: &openrouter.ResponseURLCitation{
				URL: server.URL, Title: "the page", Content: "…",
			}},
		},
	}
	menu := research.NewURLMenu()
	searchTool := research.NewWebSearchTool(research.NewSearchClient(completions), menu)
	fetchTool := research.NewFetchPageTool(research.ConfigureFetchClient(server.Client()), menu)

	input := fmt.Sprintf(`{"url": %q}`, server.URL)
	_, err := runFetch(t, fetchTool, "report-default", input)
	require.Error(t, err, "before the search, the URL is not fetchable")

	_, err = runSearch(t, authedContext(t), searchTool, `{"query": "the vendor"}`)
	require.NoError(t, err)

	decoded, err := runFetch(t, fetchTool, "report-default", input)
	require.NoError(t, err)
	require.Equal(t, "found via search", decoded["content"])
}

// Links on a fetched page make their targets fetchable — harvested from the
// served markup by trusted code, which is what keeps iterative deepening
// alive under the menu rule. Relative links resolve against the page that
// served them.
func TestFetchPage_HarvestedLinksUnlockTheNextHop(t *testing.T) {
	t.Parallel()

	second := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("second hop"))
	}))
	t.Cleanup(second.Close)

	first := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/trust" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprintf(w, `<html><body><p>Trust center.</p><a href=%q>incident history</a></body></html>`, second.URL)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><a href="/trust">Trust</a></body></html>`))
	}))
	t.Cleanup(first.Close)

	// The two test servers sign with different ephemeral certificates, so the
	// client must trust both for the chain to be walked.
	transport, ok := first.Client().Transport.(*http.Transport)
	require.True(t, ok)
	transport.TLSClientConfig.RootCAs.AddCert(second.Certificate())

	menu := research.NewURLMenu()
	menu.Allow("chat-1", first.URL)
	tool := research.NewFetchPageTool(research.ConfigureFetchClient(first.Client()), menu)

	_, err := runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, first.URL))
	require.NoError(t, err)

	// The relative link resolved against the first page and is now
	// fetchable; the absolute link to the second host is too.
	decoded, err := runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, first.URL+"/trust"))
	require.NoError(t, err)
	require.Contains(t, decoded["content"], "Trust center.")

	decoded, err = runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, second.URL))
	require.NoError(t, err)
	require.Equal(t, "second hop", decoded["content"])
}

// The canonical form is the menu key: two spellings that name the same
// request must agree, and the fragment never reaches the wire.
func TestURLMenu_CanonicalizesFragments(t *testing.T) {
	t.Parallel()

	menu := research.NewURLMenu()
	menu.Allow("chat-1", "https://vendor.example.com/security#reports")

	canonical, ok := menu.Allowed("chat-1", "https://vendor.example.com/security")
	require.True(t, ok)
	require.Equal(t, "https://vendor.example.com/security", canonical)

	_, ok = menu.Allowed("chat-1", "https://vendor.example.com/other")
	require.False(t, ok)

	// Non-https and junk never enter the menu, and a port is not a host.
	menu.Allow("chat-1", "http://vendor.example.com/plain")
	_, ok = menu.Allowed("chat-1", "http://vendor.example.com/plain")
	require.False(t, ok)

	menu.Allow("chat-1", "https://:443/path")
	_, ok = menu.Allowed("chat-1", "https://:443/path")
	require.False(t, ok)
}

// One run's fetch budget is bounded; a fresh run has its own.
func TestFetchPage_EnforcesPerRunBudget(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	tool := fetchToolAllowing(server.Client(), server.URL, "chat-budget", "chat-other")
	input := fmt.Sprintf(`{"url": %q}`, server.URL)

	for range 25 {
		_, err := runFetch(t, tool, "chat-budget", input)
		require.NoError(t, err)
	}

	_, err := runFetch(t, tool, "chat-budget", input)
	require.Error(t, err)
	require.Contains(t, err.Error(), "budget")

	_, err = runFetch(t, tool, "chat-other", input)
	require.NoError(t, err)
}
