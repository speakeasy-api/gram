package flags

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvVars_OnlyTheNewName(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"SPEAKEASY_AI_API_KEY"}, EnvVars("API_KEY"))
}

func TestApplyLegacyEnv(t *testing.T) {
	t.Parallel()

	const prefix = "Warning: GRAM_* environment variables are deprecated and still work for now. Rename "

	tests := []struct {
		name       string
		env        map[string]string
		wantEnv    map[string]string
		wantNotice string
	}{
		{name: "nothing set", env: map[string]string{}, wantEnv: map[string]string{}, wantNotice: ""},
		{
			name:       "only new names",
			env:        map[string]string{"SPEAKEASY_AI_API_KEY": "k"},
			wantEnv:    map[string]string{"SPEAKEASY_AI_API_KEY": "k"},
			wantNotice: "",
		},
		{
			name:       "new name wins",
			env:        map[string]string{"SPEAKEASY_AI_API_KEY": "k", "GRAM_API_KEY": "old"},
			wantEnv:    map[string]string{"SPEAKEASY_AI_API_KEY": "k", "GRAM_API_KEY": "old"},
			wantNotice: "",
		},
		{
			name:       "legacy name is copied",
			env:        map[string]string{"GRAM_API_KEY": "old"},
			wantEnv:    map[string]string{"GRAM_API_KEY": "old", "SPEAKEASY_AI_API_KEY": "old"},
			wantNotice: prefix + "GRAM_API_KEY to SPEAKEASY_AI_API_KEY.",
		},
		{
			name:       "empty new name does not hide the legacy one",
			env:        map[string]string{"SPEAKEASY_AI_API_KEY": "", "GRAM_API_KEY": "old"},
			wantEnv:    map[string]string{"GRAM_API_KEY": "old", "SPEAKEASY_AI_API_KEY": "old"},
			wantNotice: prefix + "GRAM_API_KEY to SPEAKEASY_AI_API_KEY.",
		},
		{
			name:       "SDK generator names are ignored",
			env:        map[string]string{"SPEAKEASY_API_KEY": "generator", "GRAM_API_KEY": "old"},
			wantEnv:    map[string]string{"SPEAKEASY_API_KEY": "generator", "GRAM_API_KEY": "old", "SPEAKEASY_AI_API_KEY": "old"},
			wantNotice: prefix + "GRAM_API_KEY to SPEAKEASY_AI_API_KEY.",
		},
		{
			name:       "unrelated GRAM variables",
			env:        map[string]string{"GRAM_SERVER_URL": "x"},
			wantEnv:    map[string]string{"GRAM_SERVER_URL": "x"},
			wantNotice: "",
		},
		{
			name:       "several legacy names on one line",
			env:        map[string]string{"GRAM_PROJECT": "p", "GRAM_API_KEY": "k", "SPEAKEASY_AI_ORG": "o", "GRAM_ORG": "old"},
			wantEnv:    map[string]string{"GRAM_PROJECT": "p", "GRAM_API_KEY": "k", "SPEAKEASY_AI_ORG": "o", "GRAM_ORG": "old", "SPEAKEASY_AI_API_KEY": "k", "SPEAKEASY_AI_PROJECT": "p"},
			wantNotice: prefix + "GRAM_API_KEY to SPEAKEASY_AI_API_KEY, GRAM_PROJECT to SPEAKEASY_AI_PROJECT.",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := tc.env
			notice, err := ApplyLegacyEnv(
				func(key string) (string, bool) {
					v, ok := env[key]
					return v, ok
				},
				func(key, value string) error {
					env[key] = value
					return nil
				},
			)
			require.NoError(t, err)
			require.Equal(t, tc.wantNotice, notice)
			require.Equal(t, tc.wantEnv, env)
		})
	}
}

func TestApplyLegacyEnv_SetterError(t *testing.T) {
	t.Parallel()

	env := map[string]string{"GRAM_API_KEY": "old"}
	notice, err := ApplyLegacyEnv(
		func(key string) (string, bool) {
			v, ok := env[key]
			return v, ok
		},
		func(string, string) error { return errSetenv },
	)
	require.ErrorIs(t, err, errSetenv)
	require.Empty(t, notice)
}

var errSetenv = errors.New("setenv failed")
