package gateway

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetUserEmailEnv(t *testing.T) {
	t.Parallel()

	t.Run("sets both names when enabled", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{"API_KEY": "k"}
		setUserEmailEnv(env, true, "user@example.com")
		require.Equal(t, map[string]string{
			"API_KEY":                 "k",
			"SPEAKEASY_AI_USER_EMAIL": "user@example.com",
			"GRAM_USER_EMAIL":         "user@example.com",
		}, env)
	})

	t.Run("drops user-supplied values", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{"SPEAKEASY_AI_USER_EMAIL": "spoofed", "GRAM_USER_EMAIL": "spoofed"}
		setUserEmailEnv(env, true, "user@example.com")
		require.Equal(t, "user@example.com", env["SPEAKEASY_AI_USER_EMAIL"])
		require.Equal(t, "user@example.com", env["GRAM_USER_EMAIL"])

		env = map[string]string{"SPEAKEASY_AI_USER_EMAIL": "spoofed", "GRAM_USER_EMAIL": "spoofed"}
		setUserEmailEnv(env, false, "user@example.com")
		require.Empty(t, env)
	})

	t.Run("sets nothing without an email", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{}
		setUserEmailEnv(env, true, "")
		require.Empty(t, env)
	})
}
