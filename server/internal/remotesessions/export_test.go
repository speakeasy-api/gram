package remotesessions

import (
	"context"
	"time"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"

	"github.com/google/uuid"
)

// WaitIdentityRestatements blocks until every detached identity restatement has finished.
func (s *RefreshService) WaitIdentityRestatements() { s.restatements.Wait() }

func (m *ChallengeManager) WaitIdentityRestatements() { m.refresher.WaitIdentityRestatements() }

// MaxEnrichmentBytes exposes the enrichment cap to e2e tests.
const MaxEnrichmentBytes = maxEnrichmentBytes

// PlanIssuerMetadataRefresh exposes the on-use decision to tests.
func PlanIssuerMetadataRefresh(use IssuerMetadataUse, now time.Time) (reproject, fetch bool) {
	plan := planIssuerMetadataRefresh(use, now)
	return plan.reproject, plan.fetch
}

// ReactiveIssuerMetadataRefreshDue exposes the reactive decision to tests.
func ReactiveIssuerMetadataRefreshDue(use IssuerMetadataUse, now time.Time) bool {
	return planReactiveIssuerMetadataRefresh(use, now).fetch
}

// IssuerMetadataUseFromRow builds the flow-time view of a stored row the way the flow queries do.
func IssuerMetadataUseFromRow(row repo.RemoteSessionIssuer) IssuerMetadataUse {
	return issuerMetadataUseFromRow(row)
}

// SetBeforeAdmit runs f before every use is admitted; tests hold a producer there to race Shutdown.
func (r *IssuerMetadataRefresher) SetBeforeAdmit(f func()) { r.beforeAdmit = f }

// Reproject exposes the scoped operation to integration tests.
func (r *IssuerMetadataRefresher) Reproject(ctx context.Context, candidate IssuerMetadataRefreshCandidate) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	existing, outcome, err := r.load(ctx, candidate)
	if err != nil || outcome != "" {
		return r.record(ctx, candidate.IssuerURL, remotesessionmetrics.IssuerMetadataRefreshReasonOnUse, outcome), err
	}
	return r.reproject(ctx, existing, remotesessionmetrics.IssuerMetadataRefreshReasonOnUse)
}

// Refresh exposes the scoped operation to integration tests.
func (r *IssuerMetadataRefresher) Refresh(ctx context.Context, candidate IssuerMetadataRefreshCandidate) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	existing, outcome, err := r.load(ctx, candidate)
	if err != nil || outcome != "" {
		return r.record(ctx, candidate.IssuerURL, remotesessionmetrics.IssuerMetadataRefreshReasonOnUse, outcome), err
	}
	return r.refresh(ctx, existing, remotesessionmetrics.IssuerMetadataRefreshReasonOnUse)
}

// SetIssuerMetadataRefreshSeam replaces the AIM-260 seam so a test can observe the refresh request a 404 makes.
func (e *SessionEnricher) SetIssuerMetadataRefreshSeam(fn func(context.Context, uuid.UUID)) {
	e.requestIssuerMetadataRefresh = fn
}
