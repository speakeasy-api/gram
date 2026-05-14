package catalog

import (
	"testing"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/stretchr/testify/require"
)

func TestDefaultDisplayName_PrefersSpecifierTail(t *testing.T) {
	t.Parallel()

	require.Equal(t, "exa", defaultDisplayName("io.modelcontextprotocol.anonymous/exa", "ignored"))
	require.Equal(t, "server", defaultDisplayName("server", "ignored"))
	require.Equal(t, "fallback", defaultDisplayName("", "fallback"))
}

func TestSelectRemote_PrefersStreamableHTTPByDefault(t *testing.T) {
	t.Parallel()

	sse := &types.ExternalMCPRemote{URL: "https://sse.example/mcp", TransportType: "sse", Headers: nil, Variables: nil}
	stream := &types.ExternalMCPRemote{URL: "https://stream.example/mcp", TransportType: "streamable-http", Headers: nil, Variables: nil}

	got, err := selectRemote([]*types.ExternalMCPRemote{sse, stream}, nil, nil)
	require.NoError(t, err)
	require.Same(t, stream, got)
}

func TestSelectRemote_FallsBackToSSE(t *testing.T) {
	t.Parallel()

	sse := &types.ExternalMCPRemote{URL: "https://sse.example/mcp", TransportType: "sse", Headers: nil, Variables: nil}

	got, err := selectRemote([]*types.ExternalMCPRemote{sse}, nil, nil)
	require.NoError(t, err)
	require.Same(t, sse, got)
}

func TestSelectRemote_RemoteURLOverride(t *testing.T) {
	t.Parallel()

	a := &types.ExternalMCPRemote{URL: "https://a.example/mcp", TransportType: "streamable-http", Headers: nil, Variables: nil}
	b := &types.ExternalMCPRemote{URL: "https://b.example/mcp", TransportType: "streamable-http", Headers: nil, Variables: nil}
	wanted := "https://b.example/mcp"

	got, err := selectRemote([]*types.ExternalMCPRemote{a, b}, &wanted, nil)
	require.NoError(t, err)
	require.Same(t, b, got)
}

func TestSelectRemote_TransportFilter(t *testing.T) {
	t.Parallel()

	sse := &types.ExternalMCPRemote{URL: "https://sse.example/mcp", TransportType: "sse", Headers: nil, Variables: nil}
	stream := &types.ExternalMCPRemote{URL: "https://stream.example/mcp", TransportType: "streamable-http", Headers: nil, Variables: nil}
	want := "sse"

	got, err := selectRemote([]*types.ExternalMCPRemote{stream, sse}, nil, &want)
	require.NoError(t, err)
	require.Same(t, sse, got)
}

func TestResolveRemoteURL_SubstitutesAndAppliesDefaults(t *testing.T) {
	t.Parallel()

	required := true
	defVal := "us-east-1"
	declared := map[string]*types.ExternalMCPRemoteVariable{
		"region":  {Description: nil, IsRequired: nil, IsSecret: nil, Default: &defVal, Choices: nil},
		"account": {Description: nil, IsRequired: &required, IsSecret: nil, Default: nil, Choices: nil},
	}

	got, err := resolveRemoteURL("https://{region}.example/{account}/mcp", declared, map[string]string{"account": "42"})
	require.NoError(t, err)
	require.Equal(t, "https://us-east-1.example/42/mcp", got)
}

func TestResolveRemoteURL_MissingRequired(t *testing.T) {
	t.Parallel()

	required := true
	declared := map[string]*types.ExternalMCPRemoteVariable{
		"account": {Description: nil, IsRequired: &required, IsSecret: nil, Default: nil, Choices: nil},
	}

	_, err := resolveRemoteURL("https://example/{account}/mcp", declared, nil)
	require.Error(t, err)
}

func TestResolveRemoteURL_RejectsUnknownChoice(t *testing.T) {
	t.Parallel()

	declared := map[string]*types.ExternalMCPRemoteVariable{
		"region": {Description: nil, IsRequired: nil, IsSecret: nil, Default: nil, Choices: []string{"us", "eu"}},
	}

	_, err := resolveRemoteURL("https://{region}.example/mcp", declared, map[string]string{"region": "ap"})
	require.Error(t, err)
}

func TestBuildHeaderInputs_SkipsOptionalAndKeepsRequired(t *testing.T) {
	t.Parallel()

	required := true
	declared := []*types.ExternalMCPRemoteHeader{
		{Name: "X-Api-Key", Description: nil, IsSecret: nil, IsRequired: &required, Placeholder: nil},
		{Name: "X-Optional", Description: nil, IsSecret: nil, IsRequired: nil, Placeholder: nil},
	}

	out, err := buildHeaderInputs(declared, map[string]string{"X-Api-Key": "abc"})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "X-Api-Key", out[0].Name)
}

func TestBuildHeaderInputs_MissingRequiredErrors(t *testing.T) {
	t.Parallel()

	required := true
	declared := []*types.ExternalMCPRemoteHeader{
		{Name: "X-Api-Key", Description: nil, IsSecret: nil, IsRequired: &required, Placeholder: nil},
	}

	_, err := buildHeaderInputs(declared, nil)
	require.Error(t, err)
}
