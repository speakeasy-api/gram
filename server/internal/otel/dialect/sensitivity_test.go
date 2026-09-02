package dialect

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifySensitiveDataKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key   string
		class SensitivityClass
	}{
		{key: "gen_ai.input.messages", class: SensitivityContent},
		{key: "gen_ai.output.messages", class: SensitivityContent},
		{key: "gen_ai.tool.call.arguments", class: SensitivityContent},
		{key: "gen_ai.tool.call.result", class: SensitivityContent},
		{key: "gen_ai.system_instructions", class: SensitivityContent},
		{key: "user_prompt", class: SensitivityContent},
		{key: "prompt", class: SensitivityContent},
		{key: "tool.args", class: SensitivityContent},
		{key: "tool_result", class: SensitivityContent},
		{key: "content", class: SensitivityContent},
		{key: "assistant", class: SensitivityContent},
		{key: "enduser.id", class: SensitivityIdentity},
		{key: "enduser.email", class: SensitivityIdentity},
		{key: "user.id", class: SensitivityIdentity},
		{key: "user.email", class: SensitivityIdentity},
		{key: "gen_ai.input", class: SensitivityNone},
		{key: "gen_ai.system", class: SensitivityNone},
		{key: "username", class: SensitivityNone},
		{key: "model", class: SensitivityNone},
	}

	for _, test := range tests {
		require.Equalf(t, test.class, ClassifySensitiveDataKey(test.key), "key %q", test.key)
	}
}
