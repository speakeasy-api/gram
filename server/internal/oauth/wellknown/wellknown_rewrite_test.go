package wellknown

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRewriteMetadataIssuer_Object rewrites the top-level issuer while leaving
// every other field of the captured upstream document untouched.
func TestRewriteMetadataIssuer_Object(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"issuer":"https://upstream.example.com","token_endpoint":"https://upstream.example.com/token"}`)
	out, err := rewriteMetadataIssuer(raw, "https://gram.example.com/mcp/foo")
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	require.Equal(t, "https://gram.example.com/mcp/foo", got["issuer"])
	require.Equal(t, "https://upstream.example.com/token", got["token_endpoint"])
}

// TestRewriteMetadataIssuer_NullPayload verifies a JSON null document, which
// unmarshals into a nil map, returns an error rather than panicking on the
// issuer assignment.
func TestRewriteMetadataIssuer_NullPayload(t *testing.T) {
	t.Parallel()

	_, err := rewriteMetadataIssuer(json.RawMessage("null"), "https://gram.example.com/mcp/foo")
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected a JSON object")
}
