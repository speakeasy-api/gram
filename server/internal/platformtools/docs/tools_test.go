package docs

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// stubBody is short enough to trip minMeaningfulPageLen, standing in for the
// section landing pages whose markdown counterpart is essentially empty.
const stubBody = "# Guides\n"

// newDocsServer serves both the sitemap and per-page markdown, and counts page
// fetches so tests can assert that an unindexed path is refused without any
// outbound request.
func newDocsServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var pageFetches atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == sitemapPath {
			_, _ = w.Write([]byte(fixtureSitemap))
			return
		}

		pageFetches.Add(1)
		w.Header().Set("Content-Type", "text/markdown")

		switch r.URL.Path {
		case "/docs/ai-control-plane/observe/tool-logs.md":
			_, _ = w.Write([]byte("# Tool Logs\n\n" + strings.Repeat("Raw execution log for every tool call. ", 10)))
		case "/docs/ai-control-plane/guides.md":
			_, _ = w.Write([]byte(stubBody))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server, &pageFetches
}

func TestListDocsReturnsWholeIndexByDefault(t *testing.T) {
	t.Parallel()

	server, _ := newDocsServer(t)
	tool := NewListDocsTool(NewClient(server.Client(), server.URL))

	var out bytes.Buffer
	require.NoError(t, tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(`{}`), &out))

	var result listDocsResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.Pages, 4)
	require.Equal(t, 4, result.Total)
	require.False(t, result.Truncated)
	require.Equal(t, []string{"guides", "observe"}, result.Sections)
	require.Equal(t, server.URL+PathPrefix, result.SourceURL)
}

func TestListDocsFiltersByQuery(t *testing.T) {
	t.Parallel()

	server, _ := newDocsServer(t)
	tool := NewListDocsTool(NewClient(server.Client(), server.URL))

	var out bytes.Buffer
	require.NoError(t, tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(`{"query":"TOOL-LOGS"}`), &out))

	var result listDocsResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.Pages, 1)
	require.Equal(t, "/docs/ai-control-plane/observe/tool-logs", result.Pages[0].Path)
}

func TestListDocsFiltersBySection(t *testing.T) {
	t.Parallel()

	server, _ := newDocsServer(t)
	tool := NewListDocsTool(NewClient(server.Client(), server.URL))

	var out bytes.Buffer
	require.NoError(t, tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(`{"section":"guides"}`), &out))

	// A section's own landing page carries that section too, so it comes back
	// alongside its children.
	var result listDocsResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.Pages, 2)
	require.Equal(t, "/docs/ai-control-plane/guides", result.Pages[0].Path)
	require.Equal(t, "/docs/ai-control-plane/guides/github", result.Pages[1].Path)
}

func TestListDocsRejectsUnknownSectionWithOptions(t *testing.T) {
	t.Parallel()

	server, _ := newDocsServer(t)
	tool := NewListDocsTool(NewClient(server.Client(), server.URL))

	var out bytes.Buffer
	err := tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(`{"section":"observability"}`), &out)
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown section "observability"`)
	require.Contains(t, err.Error(), "guides, observe")
}

func TestGetDocReturnsPageContent(t *testing.T) {
	t.Parallel()

	server, _ := newDocsServer(t)
	tool := NewGetDocTool(NewClient(server.Client(), server.URL))

	var out bytes.Buffer
	payload := `{"path":"/docs/ai-control-plane/observe/tool-logs"}`
	require.NoError(t, tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(payload), &out))

	var result getDocResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, "Tool logs", result.Title)
	require.Equal(t, "observe", result.Section)
	require.Equal(t, server.URL+"/docs/ai-control-plane/observe/tool-logs", result.URL)
	require.Contains(t, result.Content, "# Tool Logs")
	require.Empty(t, result.ChildPages)
	require.Empty(t, result.Note)
}

func TestGetDocAcceptsTrailingSlashPath(t *testing.T) {
	t.Parallel()

	server, _ := newDocsServer(t)
	tool := NewGetDocTool(NewClient(server.Client(), server.URL))

	var out bytes.Buffer
	payload := `{"path":"/docs/ai-control-plane/observe/tool-logs/"}`
	require.NoError(t, tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(payload), &out))

	var result getDocResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, "/docs/ai-control-plane/observe/tool-logs", result.Path)
}

func TestGetDocAcceptsMarkdownSuffixPath(t *testing.T) {
	t.Parallel()

	server, _ := newDocsServer(t)
	tool := NewGetDocTool(NewClient(server.Client(), server.URL))

	var out bytes.Buffer
	payload := `{"path":"/docs/ai-control-plane/observe/tool-logs.md"}`
	require.NoError(t, tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(payload), &out))

	var result getDocResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, "/docs/ai-control-plane/observe/tool-logs", result.Path)
}

func TestGetDocAcceptsFullPermalink(t *testing.T) {
	t.Parallel()

	server, _ := newDocsServer(t)
	tool := NewGetDocTool(NewClient(server.Client(), server.URL))

	var out bytes.Buffer
	payload := `{"path":"https://www.speakeasy.com/docs/ai-control-plane/observe/tool-logs"}`
	require.NoError(t, tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(payload), &out))

	var result getDocResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, "/docs/ai-control-plane/observe/tool-logs", result.Path)
}

// An off-corpus path must be refused from the index alone. This is what keeps
// the tool a documentation reader rather than a general-purpose fetcher, so it
// asserts no outbound page request was made at all.
func TestGetDocRefusesUnindexedPathWithoutFetching(t *testing.T) {
	t.Parallel()

	server, pageFetches := newDocsServer(t)
	tool := NewGetDocTool(NewClient(server.Client(), server.URL))

	var out bytes.Buffer
	payload := `{"path":"/docs/sdks/create-client-sdks"}`
	err := tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(payload), &out)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown documentation page")
	require.Equal(t, int64(0), pageFetches.Load())
}

func TestGetDocRequiresPath(t *testing.T) {
	t.Parallel()

	server, _ := newDocsServer(t)
	tool := NewGetDocTool(NewClient(server.Client(), server.URL))

	var out bytes.Buffer
	err := tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(`{}`), &out)
	require.Error(t, err)
	require.Contains(t, err.Error(), "path is required")
}

// Section landing pages have a near-empty markdown counterpart on the live
// site, so a bare body would read as "no documentation exists". The tool
// substitutes the section's child pages instead.
func TestGetDocSubstitutesChildPagesForStubLandingPage(t *testing.T) {
	t.Parallel()

	server, _ := newDocsServer(t)
	tool := NewGetDocTool(NewClient(server.Client(), server.URL))

	var out bytes.Buffer
	payload := `{"path":"/docs/ai-control-plane/guides"}`
	require.NoError(t, tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(payload), &out))

	var result getDocResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.ChildPages, 1)
	require.Equal(t, "/docs/ai-control-plane/guides/github", result.ChildPages[0].Path)
	require.Contains(t, result.Note, "section landing page")
}

// The tools share a client, so listing then reading must not re-fetch the
// sitemap.
func TestSharedClientFetchesIndexOnce(t *testing.T) {
	t.Parallel()

	var sitemapFetches atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == sitemapPath {
			sitemapFetches.Add(1)
			_, _ = w.Write([]byte(fixtureSitemap))
			return
		}
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = w.Write([]byte("# Tool Logs\n\n" + strings.Repeat("Body text. ", 30)))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.Client(), server.URL)
	list := NewListDocsTool(client)
	get := NewGetDocTool(client)

	var listOut, getOut bytes.Buffer
	require.NoError(t, list.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(`{}`), &listOut))
	payload := `{"path":"/docs/ai-control-plane/observe/tool-logs"}`
	require.NoError(t, get.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(payload), &getOut))

	require.Equal(t, int64(1), sitemapFetches.Load())
}
