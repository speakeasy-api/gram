package identityproviderconnections

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// PublicationAbsent exposes the uncertain-commit reconciliation to black-box tests.
func (p *Provisioner) PublicationAbsent(ctx context.Context, organizationID string, connectionID uuid.UUID, resourceName string) (bool, error) {
	return p.publicationAbsent(ctx, organizationID, connectionID, resourceName)
}

// SweepEMAIssuerKeys sweeps one organization and reports how many revoked
// keys stay pending KMS cleanup.
func (p *Provisioner) SweepEMAIssuerKeys(ctx context.Context, organizationID string) (int, error) {
	_, pending, err := p.sweepEMAIssuerOrganization(ctx, organizationID)
	return pending, err
}

// SweepAllEMAIssuerKeys sweeps every organization with issuer key work, with
// no time budget.
func (p *Provisioner) SweepAllEMAIssuerKeys(ctx context.Context) (swept, pending int, err error) {
	progress, err := p.SweepEMAIssuerKeysFrom(ctx, "", 0)
	return progress.Swept, progress.Pending, err
}

// SetSweepPage shrinks sweep pages so tests can cross page boundaries.
func (p *Provisioner) SetSweepPage(n int32) { p.sweepPage = n }

// SetSweepOrganizationTimeout shortens one organization's share of a sweep.
func (p *Provisioner) SetSweepOrganizationTimeout(d time.Duration) { p.sweepOrganizationTimeout = d }

// SetIssuerKeyCleanupTimeout shortens each issuer key's KMS cleanup deadline.
func (p *Provisioner) SetIssuerKeyCleanupTimeout(d time.Duration) { p.issuerKeyCleanupTimeout = d }
