package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAssistantAgentNamesFlagsDryRunByDefault(t *testing.T) {
	t.Parallel()
	cfg, err := parseAssistantAgentNamesFlags([]string{"-environment=staging", "-org=org-a"}, func(key string) string {
		if key == "GRAM_DATABASE_URL" {
			return "postgres://test"
		}
		return ""
	})
	require.NoError(t, err)
	require.False(t, cfg.apply)
	require.Equal(t, "org-a", cfg.orgID)
}

func TestParseAssistantAgentNamesFlagsRequiresWriteConfirmations(t *testing.T) {
	t.Parallel()
	getenv := func(string) string { return "postgres://user@db.internal:5432/gram" }

	_, err := parseAssistantAgentNamesFlags([]string{"-apply"}, getenv)
	require.ErrorContains(t, err, "environment is required")

	_, err = parseAssistantAgentNamesFlags([]string{"-apply", "-environment=staging"}, getenv)
	require.ErrorContains(t, err, "confirm-environment")

	_, err = parseAssistantAgentNamesFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=other.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "-confirm-target=db.internal:5432/gram")

	_, err = parseAssistantAgentNamesFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "confirm-apply")

	cfg, err := parseAssistantAgentNamesFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=assistant-agent-names"}, getenv)
	require.NoError(t, err)
	require.True(t, cfg.apply)
}

func TestParseAssistantAgentNamesFlagsRejectsExplicitlyEmptyOrg(t *testing.T) {
	t.Parallel()
	getenv := func(string) string { return "postgres://user@db.internal:5432/gram" }

	_, err := parseAssistantAgentNamesFlags([]string{"-environment=staging", "-org="}, getenv)
	require.ErrorContains(t, err, "-org must not be empty")

	cfg, err := parseAssistantAgentNamesFlags([]string{"-environment=staging"}, getenv)
	require.NoError(t, err)
	require.Empty(t, cfg.orgID)
}

func TestParseAssistantAgentNamesFlagsRejectsFallbackHosts(t *testing.T) {
	t.Parallel()
	getenv := func(string) string { return "postgres://user@db.internal:5432,other.internal:5432/gram" }

	_, err := parseAssistantAgentNamesFlags([]string{"-environment=staging"}, getenv)
	require.ErrorContains(t, err, "single host")
}

func TestParseAssistantAgentNamesProject(t *testing.T) {
	getenv := func(string) string { return "postgres://test" }
	for _, project := range []string{"", "invalid"} {
		_, err := parseAssistantAgentNamesFlags([]string{"-environment=staging", "-project=" + project}, getenv)
		require.ErrorContains(t, err, "-project")
	}
	cfg, err := parseAssistantAgentNamesFlags([]string{"-environment=staging", "-project=00000000-0000-0000-0000-000000000001"}, getenv)
	require.NoError(t, err)
	require.Equal(t, "00000000-0000-0000-0000-000000000001", cfg.projectID)
}
