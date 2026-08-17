package mcpapproval_test

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func startResearchPayload(id string) *gen.StartResearchPayload {
	return &gen.StartResearchPayload{
		ID:               id,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}
}

func TestStartResearch(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: "https://mcp.example.com/research", status: "requested", evidence: "", version: 0,
	})

	report, err := ti.service.StartResearch(ctx, startResearchPayload(requestID.String()))
	require.NoError(t, err)
	require.Equal(t, "running", report.Status)
	require.NotNil(t, report.Model)
	require.NotNil(t, report.PromptVersion)
	require.NotNil(t, report.StartedAt)

	runs := ti.research.started()
	require.Len(t, runs, 1)
	require.Equal(t, report.ID, runs[0].ReportID.String())
	require.Equal(t, requestID, runs[0].RequestID)
	require.Equal(t, ti.projectID, runs[0].ProjectID)
	require.Equal(t, ti.organizationID, runs[0].OrgID)

	// The report shows up on the request detail immediately, in running
	// state, so the panel can poll it.
	detail, err := ti.service.GetRequest(ctx, getPayload(requestID.String()))
	require.NoError(t, err)
	require.Len(t, detail.ResearchReports, 1)
	require.Equal(t, report.ID, detail.ResearchReports[0].ID)
}

// A start while a run is in flight returns the running report rather than
// spending on a second run; a completed run does not block a re-run — reports
// are additive.
func TestStartResearch_SingleFlight(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: "https://mcp.example.com/research-flight", status: "requested", evidence: "", version: 0,
	})

	first, err := ti.service.StartResearch(ctx, startResearchPayload(requestID.String()))
	require.NoError(t, err)

	second, err := ti.service.StartResearch(ctx, startResearchPayload(requestID.String()))
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID, "a second start must attach to the running report")
	require.Len(t, ti.research.started(), 1, "only one run may be enqueued")

	// Finish the run, then a new start opens a second report.
	reportID := uuid.MustParse(first.ID)
	_, err = ti.repo.CompleteResearchReport(ctx, repo.CompleteResearchReportParams{
		ID:            reportID,
		ProjectID:     ti.projectID,
		Report:        []byte(`{"summary": "done"}`),
		ReportVersion: 1,
		Model:         conv.ToPGText("test-model"),
	})
	require.NoError(t, err)

	third, err := ti.service.StartResearch(ctx, startResearchPayload(requestID.String()))
	require.NoError(t, err)
	require.NotEqual(t, first.ID, third.ID, "re-runs are additive")
	require.Len(t, ti.research.started(), 2)
}

// An enqueue failure must not strand the report in running: the row lands as
// failed and the caller sees the error.
func TestStartResearch_EnqueueFailureFailsTheReport(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: "https://mcp.example.com/research-fail", status: "requested", evidence: "", version: 0,
	})

	ti.research.failStart = true
	_, err := ti.service.StartResearch(ctx, startResearchPayload(requestID.String()))
	requireOopsCode(t, err, oops.CodeUnexpected)

	detail, err := ti.service.GetRequest(ctx, getPayload(requestID.String()))
	require.NoError(t, err)
	require.Len(t, detail.ResearchReports, 1)
	require.Equal(t, "failed", detail.ResearchReports[0].Status)
}

func TestStartResearch_UnknownRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.StartResearch(ctx, startResearchPayload(uuid.NewString()))
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.StartResearch(ctx, startResearchPayload("not-a-uuid"))
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// Research spends the org's money behind the same org-admin gate as every
// other approval surface: a caller without admin cannot buy a run, whatever
// else they hold.
func TestStartResearch_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: "https://mcp.example.com/research-scope", status: "requested", evidence: "", version: 0,
	})

	nonAdmin := withProject(t, ctx, ti, ti.projectID)
	_, err := ti.service.StartResearch(nonAdmin, startResearchPayload(requestID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
}

// Two clicks that land together must still buy one run. The check and the
// insert are one transaction behind a row lock, so the second caller waits
// for the first and then sees its report rather than paying for a second
// agent run.
func TestStartResearch_ConcurrentStartsBuyOneRun(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: "https://mcp.example.com/concurrent-research", status: "requested", evidence: "", version: 0,
	})

	const callers = 4
	var wg sync.WaitGroup
	views := make([]*gen.ResearchReport, callers)
	errs := make([]error, callers)
	start := make(chan struct{})

	for i := range callers {
		wg.Go(func() {
			<-start
			views[i], errs[i] = ti.service.StartResearch(ctx, startResearchPayload(requestID.String()))
		})
	}
	close(start)
	wg.Wait()

	for i := range callers {
		require.NoError(t, errs[i])
		require.Equal(t, views[0].ID, views[i].ID, "every caller gets the same run")
	}

	reports, err := ti.repo.ListResearchReportsForApprovalRequest(ctx, repo.ListResearchReportsForApprovalRequestParams{
		McpApprovalRequestID: requestID,
		ProjectID:            ti.projectID,
	})
	require.NoError(t, err)
	require.Len(t, reports, 1, "one run was created, so one run was paid for")
	require.Len(t, ti.research.started(), 1, "and the runner was enqueued once")
}

// A result that arrives after something else resolved the report does not
// reopen it: a completion is only for a run still in flight, so an admin who
// has been shown a failure never watches it turn into a report.
func TestCompleteResearchReport_WillNotResurrectAResolvedReport(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: "https://mcp.example.com/late-completion", status: "requested", evidence: "", version: 0,
	})
	reportID := seedResearchReport(t, ctx, ti, ti.projectID, requestID, "failed", `{}`)

	_, err := ti.repo.CompleteResearchReport(ctx, repo.CompleteResearchReportParams{
		ID:            reportID,
		ProjectID:     ti.projectID,
		Report:        []byte(`{"claims":[]}`),
		ReportVersion: 1,
		Model:         conv.ToPGText("anthropic/claude-sonnet-5"),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	reports, err := ti.repo.ListResearchReportsForApprovalRequest(ctx, repo.ListResearchReportsForApprovalRequestParams{
		McpApprovalRequestID: requestID,
		ProjectID:            ti.projectID,
	})
	require.NoError(t, err)
	require.Len(t, reports, 1)
	require.Equal(t, "failed", reports[0].Status)
}
