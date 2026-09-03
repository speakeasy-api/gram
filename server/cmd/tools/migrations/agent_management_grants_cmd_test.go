package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/agentmanagementgrants"
)

func TestParseAgentManagementGrantsFlags(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		args        []string
		environment string
		mode        agentmanagementgrants.Mode
	}{
		{name: "dry run", args: []string{"-environment=staging"}, environment: "staging", mode: agentmanagementgrants.ModeDryRun},
		{name: "verify", args: []string{"-verify", "-environment=production"}, environment: "production", mode: agentmanagementgrants.ModeVerify},
		{name: "staging apply", args: []string{"-apply", "-environment=staging", "-confirm-environment=staging"}, environment: "staging", mode: agentmanagementgrants.ModeApply},
		{name: "production apply", args: []string{"-apply", "-environment=production", "-confirm-environment=production", "-confirm-production=production"}, environment: "production", mode: agentmanagementgrants.ModeApply},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			getenv := func(key string) string {
				switch key {
				case "GRAM_DATABASE_URL":
					return "postgres://test?sslmode=require"
				case "GRAM_ENVIRONMENT":
					return test.environment
				default:
					return ""
				}
			}
			cfg, err := parseAgentManagementGrantsFlags(test.args, getenv)
			require.NoError(t, err)
			require.Equal(t, test.mode, cfg.mode)
		})
	}
}

func TestParseAgentManagementGrantsFlagsRejectsUnsafeModes(t *testing.T) {
	t.Parallel()
	getenv := func(key string) string {
		switch key {
		case "GRAM_DATABASE_URL":
			return "postgres://test?sslmode=require"
		case "GRAM_ENVIRONMENT":
			return "staging"
		default:
			return ""
		}
	}

	for _, args := range [][]string{
		{"-environment=staging", "-apply", "-verify"},
		{"-environment=staging", "-apply"},
		{"-environment=production", "-apply", "-confirm-environment=production"},
		{"-environment=staging", "-sample-limit=101"},
		{"-environment=staging", "-batch-size=0"},
		{"-environment=staging", "-lock-timeout=500us"},
		{"-environment=production", "-apply", "-confirm-environment=production", "-confirm-production=production"},
	} {
		_, err := parseAgentManagementGrantsFlags(args, getenv)
		require.Error(t, err)
	}
}

func TestParseAgentManagementGrantsFlagsRejectsPlaintextDatabaseConnections(t *testing.T) {
	t.Parallel()

	for _, databaseURL := range []string{
		"postgres://test",
		"postgres://test?sslmode=allow",
		"postgres://test?sslmode=disable",
		"postgres://test?sslmode=prefer",
	} {
		getenv := func(key string) string {
			if key == "GRAM_DATABASE_URL" {
				return databaseURL
			}
			return ""
		}
		_, err := parseAgentManagementGrantsFlags([]string{"-environment=staging"}, getenv)
		require.ErrorContains(t, err, "must require TLS")
	}
}

func TestAgentManagementGrantsErrorCategoryIncludesConnectionErrors(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("connect: %w", &pgconn.ConnectError{})
	require.Equal(t, "database_or_timeout", agentManagementGrantsErrorCategory(err))
}

func TestAgentManagementGrantsSummaryIsBoundedAndContainsNoUserData(t *testing.T) {
	t.Parallel()
	samples := make([]agentmanagementgrants.DefectSample, 100)
	for i := range samples {
		samples[i] = agentmanagementgrants.DefectSample{OrganizationID: "org-test", Scope: "agent:unexpected"}
	}
	var out bytes.Buffer
	err := writeAgentManagementGrantsSummary(&out, agentManagementGrantsCommandSummary{
		RunID: "run-test", Environment: "staging", CodeSHA: "sha-test", ContractVersion: agentManagementGrantContractVersion, Result: "blocked",
		Summary: agentmanagementgrants.Summary{
			Mode:         agentmanagementgrants.ModeVerify,
			Verification: agentmanagementgrants.Verification{UnexpectedGrantSamples: samples},
		},
	})
	require.NoError(t, err)
	require.NotContains(t, out.String(), "email")
	require.NotContains(t, out.String(), "name")

	var decoded agentManagementGrantsCommandSummary
	require.NoError(t, json.Unmarshal(out.Bytes(), &decoded))
	require.Len(t, decoded.Summary.Verification.UnexpectedGrantSamples, 100)
}
