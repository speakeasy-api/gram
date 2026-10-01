package externalmcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/stretchr/testify/require"
)

type nativeTestSource struct {
	pages   []mcpregistry.DiscoveryPage
	options []mcpregistry.DiscoveryOptions
	entry   mcpregistry.Entry
	name    string
	err     error
}

func (s *nativeTestSource) Discover(_ context.Context, o mcpregistry.DiscoveryOptions) (mcpregistry.DiscoveryPage, error) {
	s.options = append(s.options, o)
	if s.err != nil {
		return mcpregistry.DiscoveryPage{}, s.err
	}
	p := s.pages[0]
	s.pages = s.pages[1:]
	return p, nil
}
func (s *nativeTestSource) GetByName(_ context.Context, name string) (mcpregistry.Entry, error) {
	s.name = name
	return s.entry, s.err
}

const nativeRecord = `{"server":{"name":"io.example/native","title":"Native","description":"Synthetic registry server","version":"1","repository":{"url":"https://example.com/repo"},"remotes":[{"type":"sse","url":"https://example.com/first"},{"type":"sse","url":"https://example.com/last"},{"type":"streamable-http","url":"https://example.com/mcp","headers":[{"name":"X-Key","isSecret":true}],"variables":{"tenant":{"description":"Tenant"}}}]},"_meta":{"custom.example/metadata":{"keep":true},"io.modelcontextprotocol.registry/official":{"status":"deprecated"},"com.pulsemcp/server-version":{"remotes[0]":{"tools":[{"name":"read","annotations":{"readOnlyHint":true}}]},"remotes[1]":{"tools":[]}}}}`

func TestNativeList(t *testing.T) {
	source := &nativeTestSource{pages: []mcpregistry.DiscoveryPage{{NextCursor: "next"}, {Records: []json.RawMessage{json.RawMessage(nativeRecord)}}}}
	registry := Registry{ID: uuid.New()}
	search := "Native"
	result, err := NewNativeRegistryReader(source).ListServers(t.Context(), registry, ListServersParams{Search: &search})
	require.NoError(t, err)
	require.Len(t, result.Servers, 1)
	row := result.Servers[0]
	require.Equal(t, registry.ID.String(), *row.RegistryID)
	require.NotNil(t, row.Title)
	require.Equal(t, "Native", *row.Title)
	require.NotNil(t, row.Repository)
	require.Equal(t, 1, row.ToolCount)
	require.True(t, row.IsReadOnly)
	require.Equal(t, map[string]any{"keep": true}, row.Meta.(map[string]any)["custom.example/metadata"])
	require.Equal(t, "next", source.options[1].Cursor)
	require.False(t, source.options[0].IncludeDeleted)
}

func TestNativeDetailsRetainedRemoteSelection(t *testing.T) {
	for _, tc := range []struct {
		name       string
		allowed    []string
		url        string
		emptyTools bool
	}{
		{name: "http preferred", url: "https://example.com/mcp"},
		{name: "last SSE wins", allowed: []string{"https://example.com/first", "https://example.com/last"}, url: "https://example.com/last", emptyTools: true},
		{name: "no allowed match", allowed: []string{"https://example.com/none"}},
		{name: "first tools", allowed: []string{"https://example.com/first"}, url: "https://example.com/first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &nativeTestSource{entry: mcpregistry.Entry{ID: uuid.New(), Data: json.RawMessage(nativeRecord), Published: false}}
			details, err := NewNativeRegistryReader(source).GetServerDetails(t.Context(), Registry{ID: uuid.New()}, "io.example/native", tc.allowed)
			require.NoError(t, err)
			require.Equal(t, "io.example/native", source.name)
			require.Equal(t, tc.url, details.RemoteURL)
			if tc.emptyTools {
				require.NotNil(t, details.Tools)
				require.Empty(t, details.Tools)
			} else if tc.url == "https://example.com/first" {
				require.Len(t, details.Tools, 1)
			} else {
				require.Nil(t, details.Tools)
			}
			if tc.url == "https://example.com/mcp" {
				require.Len(t, details.Headers, 1)
				require.Contains(t, details.Variables, "tenant")
			}
		})
	}
}

func TestNativeErrors(t *testing.T) {
	source := &nativeTestSource{err: mcpregistry.ErrNotFound}
	reader := NewNativeRegistryReader(source)
	_, err := reader.GetServerDetails(t.Context(), Registry{}, "missing", nil)
	require.ErrorIs(t, err, mcpregistry.ErrNotFound)
	source.err = errors.New("unavailable")
	_, err = reader.ListServers(t.Context(), Registry{}, ListServersParams{})
	require.ErrorIs(t, err, source.err)
	source.err = nil
	source.entry.Data = json.RawMessage(`{`)
	_, err = reader.GetServerDetails(t.Context(), Registry{}, "broken", nil)
	require.Error(t, err)
}

func TestNativeListIgnoresStalePulseLifecycle(t *testing.T) {
	raw := strings.Replace(nativeRecord, `"status":"deprecated"`, `"status":"active"`, 1)
	raw = strings.Replace(raw, `"com.pulsemcp/server-version":{`, `"com.pulsemcp/server-version":{"status":"deleted",`, 1)
	source := &nativeTestSource{pages: []mcpregistry.DiscoveryPage{{Records: []json.RawMessage{json.RawMessage(raw)}}}}
	result, err := NewNativeRegistryReader(source).ListServers(t.Context(), Registry{ID: uuid.New()}, ListServersParams{})
	require.NoError(t, err)
	require.Len(t, result.Servers, 1)
	require.Equal(t, "deleted", result.Servers[0].Meta.(map[string]any)["com.pulsemcp/server-version"].(map[string]any)["status"])
}

func TestNativeListPrunesToolsPreservingMetadata(t *testing.T) {
	var record map[string]any
	require.NoError(t, json.Unmarshal([]byte(nativeRecord), &record))
	meta := record["_meta"].(map[string]any)
	version := meta["com.pulsemcp/server-version"].(map[string]any)
	auth := []any{map[string]any{"type": "oauth", "detail": map[string]any{"authorizationServerMetadata": map[string]any{"registration_endpoint": "https://example.com/register"}, "custom": true}}}
	for _, key := range []string{"remotes[0]", "remotes[1]", "remotes[2]", "remotes[3]", "remotes[4]", "remotes[5]", "remotes[12]"} {
		version[key] = map[string]any{"tools": []any{map[string]any{"name": "read", "inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"readOnlyHint": true}}}, "authOptions": auth, "custom": "retained"}
	}
	raw, err := json.Marshal(record)
	require.NoError(t, err)
	source := &nativeTestSource{pages: []mcpregistry.DiscoveryPage{{Records: []json.RawMessage{raw}}}}
	result, err := NewNativeRegistryReader(source).ListServers(t.Context(), Registry{ID: uuid.New()}, ListServersParams{})
	require.NoError(t, err)
	require.Len(t, result.Servers, 1)
	row := result.Servers[0]
	require.Equal(t, 1, row.ToolCount)
	require.True(t, row.IsReadOnly)
	require.True(t, row.SupportsDcr)
	got := row.Meta.(map[string]any)
	require.Equal(t, meta["custom.example/metadata"], got["custom.example/metadata"])
	require.Equal(t, meta["io.modelcontextprotocol.registry/official"], got["io.modelcontextprotocol.registry/official"])
	for _, key := range []string{"remotes[0]", "remotes[1]", "remotes[2]", "remotes[3]", "remotes[4]", "remotes[5]", "remotes[12]"} {
		remote := got["com.pulsemcp/server-version"].(map[string]any)[key].(map[string]any)
		require.Empty(t, remote["tools"], key)
		require.Equal(t, auth, remote["authOptions"])
		require.Equal(t, "retained", remote["custom"])
	}
}

func TestNativeListSearch(t *testing.T) {
	for _, tc := range []struct {
		search string
		count  int
	}{
		{"IO.EXAMPLE", 1}, {"NATIVE", 1}, {"synthetic registry", 1}, {"not present", 0},
	} {
		t.Run(tc.search, func(t *testing.T) {
			source := &nativeTestSource{pages: []mcpregistry.DiscoveryPage{{Records: []json.RawMessage{json.RawMessage(nativeRecord)}}}}
			result, err := NewNativeRegistryReader(source).ListServers(t.Context(), Registry{ID: uuid.New()}, ListServersParams{Search: &tc.search})
			require.NoError(t, err)
			require.Len(t, result.Servers, tc.count)
		})
	}
}

type nativeEvidenceTestSource struct {
	nativeTestSource
	evidencePages   []mcpregistry.Page
	evidenceOptions []mcpregistry.ListOptions
	names           []string
}

func (s *nativeEvidenceTestSource) List(_ context.Context, opts mcpregistry.ListOptions) (mcpregistry.Page, error) {
	s.evidenceOptions = append(s.evidenceOptions, opts)
	page := s.evidencePages[0]
	s.evidencePages = s.evidencePages[1:]
	return page, nil
}

func (s *nativeEvidenceTestSource) GetByName(ctx context.Context, name string) (mcpregistry.Entry, error) {
	s.names = append(s.names, name)
	return s.nativeTestSource.GetByName(ctx, name)
}

func TestNativeListEvidenceServers(t *testing.T) {
	t.Parallel()
	const valid = `{"server":{"name":"io.example/evidence","description":"Evidence server","version":"1","remotes":[{"type":"streamable-http","url":"https://example.com/mcp"}]},"_meta":{"io.modelcontextprotocol.registry/official":{"status":"active"}}}`
	summary := mcpregistry.Summary{Name: "io.example/evidence"}
	for _, tc := range []struct {
		name        string
		data        string
		pages       []mcpregistry.Page
		wantCount   int
		wantLookups int
		cursors     []string
	}{
		{name: "eligible unpublished", data: valid, pages: []mcpregistry.Page{{Entries: []mcpregistry.Summary{summary}}}, wantCount: 1, wantLookups: 1, cursors: []string{""}},
		{name: "summary issues", data: valid, pages: []mcpregistry.Page{{Entries: []mcpregistry.Summary{{Name: summary.Name, Issues: []mcpregistry.Issue{{Message: "invalid"}}}}}}, cursors: []string{""}},
		{name: "invalid stored record", data: `{}`, pages: []mcpregistry.Page{{Entries: []mcpregistry.Summary{summary}}}, wantLookups: 1, cursors: []string{""}},
		{name: "deleted", data: strings.Replace(valid, "active", "deleted", 1), pages: []mcpregistry.Page{{Entries: []mcpregistry.Summary{summary}}}, wantLookups: 1, cursors: []string{""}},
		{name: "deprecated retained", data: strings.Replace(valid, "active", "deprecated", 1), pages: []mcpregistry.Page{{Entries: []mcpregistry.Summary{summary}}}, wantCount: 1, wantLookups: 1, cursors: []string{""}},
		{name: "pagination after filtered page", data: valid, pages: []mcpregistry.Page{{Entries: []mcpregistry.Summary{{Issues: []mcpregistry.Issue{{Message: "invalid"}}}}, NextCursor: "next"}, {Entries: []mcpregistry.Summary{summary}, NextCursor: "last"}, {Entries: []mcpregistry.Summary{summary}}}, wantCount: 2, wantLookups: 2, cursors: []string{"", "next", "last"}},
		{name: "empty", data: valid, pages: []mcpregistry.Page{{}}, cursors: []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := &nativeEvidenceTestSource{nativeTestSource: nativeTestSource{entry: mcpregistry.Entry{Data: json.RawMessage(tc.data)}}, evidencePages: tc.pages}
			registry := Registry{ID: uuid.New()}
			result, err := NewNativeRegistryReader(source).ListEvidenceServers(t.Context(), registry)
			require.NoError(t, err)
			require.Len(t, result.Servers, tc.wantCount)
			require.Len(t, source.names, tc.wantLookups)
			for _, name := range source.names {
				require.Equal(t, summary.Name, name)
			}
			require.Len(t, source.evidenceOptions, len(tc.cursors))
			for i, cursor := range tc.cursors {
				require.Equal(t, mcpregistry.ListOptions{Limit: 50, Cursor: cursor}, source.evidenceOptions[i])
			}
			for _, server := range result.Servers {
				require.Equal(t, registry.ID.String(), *server.RegistryID)
			}
		})
	}
}
