package telemetry_test

import (
	"context"
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

	rows := requireShadowMCPInventoryURLsEventually(ctx, t, ti, telemetryRepo.ListShadowMCPInventoryURLsParams{
		GramProjectID: projectID,
		Limit:         50,
	}, 1)

	require.Len(t, rows, 1)
	assert.Equal(t, "https://mcp.speakeasy.com/mcp", rows[0].CanonicalServerURL)
	assert.Equal(t, "mcp.speakeasy.com", rows[0].URLHost)
	assert.Equal(t, "Speakeasy", rows[0].ServerName)
	assert.Equal(t, firstSeen, rows[0].FirstSeen)
	assert.Equal(t, lastSeen, rows[0].LastSeen)
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

	// All three calls collapse into one canonical URL, so waiting on the row
	// count alone would pass as soon as the first async insert flushes. Wait
	// for the full call count so the assertions below see complete data.
	var usage []telemetryRepo.ShadowMCPInventoryUsageRow
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var err error
		usage, err = ti.chClient.ListShadowMCPInventoryUsage(ctx, telemetryRepo.ListShadowMCPInventoryUsageParams{
			GramProjectID: projectID,
			Limit:         50,
		})
		require.NoError(c, err)
		require.Len(c, usage, 1)
		require.EqualValues(c, 3, usage[0].CallCount)
	}, 5*time.Second, 100*time.Millisecond)

	assert.Equal(t, "https://mcp.speakeasy.com/mcp", usage[0].CanonicalServerURL)
	require.NotNil(t, usage[0].LastCalled)
	assert.Equal(t, base.Add(2*time.Minute), *usage[0].LastCalled)
	assert.EqualValues(t, 2, usage[0].UserCount)
	assert.Equal(t, []string{"ada@example.com", "grace@example.com"}, usage[0].TopUsers)
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

	users := requireShadowMCPInventoryUsersEventually(ctx, t, ti, telemetryRepo.ListShadowMCPInventoryUsersParams{
		GramProjectID:      projectID,
		CanonicalServerURL: "https://mcp.speakeasy.com/mcp",
		Limit:              50,
	}, 2)

	require.Len(t, users, 2)
	assert.Equal(t, "ada@example.com", users[0].UserKey)
	assert.EqualValues(t, 2, users[0].CallCount)
	assert.Equal(t, base.Add(time.Minute), users[0].LastCalled)
	assert.Equal(t, "grace@example.com", users[1].UserKey)
	assert.EqualValues(t, 1, users[1].CallCount)
	assert.Equal(t, base.Add(2*time.Minute), users[1].LastCalled)
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

	rows := requireShadowMCPInventoryURLsEventually(ctx, t, ti, telemetryRepo.ListShadowMCPInventoryURLsParams{
		GramProjectID: projectID,
		Limit:         50,
	}, 1)

	require.Len(t, rows, 1)
	assert.Equal(t, "https://mcp.speakeasy.com/mcp", rows[0].CanonicalServerURL)
	assert.Equal(t, "Speakeasy", rows[0].ServerName)
	assert.Equal(t, seenAt, rows[0].LastSeen)
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

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		result, err := ti.service.BackfillShadowMCPInventoryURLs(ctx, telemetry.BackfillShadowMCPInventoryURLsParams{
			GramProjectID:      projectID,
			Limit:              50,
			HostedMCPHostnames: nil,
		})
		require.NoError(c, err)
		require.Equal(c, 1, result.InventoryURLCount)
	}, 5*time.Second, 100*time.Millisecond)

	result, err := ti.service.BackfillShadowMCPInventoryURLs(ctx, telemetry.BackfillShadowMCPInventoryURLsParams{
		GramProjectID:      projectID,
		Limit:              50,
		HostedMCPHostnames: nil,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.InventoryURLCount)

	rows := requireShadowMCPInventoryURLsEventually(ctx, t, ti, telemetryRepo.ListShadowMCPInventoryURLsParams{
		GramProjectID: projectID,
		Limit:         50,
	}, 1)

	require.Len(t, rows, 1)
	assert.Equal(t, "https://mcp.speakeasy.com/mcp", rows[0].CanonicalServerURL)
	assert.Equal(t, "mcp.speakeasy.com", rows[0].URLHost)
	assert.Equal(t, observedAt, rows[0].FirstSeen)
	assert.Equal(t, observedAt.Add(time.Minute), rows[0].LastSeen)
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

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		result, err := ti.service.BackfillShadowMCPInventoryURLs(ctx, telemetry.BackfillShadowMCPInventoryURLsParams{
			GramProjectID:      projectID,
			Limit:              50,
			HostedMCPHostnames: []string{"https://app.getgram.ai"},
		})
		require.NoError(c, err)
		require.Equal(c, 1, result.InventoryURLCount)
	}, 5*time.Second, 100*time.Millisecond)

	rows := requireShadowMCPInventoryURLsEventually(ctx, t, ti, telemetryRepo.ListShadowMCPInventoryURLsParams{
		GramProjectID: projectID,
		Limit:         50,
	}, 1)

	require.Len(t, rows, 1)
	assert.Equal(t, "https://external.example.com/mcp", rows[0].CanonicalServerURL)
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

func requireShadowMCPInventoryURLsEventually(
	ctx context.Context,
	t *testing.T,
	ti *testInstance,
	params telemetryRepo.ListShadowMCPInventoryURLsParams,
	wantLen int,
) []telemetryRepo.ShadowMCPInventoryURLRow {
	t.Helper()

	var rows []telemetryRepo.ShadowMCPInventoryURLRow
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var err error
		rows, err = ti.chClient.ListShadowMCPInventoryURLs(ctx, params)
		require.NoError(c, err)
		require.Len(c, rows, wantLen)
	}, 5*time.Second, 100*time.Millisecond)

	return rows
}

func requireShadowMCPInventoryUsersEventually(
	ctx context.Context,
	t *testing.T,
	ti *testInstance,
	params telemetryRepo.ListShadowMCPInventoryUsersParams,
	wantLen int,
) []telemetryRepo.ShadowMCPInventoryUserRow {
	t.Helper()

	var rows []telemetryRepo.ShadowMCPInventoryUserRow
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var err error
		rows, err = ti.chClient.ListShadowMCPInventoryUsers(ctx, params)
		require.NoError(c, err)
		require.Len(c, rows, wantLen)
	}, 5*time.Second, 100*time.Millisecond)

	return rows
}
