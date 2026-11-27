package toolconfig_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

func TestToolCallEnv_LayerPrecedence(t *testing.T) {
	t.Parallel()

	env := toolconfig.ToolCallEnv{
		SystemEnv:   toolconfig.CIEnvFrom(map[string]string{"A": "sys-a", "B": "sys-b", "C": "sys-c"}),
		SavedConfig: toolconfig.CIEnvFrom(map[string]string{"B": "saved-b", "C": "saved-c", "D": "saved-d"}),
		UserConfig:  toolconfig.CIEnvFrom(map[string]string{"C": "user-c", "E": "user-e"}),
	}

	merged := env.Merged()
	require.Equal(t, "sys-a", merged.Get("a"))
	require.Equal(t, "saved-b", merged.Get("b"))
	require.Equal(t, "user-c", merged.Get("c"))
	require.Equal(t, "saved-d", merged.Get("d"))
	require.Equal(t, "user-e", merged.Get("e"))

	require.True(t, env.HasConfiguredEnv())
	require.False(t, env.HasUserValue("A"))
	require.False(t, env.HasUserValue("B"))
	require.True(t, env.HasUserValue("C"))
	require.True(t, env.HasUserValue("E"))
	require.False(t, env.HasUserValue(""))

	require.Equal(t, "sys-a", env.ConfiguredGet("A"))
	require.Equal(t, "saved-b", env.ConfiguredGet("B"))
	require.Equal(t, "saved-c", env.ConfiguredGet("C"))
	require.Empty(t, env.ConfiguredGet("E"))

	require.Equal(t, "saved-b", env.ConfiguredOrUserGet("B"))
	require.Equal(t, "user-c", env.ConfiguredOrUserGet("C"))
	require.Equal(t, "user-e", env.ConfiguredOrUserGet("E"))
	require.Empty(t, env.ConfiguredOrUserGet("A"))
}

func TestToolCallEnv_NilSafety(t *testing.T) {
	t.Parallel()

	var empty toolconfig.ToolCallEnv
	require.False(t, empty.HasConfiguredEnv())
	require.False(t, empty.HasUserValue("KEY"))
	require.Empty(t, empty.ConfiguredGet("KEY"))
	require.Empty(t, empty.ConfiguredOrUserGet("KEY"))
	require.Empty(t, empty.Merged().All())

	savedOnly := toolconfig.ToolCallEnv{
		SavedConfig: toolconfig.CIEnvFrom(map[string]string{"KEY": "saved"}),
	}
	require.True(t, savedOnly.HasConfiguredEnv())
	require.False(t, savedOnly.HasUserValue("KEY"))
	require.Equal(t, "saved", savedOnly.ConfiguredGet("KEY"))
}
