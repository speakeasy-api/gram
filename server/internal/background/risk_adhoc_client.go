package background

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/risk"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// TemporalRiskAdhocAnalysisClient implements risk.AdhocAnalysisClient by
// starting and describing RiskAdhocAnalysisWorkflow executions.
type TemporalRiskAdhocAnalysisClient struct {
	TemporalEnv *tenv.Environment
	Logger      *slog.Logger
}

var _ risk.AdhocAnalysisClient = (*TemporalRiskAdhocAnalysisClient)(nil)

func (c *TemporalRiskAdhocAnalysisClient) Trigger(ctx context.Context, args risk.AdhocAnalysisTriggerArgs) (*risk.AdhocAnalysisStatus, error) {
	run, err := StartRiskAdhocAnalysis(ctx, c.TemporalEnv, RiskAdhocAnalysisParams{
		ProjectID:    args.ProjectID,
		RiskPolicyID: uuid.NullUUID{UUID: args.RiskPolicyID, Valid: true},
		From:         args.From,
		To:           args.To,
		Cursor:       uuid.Nil,
		Progress: RiskAdhocProgress{
			TotalMessages:      0,
			DispatchedMessages: 0,
			ProcessedMessages:  0,
			Findings:           0,
			BatchesCompleted:   0,
			BatchesFailed:      0,
			Policies:           0,
		},
	})
	if err != nil {
		var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
		if errors.As(err, &alreadyStarted) {
			return nil, risk.ErrAdhocAnalysisAlreadyRunning
		}
		return nil, fmt.Errorf("start adhoc risk analysis: %w", err)
	}

	c.Logger.InfoContext(ctx, "adhoc risk analysis started",
		attr.SlogProjectID(args.ProjectID.String()),
		attr.SlogRiskPolicyID(args.RiskPolicyID.String()),
		attr.SlogTemporalWorkflowID(run.GetID()),
	)

	return &risk.AdhocAnalysisStatus{
		WorkflowID: run.GetID(),
		Status:     "running",
		StartedAt:  nil,
		ClosedAt:   nil,
		Progress:   nil,
	}, nil
}

func (c *TemporalRiskAdhocAnalysisClient) Status(ctx context.Context, projectID uuid.UUID) (*risk.AdhocAnalysisStatus, error) {
	wfID := RiskAdhocWorkflowID(projectID)

	desc, err := c.TemporalEnv.Client().DescribeWorkflowExecution(ctx, wfID, "")
	if err != nil {
		var notFound *serviceerror.NotFound
		if errors.As(err, &notFound) {
			return nil, risk.ErrAdhocAnalysisNotFound
		}
		return nil, fmt.Errorf("describe adhoc risk analysis workflow: %w", err)
	}

	info := desc.GetWorkflowExecutionInfo()
	status := &risk.AdhocAnalysisStatus{
		WorkflowID: wfID,
		Status:     adhocWorkflowStatusString(info.GetStatus()),
		StartedAt:  nil,
		ClosedAt:   nil,
		Progress:   nil,
	}
	if ts := info.GetStartTime(); ts != nil {
		t := ts.AsTime()
		status.StartedAt = &t
	}
	if ts := info.GetCloseTime(); ts != nil {
		t := ts.AsTime()
		status.ClosedAt = &t
	}

	// Best-effort: the status above stays authoritative when the query fails
	// (e.g. no worker polling the queue right now).
	if val, qerr := c.TemporalEnv.Client().QueryWorkflow(ctx, wfID, "", RiskAdhocProgressQueryType); qerr != nil {
		c.Logger.DebugContext(ctx, "query adhoc risk analysis progress", attr.SlogError(qerr))
	} else {
		var progress RiskAdhocProgress
		if derr := val.Get(&progress); derr != nil {
			c.Logger.DebugContext(ctx, "decode adhoc risk analysis progress", attr.SlogError(derr))
		} else {
			status.Progress = &risk.AdhocAnalysisProgress{
				TotalMessages:      progress.TotalMessages,
				DispatchedMessages: progress.DispatchedMessages,
				ProcessedMessages:  progress.ProcessedMessages,
				Findings:           progress.Findings,
				BatchesCompleted:   progress.BatchesCompleted,
				BatchesFailed:      progress.BatchesFailed,
				Policies:           progress.Policies,
			}
		}
	}

	return status, nil
}

func adhocWorkflowStatusString(s enums.WorkflowExecutionStatus) string {
	switch s {
	case enums.WORKFLOW_EXECUTION_STATUS_RUNNING, enums.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW:
		return "running"
	case enums.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return "completed"
	case enums.WORKFLOW_EXECUTION_STATUS_FAILED:
		return "failed"
	case enums.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return "canceled"
	case enums.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		return "terminated"
	case enums.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return "timed_out"
	case enums.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED:
		return "unspecified"
	default:
		return "unspecified"
	}
}
