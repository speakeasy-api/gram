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
	org, email, hostname, hookSource, deviceHarness, version, generation, route, source, hookErr string
	at                                                                                           time.Time
}

func insertHookRow(t *testing.T, projectID string, r hookRow) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	attrs := map[string]any{
		"gram.event.source":  r.source,
		"gram.org.id":        r.org,
		"user.email":         r.email,
		"gram.hook.hostname": r.hostname,
		"gram.hook.source":   r.hookSource,
	}
	optional := map[string]string{
		"gram.hook.device.harness":        r.deviceHarness,
		"gram.hook.device.binary_version": r.version,
		"gram.hook.endpoint_generation":   r.generation,
		"gram.hook.device.install_route":  r.route,
		"gram.hook.error":                 r.hookErr,
	}
	for k, v := range optional {
		if v != "" {
			attrs[k] = v
		}
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

func newHookInstallsQueries(t *testing.T) *repo.Queries {
	t.Helper()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	return repo.New(conn)
}

func TestListHookInstalls_MergesOneMachineIntoOneInstall(t *testing.T) {
	t.Parallel()

	org := "org-" + uuid.NewString()
	project := uuid.NewString()
	now := time.Now().UTC()

	live := hookRow{org: org, email: "a@example.com", hostname: "laptop-a", hookSource: "claude-code", deviceHarness: "claude",
		version: "0.3.0", generation: "ingest_binary", route: "marketplace", source: "hook", at: now.Add(-48 * time.Hour)}
	insertHookRow(t, project, live)

	upgraded := live
	upgraded.version = "0.4.0"
	upgraded.route = ""
	upgraded.at = now.Add(-1 * time.Hour)
	insertHookRow(t, project, upgraded)

	replay := live
	replay.deviceHarness = ""
	replay.version = ""
	replay.at = now.Add(-3 * time.Hour)
	insertHookRow(t, project, replay)

	legacy := hookRow{org: org, email: "a@example.com", hostname: "laptop-a", hookSource: "claude-code",
		generation: "legacy_keyed", source: "hook", hookErr: "boom", at: now.Add(-72 * time.Hour)}
	insertHookRow(t, project, legacy)

	diag := hookRow{org: org, email: "a@example.com", hostname: "laptop-a", hookSource: "claude", deviceHarness: "claude",
		version: "0.4.0", source: "hook_diagnostic", at: now.Add(-2 * time.Hour)}
	insertHookRow(t, project, diag)

	q := newHookInstallsQueries(t)
	page, err := q.ListHookInstalls(t.Context(), org, now.Add(-30*24*time.Hour), 100)
	require.NoError(t, err)
	require.False(t, page.Truncated)
	require.Len(t, page.Installs, 1)

	a := page.Installs[0]
	require.Equal(t, project, a.ProjectID)
	require.Equal(t, "laptop-a", a.Hostname)
	require.Equal(t, "claude", a.Harness)
	require.Equal(t, "0.4.0", a.BinaryVersion)
	require.Equal(t, "ingest_binary", a.EndpointGeneration)
	require.Equal(t, "marketplace", a.InstallRoute)
	require.Equal(t, uint64(4), a.Events)
	require.Equal(t, map[string]uint64{"ingest_binary": 3, "legacy_keyed": 1}, a.EventsByGeneration)
	require.Equal(t, uint64(2), a.Errors)
	require.WithinDuration(t, legacy.at, a.FirstSeen, time.Millisecond)
	require.WithinDuration(t, upgraded.at, a.LastSeen, time.Millisecond)
}

func TestListHookInstalls_IgnoresRowsNotFromHookEndpoints(t *testing.T) {
	t.Parallel()

	org := "org-" + uuid.NewString()
	project := uuid.NewString()
	now := time.Now().UTC()

	// Provider OTEL, LiteLLM and imported sessions stamp the hook event
	// source but no endpoint generation.
	for _, source := range []string{"claude-code", "litellm", "claude-chat-web"} {
		insertHookRow(t, project, hookRow{org: org, email: "a@example.com", hostname: "laptop-a",
			hookSource: source, source: "hook", at: now.Add(-time.Hour)})
	}

	q := newHookInstallsQueries(t)
	page, err := q.ListHookInstalls(t.Context(), org, now.Add(-24*time.Hour), 100)
	require.NoError(t, err)
	require.Empty(t, page.Installs)
}

func TestListHookInstalls_WindowLimitAndOrg(t *testing.T) {
	t.Parallel()

	org := "org-" + uuid.NewString()
	project := uuid.NewString()
	now := time.Now().UTC()

	recent := hookRow{org: org, email: "a@example.com", hostname: "laptop-a", hookSource: "cursor", deviceHarness: "cursor",
		version: "0.4.0", generation: "ingest_binary", source: "hook", at: now.Add(-time.Hour)}
	insertHookRow(t, project, recent)

	old := recent
	old.hostname = "laptop-b"
	old.at = now.Add(-10 * 24 * time.Hour)
	insertHookRow(t, project, old)

	other := recent
	other.org = "org-" + uuid.NewString()
	insertHookRow(t, project, other)

	since := now.Add(-5 * 24 * time.Hour)
	sameDayBeforeSince := recent
	sameDayBeforeSince.hostname = "laptop-c"
	sameDayBeforeSince.at = time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, time.UTC)
	insertHookRow(t, project, sameDayBeforeSince)

	q := newHookInstallsQueries(t)

	page, err := q.ListHookInstalls(t.Context(), org, now.Add(-30*24*time.Hour), 100)
	require.NoError(t, err)
	require.Len(t, page.Installs, 3)
	require.Equal(t, "laptop-a", page.Installs[0].Hostname)

	page, err = q.ListHookInstalls(t.Context(), org, now.Add(-30*24*time.Hour), 1)
	require.NoError(t, err)
	require.Len(t, page.Installs, 1)
	require.True(t, page.Truncated)

	page, err = q.ListHookInstalls(t.Context(), org, since, 100)
	require.NoError(t, err)
	require.Len(t, page.Installs, 1)
	require.Equal(t, "laptop-a", page.Installs[0].Hostname)

	_, err = q.ListHookInstalls(t.Context(), "", now.Add(-24*time.Hour), 100)
	require.Error(t, err)
}

func TestListUnattributedHookInstalls(t *testing.T) {
	t.Parallel()

	project := uuid.NewString()
	host := "laptop-" + uuid.NewString()
	now := time.Now().UTC()

	insertHookRow(t, project, hookRow{hostname: host, hookSource: "claude-code",
		generation: "legacy_unkeyed", source: "hook", at: now.Add(-time.Hour)})

	q := newHookInstallsQueries(t)
	page, err := q.ListUnattributedHookInstalls(t.Context(), now.Add(-24*time.Hour), 10000)
	require.NoError(t, err)

	var found *repo.HookInstall
	for i := range page.Installs {
		if page.Installs[i].Hostname == host {
			found = &page.Installs[i]
		}
	}
	require.NotNil(t, found)
	require.Equal(t, "legacy_unkeyed", found.EndpointGeneration)
	require.Equal(t, "claude", found.Harness)
}

func TestListHookVersionMix(t *testing.T) {
	t.Parallel()

	org := "org-" + uuid.NewString()
	project := uuid.NewString()
	now := time.Now().UTC()
	today := now.Truncate(24 * time.Hour)
	yesterday := today.Add(-12 * time.Hour)

	insertHookRow(t, project, hookRow{org: org, email: "a@example.com", hostname: "a", hookSource: "claude-code",
		version: "0.2.0", generation: "ingest_binary", source: "hook", at: yesterday.Add(-time.Hour)})
	insertHookRow(t, project, hookRow{org: org, email: "a@example.com", hostname: "a", hookSource: "claude-code",
		version: "0.4.0", generation: "ingest_binary", source: "hook", at: yesterday})
	insertHookRow(t, project, hookRow{org: org, email: "b@example.com", hostname: "b", hookSource: "claude-code",
		version: "0.4.0", generation: "ingest_binary", source: "hook", at: yesterday})
	for range 2 {
		insertHookRow(t, project, hookRow{org: org, email: "c@example.com", hostname: "c", hookSource: "cursor",
			generation: "legacy_keyed", source: "hook", at: yesterday})
	}
	insertHookRow(t, project, hookRow{org: org, email: "d@example.com", hostname: "d", hookSource: "claude-code",
		version: "0.1.0", generation: "ingest_binary", source: "hook", at: today.Add(-10 * 24 * time.Hour)})

	q := newHookInstallsQueries(t)
	rows, err := q.ListHookVersionMix(t.Context(), today.Add(-7*24*time.Hour), today)
	require.NoError(t, err)

	got := []repo.HookVersionMixRow{}
	for _, row := range rows {
		if row.OrganizationID == org {
			got = append(got, row)
		}
	}
	require.Equal(t, []repo.HookVersionMixRow{
		{OrganizationID: org, BinaryVersion: "", EndpointGeneration: "legacy_keyed", Installs: 1, Events: 2, LegacyEvents: 2},
		{OrganizationID: org, BinaryVersion: "0.4.0", EndpointGeneration: "ingest_binary", Installs: 2, Events: 3, LegacyEvents: 0},
	}, got)
}
