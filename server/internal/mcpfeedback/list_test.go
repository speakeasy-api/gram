package mcpfeedback_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_feedback"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpfeedback"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

type listFilter struct {
	severity *types.McpFeedbackSeverity
	toolName *string
	cursor   *string
	limit    int
}

func listFeedback(ctx context.Context, ti *testInstance, serverID string, filter listFilter) (*gen.ListMcpFeedbackResult, error) {
	limit := filter.limit
	if limit == 0 {
		limit = 20
	}
	result, err := ti.service.List(ctx, &gen.ListPayload{
		McpServerID:      serverID,
		Severity:         filter.severity,
		ToolName:         filter.toolName,
		Cursor:           filter.cursor,
		Limit:            limit,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	return result, err //nolint:wrapcheck // returned for oops-code assertions on the chain
}

// submit stores one report as the signed-in member.
func submit(t *testing.T, ctx context.Context, ti *testInstance, server mcpserversrepo.McpServer, severity mcpfeedback.Severity, toolName, content string) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	caller := sampleCaller()
	caller.UserID = authCtx.UserID

	receipt, err := newTestRecorder(t, ti, noLimits).Submit(ctx, targetOf(t, ctx, server), caller, mcpfeedback.Report{
		Content:    content,
		Severity:   severity,
		Workaround: "",
		Intention:  "",
		ToolName:   toolName,
		Model:      "",
	})
	require.NoError(t, err)
	return receipt.ID
}

func TestListOfAServerWithNoFeedbackIsEmptyButShaped(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)

	result, err := listFeedback(ctx, ti, server.ID.String(), listFilter{severity: nil, toolName: nil, cursor: nil, limit: 0})
	require.NoError(t, err)
	require.Empty(t, result.Feedback)
	require.Empty(t, result.Tools)
	require.Nil(t, result.NextCursor)
	require.Zero(t, result.Counts.Total)
	require.Zero(t, result.Counts.Unspecified)

	// The window is named even with nothing in it, so the counts can say what they cover.
	start, err := time.Parse(time.RFC3339Nano, result.Counts.WindowStart)
	require.NoError(t, err)
	end, err := time.Parse(time.RFC3339Nano, result.Counts.WindowEnd)
	require.NoError(t, err)
	require.Equal(t, start, start.Truncate(24*time.Hour), "the window starts at midnight UTC")
	require.Equal(t, end.Truncate(24*time.Hour).AddDate(0, 0, -29), start)
}

func TestListReturnsFeedbackNewestFirstWithCounts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)
	other := seedHostedServer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	first := submit(t, ctx, ti, server, mcpfeedback.SeverityBlocked, "search_logs", "search_logs timed out.")
	second := submit(t, ctx, ti, server, mcpfeedback.SeverityBlocked, "search_logs", "search_logs timed out again.")
	third := submit(t, ctx, ti, server, mcpfeedback.SeverityDegraded, "get_metrics", "get_metrics needed a narrower range.")
	fourth := submit(t, ctx, ti, server, mcpfeedback.SeverityCosmetic, "", "The error text was confusing.")
	fifth := submit(t, ctx, ti, server, mcpfeedback.SeverityUnspecified, "search_logs", "search_logs results are easy to read.")
	submit(t, ctx, ti, other, mcpfeedback.SeverityBlocked, "search_logs", "Feedback for another server.")

	result, err := listFeedback(ctx, ti, server.ID.String(), listFilter{severity: nil, toolName: nil, cursor: nil, limit: 0})
	require.NoError(t, err)

	ids := make([]string, len(result.Feedback))
	for i, feedback := range result.Feedback {
		ids[i] = feedback.ID
	}
	require.Equal(t, []string{fifth.String(), fourth.String(), third.String(), second.String(), first.String()}, ids)

	newest := result.Feedback[0]
	require.Equal(t, server.ID.String(), newest.McpServerID)
	require.Equal(t, "search_logs results are easy to read.", newest.Content)
	require.Nil(t, newest.Severity, "a report with no severity has none")
	require.Equal(t, "search_logs", *newest.ToolName)
	require.Equal(t, authCtx.UserID, *newest.UserID)
	require.Equal(t, authCtx.Email, newest.UserEmail, "the member's email is resolved when the list is read")
	require.Equal(t, "claude-code", *newest.ClientName)
	require.Equal(t, types.McpFeedbackSeverity("cosmetic"), *result.Feedback[1].Severity)
	require.Nil(t, result.Feedback[1].ToolName)

	require.Equal(t, int64(5), result.Counts.Total)
	require.Equal(t, int64(2), result.Counts.Blocked)
	require.Equal(t, int64(1), result.Counts.Degraded)
	require.Equal(t, int64(1), result.Counts.Cosmetic)
	require.Equal(t, int64(1), result.Counts.Unspecified)

	require.Len(t, result.Tools, 2)
	require.Equal(t, gen.McpFeedbackToolCount{ToolName: "search_logs", FeedbackCount: 3, BlockedCount: 2}, *result.Tools[0])
	require.Equal(t, gen.McpFeedbackToolCount{ToolName: "get_metrics", FeedbackCount: 1, BlockedCount: 0}, *result.Tools[1])
}

func TestListRanksOnlyTheToolsNamedMostOften(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)

	// Eleven tools are each named twice, one more than the ranking holds.
	for i := range 11 {
		tool := fmt.Sprintf("list_things_%02d", i)
		submit(t, ctx, ti, server, mcpfeedback.SeverityBlocked, tool, tool+" always answers 500.")
		submit(t, ctx, ti, server, mcpfeedback.SeverityDegraded, tool, tool+" needed a second call.")
	}

	result, err := listFeedback(ctx, ti, server.ID.String(), listFilter{severity: nil, toolName: nil, cursor: nil, limit: 0})
	require.NoError(t, err)

	require.Len(t, result.Tools, 10)
	for i, tool := range result.Tools {
		// Ties fall back to the name, so the ranking does not reshuffle between reads.
		require.Equal(t, gen.McpFeedbackToolCount{ToolName: fmt.Sprintf("list_things_%02d", i), FeedbackCount: 2, BlockedCount: 1}, *tool)
	}
	require.Equal(t, int64(22), result.Counts.Total, "the counts still cover every report")
}

func TestListFiltersNarrowThePageAndNotTheCounts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)

	submit(t, ctx, ti, server, mcpfeedback.SeverityBlocked, "search_logs", "search_logs timed out.")
	submit(t, ctx, ti, server, mcpfeedback.SeverityDegraded, "search_logs", "search_logs was slow.")
	submit(t, ctx, ti, server, mcpfeedback.SeverityBlocked, "get_metrics", "get_metrics failed.")

	blocked, err := listFeedback(ctx, ti, server.ID.String(), listFilter{severity: new(types.McpFeedbackSeverity("blocked")), toolName: nil, cursor: nil, limit: 0})
	require.NoError(t, err)
	require.Len(t, blocked.Feedback, 2)
	require.Equal(t, int64(3), blocked.Counts.Total)

	searchLogs, err := listFeedback(ctx, ti, server.ID.String(), listFilter{severity: nil, toolName: new("search_logs"), cursor: nil, limit: 0})
	require.NoError(t, err)
	require.Len(t, searchLogs.Feedback, 2)

	both, err := listFeedback(ctx, ti, server.ID.String(), listFilter{severity: new(types.McpFeedbackSeverity("blocked")), toolName: new("search_logs"), cursor: nil, limit: 0})
	require.NoError(t, err)
	require.Len(t, both.Feedback, 1)
	require.Equal(t, "search_logs timed out.", both.Feedback[0].Content)

	none, err := listFeedback(ctx, ti, server.ID.String(), listFilter{severity: nil, toolName: new("delete_everything"), cursor: nil, limit: 0})
	require.NoError(t, err)
	require.Empty(t, none.Feedback)

	// A blank tool filter is no filter.
	blank, err := listFeedback(ctx, ti, server.ID.String(), listFilter{severity: nil, toolName: new("  "), cursor: nil, limit: 0})
	require.NoError(t, err)
	require.Len(t, blank.Feedback, 3)
}

func TestListPagesThroughEveryReportOnce(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)

	var submitted []string
	for i := range 5 {
		id := submit(t, ctx, ti, server, mcpfeedback.SeverityCosmetic, "", "Report "+string(rune('A'+i)))
		submitted = append([]string{id.String()}, submitted...)
	}

	var seen []string
	var cursor *string
	for page := range 3 {
		result, err := listFeedback(ctx, ti, server.ID.String(), listFilter{severity: nil, toolName: nil, cursor: cursor, limit: 2})
		require.NoError(t, err)
		for _, feedback := range result.Feedback {
			seen = append(seen, feedback.ID)
		}
		cursor = result.NextCursor
		if page < 2 {
			require.NotNil(t, cursor, "page %d has a next page", page)
			require.Len(t, result.Feedback, 2)
		}
	}
	require.Nil(t, cursor, "the last page has no next page")
	require.Equal(t, submitted, seen)
}

func TestListRejectsWhatItCannotPage(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)

	_, err := listFeedback(ctx, ti, server.ID.String(), listFilter{severity: nil, toolName: nil, cursor: new("not-a-cursor"), limit: 0})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = listFeedback(ctx, ti, server.ID.String(), listFilter{severity: nil, toolName: nil, cursor: nil, limit: 51})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = listFeedback(ctx, ti, server.ID.String(), listFilter{severity: new(types.McpFeedbackSeverity("urgent")), toolName: nil, cursor: nil, limit: 0})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = listFeedback(ctx, ti, "not-a-uuid", listFilter{severity: nil, toolName: nil, cursor: nil, limit: 0})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = listFeedback(ctx, ti, uuid.NewString(), listFilter{severity: nil, toolName: nil, cursor: nil, limit: 0})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestListFollowsAccessToTheServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)
	other := seedHostedServer(t, ctx, ti)
	submit(t, ctx, ti, server, mcpfeedback.SeverityBlocked, "search_logs", "search_logs timed out.")

	noGrants := authztest.WithExactGrants(t, ctx)
	_, err := listFeedback(noGrants, ti, server.ID.String(), listFilter{severity: nil, toolName: nil, cursor: nil, limit: 0})
	requireOopsCode(t, err, oops.CodeForbidden)

	reader := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeMCPRead, server.ToolsetID.UUID.String()))
	result, err := listFeedback(reader, ti, server.ID.String(), listFilter{severity: nil, toolName: nil, cursor: nil, limit: 0})
	require.NoError(t, err)
	require.Len(t, result.Feedback, 1)
	_, err = listFeedback(reader, ti, other.ID.String(), listFilter{severity: nil, toolName: nil, cursor: nil, limit: 0})
	requireOopsCode(t, err, oops.CodeForbidden)

	// Another project cannot read this server's feedback even by its id.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	otherProject := uuid.New()
	otherAuth := *authCtx
	otherAuth.ProjectID = &otherProject
	_, err = listFeedback(contextvalues.SetAuthContext(ctx, &otherAuth), ti, server.ID.String(), listFilter{severity: nil, toolName: nil, cursor: nil, limit: 0})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// A report keeps naming its sender by id once they leave the organization, but
// their email is no longer resolved for it.
func TestListOmitsTheEmailOfARemovedMember(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	submit(t, ctx, ti, server, mcpfeedback.SeverityBlocked, "search_logs", "search_logs timed out.")
	require.NoError(t, testrepo.New(ti.conn).ForceSoftDeleteOrganizationUserRelationship(ctx, testrepo.ForceSoftDeleteOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         pgtype.Text{String: authCtx.UserID, Valid: true},
	}))

	result, err := listFeedback(ctx, ti, server.ID.String(), listFilter{severity: nil, toolName: nil, cursor: nil, limit: 0})
	require.NoError(t, err)
	require.Len(t, result.Feedback, 1)
	require.Equal(t, authCtx.UserID, *result.Feedback[0].UserID)
	require.Nil(t, result.Feedback[0].UserEmail)
}
