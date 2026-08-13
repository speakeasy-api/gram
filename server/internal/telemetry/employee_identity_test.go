package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksRepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

// The employee views pass one identifier, but ingest attributes a person's rows
// two different ways: hook events carry a resolved gram user id, while the rows
// that carry tokens and cost (Claude/Codex OTEL and the usage imports) carry
// only the provider account's email. These tests pin the fold — DNO-827, where
// the employee page showed sessions and tool calls with no tokens or cost.

func TestGetUserMetricsSummary_FoldsEmailOnlyUsageIntoEmployee(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	employeeID, employeeEmail := seedConnectedOrgUser(t, ctx, ti, "employee")
	// A personal AI account signs in with its own email, so its usage rows only
	// reach the employee through the user_accounts directory.
	personalEmail := "personal-" + uuid.New().String() + "@example.com"
	linkUserAccount(t, ctx, ti, employeeID, personalEmail, "personal")

	strangerEmail := "stranger-" + uuid.New().String() + "@example.com"

	now := time.Now().UTC()

	// Attributed by gram user id, carrying a tool call but no token usage.
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), "tools:http:petstore:listPets", 200, 0.5, employeeID, "")

	// Usage-import shaped rows: tokens and cost, attributed by email only.
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), employeeEmail, 100, 50, 2.5)
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), personalEmail, 400, 200, 7.5)

	// Someone else's usage must stay out of this employee's totals.
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-7*time.Minute), strangerEmail, 9000, 9000, 99)

	// Telemetry writes use ClickHouse async inserts; drain the queue synchronously
	// so the rows are deterministically visible (no polling — see the telemetry
	// README).
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	m := userMetrics(t, ctx, ti, employeeID)

	// Directory email row (100/50) + personal account row (400/200).
	require.Equal(t, int64(500), m.TotalInputTokens)
	require.Equal(t, int64(250), m.TotalOutputTokens)
	require.Equal(t, int64(750), m.TotalTokens)
	require.Equal(t, int64(2), m.TotalChatRequests)

	// Cost is what the page's tile reads, and it only reached the response once
	// this query started selecting it.
	require.InDelta(t, 10.0, m.TotalCost, 0.001)

	// The id-carrying row still aggregates alongside them.
	require.Equal(t, int64(1), m.TotalToolCalls)
}

// An email identifier that resolves to a directory user must still pick up the
// rows carrying that user's gram id, which is the mirror of the fold above.
func TestGetUserMetricsSummary_EmailIdentifierFoldsIDCarryingRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	employeeID, employeeEmail := seedConnectedOrgUser(t, ctx, ti, "employee")

	now := time.Now().UTC()
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), "tools:http:petstore:listPets", 200, 0.5, employeeID, "")
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), employeeEmail, 100, 50, 2.5)

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	m := userMetrics(t, ctx, ti, employeeEmail)

	require.Equal(t, int64(100), m.TotalInputTokens)
	require.Equal(t, int64(1), m.TotalToolCalls)
}

// The employee page falls back to an email identifier for someone who has usage
// but no directory row to resolve. Nothing resolves, so the identity is the
// email alone, and their usage must still aggregate.
func TestGetUserMetricsSummary_EmailIdentifierWithNoDirectoryRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	// Deliberately never seeded into the org directory.
	unknownEmail := "contractor-" + uuid.New().String() + "@example.com"

	now := time.Now().UTC()
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), unknownEmail, 100, 50, 2.5)

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	m := userMetrics(t, ctx, ti, unknownEmail)

	require.Equal(t, int64(100), m.TotalInputTokens)
	require.InDelta(t, 2.5, m.TotalCost, 0.001)
}

// A row pairing this employee's email with somebody else's gram user id belongs
// to that other person: attribution follows user_id whenever a row carries one,
// so widening the filter to an identity set must not double count it (DNO-509).
func TestGetUserMetricsSummary_IgnoresEmailRowsOwnedByAnotherUser(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	employeeID, employeeEmail := seedConnectedOrgUser(t, ctx, ti, "employee")
	otherID, _ := seedConnectedOrgUser(t, ctx, ti, "other")

	now := time.Now().UTC()
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), employeeEmail, 100, 50, 2.5)
	// Same email, but the row already names a different owner.
	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), otherID, employeeEmail, 700, 300, 42)

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	m := userMetrics(t, ctx, ti, employeeID)

	require.Equal(t, int64(100), m.TotalInputTokens)
	require.InDelta(t, 2.5, m.TotalCost, 0.001)
}

// userMetrics fetches one employee's metrics summary over a window wide enough
// to cover everything these tests insert.
func userMetrics(t *testing.T, ctx context.Context, ti *testInstance, identifier string) *gen.ProjectSummary {
	t.Helper()

	now := time.Now().UTC()
	res, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
		From:   now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:     now.Add(1 * time.Hour).Format(time.RFC3339),
		UserID: &identifier,
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, res.Metrics)

	return res.Metrics
}

func linkUserAccount(t *testing.T, ctx context.Context, ti *testInstance, userID, email, accountType string) {
	t.Helper()

	_, err := hooksRepo.New(ti.conn).UpsertUserAccount(ctx, hooksRepo.UpsertUserAccountParams{
		OrganizationID:      ti.orgID,
		Provider:            "anthropic",
		ExternalAccountUuid: uuid.New().String(),
		UserID:              conv.ToPGText(userID),
		ExternalOrgID:       conv.PtrToPGText(nil),
		ExternalAccountID:   conv.PtrToPGText(nil),
		Email:               conv.ToPGText(email),
		AccountType:         conv.ToPGText(accountType),
	})
	require.NoError(t, err)
}
