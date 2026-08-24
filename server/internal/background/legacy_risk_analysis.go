package background

import (
	"github.com/google/uuid"
	"go.temporal.io/sdk/workflow"
)

const legacyDrainRiskAnalysisWorkflowName = "DrainRiskAnalysisWorkflow"

// legacyDrainRiskAnalysisParams preserves the payload contract for executions
// created before risk analysis moved to a per-project coordinator.
type legacyDrainRiskAnalysisParams struct {
	ProjectID    uuid.UUID
	RiskPolicyID uuid.UUID
	MaxMessages  int32
}

// legacyDrainRiskAnalysisWorkflow retires obsolete per-policy executions. New
// risk analysis work is handled by RiskAnalysisCoordinatorWorkflow.
func legacyDrainRiskAnalysisWorkflow(workflow.Context, legacyDrainRiskAnalysisParams) error {
	return nil
}
