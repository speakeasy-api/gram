package usage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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
		"gen_ai.conversation.id":     chatID,
		"gram.tool.urn":              "tools:http:petstore:listPets",
		"http.response.status_code":  200,
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

	now := time.Now().UTC()
	storedToolChat := uuid.New().String()
	storedChatEventChat := uuid.New().String()
	forwardedOnlyChat := uuid.New().String()

	// Stored session: token rows plus a tool-call row. Counted.
	insertTokenUsageRow(t, chConn, projectID.String(), now.Add(-10*time.Minute), storedToolChat, 100)
	insertTokenUsageRow(t, chConn, projectID.String(), now.Add(-9*time.Minute), storedToolChat, 200)
	insertToolCallRow(t, chConn, projectID.String(), now.Add(-9*time.Minute), storedToolChat)

	// Stored session: token row plus a chat event row without usage. Counted.
	insertTokenUsageRow(t, chConn, projectID.String(), now.Add(-8*time.Minute), storedChatEventChat, 1000)
	insertChatEventRow(t, chConn, projectID.String(), now.Add(-8*time.Minute), storedChatEventChat)

	// OTEL-forwarded-only session: token rows with no stored chat or tool
	// call data. Excluded.
	insertTokenUsageRow(t, chConn, projectID.String(), now.Add(-7*time.Minute), forwardedOnlyChat, 5000)

	// Token row with no chat id at all. Excluded.
	insertTokenUsageRow(t, chConn, projectID.String(), now.Add(-6*time.Minute), "", 777)

	// Stored session rows outside the window. Excluded.
	insertTokenUsageRow(t, chConn, projectID.String(), now.Add(-3*time.Hour), storedToolChat, 9000)

	// Wait for ClickHouse eventual consistency.
	time.Sleep(200 * time.Millisecond)

	tokens, err := telemetryrepo.New(chConn).GetTokensUnderManagement(t.Context(), telemetryrepo.GetTokensUnderManagementParams{
		ProjectIDs:    []string{projectID.String()},
		StartUnixNano: now.Add(-1 * time.Hour).UnixNano(),
		EndUnixNano:   now.UnixNano(),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1300), tokens, "should count only sessions with stored non-metrics data inside the window")
}

func TestGetTokensUnderManagementQuery_NoProjects(t *testing.T) {
	t.Parallel()

	_, _, chConn, _ := newTUMTestService(t, "org-tum-no-projects")

	tokens, err := telemetryrepo.New(chConn).GetTokensUnderManagement(t.Context(), telemetryrepo.GetTokensUnderManagementParams{
		ProjectIDs:    nil,
		StartUnixNano: time.Now().Add(-1 * time.Hour).UnixNano(),
		EndUnixNano:   time.Now().UnixNano(),
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), tokens)
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

	time.Sleep(200 * time.Millisecond)

	result, err := svc.GetTokensUnderManagement(testAuthContext(orgID), &gen.GetTokensUnderManagementPayload{})
	require.NoError(t, err)
	require.Equal(t, int64(450), result.Tokens)
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
	email := "billing-alerts@example.test"
	result, err := svc.SetBillingMetadata(ctx, &gen.SetBillingMetadataPayload{
		MonthlyTokenLimit:     &limit,
		AlertEmail:            &email,
		BillingCycleAnchorDay: 5,
	})
	require.NoError(t, err)
	require.NotNil(t, result.MonthlyTokenLimit)
	require.Equal(t, int64(1_000_000), *result.MonthlyTokenLimit)
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
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.InDelta(t, float64(2_000_000), afterSnapshot["tum_monthly_token_limit"], 0)
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
