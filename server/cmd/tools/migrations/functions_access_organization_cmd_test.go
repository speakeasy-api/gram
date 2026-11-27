package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/secretbackfills/functionsaccessorganization"
)

func TestParseFunctionsAccessOrganizationFlags(t *testing.T) {
	t.Parallel()
	databaseURL := "postgres://user@db.internal:5432/gram?sslmode=require"
	getenv := func(key string) string {
		if key == "GRAM_DATABASE_URL" {
			return databaseURL
		}
		return ""
	}

	cfg, err := parseFunctionsAccessOrganizationFlags([]string{"-environment=staging"}, getenv)
	require.NoError(t, err)
	require.False(t, cfg.apply, "a run without -apply is a dry run")
	require.EqualValues(t, functionsAccessOrganizationDefaultBatch, cfg.batchSize)

	_, err = parseFunctionsAccessOrganizationFlags(nil, getenv)
	require.ErrorContains(t, err, "environment is required")

	_, err = parseFunctionsAccessOrganizationFlags([]string{"-environment=staging", "-batch-size=0"}, getenv)
	require.ErrorContains(t, err, "batch-size")

	_, err = parseFunctionsAccessOrganizationFlags([]string{"-environment=staging", "-batch-size=10001"}, getenv)
	require.ErrorContains(t, err, "batch-size")

	_, err = parseFunctionsAccessOrganizationFlags([]string{"-apply", "-environment=staging"}, getenv)
	require.ErrorContains(t, err, "confirm-environment")

	_, err = parseFunctionsAccessOrganizationFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=other.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "-confirm-target=db.internal:5432/gram")

	_, err = parseFunctionsAccessOrganizationFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "confirm-apply")

	cfg, err = parseFunctionsAccessOrganizationFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=functions-access-organization", "-batch-size=50"}, getenv)
	require.NoError(t, err)
	require.True(t, cfg.apply)
	require.EqualValues(t, 50, cfg.batchSize)
}

func TestParseFunctionsAccessOrganizationFlagsTLS(t *testing.T) {
	t.Parallel()
	apply := []string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=functions-access-organization"}

	tests := []struct {
		name    string
		url     string
		args    []string
		wantErr string
	}{
		{name: "write over plaintext", url: "postgres://user@db.internal:5432/gram?sslmode=disable", args: apply, wantErr: "TLS"},
		{name: "remote preview over plaintext", url: "postgres://user@db.internal:5432/gram?sslmode=disable", args: []string{"-environment=staging"}, wantErr: "TLS"},
		{name: "loopback preview over plaintext", url: "postgres://user@127.0.0.1:5432/gram?sslmode=disable", args: []string{"-environment=local"}, wantErr: ""},
		{name: "write with a fallback to another host", url: "postgres://user@db.internal:5432,other.internal:5432/gram?sslmode=require", args: apply, wantErr: "single host"},
		{name: "write with a fallback on another port", url: "postgres://user@db.internal:5432,db.internal:6432/gram?sslmode=require", args: apply, wantErr: "single host"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			getenv := func(string) string { return tc.url }

			_, err := parseFunctionsAccessOrganizationFlags(tc.args, getenv)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestFormatFunctionsAccessOrganizationReport(t *testing.T) {
	t.Parallel()

	report := functionsaccessorganization.Report{
		Filled: 7,
		Counts: functionsaccessorganization.Counts{Pending: 1, MissingOrganization: 2},
	}
	require.Equal(t,
		"mode=apply environment=dev filled=7 pending=1 missing_organization=2\n",
		formatFunctionsAccessOrganizationReport("apply", "dev", report))
}
