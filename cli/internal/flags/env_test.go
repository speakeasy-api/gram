package flags

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func lookupIn(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

func TestEnvVars_NewNameFirst(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"SPEAKEASY_AI_API_KEY", "GRAM_API_KEY"}, EnvVars("API_KEY"))
}

func TestLegacyEnvNotice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "nothing set", env: map[string]string{}, want: ""},
		{name: "only new names", env: map[string]string{"SPEAKEASY_AI_API_KEY": "k", "SPEAKEASY_AI_ORG": "o"}, want: ""},
		{name: "both names set", env: map[string]string{"SPEAKEASY_AI_API_KEY": "k", "GRAM_API_KEY": "old"}, want: ""},
		{
			name: "empty new name does not count",
			env:  map[string]string{"SPEAKEASY_AI_API_KEY": "", "GRAM_API_KEY": "old"},
			want: "Warning: GRAM_* environment variables are deprecated and still work for now. Rename GRAM_API_KEY to SPEAKEASY_AI_API_KEY.",
		},
		{name: "SDK generator variables", env: map[string]string{"SPEAKEASY_API_KEY": "k"}, want: ""},
		{
			name: "SDK generator variables do not replace legacy names",
			env:  map[string]string{"SPEAKEASY_API_KEY": "k", "GRAM_API_KEY": "old"},
			want: "Warning: GRAM_* environment variables are deprecated and still work for now. Rename GRAM_API_KEY to SPEAKEASY_AI_API_KEY.",
		},
		{name: "unrelated GRAM variables", env: map[string]string{"GRAM_SERVER_URL": "x", "GRAM_HOOKS_AUTH_FILE": "y"}, want: ""},
		{
			name: "one legacy name",
			env:  map[string]string{"GRAM_API_KEY": "k"},
			want: "Warning: GRAM_* environment variables are deprecated and still work for now. Rename GRAM_API_KEY to SPEAKEASY_AI_API_KEY.",
		},
		{
			name: "several legacy names on one line",
			env:  map[string]string{"GRAM_PROJECT": "p", "GRAM_API_KEY": "k", "SPEAKEASY_AI_ORG": "o", "GRAM_ORG": "old", "GRAM_LOG_LEVEL": ""},
			want: "Warning: GRAM_* environment variables are deprecated and still work for now. Rename GRAM_API_KEY to SPEAKEASY_AI_API_KEY, GRAM_PROJECT to SPEAKEASY_AI_PROJECT, GRAM_LOG_LEVEL to SPEAKEASY_AI_LOG_LEVEL.",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, LegacyEnvNotice(lookupIn(tc.env)))
		})
	}
}

func TestUnsetEmptyEnv(t *testing.T) {
	t.Parallel()

	env := map[string]string{"SPEAKEASY_AI_API_KEY": "", "SPEAKEASY_AI_ORG": "o", "GRAM_API_KEY": "", "SPEAKEASY_API_KEY": ""}
	err := UnsetEmptyEnv(lookupIn(env), func(key string) error {
		delete(env, key)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"SPEAKEASY_AI_ORG": "o", "GRAM_API_KEY": "", "SPEAKEASY_API_KEY": ""}, env)
}
