package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminAgentCreationFlagsAreDryRunAndExactlyScoped(t *testing.T) {
	t.Parallel()
	getenv := func(string) string { return "postgres://user@localhost:5432/gram" }
	cfg, err := parseAdminAgentCreationFlags([]string{"--environment=local", "--organization=org-test"}, getenv)
	require.NoError(t, err)
	require.False(t, cfg.apply)
	require.Equal(t, "org-test", cfg.organizationID)
	for _, args := range [][]string{
		{}, {"--environment=local"}, {"--environment=local", "--organization=*"},
		{"--environment=local", "--organization=org-test", "--apply"},
	} {
		_, err := parseAdminAgentCreationFlags(args, getenv)
		require.Error(t, err)
	}
}

func TestAdminAgentCreationFlagsRequireEveryWriteConfirmation(t *testing.T) {
	t.Parallel()
	args := []string{"--environment=local", "--organization=org-test", "--apply", "--confirm-environment=local", "--confirm-organization=org-test", "--confirm-target=localhost:5432/gram", "--confirm-apply=admin-agent-creation"}
	getenv := func(string) string { return "postgres://user@localhost:5432/gram" }
	cfg, err := parseAdminAgentCreationFlags(args, getenv)
	require.NoError(t, err)
	require.True(t, cfg.apply)
	for i := 3; i < len(args); i++ {
		withoutConfirmation := append([]string{}, args[:i]...)
		withoutConfirmation = append(withoutConfirmation, args[i+1:]...)
		_, err := parseAdminAgentCreationFlags(withoutConfirmation, getenv)
		require.Error(t, err)
	}
}
