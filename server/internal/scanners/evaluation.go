package scanners

import (
	"strconv"
	"time"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/server/internal/riskmeter"
)

// EvaluationForAnalysis builds retry-stable metadata for an anchored scanner request.
func EvaluationForAnalysis(organizationID, projectID, requestID, chatMessageID, contentPartID, policyID string, policyVersion int64, detector meteringv1.RiskEvaluation_Detector, mode meteringv1.RiskEvaluation_ExecutionMode, createdAt string) riskmeter.Evaluation {
	anchorKind := "request"
	anchorID := requestID
	if chatMessageID != "" {
		anchorKind = "chat_message"
		anchorID = chatMessageID
	} else if contentPartID != "" {
		anchorKind = "content_part"
		anchorID = contentPartID
	}

	occurredAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		occurredAt = time.Now().UTC()
	} else {
		occurredAt = occurredAt.UTC()
	}
	return riskmeter.Evaluation{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		OperationID:    riskmeter.OperationID(anchorKind, anchorID, policyID, strconv.FormatInt(policyVersion, 10)),
		Detector:       detector,
		ExecutionMode:  mode,
		PolicyID:       policyID,
		PolicyVersion:  policyVersion,
		OccurredAt:     occurredAt,
	}
}
