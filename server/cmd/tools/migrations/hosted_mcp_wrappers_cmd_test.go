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

	cfg, err := parseHostedMCPWrappersFlags([]string{"-all-projects"}, hostedMCPWrappersGetenv, io.Discard)
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

	_, err := parseHostedMCPWrappersFlags([]string{"-all-projects"}, func(string) string { return "" }, io.Discard)
	require.ErrorContains(t, err, "GRAM_DATABASE_URL")
	_, err = parseHostedMCPWrappersFlags([]string{"-project=nope"}, hostedMCPWrappersGetenv, io.Discard)
	require.ErrorContains(t, err, "invalid -project")
	_, err = parseHostedMCPWrappersFlags([]string{"-all-projects", "-cursor=nope"}, hostedMCPWrappersGetenv, io.Discard)
	require.ErrorContains(t, err, "invalid -cursor")
	_, err = parseHostedMCPWrappersFlags([]string{"-all-projects", "-limit=-1"}, hostedMCPWrappersGetenv, io.Discard)
	require.ErrorContains(t, err, "nonnegative")
	_, err = parseHostedMCPWrappersFlags([]string{"-all-projects", "-readers-cut-over"}, hostedMCPWrappersGetenv, io.Discard)
	require.ErrorContains(t, err, "only applies to -move-dependents")
	_, err = parseHostedMCPWrappersFlags([]string{"-all-projects", "-cleanup-orphans", "-move-dependents"}, hostedMCPWrappersGetenv, io.Discard)
	require.ErrorContains(t, err, "separate phases")
	_, err = parseHostedMCPWrappersFlags([]string{"-all-projects", "-cleanup-orphans", "-apply"}, hostedMCPWrappersGetenv, io.Discard)
	require.ErrorContains(t, err, "-cleanup-orphans -apply requires -report")
	_, err = parseHostedMCPWrappersFlags([]string{"-all-projects", "-cleanup-orphans", "-readers-cut-over"}, hostedMCPWrappersGetenv, io.Discard)
	require.ErrorContains(t, err, "only applies to -move-dependents")
	_, err = parseHostedMCPWrappersFlags([]string{"-all-projects", "extra"}, hostedMCPWrappersGetenv, io.Discard)
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
	code := runHostedMCPWrappers([]string{"-all-projects", "-apply", "-report", report}, &out, hostedMCPWrappersGetenv)
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
	code := runHostedMCPWrappers([]string{"-all-projects", "-report", report}, io.Discard, getenv)
	require.Equal(t, 1, code)
	got, err := os.ReadFile(report)
	require.NoError(t, err)
	require.Equal(t, "previous run", string(got), "a run that never wrote must keep the previous report")
}

func TestRunHostedMCPWrappersApplyRefusesAnExistingReport(t *testing.T) {
	t.Parallel()

	report := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, os.WriteFile(report, []byte("previous batch"), 0o600))
	code := runHostedMCPWrappers([]string{"-all-projects", "-apply", "-report", report}, io.Discard, hostedMCPWrappersGetenv)
	require.Equal(t, 2, code, "an apply must not overwrite another batch's report")
	got, err := os.ReadFile(report)
	require.NoError(t, err)
	require.Equal(t, "previous batch", string(got))
}

func TestRunHostedMCPWrappersMoveApplyRefusesAnExistingJournal(t *testing.T) {
	t.Parallel()

	report := filepath.Join(t.TempDir(), "report.json")
	journal := report + ".journal.jsonl"
	require.NoError(t, os.WriteFile(journal, []byte("previous batch\n"), 0o600))
	code := runHostedMCPWrappers([]string{"-all-projects", "-move-dependents", "-apply", "-readers-cut-over", "-report", report}, io.Discard, hostedMCPWrappersGetenv)
	require.Equal(t, 2, code, "an apply must not append to another batch's journal")
	got, err := os.ReadFile(journal)
	require.NoError(t, err)
	require.Equal(t, "previous batch\n", string(got))
	_, err = os.Stat(report)
	require.ErrorIs(t, err, os.ErrNotExist, "the refused run leaves no report behind")
}

func TestParseHostedMCPWrappersFlagsCleanupOrphansJournalsOnApply(t *testing.T) {
	t.Parallel()

	dry, err := parseHostedMCPWrappersFlags([]string{"-all-projects", "-cleanup-orphans", "-report=/tmp/r.json"}, hostedMCPWrappersGetenv, io.Discard)
	require.NoError(t, err)
	require.True(t, dry.options.CleanupOrphans)
	require.Empty(t, dry.journalPath)
	applied, err := parseHostedMCPWrappersFlags([]string{"-all-projects", "-cleanup-orphans", "-apply", "-report=/tmp/r.json"}, hostedMCPWrappersGetenv, io.Discard)
	require.NoError(t, err)
	require.True(t, applied.options.Apply)
	require.True(t, applied.options.CleanupOrphans)
	require.Equal(t, "/tmp/r.json.journal.jsonl", applied.journalPath)
	require.False(t, applied.options.Publication.Enabled)

	emitEnv := func(key string) string {
		if key == "GRAM_PLUGIN_PUBLICATION_EMIT_ENABLED" {
			return "true"
		}
		return hostedMCPWrappersGetenv(key)
	}
	fromEnv, err := parseHostedMCPWrappersFlags([]string{"-all-projects", "-cleanup-orphans"}, emitEnv, io.Discard)
	require.NoError(t, err)
	require.True(t, fromEnv.options.Publication.Enabled)
	overridden, err := parseHostedMCPWrappersFlags([]string{"-all-projects", "-cleanup-orphans", "-plugin-publication-emit=false"}, emitEnv, io.Discard)
	require.NoError(t, err)
	require.False(t, overridden.options.Publication.Enabled)
	_, err = parseHostedMCPWrappersFlags([]string{"-all-projects", "-cleanup-orphans"}, func(key string) string {
		if key == "GRAM_PLUGIN_PUBLICATION_EMIT_ENABLED" {
			return "yes"
		}
		return hostedMCPWrappersGetenv(key)
	}, io.Discard)
	require.ErrorContains(t, err, "GRAM_PLUGIN_PUBLICATION_EMIT_ENABLED")
}

func TestHostedMCPWrappersModeFlagsKeepTheResumeInItsPhase(t *testing.T) {
	t.Parallel()

	require.Equal(t, "-apply -cleanup-orphans", hostedMCPWrappersModeFlags(hostedmcpbackfill.Options{CleanupOrphans: true}))
	require.Equal(t, "-apply -move-dependents -readers-cut-over", hostedMCPWrappersModeFlags(hostedmcpbackfill.Options{MoveDependents: true}))
	require.Equal(t, "-apply", hostedMCPWrappersModeFlags(hostedmcpbackfill.Options{}))
}

func TestHostedMCPWrappersResumeFlagsKeepProjectAndPublicationEmit(t *testing.T) {
	t.Parallel()
	project := uuid.NewString()

	plain, err := parseHostedMCPWrappersFlags([]string{"-all-projects", "-delete-orphan-wrapper-servers", "-apply", "-report=/tmp/r.json"}, hostedMCPWrappersGetenv, io.Discard)
	require.NoError(t, err)
	require.Equal(t, "-apply -delete-orphan-wrapper-servers -all-projects -plugin-publication-emit=false", hostedMCPWrappersResumeFlags(plain), "the effective publication choice is stated even when false")

	scoped, err := parseHostedMCPWrappersFlags([]string{"-cleanup-orphans", "-apply", "-report=/tmp/r.json", "-project", project, "-plugin-publication-emit=false"}, hostedMCPWrappersGetenv, io.Discard)
	require.NoError(t, err)
	require.Equal(t, "-apply -cleanup-orphans -project "+project+" -plugin-publication-emit=false", hostedMCPWrappersResumeFlags(scoped))

	fromEnv, err := parseHostedMCPWrappersFlags([]string{"-all-projects", "-delete-orphan-wrapper-servers", "-apply", "-report=/tmp/r.json"}, func(key string) string {
		if key == "GRAM_PLUGIN_PUBLICATION_EMIT_ENABLED" {
			return "true"
		}
		return hostedMCPWrappersGetenv(key)
	}, io.Discard)
	require.NoError(t, err)
	require.Equal(t, "-apply -delete-orphan-wrapper-servers -all-projects -plugin-publication-emit=true", hostedMCPWrappersResumeFlags(fromEnv))

	limited, err := parseHostedMCPWrappersFlags([]string{"-rekey-removal-records", "-apply", "-report=/tmp/r.json", "-project", project, "-limit", "25"}, hostedMCPWrappersGetenv, io.Discard)
	require.NoError(t, err)
	require.Equal(t, "-apply -rekey-removal-records -project "+project+" -limit 25", hostedMCPWrappersResumeFlags(limited), "a phase that requests no publication states no publication choice")
}

func TestParseHostedMCPWrappersFlagsRequiresAnExplicitScope(t *testing.T) {
	t.Parallel()
	project := uuid.NewString()

	for _, args := range [][]string{
		nil,
		{"-apply"},
		{"-cleanup-orphans"},
		{"-delete-orphan-wrapper-servers", "-apply", "-report=/tmp/r.json"},
		{"-delete-leftover-removal-records"},
		{"-rekey-removal-records", "-apply", "-report=/tmp/r.json"},
		{"-move-dependents"},
	} {
		_, err := parseHostedMCPWrappersFlags(args, hostedMCPWrappersGetenv, io.Discard)
		require.ErrorContains(t, err, "pass -project <id> or -all-projects", args)
		require.Equal(t, 2, runHostedMCPWrappers(args, io.Discard, hostedMCPWrappersGetenv), args)
	}
	both := []string{"-delete-orphan-wrapper-servers", "-project", project, "-all-projects"}
	_, err := parseHostedMCPWrappersFlags(both, hostedMCPWrappersGetenv, io.Discard)
	require.ErrorContains(t, err, "mutually exclusive")
	require.Equal(t, 2, runHostedMCPWrappers(both, io.Discard, hostedMCPWrappersGetenv))

	all, err := parseHostedMCPWrappersFlags([]string{"-delete-leftover-removal-records", "-all-projects"}, hostedMCPWrappersGetenv, io.Discard)
	require.NoError(t, err)
	require.False(t, all.options.ProjectID.Valid)
	scoped, err := parseHostedMCPWrappersFlags([]string{"-delete-leftover-removal-records", "-project", project}, hostedMCPWrappersGetenv, io.Discard)
	require.NoError(t, err)
	require.Equal(t, project, scoped.options.ProjectID.UUID.String())
}

func TestFileJournalAppendsOneLinePerRecord(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "journal.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	journal := hostedmcpbackfill.NewFileJournal(f)
	rowID, toolsetID := uuid.New(), uuid.New()
	require.NoError(t, journal.Append(hostedmcpbackfill.JournalRecord{Event: hostedmcpbackfill.JournalIntent, RowID: rowID, ToolsetID: toolsetID}))
	require.NoError(t, journal.Append(hostedmcpbackfill.JournalRecord{Event: hostedmcpbackfill.JournalCommitted, RowID: rowID, ToolsetID: toolsetID}))
	require.NoError(t, f.Close())

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := bytes.Split(bytes.TrimSpace(got), []byte("\n"))
	require.Len(t, lines, 2)
	var first hostedmcpbackfill.JournalRecord
	require.NoError(t, json.Unmarshal(lines[0], &first))
	require.Equal(t, hostedmcpbackfill.JournalIntent, first.Event)
	require.Equal(t, rowID, first.RowID)
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
	code := runHostedMCPWrappers([]string{"-all-projects", "-report", report}, failingWriter{}, getenv)
	require.Equal(t, 1, code)
	got, err := os.ReadFile(report)
	require.NoError(t, err)
	var decoded hostedmcpbackfill.Report
	require.NoError(t, json.Unmarshal(got, &decoded), "the report must be written before the summary")
}

func TestParseHostedMCPWrappersFlagsMoveDependents(t *testing.T) {
	t.Parallel()

	cfg, err := parseHostedMCPWrappersFlags([]string{"-all-projects", "-move-dependents"}, hostedMCPWrappersGetenv, io.Discard)
	require.NoError(t, err)
	require.True(t, cfg.options.MoveDependents)
	require.False(t, cfg.options.Apply)

	_, err = parseHostedMCPWrappersFlags([]string{"-all-projects", "-move-dependents", "-apply"}, hostedMCPWrappersGetenv, io.Discard)
	require.ErrorContains(t, err, "-readers-cut-over")

	_, err = parseHostedMCPWrappersFlags([]string{"-all-projects", "-move-dependents", "-apply", "-readers-cut-over"}, hostedMCPWrappersGetenv, io.Discard)
	require.ErrorContains(t, err, "-report")

	cfg, err = parseHostedMCPWrappersFlags([]string{"-all-projects", "-move-dependents", "-apply", "-readers-cut-over", "-report=/tmp/r.json"}, hostedMCPWrappersGetenv, io.Discard)
	require.NoError(t, err)
	require.True(t, cfg.options.MoveDependents)
	require.True(t, cfg.options.Apply)
	require.Equal(t, "/tmp/r.json.journal.jsonl", cfg.journalPath)
}

func TestParseHostedMCPWrappersFlagsRekeyRemovalRecords(t *testing.T) {
	t.Parallel()

	dry, err := parseHostedMCPWrappersFlags([]string{"-all-projects", "-rekey-removal-records"}, hostedMCPWrappersGetenv, io.Discard)
	require.NoError(t, err)
	require.True(t, dry.options.RekeyRemovals)
	require.False(t, dry.options.Apply)
	require.Empty(t, dry.journalPath)

	_, err = parseHostedMCPWrappersFlags([]string{"-all-projects", "-rekey-removal-records", "-apply"}, hostedMCPWrappersGetenv, io.Discard)
	require.ErrorContains(t, err, "-rekey-removal-records -apply requires -report")
	for _, other := range []string{"-move-dependents", "-cleanup-orphans"} {
		_, err = parseHostedMCPWrappersFlags([]string{"-all-projects", "-rekey-removal-records", other}, hostedMCPWrappersGetenv, io.Discard)
		require.ErrorContains(t, err, "separate phases", other)
	}
	_, err = parseHostedMCPWrappersFlags([]string{"-all-projects", "-rekey-removal-records", "-readers-cut-over"}, hostedMCPWrappersGetenv, io.Discard)
	require.ErrorContains(t, err, "only applies to -move-dependents")

	applied, err := parseHostedMCPWrappersFlags([]string{"-all-projects", "-rekey-removal-records", "-apply", "-report=/tmp/r.json"}, hostedMCPWrappersGetenv, io.Discard)
	require.NoError(t, err)
	require.True(t, applied.options.Apply)
	require.Equal(t, "/tmp/r.json.journal.jsonl", applied.journalPath)
	require.Equal(t, "-apply -rekey-removal-records", hostedMCPWrappersModeFlags(applied.options))
}

func TestParseHostedMCPWrappersFlagsDeleteLeftoverRemovalRecords(t *testing.T) {
	t.Parallel()

	dry, err := parseHostedMCPWrappersFlags([]string{"-all-projects", "-delete-leftover-removal-records"}, hostedMCPWrappersGetenv, io.Discard)
	require.NoError(t, err)
	require.True(t, dry.options.DeleteLeftoverRemovals)
	require.False(t, dry.options.Apply)
	require.Empty(t, dry.journalPath)

	_, err = parseHostedMCPWrappersFlags([]string{"-all-projects", "-delete-leftover-removal-records", "-apply"}, hostedMCPWrappersGetenv, io.Discard)
	require.ErrorContains(t, err, "-delete-leftover-removal-records -apply requires -report")
	for _, other := range []string{"-move-dependents", "-cleanup-orphans", "-rekey-removal-records"} {
		_, err = parseHostedMCPWrappersFlags([]string{"-all-projects", "-delete-leftover-removal-records", other}, hostedMCPWrappersGetenv, io.Discard)
		require.ErrorContains(t, err, "separate phases", other)
	}

	applied, err := parseHostedMCPWrappersFlags([]string{"-all-projects", "-delete-leftover-removal-records", "-apply", "-report=/tmp/r.json"}, hostedMCPWrappersGetenv, io.Discard)
	require.NoError(t, err)
	require.True(t, applied.options.Apply)
	require.Equal(t, "/tmp/r.json.journal.jsonl", applied.journalPath)
	require.Equal(t, "-apply -delete-leftover-removal-records", hostedMCPWrappersModeFlags(applied.options))
}

func TestParseHostedMCPWrappersFlagsDeleteOrphanWrapperServers(t *testing.T) {
	t.Parallel()

	dry, err := parseHostedMCPWrappersFlags([]string{"-all-projects", "-delete-orphan-wrapper-servers"}, hostedMCPWrappersGetenv, io.Discard)
	require.NoError(t, err)
	require.True(t, dry.options.DeleteOrphanWrappers)
	require.False(t, dry.options.Apply)
	require.Empty(t, dry.journalPath)

	_, err = parseHostedMCPWrappersFlags([]string{"-all-projects", "-delete-orphan-wrapper-servers", "-apply"}, hostedMCPWrappersGetenv, io.Discard)
	require.ErrorContains(t, err, "-delete-orphan-wrapper-servers -apply requires -report")
	for _, other := range []string{"-move-dependents", "-cleanup-orphans", "-rekey-removal-records", "-delete-leftover-removal-records"} {
		_, err = parseHostedMCPWrappersFlags([]string{"-all-projects", "-delete-orphan-wrapper-servers", other}, hostedMCPWrappersGetenv, io.Discard)
		require.ErrorContains(t, err, "separate phases", other)
	}

	applied, err := parseHostedMCPWrappersFlags([]string{"-all-projects", "-delete-orphan-wrapper-servers", "-apply", "-report=/tmp/r.json", "-plugin-publication-emit"}, hostedMCPWrappersGetenv, io.Discard)
	require.NoError(t, err)
	require.True(t, applied.options.Apply)
	require.True(t, applied.options.Publication.Enabled)
	require.Equal(t, "/tmp/r.json.journal.jsonl", applied.journalPath)
	require.Equal(t, "-apply -delete-orphan-wrapper-servers", hostedMCPWrappersModeFlags(applied.options))
}
