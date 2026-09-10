package remotesessions

import (
	"context"
	"time"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
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
		return r.record(ctx, candidate.IssuerURL, outcome), err
	}
	return r.reproject(ctx, existing)
}

// Refresh exposes the scoped operation to integration tests.
func (r *IssuerMetadataRefresher) Refresh(ctx context.Context, candidate IssuerMetadataRefreshCandidate) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	existing, outcome, err := r.load(ctx, candidate)
	if err != nil || outcome != "" {
		return r.record(ctx, candidate.IssuerURL, outcome), err
	}
	return r.refresh(ctx, existing)
}
