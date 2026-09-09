package remotesessions

import (
	"time"

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
