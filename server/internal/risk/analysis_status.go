package risk

import (
	"context"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// GetRiskAnalysisStatus reports the run state of the project's risk analysis
// coordinator for the Watchdog "last analyzed" badge. It shares the signals
// endpoint's gates (org:admin plus the Watchdog flag) because it describes
// the same surface, and it reads Temporal directly rather than Postgres:
// the coordinator is signal-driven, so the latest workflow run is the only
// record of when analysis last happened.
func (s *Service) GetRiskAnalysisStatus(ctx context.Context, _ *gen.GetRiskAnalysisStatusPayload) (*gen.RiskAnalysisStatusResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	if !s.riskWatchdogEnabled(ctx, authCtx) {
		return nil, oops.E(oops.CodeForbidden, nil, "risk signals are not enabled for this organization")
	}

	status, err := s.signaler.Describe(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to describe risk analysis status").LogError(ctx, s.logger)
	}

	return &gen.RiskAnalysisStatusResult{
		State:            string(status.State),
		RunningSince:     formatOptionalTime(status.RunningSince),
		LastRunStartedAt: formatOptionalTime(status.LastRunStartedAt),
		LastRunAt:        formatOptionalTime(status.LastRunAt),
		LastRunOutcome:   conv.PtrEmpty(status.LastRunOutcome),
	}, nil
}

// formatOptionalTime renders a nullable timestamp as the RFC3339 UTC string
// the rest of the risk API uses, keeping nil as nil so the result omits
// fields that do not apply to the current state.
func formatOptionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return conv.PtrEmpty(t.UTC().Format(time.RFC3339))
}
