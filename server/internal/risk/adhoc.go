package risk

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrAdhocAnalysisAlreadyRunning is returned by AdhocAnalysisClient.Trigger
// when a run is already in flight for the project.
var ErrAdhocAnalysisAlreadyRunning = errors.New("an ad-hoc risk analysis run is already in flight for this project")

// ErrAdhocAnalysisNotFound is returned by AdhocAnalysisClient.Status when the
// project has never had an ad-hoc run.
var ErrAdhocAnalysisNotFound = errors.New("no ad-hoc risk analysis run found for this project")

// AdhocAnalysisTriggerArgs describes an operator-triggered re-scan of a
// project's chat messages against one policy over [From, To).
type AdhocAnalysisTriggerArgs struct {
	ProjectID    uuid.UUID
	RiskPolicyID uuid.UUID
	From         time.Time
	To           time.Time
}

// AdhocAnalysisProgress mirrors the workflow's live progress counters.
type AdhocAnalysisProgress struct {
	TotalMessages      int64
	DispatchedMessages int64
	ProcessedMessages  int64
	Findings           int64
	BatchesCompleted   int64
	BatchesFailed      int64
	Policies           int
}

// AdhocAnalysisStatus reports a project's most recent ad-hoc run.
type AdhocAnalysisStatus struct {
	WorkflowID string
	// Status is one of: running, completed, failed, canceled, terminated,
	// timed_out.
	Status    string
	StartedAt *time.Time
	ClosedAt  *time.Time
	// Progress is nil when the workflow cannot be queried (e.g. no worker
	// available); status remains authoritative.
	Progress *AdhocAnalysisProgress
}

// AdhocAnalysisClient starts and inspects ad-hoc risk analysis workflow runs.
// Implemented by background.TemporalRiskAdhocAnalysisClient.
type AdhocAnalysisClient interface {
	Trigger(ctx context.Context, args AdhocAnalysisTriggerArgs) (*AdhocAnalysisStatus, error)
	Status(ctx context.Context, projectID uuid.UUID) (*AdhocAnalysisStatus, error)
}
