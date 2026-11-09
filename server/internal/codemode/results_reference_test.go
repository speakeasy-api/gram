package codemode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestRejectedResult_ACappedMessageKeepsTheWholeReference(t *testing.T) {
	t.Parallel()

	reference := contextvalues.ConnectionReferenceMarker + "6f1c0000-0000-0000-0000-000000000000"
	result := rejectedResult(strings.Repeat("x", maxRejectionBytes*2) + reference)

	var content []map[string]string
	require.NoError(t, json.Unmarshal(result.Content, &content))
	text := content[0]["text"]
	require.True(t, strings.HasSuffix(text, reference))
	require.LessOrEqual(t, len(text), maxRejectionBytes)
}

func TestRejectedResult_NoReferenceIsCappedAsBefore(t *testing.T) {
	t.Parallel()

	result := rejectedResult(strings.Repeat("x", maxRejectionBytes*2))
	var content []map[string]string
	require.NoError(t, json.Unmarshal(result.Content, &content))
	require.Len(t, content[0]["text"], maxRejectionBytes)
}

func TestRejectedResult_AnOversizedUpstreamMarkerCannotPanic(t *testing.T) {
	t.Parallel()

	for _, message := range []string{
		strings.Repeat("x", 2000) + contextvalues.ConnectionReferenceMarker + strings.Repeat("y", 2000),
		contextvalues.ConnectionReferenceMarker + strings.Repeat("y", maxRejectionBytes*3),
		strings.Repeat("x", 2000) + contextvalues.ConnectionReferenceMarker + "not-a-uuid",
	} {
		result := rejectedResult(message)
		var content []map[string]string
		require.NoError(t, json.Unmarshal(result.Content, &content))
		require.LessOrEqual(t, len(content[0]["text"]), maxRejectionBytes)
	}
}
