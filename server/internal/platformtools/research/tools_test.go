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
	err         error
}

func (f *fakeCompletions) GetCompletion(_ context.Context, req openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	f.request = req
	if f.err != nil {
		return nil, f.err
	}

	return &openrouter.CompletionResponse{Annotations: f.annotations, Content: "prose the tool must discard"}, nil
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
	if err := tool.Call(ctx, toolconfig.ToolCallEnv{}, strings.NewReader(input), &out); err != nil {
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
	tool := research.NewWebSearchTool(research.NewSearchClient(completions))

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
}

func TestWebSearch_ClampsMaxResults(t *testing.T) {
	t.Parallel()

	completions := &fakeCompletions{}
	tool := research.NewWebSearchTool(research.NewSearchClient(completions))

	_, err := runSearch(t, authedContext(t), tool, `{"query": "q", "max_results": 50}`)
	require.NoError(t, err)
	require.Equal(t, 10, completions.request.WebSearch.MaxResults)
}

func TestWebSearch_RequiresQuery(t *testing.T) {
	t.Parallel()

	tool := research.NewWebSearchTool(research.NewSearchClient(&fakeCompletions{}))

	_, err := runSearch(t, authedContext(t), tool, `{"query": "  "}`)
	require.Error(t, err)
}

func TestWebSearch_RequiresAuthContext(t *testing.T) {
	t.Parallel()

	tool := research.NewWebSearchTool(research.NewSearchClient(&fakeCompletions{}))

	_, err := runSearch(t, t.Context(), tool, `{"query": "q"}`)
	require.Error(t, err)
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>SomeVendor</title><style>.x{color:red}</style></head>
			<body><script>alert("never this")</script>
			<h1>Security</h1><p>We take security seriously.</p><p>Contact us.</p></body></html>`))
	}))
	t.Cleanup(server.Close)

	tool := research.NewFetchPageTool(research.ConfigureFetchClient(server.Client()))
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

func TestFetchPage_TruncatesLongPages(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(bytes.Repeat([]byte("lengthy page content "), 4000))
	}))
	t.Cleanup(server.Close)

	tool := research.NewFetchPageTool(research.ConfigureFetchClient(server.Client()))
	decoded, err := runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, server.URL))
	require.NoError(t, err)

	require.Equal(t, true, decoded["truncated"])
	content, ok := decoded["content"].(string)
	require.True(t, ok)
	require.LessOrEqual(t, len(content), 41_000)
}

func TestFetchPage_RefusesBinaryContent(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 0x50, 0x4e, 0x47})
	}))
	t.Cleanup(server.Close)

	tool := research.NewFetchPageTool(research.ConfigureFetchClient(server.Client()))
	_, err := runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, server.URL))
	require.Error(t, err)
	require.Contains(t, err.Error(), "image/png")
}

func TestFetchPage_RefusesNonHTTPSchemes(t *testing.T) {
	t.Parallel()

	tool := research.NewFetchPageTool(research.ConfigureFetchClient(&http.Client{}))

	_, err := runFetch(t, tool, "chat-1", `{"url": "ftp://host/file"}`)
	require.Error(t, err)

	_, err = runFetch(t, tool, "chat-1", `{"url": "not a url"}`)
	require.Error(t, err)
}

func TestFetchPage_SurfacesErrorStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	tool := research.NewFetchPageTool(research.ConfigureFetchClient(server.Client()))
	_, err := runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, server.URL))
	require.Error(t, err)
	require.Contains(t, err.Error(), "404")
}

func TestFetchPage_BoundsRedirects(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path+"x", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	tool := research.NewFetchPageTool(research.ConfigureFetchClient(server.Client()))
	_, err := runFetch(t, tool, "chat-1", fmt.Sprintf(`{"url": %q}`, server.URL))
	require.Error(t, err)
	require.Contains(t, err.Error(), "redirects")
}

// One run's fetch budget is bounded; a fresh run has its own.
func TestFetchPage_EnforcesPerRunBudget(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	tool := research.NewFetchPageTool(research.ConfigureFetchClient(server.Client()))
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
