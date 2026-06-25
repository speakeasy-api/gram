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

// insertTokenUsageRow inserts a token-usage (metrics) row, the kind produced
// by OTEL forwarding. An empty chatID omits the conversation id attribute.
func insertTokenUsageRow(t *testing.T, conn driver.Conn, projectID string, timestamp time.Time, chatID string, totalTokens int) {
	t.Helper()

	attributes := map[string]any{
		"gen_ai.usage.input_tokens":  totalTokens / 2,
		"gen_ai.usage.output_tokens": totalTokens - totalTokens/2,
		"gen_ai.usage.total_tokens":  totalTokens,
		"gram.resource.urn":          "agents:chat:completion",
	}
	if chatID != "" {
		attributes["gen_ai.conversation.id"] = chatID
	}

	insertTelemetryRow(t, conn, projectID, timestamp, "agents:chat:completion", attributes)
}

// insertToolCallRow inserts a tool-call row tied to a chat — non-metrics
// evidence that Gram stored the session.
func insertToolCallRow(t *testing.T, conn driver.Conn, projectID string, timestamp time.Time, chatID string) {
	t.Helper()

	insertTelemetryRow(t, conn, projectID, timestamp, "tools:http:petstore:listPets", map[string]any{
		"gen_ai.conversation.id":    chatID,
		"gram.tool.urn":             "tools:http:petstore:listPets",
		"http.response.status_code": 200,
	})
}

// insertChatEventRow inserts a chat event row without token-usage attributes
// — non-metrics evidence that Gram stored the session.
func insertChatEventRow(t *testing.T, conn driver.Conn, projectID string, timestamp time.Time, chatID string) {
	t.Helper()

	insertTelemetryRow(t, conn, projectID, timestamp, "agents:chat:message", map[string]any{
		"gen_ai.conversation.id": chatID,
	})
}

func TestGetTokensUnderManagementQuery_FiltersUnstoredSessions(t *testing.T) {
	t.Parallel()

	_, _, chConn, projectID := newTUMTestService(t, "org-tum-query")

	// The summary table buckets by UTC day, so the window is day-aligned and
	// the out-of-window rows sit several days back.
	now := time.Now().UTC()
	dayStart := now.Truncate(24 * time.Hour)
	windowStart := dayStart.Add(-2 * 24 * time.Hour)
	windowEnd := dayStart.Add(24 * time.Hour)

	storedToolChat := uuid.New().String()
	storedChatEventChat := uuid.New().String()
	forwardedOnlyChat := uuid.New().String()

	// Stored session: token rows plus a tool-call row. Counted.
	insertTokenUsageRow(t, chConn, projectID.String(), now, storedToolChat, 100)
	insertTokenUsageRow(t, chConn, projectID.String(), now, storedToolChat, 200)
	insertToolCallRow(t, chConn, projectID.String(), now, storedToolChat)

	// Stored session: token row plus a chat event row without usage. Counted.
	insertTokenUsageRow(t, chConn, projectID.String(), now, storedChatEventChat, 1000)
	insertChatEventRow(t, chConn, projectID.String(), now, storedChatEventChat)

	// OTEL-forwarded-only session: token rows with no stored chat or tool
	// call data. Excluded.
	insertTokenUsageRow(t, chConn, projectID.String(), now, forwardedOnlyChat, 5000)

	// Token row with no chat id at all. Excluded.
	insertTokenUsageRow(t, chConn, projectID.String(), now, "", 777)

	// Stored session rows outside the window. Excluded.
	insertTokenUsageRow(t, chConn, projectID.String(), now.Add(-5*24*time.Hour), storedToolChat, 9000)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := telemetryrepo.New(chConn).GetTokensUnderManagementByDay(t.Context(), telemetryrepo.GetTokensUnderManagementParams{
			ProjectIDs:    []string{projectID.String()},
			StartUnixNano: windowStart.UnixNano(),
			EndUnixNano:   windowEnd.UnixNano(),
		})
		if !assert.NoError(c, err) {
			return
		}
		assert.Equal(c, int64(1300), sumTumBuckets(res), "should count only sessions with stored non-metrics data inside the window")
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

	chatID := uuid.New().String()

	// Evidence on one day qualifies the chat for the whole window; token rows
	// land in their own day buckets.
	insertToolCallRow(t, chConn, projectID.String(), now, chatID)
	insertTokenUsageRow(t, chConn, projectID.String(), now, chatID, 300)
	insertTokenUsageRow(t, chConn, projectID.String(), now.Add(-24*time.Hour), chatID, 200)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := telemetryrepo.New(chConn).GetTokensUnderManagementByDay(t.Context(), telemetryrepo.GetTokensUnderManagementParams{
			ProjectIDs:    []string{projectID.String()},
			StartUnixNano: windowStart.UnixNano(),
			EndUnixNano:   windowEnd.UnixNano(),
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

func TestGetTokensUnderManagementQuery_EvidenceOutsideWindowDoesNotCount(t *testing.T) {
	t.Parallel()

	_, _, chConn, projectID := newTUMTestService(t, "org-tum-stale-evidence")

	now := time.Now().UTC()
	dayStart := now.Truncate(24 * time.Hour)
	windowStart := dayStart.Add(-2 * 24 * time.Hour)
	windowEnd := dayStart.Add(24 * time.Hour)

	chatID := uuid.New().String()

	// Token usage inside the window, but the only stored evidence for the
	// chat predates the window — the session does not count this cycle.
	insertTokenUsageRow(t, chConn, projectID.String(), now, chatID, 4000)
	insertToolCallRow(t, chConn, projectID.String(), now.Add(-5*24*time.Hour), chatID)

	// Confirm the inserted rows have propagated using a wider window that
	// includes the stale evidence, which qualifies the session and surfaces
	// the in-window tokens. Once visible there, the narrow window below is a
	// reliable negative.
	widerStart := dayStart.Add(-7 * 24 * time.Hour)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := telemetryrepo.New(chConn).GetTokensUnderManagementByDay(t.Context(), telemetryrepo.GetTokensUnderManagementParams{
			ProjectIDs:    []string{projectID.String()},
			StartUnixNano: widerStart.UnixNano(),
			EndUnixNano:   windowEnd.UnixNano(),
		})
		if !assert.NoError(c, err) {
			return
		}
		assert.Equal(c, int64(4000), sumTumBuckets(res))
	}, 10*time.Second, 200*time.Millisecond)

	buckets, err := telemetryrepo.New(chConn).GetTokensUnderManagementByDay(t.Context(), telemetryrepo.GetTokensUnderManagementParams{
		ProjectIDs:    []string{projectID.String()},
		StartUnixNano: windowStart.UnixNano(),
		EndUnixNano:   windowEnd.UnixNano(),
	})
	require.NoError(t, err)
	require.Empty(t, buckets)
}

func TestGetTokensUnderManagementQuery_NoProjects(t *testing.T) {
	t.Parallel()

	_, _, chConn, _ := newTUMTestService(t, "org-tum-no-projects")

	buckets, err := telemetryrepo.New(chConn).GetTokensUnderManagementByDay(t.Context(), telemetryrepo.GetTokensUnderManagementParams{
		ProjectIDs:    nil,
		StartUnixNano: time.Now().Add(-1 * time.Hour).UnixNano(),
		EndUnixNano:   time.Now().UnixNano(),
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

func TestGetTokensUnderManagement_CountsStoredSessions(t *testing.T) {
	t.Parallel()

	orgID := "org-tum-counts"
	svc, _, chConn, projectID := newTUMTestService(t, orgID)

	now := time.Now().UTC()
	storedChat := uuid.New().String()
	forwardedChat := uuid.New().String()

	// Timestamps sit at "now" so they always land inside the active cycle.
	insertTokenUsageRow(t, chConn, projectID.String(), now, storedChat, 450)
	insertToolCallRow(t, chConn, projectID.String(), now, storedChat)
	insertTokenUsageRow(t, chConn, projectID.String(), now, forwardedChat, 9000)

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
		MonthlyTokenLimit:       &limit,
		TunnelledMcpServerLimit: &tunnelLimit,
		AlertEmail:              &email,
		BillingCycleAnchorDay:   5,
	})
	require.NoError(t, err)
	require.NotNil(t, result.MonthlyTokenLimit)
	require.Equal(t, int64(1_000_000), *result.MonthlyTokenLimit)
	require.NotNil(t, result.TunnelledMcpServerLimit)
	require.Equal(t, 7, *result.TunnelledMcpServerLimit)
	require.NotNil(t, result.AlertEmail)
	require.Equal(t, email, *result.AlertEmail)
	require.Equal(t, 5, result.BillingCycleAnchorDay)

	// Updating again overwrites the contract rather than duplicating it.
	newLimit := int64(2_000_000)
	result, err = svc.SetBillingMetadata(ctx, &gen.SetBillingMetadataPayload{
		MonthlyTokenLimit:     &newLimit,
		AlertEmail:            nil,
		BillingCycleAnchorDay: 12,
	})
	require.NoError(t, err)
	require.NotNil(t, result.MonthlyTokenLimit)
	require.Equal(t, int64(2_000_000), *result.MonthlyTokenLimit)
	require.Nil(t, result.TunnelledMcpServerLimit)
	require.Nil(t, result.AlertEmail)
	require.Equal(t, 12, result.BillingCycleAnchorDay)

	after, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionBillingMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, baseline+2, after)

	record, err := audittest.LatestAuditLogByAction(t.Context(), db, audit.ActionBillingMetadataUpdate)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.InDelta(t, float64(1_000_000), beforeSnapshot["tum_monthly_token_limit"], 0)
	require.InDelta(t, float64(7), beforeSnapshot["tunnelled_mcp_server_limit"], 0)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.InDelta(t, float64(2_000_000), afterSnapshot["tum_monthly_token_limit"], 0)
	require.Nil(t, afterSnapshot["tunnelled_mcp_server_limit"])
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
