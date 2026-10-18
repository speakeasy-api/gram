package chat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMessageModelAttributes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		model    string
		provider string
		want     map[string]string
	}{
		{name: "unknown"},
		{name: "blank", model: "  ", provider: "\t"},
		{name: "unknown sentinels", model: "<unset>", provider: "unknown"},
		{name: "first party", model: "gpt-6.1-sol", provider: "OpenAI", want: map[string]string{"model_name": "gpt-6.1-sol", "model_family": "openai", "model_provider": "openai"}},
		{name: "routed", model: "anthropic/claude-opus-5", provider: " OpenRouter ", want: map[string]string{"model_name": "anthropic/claude-opus-5", "model_family": "anthropic", "model_provider": "openrouter"}},
		{name: "bedrock", model: "anthropic/claude-opus-5", provider: "Amazon Bedrock", want: map[string]string{"model_name": "anthropic/claude-opus-5", "model_family": "anthropic", "model_provider": "bedrock"}},
		{name: "bedrock region", model: "us.anthropic.claude-sonnet-4-6-v1:0", provider: "bedrock", want: map[string]string{"model_name": "us.anthropic.claude-sonnet-4-6-v1:0", "model_family": "anthropic", "model_provider": "bedrock"}},
		{name: "routed model namespace", model: "openrouter/anthropic/claude-opus-5", want: map[string]string{"model_name": "openrouter/anthropic/claude-opus-5", "model_family": "anthropic"}},
		{name: "family is not provider", model: "claude-opus-5", want: map[string]string{"model_name": "claude-opus-5", "model_family": "anthropic"}},
		{name: "provider is not family", provider: "openai", want: map[string]string{"model_provider": "openai"}},
		{name: "unknown namespace", model: "deployment/custom-model", provider: "openrouter", want: map[string]string{"model_name": "deployment/custom-model", "model_provider": "openrouter"}},
		{name: "unknown alias", model: "best-model", want: map[string]string{"model_name": "best-model"}},
		{name: "invalid namespace", model: "anthropic/", want: map[string]string{"model_name": "anthropic/"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, messageAttributes(tc.model, tc.provider, ""))
		})
	}
}
