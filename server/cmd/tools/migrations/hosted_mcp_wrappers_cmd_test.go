package main

import (
	"bytes"
	"flag"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func hostedMCPWrappersGetenv(key string) string {
	if key == "GRAM_DATABASE_URL" {
		return "postgres://test"
	}
	return ""
}

func TestParseHostedMCPWrappersFlagsDefaultsToDryRun(t *testing.T) {
	t.Parallel()

	cfg, err := parseHostedMCPWrappersFlags(nil, hostedMCPWrappersGetenv, io.Discard)
	require.NoError(t, err)
	require.False(t, cfg.options.Apply)
	require.False(t, cfg.options.ProjectID.Valid)
}

func TestParseHostedMCPWrappersFlagsParsesScope(t *testing.T) {
	t.Parallel()

	project, cursor := uuid.New(), uuid.New()
	cfg, err := parseHostedMCPWrappersFlags([]string{
		"-apply", "-project=" + project.String(), "-cursor=" + cursor.String(), "-limit=5", "-report=/tmp/r.json",
	}, hostedMCPWrappersGetenv, io.Discard)
	require.NoError(t, err)
	require.True(t, cfg.options.Apply)
	require.Equal(t, uuid.NullUUID{UUID: project, Valid: true}, cfg.options.ProjectID)
	require.Equal(t, cursor, cfg.options.Cursor)
	require.Equal(t, 5, cfg.options.Limit)
	require.Equal(t, "/tmp/r.json", cfg.reportPath)
}

func TestParseHostedMCPWrappersFlagsRejectsBadInput(t *testing.T) {
	t.Parallel()

	_, err := parseHostedMCPWrappersFlags(nil, func(string) string { return "" }, io.Discard)
	require.ErrorContains(t, err, "GRAM_DATABASE_URL")
	_, err = parseHostedMCPWrappersFlags([]string{"-project=nope"}, hostedMCPWrappersGetenv, io.Discard)
	require.ErrorContains(t, err, "invalid -project")
	_, err = parseHostedMCPWrappersFlags([]string{"-cursor=nope"}, hostedMCPWrappersGetenv, io.Discard)
	require.ErrorContains(t, err, "invalid -cursor")
	_, err = parseHostedMCPWrappersFlags([]string{"-limit=-1"}, hostedMCPWrappersGetenv, io.Discard)
	require.ErrorContains(t, err, "nonnegative")
	_, err = parseHostedMCPWrappersFlags([]string{"-move-dependents"}, hostedMCPWrappersGetenv, io.Discard)
	require.Error(t, err)
	_, err = parseHostedMCPWrappersFlags([]string{"extra"}, hostedMCPWrappersGetenv, io.Discard)
	require.ErrorContains(t, err, "positional")
}

func TestParseHostedMCPWrappersFlagsHelpPrintsUsage(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	_, err := parseHostedMCPWrappersFlags([]string{"-h"}, hostedMCPWrappersGetenv, &out)
	require.ErrorIs(t, err, flag.ErrHelp)
	require.Contains(t, out.String(), "-apply")
	require.Equal(t, 0, runHostedMCPWrappers([]string{"-h"}, io.Discard, hostedMCPWrappersGetenv))
}
