package dialect

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsSensitiveDataKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key       string
		sensitive bool
	}{
		{key: "gen_ai.input.messages", sensitive: true},
		{key: "gen_ai.output.messages", sensitive: true},
		{key: "gen_ai.tool.call.arguments", sensitive: true},
		{key: "gen_ai.tool.call.result", sensitive: true},
		{key: "gen_ai.system_instructions", sensitive: true},
		{key: "user_prompt", sensitive: true},
		{key: "prompt", sensitive: true},
		{key: "tool.args", sensitive: true},
		{key: "tool_result", sensitive: true},
		{key: "content", sensitive: true},
		{key: "assistant", sensitive: true},
		{key: "enduser.id", sensitive: true},
		{key: "enduser.email", sensitive: true},
		{key: "user.id", sensitive: true},
		{key: "user.email", sensitive: true},
		{key: "gen_ai.input", sensitive: false},
		{key: "gen_ai.system", sensitive: false},
		{key: "username", sensitive: false},
		{key: "model", sensitive: false},
	}

	for _, test := range tests {
		require.Equalf(t, test.sensitive, IsSensitiveDataKey(test.key), "key %q", test.key)
	}
}
