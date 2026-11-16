package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/remotesessionsorganization"
)

func TestParseRemoteSessionsOrganizationFlags(t *testing.T) {
	t.Parallel()
	databaseURL := "postgres://user@db.internal:5432/gram?sslmode=require"
	getenv := func(key string) string {
		if key == "GRAM_DATABASE_URL" {
			return databaseURL
		}
		return ""
	}

	cfg, err := parseRemoteSessionsOrganizationFlags([]string{"-environment=staging"}, getenv)
	require.NoError(t, err)
	require.False(t, cfg.apply, "a run without -apply is a dry run")
	require.EqualValues(t, remoteSessionsOrganizationDefaultBatch, cfg.batchSize)

	_, err = parseRemoteSessionsOrganizationFlags(nil, getenv)
	require.ErrorContains(t, err, "environment is required")

	_, err = parseRemoteSessionsOrganizationFlags([]string{"-environment=staging", "-batch-size=0"}, getenv)
	require.ErrorContains(t, err, "batch-size")

	_, err = parseRemoteSessionsOrganizationFlags([]string{"-environment=staging", "-batch-size=10001"}, getenv)
	require.ErrorContains(t, err, "batch-size")

	_, err = parseRemoteSessionsOrganizationFlags([]string{"-apply", "-environment=staging"}, getenv)
	require.ErrorContains(t, err, "confirm-environment")

	_, err = parseRemoteSessionsOrganizationFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=other.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "-confirm-target=db.internal:5432/gram")

	_, err = parseRemoteSessionsOrganizationFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "confirm-apply")

	cfg, err = parseRemoteSessionsOrganizationFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=remote-sessions-organization", "-batch-size=50"}, getenv)
	require.NoError(t, err)
	require.True(t, cfg.apply)
	require.EqualValues(t, 50, cfg.batchSize)
}

func TestParseRemoteSessionsOrganizationFlagsRequiresTLSForWrites(t *testing.T) {
	t.Parallel()
	getenv := func(key string) string {
		if key == "GRAM_DATABASE_URL" {
			return "postgres://user@db.internal:5432/gram?sslmode=disable"
		}
		return ""
	}

	_, err := parseRemoteSessionsOrganizationFlags([]string{"-apply", "-environment=local", "-confirm-environment=local", "-confirm-target=db.internal:5432/gram", "-confirm-apply=remote-sessions-organization"}, getenv)
	require.ErrorContains(t, err, "TLS")
}

func TestParseRemoteSessionsOrganizationFlagsPreviewTLS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "remote plaintext", url: "postgres://user@db.internal:5432/gram?sslmode=disable", wantErr: true},
		{name: "remote prefer", url: "postgres://user@db.internal:5432/gram?sslmode=prefer", wantErr: true},
		{name: "remote TLS", url: "postgres://user@db.internal:5432/gram?sslmode=require", wantErr: false},
		{name: "loopback plaintext", url: "postgres://user@127.0.0.1:5432/gram?sslmode=disable", wantErr: false},
		{name: "localhost plaintext", url: "postgres://user@localhost:5432/gram?sslmode=disable", wantErr: false},
		{name: "socket plaintext", url: "host=/tmp user=user dbname=gram sslmode=disable", wantErr: false},
		{name: "loopback with a remote fallback", url: "postgres://user@127.0.0.1:5432,db.internal:5432/gram?sslmode=disable", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			getenv := func(string) string { return tc.url }

			_, err := parseRemoteSessionsOrganizationFlags([]string{"-environment=staging"}, getenv)
			if tc.wantErr {
				require.ErrorContains(t, err, "TLS")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestParseRemoteSessionsOrganizationFlagsRejectsOtherFallbackHosts(t *testing.T) {
	t.Parallel()
	apply := []string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=remote-sessions-organization"}
	getenv := func(string) string {
		return "postgres://user@db.internal:5432,other.internal:5432/gram?sslmode=require"
	}

	_, err := parseRemoteSessionsOrganizationFlags(apply, getenv)
	require.ErrorContains(t, err, "single host")
}

func TestParseRemoteSessionsOrganizationFlagsRejectsAFallbackOnAnotherPort(t *testing.T) {
	t.Parallel()
	apply := []string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=remote-sessions-organization"}
	getenv := func(string) string {
		return "postgres://user@db.internal:5432,db.internal:6432/gram?sslmode=require"
	}

	// Another port on the same host can be another server, so the check
	// compares the port too, not only the hostname.
	_, err := parseRemoteSessionsOrganizationFlags(apply, getenv)
	require.ErrorContains(t, err, "single host")
}

func TestFormatRemoteSessionsOrganizationReport(t *testing.T) {
	t.Parallel()

	report := remotesessionsorganization.Report{
		Filled: 7,
		Counts: remotesessionsorganization.Counts{Pending: 1, GlobalClient: 2, MissingOrganization: 3, Mismatched: 4},
	}
	require.Equal(t,
		"mode=apply environment=dev filled=7 pending=1 global_client=2 missing_organization=3 mismatched=4\n",
		formatRemoteSessionsOrganizationReport("apply", "dev", report))
}
