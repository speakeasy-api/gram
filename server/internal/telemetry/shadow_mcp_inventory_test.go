package telemetry_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryRepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestShadowMCPInventoryURLs_UpsertAndList(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	projectID := uuid.NewString()
	otherProjectID := uuid.NewString()
	firstSeen := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	lastSeen := firstSeen.Add(time.Hour)

	require.NoError(t, ti.chClient.UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://mcp.speakeasy.com/mcp",
			URLHost:            "mcp.speakeasy.com",
			ServerName:         "",
			SeenAt:             firstSeen,
		},
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://mcp.speakeasy.com/mcp",
			URLHost:            "mcp.speakeasy.com",
			ServerName:         "Speakeasy",
			SeenAt:             lastSeen,
		},
		{
			GramProjectID:      otherProjectID,
			CanonicalServerURL: "https://mcp.speakeasy.com/mcp",
			URLHost:            "mcp.speakeasy.com",
			ServerName:         "Other",
			SeenAt:             lastSeen.Add(time.Hour),
		},
	}))

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	rows, err := ti.chClient.ListShadowMCPInventoryURLs(ctx, telemetryRepo.ListShadowMCPInventoryURLsParams{
		GramProjectID: projectID,
		Limit:         50,
		Cursor:        "",
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "https://mcp.speakeasy.com/mcp", rows[0].CanonicalServerURL)
	require.Equal(t, "mcp.speakeasy.com", rows[0].URLHost)
	require.Equal(t, "Speakeasy", rows[0].ServerName)
	require.Equal(t, firstSeen, rows[0].FirstSeen)
	require.Equal(t, lastSeen, rows[0].LastSeen)
}

func TestShadowMCPInventoryURLs_NameOverrideSurvivesLaterObservation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	projectID := uuid.NewString()
	serverURL := "https://github.example.com/mcp"
	firstSeen := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)

	require.NoError(t, ti.chClient.UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{{
		GramProjectID:      projectID,
		CanonicalServerURL: serverURL,
		URLHost:            "github.example.com",
		ServerName:         "GitHub MCP",
		SeenAt:             firstSeen,
		UpdatedAt:          firstSeen,
	}}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	updated, err := ti.chClient.UpdateShadowMCPInventoryURLNameOverride(ctx, telemetryRepo.UpdateShadowMCPInventoryURLNameOverrideParams{
		GramProjectID:      projectID,
		CanonicalServerURL: serverURL,
		ServerNameOverride: "Engineering GitHub",
		UpdatedAt:          firstSeen.Add(time.Minute),
	})
	require.NoError(t, err)
	require.True(t, updated)

	require.NoError(t, ti.chClient.UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{{
		GramProjectID:      projectID,
		CanonicalServerURL: serverURL,
		URLHost:            "github.example.com",
		ServerName:         "GitHub Enterprise MCP",
		SeenAt:             firstSeen.Add(2 * time.Minute),
		UpdatedAt:          firstSeen.Add(2 * time.Minute),
	}}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	rows, err := ti.chClient.ListShadowMCPInventoryURLs(ctx, telemetryRepo.ListShadowMCPInventoryURLsParams{
		GramProjectID: projectID,
		Limit:         10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "GitHub Enterprise MCP", rows[0].ServerName)
	require.Equal(t, "Engineering GitHub", rows[0].ServerNameOverride)
}

func TestShadowMCPInventoryURLs_NameOverrideCanBeCleared(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	projectID := uuid.NewString()
	serverURL := "https://github.example.com/mcp"
	seenAt := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)

	require.NoError(t, ti.chClient.UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{{
		GramProjectID:      projectID,
		CanonicalServerURL: serverURL,
		URLHost:            "github.example.com",
		ServerName:         "GitHub MCP",
		SeenAt:             seenAt,
		UpdatedAt:          seenAt,
	}}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	updated, err := ti.chClient.UpdateShadowMCPInventoryURLNameOverride(ctx, telemetryRepo.UpdateShadowMCPInventoryURLNameOverrideParams{
		GramProjectID:      projectID,
		CanonicalServerURL: serverURL,
		ServerNameOverride: "Engineering GitHub",
		UpdatedAt:          seenAt.Add(time.Minute),
	})
	require.NoError(t, err)
	require.True(t, updated)

	updated, err = ti.chClient.UpdateShadowMCPInventoryURLNameOverride(ctx, telemetryRepo.UpdateShadowMCPInventoryURLNameOverrideParams{
		GramProjectID:      projectID,
		CanonicalServerURL: serverURL,
		ServerNameOverride: "",
		UpdatedAt:          seenAt.Add(2 * time.Minute),
	})
	require.NoError(t, err)
	require.True(t, updated)

	rows, err := ti.chClient.ListShadowMCPInventoryURLs(ctx, telemetryRepo.ListShadowMCPInventoryURLsParams{
		GramProjectID: projectID,
		Limit:         10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "GitHub MCP", rows[0].ServerName)
	require.Empty(t, rows[0].ServerNameOverride)
}

func TestShadowMCPInventoryURLs_ListBySlugHash(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	projectID := uuid.NewString()
	otherProjectID := uuid.NewString()
	seenAt := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)

	require.NoError(t, ti.chClient.UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://detail.example.com/mcp",
			URLHost:            "detail.example.com",
			ServerName:         "Detail MCP",
			SeenAt:             seenAt,
		},
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://other.example.com/mcp",
			URLHost:            "other.example.com",
			ServerName:         "Other MCP",
			SeenAt:             seenAt,
		},
		{
			GramProjectID:      otherProjectID,
			CanonicalServerURL: "https://detail.example.com/mcp",
			URLHost:            "detail.example.com",
			ServerName:         "Other Project",
			SeenAt:             seenAt,
		},
	}))

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	rows, err := ti.chClient.ListShadowMCPInventoryURLsBySlugHash(ctx, telemetryRepo.ListShadowMCPInventoryURLsBySlugHashParams{
		GramProjectID: projectID,
		SlugHash:      "30d7c46c",
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "https://detail.example.com/mcp", rows[0].CanonicalServerURL)
	require.Equal(t, "Detail MCP", rows[0].ServerName)

	explainRows, err := ti.chConn.Query(ctx, `
		EXPLAIN indexes = 1
		SELECT canonical_server_url
		FROM shadow_mcp_inventory_urls
		WHERE gram_project_id = ?
		  AND substring(lower(hex(SHA256(canonical_server_url))), 1, 8) = ?
	`, projectID, "30d7c46c")
	require.NoError(t, err)

	var explainPlan strings.Builder
	for explainRows.Next() {
		var line string
		require.NoError(t, explainRows.Scan(&line))
		explainPlan.WriteString(line)
		explainPlan.WriteByte('\n')
	}
	require.NoError(t, explainRows.Err())
	require.NoError(t, explainRows.Close())
	require.Contains(t, explainPlan.String(), "PrimaryKey")
	require.Contains(t, explainPlan.String(), "idx_shadow_mcp_inventory_urls_slug_hash")
}

func TestShadowMCPInventoryURLs_PaginatesLastSeen(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	projectID := uuid.NewString()
	base := time.Date(2026, 6, 29, 12, 30, 0, 0, time.UTC)

	require.NoError(t, ti.chClient.UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{
		{GramProjectID: projectID, CanonicalServerURL: "https://gamma.example.com/mcp", URLHost: "gamma.example.com", ServerName: "Gamma", SeenAt: base.Add(3 * time.Minute), FirstSeen: time.Time{}, LastSeen: time.Time{}, UpdatedAt: time.Time{}},
		{GramProjectID: projectID, CanonicalServerURL: "https://alpha.example.com/mcp", URLHost: "alpha.example.com", ServerName: "Alpha", SeenAt: base.Add(2 * time.Minute), FirstSeen: time.Time{}, LastSeen: time.Time{}, UpdatedAt: time.Time{}},
		{GramProjectID: projectID, CanonicalServerURL: "https://beta.example.com/mcp", URLHost: "beta.example.com", ServerName: "Beta", SeenAt: base.Add(2 * time.Minute), FirstSeen: time.Time{}, LastSeen: time.Time{}, UpdatedAt: time.Time{}},
		{GramProjectID: projectID, CanonicalServerURL: "https://delta.example.com/mcp", URLHost: "delta.example.com", ServerName: "Delta", SeenAt: base.Add(time.Minute), FirstSeen: time.Time{}, LastSeen: time.Time{}, UpdatedAt: time.Time{}},
	}))

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	firstPage, err := ti.chClient.ListShadowMCPInventoryURLs(ctx, telemetryRepo.ListShadowMCPInventoryURLsParams{
		GramProjectID: projectID,
		Limit:         2,
		Cursor:        "",
	})
	require.NoError(t, err)
	require.Len(t, firstPage, 2)
	require.Equal(t, []string{
		"https://gamma.example.com/mcp",
		"https://alpha.example.com/mcp",
	}, inventoryURLRows(firstPage))

	cursor, err := telemetryRepo.EncodeShadowMCPInventoryURLCursor(firstPage[1])
	require.NoError(t, err)

	secondPage, err := ti.chClient.ListShadowMCPInventoryURLs(ctx, telemetryRepo.ListShadowMCPInventoryURLsParams{
		GramProjectID: projectID,
		Limit:         2,
		Cursor:        cursor,
	})
	require.NoError(t, err)
	require.Len(t, secondPage, 2)
	require.Equal(t, []string{
		"https://beta.example.com/mcp",
		"https://delta.example.com/mcp",
	}, inventoryURLRows(secondPage))
}

func TestShadowMCPInventoryURLs_PaginatesLastCalledThenLastSeen(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	projectID := uuid.NewString()
	base := time.Date(2026, 6, 29, 12, 45, 0, 0, time.UTC)

	require.NoError(t, ti.chClient.UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{
		{GramProjectID: projectID, CanonicalServerURL: "https://never-called.example.com/mcp", URLHost: "never-called.example.com", ServerName: "Never Called", SeenAt: base.Add(5 * time.Minute), FirstSeen: time.Time{}, LastSeen: time.Time{}, UpdatedAt: time.Time{}},
		{GramProjectID: projectID, CanonicalServerURL: "https://most-recent-call.example.com/mcp", URLHost: "most-recent-call.example.com", ServerName: "Most Recent Call", SeenAt: base, FirstSeen: time.Time{}, LastSeen: time.Time{}, UpdatedAt: time.Time{}},
		{GramProjectID: projectID, CanonicalServerURL: "https://same-call-newer-seen.example.com/mcp", URLHost: "same-call-newer-seen.example.com", ServerName: "Same Call Newer Seen", SeenAt: base.Add(3 * time.Minute), FirstSeen: time.Time{}, LastSeen: time.Time{}, UpdatedAt: time.Time{}},
		{GramProjectID: projectID, CanonicalServerURL: "https://same-call-older-seen.example.com/mcp", URLHost: "same-call-older-seen.example.com", ServerName: "Same Call Older Seen", SeenAt: base.Add(2 * time.Minute), FirstSeen: time.Time{}, LastSeen: time.Time{}, UpdatedAt: time.Time{}},
	}))

	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://most-recent-call.example.com/mcp",
		ServerName: "Most Recent Call",
		UserEmail:  "ada@example.com",
		ObservedAt: base.Add(4 * time.Minute),
	})
	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://same-call-newer-seen.example.com/mcp",
		ServerName: "Same Call Newer Seen",
		UserEmail:  "ada@example.com",
		ObservedAt: base.Add(time.Minute),
	})
	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://same-call-older-seen.example.com/mcp",
		ServerName: "Same Call Older Seen",
		UserEmail:  "ada@example.com",
		ObservedAt: base.Add(time.Minute),
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	firstPage, err := ti.chClient.ListShadowMCPInventoryURLs(ctx, telemetryRepo.ListShadowMCPInventoryURLsParams{
		GramProjectID: projectID,
		Limit:         2,
		Cursor:        "",
	})
	require.NoError(t, err)
	require.Len(t, firstPage, 2)
	require.Equal(t, []string{
		"https://most-recent-call.example.com/mcp",
		"https://same-call-newer-seen.example.com/mcp",
	}, inventoryURLRows(firstPage))

	cursor, err := telemetryRepo.EncodeShadowMCPInventoryURLCursor(firstPage[1])
	require.NoError(t, err)

	secondPage, err := ti.chClient.ListShadowMCPInventoryURLs(ctx, telemetryRepo.ListShadowMCPInventoryURLsParams{
		GramProjectID: projectID,
		Limit:         2,
		Cursor:        cursor,
	})
	require.NoError(t, err)
	require.Len(t, secondPage, 2)
	require.Equal(t, []string{
		"https://same-call-older-seen.example.com/mcp",
		"https://never-called.example.com/mcp",
	}, inventoryURLRows(secondPage))
}

func TestShadowMCPInventoryURLs_RejectsInvalidCursor(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	projectID := uuid.NewString()
	lastSeen := time.Date(2026, 6, 29, 12, 45, 0, 0, time.UTC)

	invalidBase64 := "not base64"
	invalidJSON := base64.RawURLEncoding.EncodeToString([]byte("not-json"))
	missingURL := encodeRawInventoryURLCursor(t, map[string]any{
		"last_called_unix_nano": int64(0),
		"last_seen_unix_nano":   lastSeen.UnixNano(),
	})
	missingLastCalled := encodeRawInventoryURLCursor(t, map[string]any{
		"canonical_server_url": "https://alpha.example.com/mcp",
		"last_seen_unix_nano":  lastSeen.UnixNano(),
	})
	missingLastSeen := encodeRawInventoryURLCursor(t, map[string]any{
		"canonical_server_url":  "https://alpha.example.com/mcp",
		"last_called_unix_nano": int64(0),
	})

	invalidCursors := []struct {
		name   string
		cursor string
	}{
		{name: "invalid base64", cursor: invalidBase64},
		{name: "invalid JSON", cursor: invalidJSON},
		{name: "missing URL", cursor: missingURL},
		{name: "missing last called", cursor: missingLastCalled},
		{name: "missing last seen", cursor: missingLastSeen},
	}

	for _, invalidCursor := range invalidCursors {
		_, err := ti.chClient.ListShadowMCPInventoryURLs(ctx, telemetryRepo.ListShadowMCPInventoryURLsParams{
			GramProjectID: projectID,
			Limit:         2,
			Cursor:        invalidCursor.cursor,
		})
		require.Error(t, err, invalidCursor.name)
	}
}

func TestShadowMCPInventoryUsage_FromTelemetry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	projectID := uuid.NewString()
	otherProjectID := uuid.NewString()
	base := time.Date(2026, 6, 29, 13, 0, 0, 0, time.UTC)

	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp?token=secret",
		ServerName: "Speakeasy",
		UserEmail:  "ada@example.com",
		ObservedAt: base,
	})
	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp?other=1#frag",
		ServerName: "Speakeasy",
		UserEmail:  "ada@example.com",
		ObservedAt: base.Add(time.Minute),
	})
	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp",
		ServerName: "Speakeasy",
		UserEmail:  "grace@example.com",
		ObservedAt: base.Add(2 * time.Minute),
	})
	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  otherProjectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp",
		ServerName: "Other",
		UserEmail:  "other@example.com",
		ObservedAt: base.Add(3 * time.Minute),
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	usage, err := ti.chClient.ListShadowMCPInventoryUsage(ctx, telemetryRepo.ListShadowMCPInventoryUsageParams{
		GramProjectID: projectID,
		Limit:         50,
	})
	require.NoError(t, err)
	require.Len(t, usage, 1)
	require.EqualValues(t, 3, usage[0].CallCount)
	require.Equal(t, "https://mcp.speakeasy.com/mcp", usage[0].CanonicalServerURL)
	require.NotNil(t, usage[0].LastCalled)
	require.Equal(t, base.Add(2*time.Minute), *usage[0].LastCalled)
	require.EqualValues(t, 2, usage[0].UserCount)
	require.Equal(t, []string{"ada@example.com", "grace@example.com"}, usage[0].TopUsers)
}

func TestShadowMCPInventoryUsage_FiltersToCanonicalURLsBeforeLimit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	projectID := uuid.NewString()
	base := time.Date(2026, 6, 29, 13, 30, 0, 0, time.UTC)

	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://target.example.com/mcp?token=older",
		ServerName: "Target",
		UserEmail:  "alex@example.com",
		ObservedAt: base,
	})
	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://other.example.com/mcp",
		ServerName: "Other",
		UserEmail:  "sam@example.com",
		ObservedAt: base.Add(time.Hour),
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	usage, err := ti.chClient.ListShadowMCPInventoryUsage(ctx, telemetryRepo.ListShadowMCPInventoryUsageParams{
		GramProjectID:       projectID,
		CanonicalServerURLs: []string{"https://target.example.com/mcp"},
		Limit:               1,
	})
	require.NoError(t, err)
	require.Len(t, usage, 1)
	require.Equal(t, "https://target.example.com/mcp", usage[0].CanonicalServerURL)
	require.EqualValues(t, 1, usage[0].CallCount)
	require.EqualValues(t, 1, usage[0].UserCount)
	require.Equal(t, []string{"alex@example.com"}, usage[0].TopUsers)
}

func TestListShadowMCPInventoryUsers_FromTelemetry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	projectID := uuid.NewString()
	base := time.Date(2026, 6, 29, 14, 0, 0, 0, time.UTC)

	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp?token=secret",
		ServerName: "Speakeasy",
		UserEmail:  "ada@example.com",
		ObservedAt: base,
	})
	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp?other=1",
		ServerName: "Speakeasy",
		UserEmail:  "ada@example.com",
		ObservedAt: base.Add(time.Minute),
	})
	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp",
		ServerName: "Speakeasy",
		UserEmail:  "grace@example.com",
		ObservedAt: base.Add(2 * time.Minute),
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	users, err := ti.chClient.ListShadowMCPInventoryUsers(ctx, telemetryRepo.ListShadowMCPInventoryUsersParams{
		GramProjectID:      projectID,
		CanonicalServerURL: "https://mcp.speakeasy.com/mcp",
		Limit:              50,
	})
	require.NoError(t, err)
	require.Len(t, users, 2)
	require.Equal(t, "ada@example.com", users[0].UserKey)
	require.Equal(t, "ada@example.com", users[0].UserEmail)
	require.EqualValues(t, 2, users[0].CallCount)
	require.Equal(t, base.Add(time.Minute), users[0].LastCalled)
	require.Equal(t, "grace@example.com", users[1].UserKey)
	require.Equal(t, "grace@example.com", users[1].UserEmail)
	require.EqualValues(t, 1, users[1].CallCount)
	require.Equal(t, base.Add(2*time.Minute), users[1].LastCalled)
}

func TestListShadowMCPInventoryUsers_Paginates(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	projectID := uuid.NewString()
	base := time.Date(2026, 6, 29, 14, 30, 0, 0, time.UTC)

	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp?token=secret",
		ServerName: "Speakeasy",
		UserEmail:  "ada@example.com",
		ObservedAt: base,
	})
	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp",
		ServerName: "Speakeasy",
		UserEmail:  "ada@example.com",
		ObservedAt: base.Add(time.Minute),
	})
	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp",
		ServerName: "Speakeasy",
		UserEmail:  "grace@example.com",
		ObservedAt: base.Add(2 * time.Minute),
	})
	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp",
		ServerName: "Speakeasy",
		UserEmail:  "linus@example.com",
		ObservedAt: base.Add(3 * time.Minute),
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	firstPage, err := ti.chClient.ListShadowMCPInventoryUsers(ctx, telemetryRepo.ListShadowMCPInventoryUsersParams{
		GramProjectID:      projectID,
		CanonicalServerURL: "https://mcp.speakeasy.com/mcp",
		Limit:              2,
	})
	require.NoError(t, err)
	require.Len(t, firstPage, 2)

	require.Equal(t, "ada@example.com", firstPage[0].UserKey)
	require.Equal(t, "linus@example.com", firstPage[1].UserKey)
	cursor, err := telemetryRepo.EncodeShadowMCPInventoryUserCursor(firstPage[1])
	require.NoError(t, err)

	secondPage, err := ti.chClient.ListShadowMCPInventoryUsers(ctx, telemetryRepo.ListShadowMCPInventoryUsersParams{
		GramProjectID:      projectID,
		CanonicalServerURL: "https://mcp.speakeasy.com/mcp",
		Limit:              2,
		Cursor:             cursor,
	})
	require.NoError(t, err)
	require.Len(t, secondPage, 1)

	require.Equal(t, "grace@example.com", secondPage[0].UserKey)
}

func TestLoggerUpsertShadowMCPInventoryURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	projectID := uuid.NewString()
	seenAt := time.Date(2026, 6, 29, 15, 0, 0, 0, time.UTC)
	invURL, ok := shadowmcp.CanonicalizeInventoryURL("https://mcp.speakeasy.com/mcp?token=secret")
	require.True(t, ok)

	require.NoError(t, ti.telemLogger.UpsertShadowMCPInventoryURLs(ctx, []telemetry.ShadowMCPInventoryURL{
		{
			GramProjectID: projectID,
			ServerURL:     invURL,
			ServerName:    "Speakeasy",
			SeenAt:        seenAt,
		},
	}))

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	rows, err := ti.chClient.ListShadowMCPInventoryURLs(ctx, telemetryRepo.ListShadowMCPInventoryURLsParams{
		GramProjectID: projectID,
		Limit:         50,
		Cursor:        "",
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "https://mcp.speakeasy.com/mcp", rows[0].CanonicalServerURL)
	require.Equal(t, "Speakeasy", rows[0].ServerName)
	require.Equal(t, seenAt, rows[0].LastSeen)
}

func TestBackfillShadowMCPInventoryURLs_CanonicalizesAndUpserts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	projectID := ti.projectID
	otherProjectID := uuid.NewString()
	observedAt := time.Date(2026, 6, 29, 16, 0, 0, 0, time.UTC)

	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp?token=secret#frag",
		ServerName: "Speakeasy",
		UserEmail:  "ada@example.com",
		ObservedAt: observedAt,
	})
	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp?other=1",
		ServerName: "Speakeasy MCP",
		UserEmail:  "grace@example.com",
		ObservedAt: observedAt.Add(time.Minute),
	})
	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  otherProjectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp?token=secret#frag",
		ServerName: "Other",
		UserEmail:  "other@example.com",
		ObservedAt: observedAt.Add(time.Hour),
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	result, err := ti.service.BackfillShadowMCPInventoryURLs(ctx, telemetry.BackfillShadowMCPInventoryURLsParams{
		GramProjectID:      projectID,
		Limit:              50,
		HostedMCPHostnames: nil,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.InventoryURLCount)

	// A second run sees the same usage and upserts the same single URL.
	result, err = ti.service.BackfillShadowMCPInventoryURLs(ctx, telemetry.BackfillShadowMCPInventoryURLsParams{
		GramProjectID:      projectID,
		Limit:              50,
		HostedMCPHostnames: nil,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.InventoryURLCount)

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		rows, err := ti.chClient.ListShadowMCPInventoryURLs(ctx, telemetryRepo.ListShadowMCPInventoryURLsParams{
			GramProjectID: projectID,
			Limit:         50,
			Cursor:        "",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.Len(c, rows, 1) {
			return
		}
		assert.Equal(c, "https://mcp.speakeasy.com/mcp", rows[0].CanonicalServerURL)
		assert.Equal(c, "mcp.speakeasy.com", rows[0].URLHost)
		assert.Equal(c, observedAt, rows[0].FirstSeen)
		assert.Equal(c, observedAt.Add(time.Minute), rows[0].LastSeen)
	}, 5*time.Second, 100*time.Millisecond, "shadow mcp inventory should eventually be visible")
}

func TestBackfillShadowMCPInventoryURLs_ExcludesHostedHostnames(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	projectID := ti.projectID
	observedAt := time.Date(2026, 6, 29, 17, 0, 0, 0, time.UTC)
	customDomain := "gram-hosted-" + uuid.NewString()[:8] + ".example.com"

	_, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID:  ti.orgID,
		Domain:          customDomain,
		IngressName:     pgtype.Text{},
		CertSecretName:  pgtype.Text{},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://external.example.com/mcp?token=secret",
		ServerName: "External",
		UserEmail:  "ada@example.com",
		ObservedAt: observedAt,
	})
	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://app.getgram.ai/mcp/hosted",
		ServerName: "Gram Hosted",
		UserEmail:  "grace@example.com",
		ObservedAt: observedAt.Add(time.Minute),
	})
	insertHistoricalShadowMCPCall(t, ctx, ti, historicalShadowMCPCall{
		ProjectID:  projectID,
		ServerURL:  "https://" + customDomain + "/mcp/custom",
		ServerName: "Custom Domain Hosted",
		UserEmail:  "linus@example.com",
		ObservedAt: observedAt.Add(2 * time.Minute),
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	result, err := ti.service.BackfillShadowMCPInventoryURLs(ctx, telemetry.BackfillShadowMCPInventoryURLsParams{
		GramProjectID:      projectID,
		Limit:              50,
		HostedMCPHostnames: []string{"https://app.getgram.ai"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.InventoryURLCount)

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		rows, err := ti.chClient.ListShadowMCPInventoryURLs(ctx, telemetryRepo.ListShadowMCPInventoryURLsParams{
			GramProjectID: projectID,
			Limit:         50,
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.Len(c, rows, 1) {
			return
		}
		assert.Equal(c, "https://external.example.com/mcp", rows[0].CanonicalServerURL)
	}, 5*time.Second, 100*time.Millisecond, "shadow mcp inventory should eventually be visible")
}

type historicalShadowMCPCall struct {
	ProjectID  string
	ServerURL  string
	ServerName string
	UserEmail  string
	ObservedAt time.Time
}

func insertHistoricalShadowMCPCall(t *testing.T, ctx context.Context, ti *testInstance, p historicalShadowMCPCall) {
	t.Helper()

	attrs := map[string]any{
		"gram.event.source":     "hook",
		"gram.hook.source":      "claude-code",
		"gram.mcp.server_url":   p.ServerURL,
		"gram.tool_call.source": p.ServerName,
		"gram.tool.name":        "mcp__speakeasy__search",
		"user.email":            p.UserEmail,
	}
	attrsJSON, err := json.Marshal(attrs)
	require.NoError(t, err)

	spanID := uuid.New().String()[:16]
	traceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	err = ti.chClient.InsertTelemetryLog(ctx, telemetryRepo.InsertTelemetryLogParams{
		ID:                   uuid.NewString(),
		TimeUnixNano:         p.ObservedAt.UnixNano(),
		ObservedTimeUnixNano: p.ObservedAt.UnixNano(),
		SeverityText:         nil,
		Body:                 "historical shadow mcp call",
		TraceID:              &traceID,
		SpanID:               &spanID,
		Attributes:           string(attrsJSON),
		ResourceAttributes:   "{}",
		GramProjectID:        p.ProjectID,
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              "hooks:mcp__speakeasy__search",
		ServiceName:          "gram-hooks",
		ServiceVersion:       nil,
		GramChatID:           nil,
	})
	require.NoError(t, err)
}
func inventoryURLRows(rows []telemetryRepo.ShadowMCPInventoryURLRow) []string {
	urls := make([]string, 0, len(rows))
	for _, row := range rows {
		urls = append(urls, row.CanonicalServerURL)
	}
	return urls
}

func encodeRawInventoryURLCursor(t *testing.T, payload map[string]any) string {
	t.Helper()

	data, err := json.Marshal(payload)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(data)
}
