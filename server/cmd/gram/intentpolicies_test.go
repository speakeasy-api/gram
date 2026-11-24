package gram

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveIntentPoliciesKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, intent, sigint string
		local                bool
		dev, want            string
	}{
		{name: "intent key wins", intent: "a", sigint: "b", local: true, dev: "c", want: "a"},
		{name: "empty intent falls back to sigint", intent: "  ", sigint: "b", want: "b"},
		{name: "local falls back to dev key", local: true, dev: "c", want: "c"},
		{name: "dev key ignored outside local", dev: "c", want: ""},
		{name: "dev placeholder disables", local: true, dev: "unset", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, resolveIntentPoliciesKey(tt.intent, tt.sigint, tt.local, tt.dev))
		})
	}
}
