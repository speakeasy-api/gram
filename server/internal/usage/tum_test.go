package usage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

// newTUMTestService builds a usage service with the dependencies the TUM
// endpoints need: Postgres (billing metadata, projects), ClickHouse
// (telemetry), and an audit logger. It seeds an organization_metadata row so
// billing_metadata foreign keys resolve, plus one project for the org.
func newTUMTestService(t *testing.T, orgID string) (*Service, *pgxpool.Pool, driver.Conn, uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tp := testenv.NewTracerProvider(t)

	db, err := infra.CloneTestDatabase(t, "usage")
	require.NoError(t, err)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	_, err = orgRepo.New(db).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "TUM Test Org",
		Slug:        orgID,
		WorkosID:    conv.ToPGText("workos-" + orgID),
		Whitelisted: conv.PtrToPGBool(nil),
	})
	require.NoError(t, err)

	project, err := projectsRepo.New(db).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name:           "TUM Test Project",
		Slug:           "tum-" + orgID,
		OrganizationID: orgID,
	})
	require.NoError(t, err)
	projectID := project.ID

	authzEngine := authz.NewEngine(logger, db, chConn, rbacDisabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	svc := &Service{
		tracer:        tp.Tracer("test"),
		logger:        logger,
		authz:         authzEngine,
		db:            db,
		repo:          repo.New(db),
		telemetryRepo: telemetryrepo.New(chConn),
		auditLogger:   audit.NewLogger(),
	}

	return svc, db, chConn, projectID
}

func testAdminAuthContext(orgID string) context.Context {
	email := "admin@example.test"
	return contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		AccountType:          "enterprise",
		UserID:               "user-tum-admin",
		Email:                &email,
		IsAdmin:              true,
	})
}

// insertTelemetryRow inserts a raw telemetry_logs row with the given
// attributes. The chat_id materialized column derives from
// attributes["gen_ai.conversation.id"].
func insertTelemetryRow(t *testing.T, conn driver.Conn, projectID string, timestamp time.Time, gramURN string, attributes map[string]any) {
	t.Helper()

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(t.Context(), `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "tum test",
		nil, nil, string(attrsJSON), "{}",
		projectID, gramURN, "gram-test")
	require.NoError(t, err)
}

// insertObservedClaudeRow inserts the Claude Code api_request row the
// provenance-first attribute_metrics_summaries MV admits — observed agent
// traffic, the tokens-under-management population. The token-type split is
// explicit so tests can pin the cache-read exclusion.
func insertObservedClaudeRow(t *testing.T, conn driver.Conn, projectID string, timestamp time.Time, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens int) {
	t.Helper()

	insertTelemetryRow(t, conn, projectID, timestamp, "claude-code:otel:logs", map[string]any{
		"gen_ai.conversation.id": uuid.NewString(),
		"prompt.id":              uuid.NewString(),
		"event.name":             "api_request",
		"input_tokens":           inputTokens,
		"output_tokens":          outputTokens,
		"cache_read_tokens":      cacheReadTokens,
		"cache_creation_tokens":  cacheCreationTokens,
		"model":                  "claude-4.6",
		"gram.hook.source":       "claude-code",
	})
}

// insertObservedAgentUsageRow inserts a Codex/Cursor usage-metrics row — the
// other observed-traffic shape the aggregate admits. The tokens land as
// input, so the TUM measure counts exactly totalTokens.
func insertObservedAgentUsageRow(t *testing.T, conn driver.Conn, projectID string, timestamp time.Time, provider string, totalTokens int) {
	t.Helper()

	insertTelemetryRow(t, conn, projectID, timestamp, provider+":usage:metrics", map[string]any{
		"gen_ai.conversation.id":    uuid.NewString(),
		"gen_ai.usage.input_tokens": totalTokens,
		"gen_ai.usage.total_tokens": totalTokens,
		"gen_ai.response.model":     provider + "-model",
		"gram.hook.source":          provider,
	})
}

// insertRetainedGramAggregateRow seeds attribute_metrics_summaries directly
// with a Gram-hosted completion row, the shape RETAINED from before the
// provenance-first MV cutover stopped admitting Gram completions. The
// tokens-under-management reads must exclude these at read time — that
// exclusion is untestable through the MV (it no longer ingests such rows),
// hence the direct aggregate-state insert.
func insertRetainedGramAggregateRow(t *testing.T, conn driver.Conn, projectID string, timestamp time.Time, hookSource string, tokens int64) {
	t.Helper()

	err := conn.Exec(t.Context(), `
		INSERT INTO attribute_metrics_summaries
		SELECT
			toUUID(?) AS gram_project_id,
			toStartOfHour(fromUnixTimestamp64Nano(?)) AS time_bucket,
			'' AS department_name, '' AS job_title, '' AS employee_type,
			'' AS division_name, '' AS cost_center_name,
			'' AS user_email, 'gram-model' AS model, ? AS hook_source,
			[]::Array(String) AS roles, []::Array(String) AS groups,
			uniqExactIfState(toString('retained-chat'), toUInt8(1)) AS total_chats,
			sumIfState(toInt64(?), toUInt8(1)) AS total_input_tokens,
			sumIfState(toInt64(0), toUInt8(1)) AS total_output_tokens,
			sumIfState(toInt64(?), toUInt8(1)) AS total_tokens,
			sumIfState(toInt64(0), toUInt8(1)) AS cache_read_input_tokens,
			sumIfState(toInt64(0), toUInt8(1)) AS cache_creation_input_tokens,
			sumIfState(toFloat64(0), toUInt8(1)) AS total_cost,
			countIfState(toUInt8(0)) AS total_tool_calls,
			uniqExactIfState(toString(''), toUInt8(0)) AS unique_tool_calls,
			'' AS account_type, '' AS provider, '' AS billing_mode,
			'' AS query_source, '' AS skill_name, '' AS agent_name,
			'' AS mcp_server_name, '' AS mcp_tool_name
	`, projectID, timestamp.UnixNano(), hookSource, tokens, tokens)
	require.NoError(t, err)
}

func TestGetTokensUnderManagementQuery_ExcludesCacheReads(t *testing.T) {
	t.Parallel()

	_, _, chConn, projectID := newTUMTestService(t, "org-tum-query")

	// The read buckets by UTC day, so the window is day-aligned and the
	// out-of-window rows sit several days back.
	now := time.Now().UTC()
	dayStart := now.Truncate(24 * time.Hour)
	windowStart := dayStart.Add(-2 * 24 * time.Hour)
	windowEnd := dayStart.Add(24 * time.Hour)

	// A Claude session whose turn re-read a huge cached prefix: input,
	// output, and the cache WRITE count — the cache READ is excluded.
	insertObservedClaudeRow(t, chConn, projectID.String(), now, 100, 50, 100000, 25)

	// A Codex usage row: observed traffic from the other admitted shape.
	insertObservedAgentUsageRow(t, chConn, projectID.String(), now, "codex", 400)

	// Observed rows outside the window. Excluded.
	insertObservedClaudeRow(t, chConn, projectID.String(), now.Add(-5*24*time.Hour), 9000, 0, 0, 0)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := telemetryrepo.New(chConn).GetTokensUnderManagementByDay(t.Context(), telemetryrepo.GetTokensUnderManagementParams{
			ProjectIDs:          []string{projectID.String()},
			StartUnixNano:       windowStart.UnixNano(),
			EndUnixNano:         windowEnd.UnixNano(),
			ExcludedHookSources: nil,
		})
		if !assert.NoError(c, err) {
			return
		}
		assert.Equal(c, int64(575), sumTumBuckets(res), "input + output + cache writes — never cache reads")
		if !assert.Len(c, res, 1, "all in-window rows share one day bucket") {
			return
		}
		assert.Equal(c, dayStart, res[0].Day.UTC())
	}, 10*time.Second, 200*time.Millisecond)
}

func sumTumBuckets(buckets []telemetryrepo.TumDayBucket) int64 {
	var total int64
	for _, b := range buckets {
		total += b.Tokens
	}
	return total
}

func TestGetTokensUnderManagementQuery_DailyBreakdown(t *testing.T) {
	t.Parallel()

	_, _, chConn, projectID := newTUMTestService(t, "org-tum-daily")

	now := time.Now().UTC()
	dayStart := now.Truncate(24 * time.Hour)
	windowStart := dayStart.Add(-2 * 24 * time.Hour)
	windowEnd := dayStart.Add(24 * time.Hour)

	// Observed rows land in their own day buckets.
	insertObservedClaudeRow(t, chConn, projectID.String(), now, 200, 100, 0, 0)
	insertObservedClaudeRow(t, chConn, projectID.String(), now.Add(-24*time.Hour), 200, 0, 0, 0)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := telemetryrepo.New(chConn).GetTokensUnderManagementByDay(t.Context(), telemetryrepo.GetTokensUnderManagementParams{
			ProjectIDs:          []string{projectID.String()},
			StartUnixNano:       windowStart.UnixNano(),
			EndUnixNano:         windowEnd.UnixNano(),
			ExcludedHookSources: nil,
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.Len(c, res, 2) {
			return
		}
		assert.Equal(c, dayStart.Add(-24*time.Hour), res[0].Day.UTC())
		assert.Equal(c, int64(200), res[0].Tokens)
		assert.Equal(c, dayStart, res[1].Day.UTC())
		assert.Equal(c, int64(300), res[1].Tokens)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestGetTokensUnderManagementQuery_NoProjects(t *testing.T) {
	t.Parallel()

	_, _, chConn, _ := newTUMTestService(t, "org-tum-no-projects")

	buckets, err := telemetryrepo.New(chConn).GetTokensUnderManagementByDay(t.Context(), telemetryrepo.GetTokensUnderManagementParams{
		ProjectIDs:          nil,
		StartUnixNano:       time.Now().Add(-1 * time.Hour).UnixNano(),
		EndUnixNano:         time.Now().UnixNano(),
		ExcludedHookSources: nil,
	})
	require.NoError(t, err)
	require.Empty(t, buckets)
}

func TestGetTokensUnderManagement_Unconfigured(t *testing.T) {
	t.Parallel()

	orgID := "org-tum-unconfigured"
	svc, _, _, _ := newTUMTestService(t, orgID)

	result, err := svc.GetTokensUnderManagement(testAuthContext(orgID), &gen.GetTokensUnderManagementPayload{})
	require.NoError(t, err)

	require.Equal(t, int64(0), result.Tokens)
	require.Nil(t, result.MonthlyTokenLimit)
	require.Nil(t, result.AlertEmail)
	require.Equal(t, 1, result.BillingCycleAnchorDay)

	require.Len(t, result.History, 12)
	last := result.History[len(result.History)-1]
	require.Equal(t, result.PeriodStart, last.PeriodStart, "last history entry is the active cycle")
	require.Equal(t, result.PeriodEnd, last.PeriodEnd)

	start, err := time.Parse(time.RFC3339, result.PeriodStart)
	require.NoError(t, err)
	end, err := time.Parse(time.RFC3339, result.PeriodEnd)
	require.NoError(t, err)
	now := time.Now().UTC()
	require.False(t, start.After(now), "cycle start should not be in the future")
	require.True(t, end.After(now), "cycle end should be in the future")
}

func TestGetTokensUnderManagement_CountsObservedTraffic(t *testing.T) {
	t.Parallel()

	orgID := "org-tum-counts"
	svc, _, chConn, projectID := newTUMTestService(t, orgID)

	// Timestamps sit at "now" so they always land inside the active cycle.
	now := time.Now().UTC()

	// Observed agent traffic: counted. The retained Gram-hosted rows (a
	// playground completion and pre-tagging '' history) must be excluded by
	// the service's exclusion list.
	insertObservedClaudeRow(t, chConn, projectID.String(), now, 450, 0, 0, 0)
	insertRetainedGramAggregateRow(t, chConn, projectID.String(), now, "playground", 9000)
	insertRetainedGramAggregateRow(t, chConn, projectID.String(), now, "", 7000)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := svc.GetTokensUnderManagement(testAuthContext(orgID), &gen.GetTokensUnderManagementPayload{})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Equal(c, int64(450), res.Tokens)

		// The active cycle's daily points sum to the headline number.
		last := res.History[len(res.History)-1]
		assert.Equal(c, int64(450), last.Tokens)
		var daySum int64
		for _, day := range last.Days {
			daySum += day.Tokens
		}
		assert.Equal(c, int64(450), daySum)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestSetBillingMetadata_RequiresPlatformAdmin(t *testing.T) {
	t.Parallel()

	orgID := "org-tum-non-admin"
	svc, _, _, _ := newTUMTestService(t, orgID)

	_, err := svc.SetBillingMetadata(testAuthContext(orgID), &gen.SetBillingMetadataPayload{
		BillingCycleAnchorDay: 1,
	})

	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestSetBillingMetadata_UpsertAndAudit(t *testing.T) {
	t.Parallel()

	orgID := "org-tum-upsert"
	svc, db, _, _ := newTUMTestService(t, orgID)
	ctx := testAdminAuthContext(orgID)

	baseline, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionBillingMetadataUpdate)
	require.NoError(t, err)

	limit := int64(1_000_000)
	tunnelLimit := 7
	email := "billing-alerts@example.test"
	result, err := svc.SetBillingMetadata(ctx, &gen.SetBillingMetadataPayload{
		MonthlyTokenLimit:      &limit,
		TunneledMcpServerLimit: &tunnelLimit,
		AlertEmail:             &email,
		BillingCycleAnchorDay:  5,
	})
	require.NoError(t, err)
	require.NotNil(t, result.MonthlyTokenLimit)
	require.Equal(t, int64(1_000_000), *result.MonthlyTokenLimit)
	require.NotNil(t, result.TunneledMcpServerLimit)
	require.Equal(t, 7, *result.TunneledMcpServerLimit)
	require.NotNil(t, result.AlertEmail)
	require.Equal(t, email, *result.AlertEmail)
	require.Equal(t, 5, result.BillingCycleAnchorDay)

	// Updating again overwrites the contract rather than duplicating it. An
	// omitted tunneled_mcp_server_limit preserves the configured cap — callers
	// that predate the field (dashboard TUM form, older SDKs) must not
	// silently clear it when editing unrelated billing settings.
	newLimit := int64(2_000_000)
	result, err = svc.SetBillingMetadata(ctx, &gen.SetBillingMetadataPayload{
		MonthlyTokenLimit:     &newLimit,
		AlertEmail:            nil,
		BillingCycleAnchorDay: 12,
	})
	require.NoError(t, err)
	require.NotNil(t, result.MonthlyTokenLimit)
	require.Equal(t, int64(2_000_000), *result.MonthlyTokenLimit)
	require.NotNil(t, result.TunneledMcpServerLimit)
	require.Equal(t, 7, *result.TunneledMcpServerLimit)
	require.Nil(t, result.AlertEmail)
	require.Equal(t, 12, result.BillingCycleAnchorDay)

	// An explicit value still overwrites the preserved cap.
	updatedTunnelLimit := 25
	result, err = svc.SetBillingMetadata(ctx, &gen.SetBillingMetadataPayload{
		MonthlyTokenLimit:      &newLimit,
		TunneledMcpServerLimit: &updatedTunnelLimit,
		AlertEmail:             nil,
		BillingCycleAnchorDay:  12,
	})
	require.NoError(t, err)
	require.NotNil(t, result.TunneledMcpServerLimit)
	require.Equal(t, 25, *result.TunneledMcpServerLimit)

	after, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionBillingMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, baseline+3, after)

	record, err := audittest.LatestAuditLogByAction(t.Context(), db, audit.ActionBillingMetadataUpdate)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.InDelta(t, float64(2_000_000), beforeSnapshot["tum_monthly_token_limit"], 0)
	require.InDelta(t, float64(7), beforeSnapshot["tunneled_mcp_server_limit"], 0)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.InDelta(t, float64(2_000_000), afterSnapshot["tum_monthly_token_limit"], 0)
	require.InDelta(t, float64(25), afterSnapshot["tunneled_mcp_server_limit"], 0)
}

func TestSetBillingMetadata_RejectsOversizedTunneledMcpServerLimit(t *testing.T) {
	t.Parallel()

	orgID := "org-tum-oversized-tunnel-limit"
	svc, _, _, _ := newTUMTestService(t, orgID)
	tooLarge := maxTunneledMcpServerLimit + 1

	_, err := svc.SetBillingMetadata(testAdminAuthContext(orgID), &gen.SetBillingMetadataPayload{
		TunneledMcpServerLimit: &tooLarge,
		BillingCycleAnchorDay:  1,
	})

	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}

func TestGetTokensUnderManagement_AlertEmailAdminOnly(t *testing.T) {
	t.Parallel()

	orgID := "org-tum-email-visibility"
	svc, _, _, _ := newTUMTestService(t, orgID)

	limit := int64(500)
	email := "billing-alerts@example.test"
	_, err := svc.SetBillingMetadata(testAdminAuthContext(orgID), &gen.SetBillingMetadataPayload{
		MonthlyTokenLimit:     &limit,
		AlertEmail:            &email,
		BillingCycleAnchorDay: 1,
	})
	require.NoError(t, err)

	// Regular org members see the limit but not the internal alert email.
	result, err := svc.GetTokensUnderManagement(testAuthContext(orgID), &gen.GetTokensUnderManagementPayload{})
	require.NoError(t, err)
	require.NotNil(t, result.MonthlyTokenLimit)
	require.Equal(t, int64(500), *result.MonthlyTokenLimit)
	require.Nil(t, result.AlertEmail)

	// Platform admins see it.
	result, err = svc.GetTokensUnderManagement(testAdminAuthContext(orgID), &gen.GetTokensUnderManagementPayload{})
	require.NoError(t, err)
	require.NotNil(t, result.AlertEmail)
	require.Equal(t, email, *result.AlertEmail)
}

func TestGetTokensUnderManagementQuery_ExcludesGramHostedSources(t *testing.T) {
	t.Parallel()

	_, _, chConn, projectID := newTUMTestService(t, "org-tum-billed-sources")

	now := time.Now().UTC()
	dayStart := now.Truncate(24 * time.Hour)
	windowStart := dayStart.Add(-2 * 24 * time.Hour)
	windowEnd := dayStart.Add(24 * time.Hour)

	// Observed agent traffic: counted.
	insertObservedClaudeRow(t, chConn, projectID.String(), now, 100, 0, 0, 0)
	insertObservedAgentUsageRow(t, chConn, projectID.String(), now, "cursor", 40)

	// Gram-hosted completion rows retained from before the provenance-first
	// cutover: a user-facing surface, the platform's scanning inference, and
	// a pre-tagging '' row. All excluded — Gram-spent inference is never
	// tokens under management.
	insertRetainedGramAggregateRow(t, chConn, projectID.String(), now, "playground", 5000)
	insertRetainedGramAggregateRow(t, chConn, projectID.String(), now, "risk-analysis", 300)
	insertRetainedGramAggregateRow(t, chConn, projectID.String(), now, "", 60)

	// Confirm everything materialized (an unscoped read sees all five rows),
	// THEN assert the scoped read excludes exactly the Gram-hosted tokens —
	// the exclusion cannot pass vacuously against a half-ingested view.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := telemetryrepo.New(chConn).GetTokensUnderManagementByDay(t.Context(), telemetryrepo.GetTokensUnderManagementParams{
			ProjectIDs:          []string{projectID.String()},
			StartUnixNano:       windowStart.UnixNano(),
			EndUnixNano:         windowEnd.UnixNano(),
			ExcludedHookSources: nil,
		})
		if !assert.NoError(c, err) {
			return
		}
		assert.Equal(c, int64(5500), sumTumBuckets(res))
	}, 10*time.Second, 200*time.Millisecond)

	buckets, err := telemetryrepo.New(chConn).GetTokensUnderManagementByDay(t.Context(), telemetryrepo.GetTokensUnderManagementParams{
		ProjectIDs:          []string{projectID.String()},
		StartUnixNano:       windowStart.UnixNano(),
		EndUnixNano:         windowEnd.UnixNano(),
		ExcludedHookSources: billing.GramHostedHookSourceStrings(),
	})
	require.NoError(t, err)
	require.Equal(t, int64(140), sumTumBuckets(buckets),
		"the billed sum counts only observed agent traffic — never Gram-hosted surfaces, scanning inference, or untagged Gram-era rows")
}
