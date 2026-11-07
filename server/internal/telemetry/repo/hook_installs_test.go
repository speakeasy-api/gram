package repo_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

type hookRow struct {
	org, email, hostname, harness, version, generation, route, source, hookErr string
	at                                                                         time.Time
}

func insertHookRow(t *testing.T, projectID string, r hookRow) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	attrs := map[string]any{
		"gram.event.source":               r.source,
		"gram.org.id":                     r.org,
		"user.email":                      r.email,
		"gram.hook.hostname":              r.hostname,
		"gram.hook.source":                r.harness,
		"gram.hook.device.binary_version": r.version,
		"gram.hook.endpoint_generation":   r.generation,
		"gram.hook.device.install_route":  r.route,
	}
	if r.hookErr != "" {
		attrs["gram.hook.error"] = r.hookErr
	}
	attrsJSON, err := json.Marshal(attrs)
	require.NoError(t, err)

	err = conn.Exec(t.Context(), `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			attributes, resource_attributes, gram_project_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, uuid.NewString(), r.at.UnixNano(), r.at.UnixNano(), "INFO", "Hook: PreToolUse",
		string(attrsJSON), "{}", projectID, "", "gram-hooks")
	require.NoError(t, err)
}

func TestListHookInstalls_LatestValuesPerInstall(t *testing.T) {
	t.Parallel()

	org := "org-" + uuid.NewString()
	project := uuid.NewString()
	now := time.Now().UTC()

	base := hookRow{org: org, email: "a@example.com", hostname: "laptop-a", harness: "claude",
		version: "0.3.0", generation: "ingest_binary", route: "marketplace", source: "hook", at: now.Add(-48 * time.Hour)}
	insertHookRow(t, project, base)

	upgraded := base
	upgraded.version = "0.4.0"
	upgraded.route = ""
	upgraded.at = now.Add(-1 * time.Hour)
	insertHookRow(t, project, upgraded)

	diag := base
	diag.source = "hook_diagnostic"
	diag.version = ""
	diag.generation = ""
	diag.at = now.Add(-2 * time.Hour)
	insertHookRow(t, project, diag)

	legacy := hookRow{org: org, email: "b@example.com", hostname: "laptop-b", harness: "cursor",
		generation: "legacy_keyed", source: "hook", hookErr: "boom", at: now.Add(-10 * 24 * time.Hour)}
	insertHookRow(t, project, legacy)

	other := base
	other.org = "org-" + uuid.NewString()
	insertHookRow(t, project, other)

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	q := repo.New(conn)
	installs, err := q.ListHookInstalls(t.Context(), org, now.Add(-30*24*time.Hour), 100)
	require.NoError(t, err)
	require.Len(t, installs, 2)

	a := installs[0]
	require.Equal(t, project, a.ProjectID)
	require.Equal(t, "laptop-a", a.Hostname)
	require.Equal(t, "claude", a.Harness)
	require.Equal(t, "0.4.0", a.BinaryVersion)
	require.Equal(t, "ingest_binary", a.EndpointGeneration)
	require.Equal(t, "marketplace", a.InstallRoute)
	require.Equal(t, uint64(3), a.Events)
	require.Equal(t, uint64(1), a.Errors)
	require.WithinDuration(t, base.at, a.FirstSeen, time.Millisecond)
	require.WithinDuration(t, upgraded.at, a.LastSeen, time.Millisecond)

	b := installs[1]
	require.Equal(t, "laptop-b", b.Hostname)
	require.Empty(t, b.BinaryVersion)
	require.Equal(t, "legacy_keyed", b.EndpointGeneration)
	require.Equal(t, uint64(1), b.Errors)

	recent, err := q.ListHookInstalls(t.Context(), org, now.Add(-5*24*time.Hour), 100)
	require.NoError(t, err)
	require.Len(t, recent, 1)
}

func TestSummarizeHookInstalls(t *testing.T) {
	t.Parallel()

	now := time.Now()
	installs := []repo.HookInstall{
		{BinaryVersion: "0.4.0", EndpointGeneration: "ingest_binary", Events: 10, LastSeen: now},
		{BinaryVersion: "0.10.0", EndpointGeneration: "ingest_binary", Events: 5, LastSeen: now},
		{BinaryVersion: "0.3.0", EndpointGeneration: "ingest_binary", Events: 1, LastSeen: now.Add(-72 * time.Hour)},
		{BinaryVersion: "", EndpointGeneration: "legacy_unkeyed", Events: 7, LastSeen: now},
		{BinaryVersion: "0.4.0", EndpointGeneration: "ingest_script", Events: 2, LastSeen: now},
	}

	s := repo.SummarizeHookInstalls(installs, "0.4.0", now.Add(-24*time.Hour))
	require.Equal(t, 5, s.Installs)
	require.Equal(t, []repo.HookVersionCount{
		{Version: "0.10.0", Installs: 1},
		{Version: "0.4.0", Installs: 2},
		{Version: "0.3.0", Installs: 1},
		{Version: "", Installs: 1},
	}, s.Versions)
	require.Equal(t, 2, s.Outdated)
	require.Equal(t, uint64(7), s.EventsByGeneration["legacy_unkeyed"])
	require.Equal(t, uint64(16), s.EventsByGeneration["ingest_binary"])
	require.Len(t, s.Quiet, 1)
	require.Equal(t, "0.3.0", s.Quiet[0].BinaryVersion)

	require.Zero(t, repo.SummarizeHookInstalls(installs, "", now).Outdated)
}
