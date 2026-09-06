package scanners_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/riskmeter"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

func TestEvaluationForAnalysisIgnoresBatchRequestForAnchoredWork(t *testing.T) {
	t.Parallel()

	first := scanners.EvaluationForAnalysis("org", "00000000-0000-0000-0000-000000000001", "batch-a", "message-1", "", "policy-1", 3, riskmeter.DetectorGitleaks, riskmeter.ModeShadow, "2026-09-06T12:00:00Z")
	regrouped := scanners.EvaluationForAnalysis("org", "00000000-0000-0000-0000-000000000001", "batch-b", "message-1", "", "policy-1", 3, riskmeter.DetectorGitleaks, riskmeter.ModeShadow, "2026-09-06T12:01:00Z")

	require.Equal(t, first.OperationID, regrouped.OperationID)
	require.Equal(t, riskmeter.EvaluationID(first), riskmeter.EvaluationID(regrouped))
}

func TestEvaluationForAnalysisUsesRequestForUnanchoredWork(t *testing.T) {
	t.Parallel()

	first := scanners.EvaluationForAnalysis("org", "00000000-0000-0000-0000-000000000001", "request-a", "", "", "", 0, riskmeter.DetectorGitleaks, riskmeter.ModeRealtime, "2026-09-06T12:00:00Z")
	second := scanners.EvaluationForAnalysis("org", "00000000-0000-0000-0000-000000000001", "request-b", "", "", "", 0, riskmeter.DetectorGitleaks, riskmeter.ModeRealtime, "2026-09-06T12:00:00Z")

	require.NotEqual(t, first.OperationID, second.OperationID)
	require.NotEqual(t, riskmeter.EvaluationID(first), riskmeter.EvaluationID(second))
}
