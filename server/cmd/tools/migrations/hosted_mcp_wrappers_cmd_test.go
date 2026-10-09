package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/hostedmcpbackfill"
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

func TestRunHostedMCPWrappersRejectsUnwritableReportBeforeRunning(t *testing.T) {
	t.Parallel()

	report := filepath.Join(t.TempDir(), "missing", "report.json")
	var out bytes.Buffer
	code := runHostedMCPWrappers([]string{"-apply", "-report", report}, &out, hostedMCPWrappersGetenv)
	require.Equal(t, 2, code, "a bad report path must fail before any apply")
	require.Empty(t, out.String())
}

func TestRunHostedMCPWrappersKeepsPreviousReportWhenConnectFails(t *testing.T) {
	t.Parallel()

	report := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, os.WriteFile(report, []byte("previous run"), 0o600))
	getenv := func(key string) string {
		if key == "GRAM_DATABASE_URL" {
			return "postgres://%zz"
		}
		return ""
	}
	code := runHostedMCPWrappers([]string{"-apply", "-report", report}, io.Discard, getenv)
	require.Equal(t, 1, code)
	got, err := os.ReadFile(report)
	require.NoError(t, err)
	require.Equal(t, "previous run", string(got), "a run that never wrote must keep the previous report")
}

func TestWriteHostedMCPWrappersReportReplacesPreviousContent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, os.WriteFile(path, bytes.Repeat([]byte("x"), 4096), 0o600))
	f, err := os.OpenFile(path, os.O_WRONLY, 0o600)
	require.NoError(t, err)
	require.NoError(t, writeHostedMCPWrappersReport(f, hostedmcpbackfill.Report{Mode: "dry-run"}))
	require.NoError(t, f.Close())

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	var decoded hostedmcpbackfill.Report
	require.NoError(t, json.Unmarshal(got, &decoded), "the old report must not trail the new one")
	require.Equal(t, "dry-run", decoded.Mode)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("stdout closed") }

func TestRunHostedMCPWrappersWritesReportEvenWhenSummaryFails(t *testing.T) {
	t.Parallel()

	report := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, os.WriteFile(report, []byte("previous run"), 0o600))
	getenv := func(key string) string {
		if key == "GRAM_DATABASE_URL" {
			return "postgres://127.0.0.1:1/unreachable?connect_timeout=1"
		}
		return ""
	}
	code := runHostedMCPWrappers([]string{"-report", report}, failingWriter{}, getenv)
	require.Equal(t, 1, code)
	got, err := os.ReadFile(report)
	require.NoError(t, err)
	var decoded hostedmcpbackfill.Report
	require.NoError(t, json.Unmarshal(got, &decoded), "the report must be written before the summary")
}
