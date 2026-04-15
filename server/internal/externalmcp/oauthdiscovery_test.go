package externalmcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildWellKnownURL_OriginOnly(t *testing.T) {
	t.Parallel()
	result := buildWellKnownURL("https://auth.example.com")
	require.Equal(t, "https://auth.example.com/.well-known/oauth-authorization-server", result)
}

func TestBuildWellKnownURL_WithPath(t *testing.T) {
	t.Parallel()
	result := buildWellKnownURL("https://app.getgram.ai/mcp/speakeasy-team-snowflake")
	require.Equal(t, "https://app.getgram.ai/.well-known/oauth-authorization-server/mcp/speakeasy-team-snowflake", result)
}

func TestBuildWellKnownURL_WithTrailingSlash(t *testing.T) {
	t.Parallel()
	result := buildWellKnownURL("https://app.getgram.ai/mcp/speakeasy-team-snowflake/")
	require.Equal(t, "https://app.getgram.ai/.well-known/oauth-authorization-server/mcp/speakeasy-team-snowflake", result)
}

func TestBuildWellKnownResourceURL_OriginOnly(t *testing.T) {
	t.Parallel()
	result := buildWellKnownResourceURL("https://auth.example.com")
	require.Equal(t, "https://auth.example.com/.well-known/oauth-protected-resource", result)
}

func TestBuildWellKnownResourceURL_WithPath(t *testing.T) {
	t.Parallel()
	result := buildWellKnownResourceURL("https://app.getgram.ai/mcp/speakeasy-team-snowflake")
	require.Equal(t, "https://app.getgram.ai/.well-known/oauth-protected-resource/mcp/speakeasy-team-snowflake", result)
}

func TestParseWWWAuthenticate_Empty(t *testing.T) {
	t.Parallel()
	params := parseWWWAuthenticate("")
	require.Empty(t, params)
}

func TestParseWWWAuthenticate_WithParams(t *testing.T) {
	t.Parallel()
	header := `Bearer realm="OAuth", resource_metadata="https://example.com/.well-known/oauth-protected-resource/mcp/test"`
	params := parseWWWAuthenticate(header)
	require.Equal(t, "OAuth", params["realm"])
	require.Equal(t, "https://example.com/.well-known/oauth-protected-resource/mcp/test", params["resource_metadata"])
}
