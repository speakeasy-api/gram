package openrouter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJudgeRateLimitKey(t *testing.T) {
	t.Parallel()

	platform := JudgeRateLimitKey(PlatformKey(), "google/gemini-3.1-flash-lite")

	t.Run("platform keys bucket per model across orgs", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "model:google/gemini-3.1-flash-lite", platform)
		require.NotEqual(t, platform, JudgeRateLimitKey(PlatformKey(), "other/model"))
	})

	t.Run("customer keys bucket per key and model, never leaking the secret", func(t *testing.T) {
		t.Parallel()
		a := JudgeRateLimitKey(ResolvedKey{Key: "sk-or-aaa", Customer: true}, "m")
		b := JudgeRateLimitKey(ResolvedKey{Key: "sk-or-bbb", Customer: true}, "m")
		require.NotEqual(t, a, b)
		require.NotEqual(t, a, JudgeRateLimitKey(ResolvedKey{Key: "sk-or-aaa", Customer: true}, "m2"))
		require.Equal(t, a, JudgeRateLimitKey(ResolvedKey{Key: "sk-or-aaa", Customer: true}, "m"))
		require.NotContains(t, a, "sk-or-aaa")
	})

	t.Run("empty customer key falls back to the platform bucket", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, platform, JudgeRateLimitKey(ResolvedKey{Key: "", Customer: true}, "google/gemini-3.1-flash-lite"))
	})
}
