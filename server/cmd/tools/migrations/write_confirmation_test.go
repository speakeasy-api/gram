package main

import (
	"flag"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteConfirmationCheck(t *testing.T) {
	t.Parallel()

	const dbURL = "postgres://gram:secret@db.internal:5432/gram"
	confirmed := []string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=example"}
	tests := []struct {
		name    string
		args    []string
		dbURL   string
		apply   bool
		wantErr string
	}{
		{"dry run needs no confirmation", []string{"-environment=staging"}, dbURL, false, ""},
		{"environment is required", []string{"-apply"}, dbURL, false, "environment is required"},
		{"database url is required", []string{"-environment=staging"}, "", false, "missing $GRAM_DATABASE_URL"},
		{"environment must be confirmed", []string{"-apply", "-environment=staging", "-confirm-environment=prod"}, dbURL, false, "confirm-environment"},
		{"target must match the parsed url", []string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=other.internal:5432/gram"}, dbURL, false, "-confirm-target=db.internal:5432/gram"},
		{"apply must name the backfill", []string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=other"}, dbURL, false, "-confirm-apply=example"},
		{"a multi-host url is refused", confirmed, "postgres://gram:secret@db.internal:5432,replica.internal:5432/gram", false, "single host"},
		{"fully confirmed writes", confirmed, dbURL, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := newTestFlagSet()
			confirmation := registerWriteConfirmation(fs, "example")
			require.NoError(t, fs.Parse(tt.args))
			apply, err := confirmation.check(tt.dbURL)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.apply, apply)
		})
	}
}

func TestBackfillFlagsUseWriteConfirmation(t *testing.T) {
	t.Parallel()

	getenv := func(string) string { return "postgres://gram:secret@db.internal:5432/gram" }

	clients, err := parseUpstreamClientsFlags([]string{"-environment=staging", "-organization=org"}, getenv)
	require.NoError(t, err)
	require.False(t, clients.options.Apply)
	require.Equal(t, "org", clients.options.OrganizationID)
	_, err = parseUpstreamClientsFlags([]string{"-apply", "-environment=staging"}, getenv)
	require.ErrorContains(t, err, "confirm-environment")

	selections, err := parseUpstreamSelectionsFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=" + upstreamSelectionsConfirm}, getenv)
	require.NoError(t, err)
	require.True(t, selections.options.Apply)
	_, err = parseUpstreamSelectionsFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=" + upstreamClientsConfirm}, getenv)
	require.ErrorContains(t, err, upstreamSelectionsConfirm, "each backfill confirms its own name")
}

func newTestFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("example", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}
