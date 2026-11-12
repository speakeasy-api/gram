package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
)

func legacyAliasEndpointsEnv(key string) string {
	if key == "GRAM_DATABASE_URL" {
		return "postgres://user@db.example.internal:5432/gram"
	}
	return ""
}

var legacyAliasApplyArgs = []string{
	"-apply", "-environment=dev", "-confirm-environment=dev", "-confirm-target=db.example.internal:5432/gram",
	"-confirm-apply=legacy-alias-endpoints",
}

func TestParseLegacyAliasEndpointsFlagsDryRunByDefault(t *testing.T) {
	t.Parallel()
	cfg, err := parseLegacyAliasEndpointsFlags([]string{"-environment=dev"}, legacyAliasEndpointsEnv)
	require.NoError(t, err)
	require.False(t, cfg.apply)
}

func TestParseLegacyAliasEndpointsFlagsWriteGuards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "missing environment", args: []string{}, wantErr: "environment is required"},
		{name: "wrong confirm environment", args: []string{"-apply", "-environment=dev", "-confirm-environment=prod", "-journal=j"}, wantErr: "-confirm-environment"},
		{name: "wrong target", args: []string{"-apply", "-environment=dev", "-confirm-environment=dev", "-confirm-target=other:5432/gram", "-journal=j"}, wantErr: "-confirm-target=db.example.internal:5432/gram"},
		{name: "missing confirm apply", args: []string{"-apply", "-environment=dev", "-confirm-environment=dev", "-confirm-target=db.example.internal:5432/gram", "-journal=j"}, wantErr: "-confirm-apply=legacy-alias-endpoints"},
		{name: "missing journal", args: legacyAliasApplyArgs, wantErr: "-journal"},
		{name: "invalid project", args: []string{"-environment=dev", "-project=nope"}, wantErr: "invalid -project"},
		{name: "confirmed", args: append([]string{"-journal=j"}, legacyAliasApplyArgs...), wantErr: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseLegacyAliasEndpointsFlags(tc.args, legacyAliasEndpointsEnv)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestRunLegacyAliasEndpointsRefusesExistingJournal(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("earlier run\n"), 0o600))

	var out bytes.Buffer
	code := runLegacyAliasEndpoints(append([]string{"-journal=" + path}, legacyAliasApplyArgs...), &out, legacyAliasEndpointsEnv)
	require.Equal(t, 2, code)
	require.Contains(t, out.String(), "open -journal")
	contents, err := os.ReadFile(path) // #nosec G304 -- test temp file
	require.NoError(t, err)
	require.Equal(t, "earlier run\n", string(contents))
}

type recordingJournal struct {
	bytes.Buffer
	syncs int
}

func (j *recordingJournal) Sync() error {
	j.syncs++
	return nil
}

func legacyAliasPages(pages ...[]mcpendpoints.AliasBackfillCandidate) func(uuid.UUID) ([]mcpendpoints.AliasBackfillCandidate, error) {
	return func(uuid.UUID) ([]mcpendpoints.AliasBackfillCandidate, error) {
		if len(pages) == 0 {
			return nil, nil
		}
		page := pages[0]
		pages = pages[1:]
		return page, nil
	}
}

func TestLegacyAliasEndpointsRunJournalsIntentBeforeCommit(t *testing.T) {
	t.Parallel()
	created, failing, unknown := uuid.New(), uuid.New(), uuid.New()
	projectID, endpointID, domainID := uuid.New(), uuid.New(), uuid.New()
	journal := &recordingJournal{}
	var out bytes.Buffer
	planned := mcpendpoints.AliasBackfillResult{
		Rows: []mcpendpoints.AliasBackfillRow{
			{ToolsetID: created, ProjectID: projectID, Slug: "old", CustomDomainID: uuid.NullUUID{UUID: domainID, Valid: true}, Action: mcpendpoints.AliasBackfillSkip, Reason: "custom domain deleted"},
			{ToolsetID: created, ProjectID: projectID, Slug: "old", PlatformForCustomDomain: true, Action: mcpendpoints.AliasBackfillCreate, EndpointID: uuid.NullUUID{UUID: endpointID, Valid: true}},
		},
		AliasMove: &mcpendpoints.AliasMove{
			From: legacyalias.Alias{Slug: pgtype.Text{String: "old", Valid: true}, CustomDomainID: uuid.NullUUID{UUID: domainID, Valid: true}},
			To:   legacyalias.Alias{Slug: pgtype.Text{String: "old", Valid: true}},
		},
	}
	var journalAtCommit []string
	run := legacyAliasEndpointsRun{
		list: legacyAliasPages([]mcpendpoints.AliasBackfillCandidate{
			{ToolsetID: created, ProjectID: projectID, OrganizationID: "org"},
			{ToolsetID: failing, ProjectID: projectID, OrganizationID: "org"},
			{ToolsetID: unknown, ProjectID: projectID, OrganizationID: "org"},
		}),
		backfill: func(candidate mcpendpoints.AliasBackfillCandidate, beforeCommit func(mcpendpoints.AliasBackfillResult) error) (mcpendpoints.AliasBackfillResult, error) {
			switch candidate.ToolsetID {
			case failing:
				return mcpendpoints.AliasBackfillResult{}, errors.New("boom")
			case unknown:
				require.NoError(t, beforeCommit(mcpendpoints.AliasBackfillResult{Rows: []mcpendpoints.AliasBackfillRow{{ToolsetID: unknown, ProjectID: projectID, Slug: "u", Action: mcpendpoints.AliasBackfillCreate, EndpointID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}}}))
				// An interrupted commit must stay reported as unknown.
				return mcpendpoints.AliasBackfillResult{}, fmt.Errorf("backfill candidate: %w", fmt.Errorf("%w: %w", mcpendpoints.ErrAliasBackfillCommitUnknown, context.Canceled))
			}
			require.NoError(t, beforeCommit(planned))
			journalAtCommit = strings.Split(strings.TrimSpace(journal.String()), "\n")
			return planned, nil
		},
		apply:   true,
		stdout:  &out,
		journal: journal,
	}

	totals, err := run.execute()
	require.NoError(t, err)
	require.Equal(t, legacyAliasEndpointsTotals{"skip": 1, "created": 1, "alias_moved": 1, "failed": 1, "commit_outcome_unknown": 1}, totals)

	require.Len(t, journalAtCommit, 1, "the pending entry is flushed before the commit")
	lines := strings.Split(strings.TrimSpace(journal.String()), "\n")
	require.Len(t, lines, 4)
	require.Equal(t, 4, journal.syncs)
	entries := make([]legacyAliasJournalEntry, len(lines))
	for i, line := range lines {
		require.NoError(t, json.Unmarshal([]byte(line), &entries[i]))
	}
	require.Equal(t, legacyAliasJournalEntry{
		Status: "pending", McpServerID: created, ProjectID: projectID,
		Endpoints: []legacyAliasJournalEndpoint{{EndpointID: endpointID, Address: legacyAliasJournalAddress{Slug: "old", CustomDomainID: nil}}},
		AliasMove: &legacyAliasJournalMove{From: legacyAliasJournalAddress{Slug: "old", CustomDomainID: &domainID}, To: legacyAliasJournalAddress{Slug: "old", CustomDomainID: nil}},
	}, entries[0])
	require.Equal(t, legacyAliasJournalEntry{Status: "committed", McpServerID: created, ProjectID: projectID}, entries[1])
	require.Equal(t, "pending", entries[2].Status)
	require.Equal(t, unknown, entries[2].McpServerID)
	require.Equal(t, legacyAliasJournalEntry{Status: "commit_outcome_unknown", McpServerID: unknown, ProjectID: projectID}, entries[3])

	var report bytes.Buffer
	code := legacyAliasEndpointsReport(&report, legacyAliasEndpointsConfig{environment: "dev", apply: true}, totals, nil)
	require.Equal(t, 1, code)
	require.Contains(t, report.String(), "mode=apply environment=dev alias_moved=1 commit_outcome_unknown=1 created=1 failed=1 skip=1")
	require.Contains(t, out.String(), "toolset="+unknown.String()+" project="+projectID.String()+" action=commit_outcome_unknown")
	require.Contains(t, out.String(), "action=failed")
}

type failingJournal struct{}

func (failingJournal) Write([]byte) (int, error) { return 0, errors.New("disk full") }
func (failingJournal) Sync() error               { return nil }

func TestLegacyAliasEndpointsJournalFailureStopsBeforeCommit(t *testing.T) {
	t.Parallel()
	committed := false
	var out bytes.Buffer
	run := legacyAliasEndpointsRun{
		list: legacyAliasPages([]mcpendpoints.AliasBackfillCandidate{{ToolsetID: uuid.New(), ProjectID: uuid.New(), OrganizationID: "org"}}),
		backfill: func(candidate mcpendpoints.AliasBackfillCandidate, beforeCommit func(mcpendpoints.AliasBackfillResult) error) (mcpendpoints.AliasBackfillResult, error) {
			result := mcpendpoints.AliasBackfillResult{Rows: []mcpendpoints.AliasBackfillRow{{ToolsetID: candidate.ToolsetID, Slug: "s", Action: mcpendpoints.AliasBackfillCreate, EndpointID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}}}
			if err := beforeCommit(result); err != nil {
				return result, fmt.Errorf("%w: %w", mcpendpoints.ErrAliasBackfillNotRecorded, err)
			}
			committed = true
			return result, nil
		},
		apply:   true,
		stdout:  &out,
		journal: failingJournal{},
	}
	totals, err := run.execute()
	require.ErrorIs(t, err, mcpendpoints.ErrAliasBackfillNotRecorded)
	require.False(t, committed)
	require.Equal(t, legacyAliasEndpointsTotals{"failed": 1}, totals)
}

func TestLegacyAliasEndpointsDryRunWritesNoJournalAndExitsZero(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	run := legacyAliasEndpointsRun{
		list: legacyAliasPages([]mcpendpoints.AliasBackfillCandidate{{ToolsetID: uuid.New(), ProjectID: uuid.New(), OrganizationID: "org"}}),
		backfill: func(candidate mcpendpoints.AliasBackfillCandidate, _ func(mcpendpoints.AliasBackfillResult) error) (mcpendpoints.AliasBackfillResult, error) {
			return mcpendpoints.AliasBackfillResult{Rows: []mcpendpoints.AliasBackfillRow{{ToolsetID: candidate.ToolsetID, Slug: "s", Action: mcpendpoints.AliasBackfillCreate}}}, nil
		},
		apply:   false,
		stdout:  &out,
		journal: nil,
	}
	totals, err := run.execute()
	require.NoError(t, err)
	require.Equal(t, legacyAliasEndpointsTotals{"would_create": 1}, totals)
	require.Equal(t, 0, legacyAliasEndpointsReport(&out, legacyAliasEndpointsConfig{environment: "dev"}, totals, nil))
}

func TestLegacyAliasEndpointsRunPagesFromLastID(t *testing.T) {
	t.Parallel()
	first, second, third := uuid.New(), uuid.New(), uuid.New()
	pages := map[uuid.UUID][]mcpendpoints.AliasBackfillCandidate{
		uuid.Nil: {{ToolsetID: first, ProjectID: uuid.New(), OrganizationID: "org"}, {ToolsetID: second, ProjectID: uuid.New(), OrganizationID: "org"}},
		second:   {{ToolsetID: third, ProjectID: uuid.New(), OrganizationID: "org"}},
	}
	var cursors []uuid.UUID
	var seen []uuid.UUID
	var out bytes.Buffer
	run := legacyAliasEndpointsRun{
		list: func(after uuid.UUID) ([]mcpendpoints.AliasBackfillCandidate, error) {
			cursors = append(cursors, after)
			return pages[after], nil
		},
		backfill: func(candidate mcpendpoints.AliasBackfillCandidate, _ func(mcpendpoints.AliasBackfillResult) error) (mcpendpoints.AliasBackfillResult, error) {
			seen = append(seen, candidate.ToolsetID)
			return mcpendpoints.AliasBackfillResult{}, nil
		},
		apply:   false,
		stdout:  &out,
		journal: nil,
	}
	_, err := run.execute()
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{uuid.Nil, second, third}, cursors)
	require.Equal(t, []uuid.UUID{first, second, third}, seen)
}
