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

// SetSweepPage shrinks sweep pages so tests can cross page boundaries.
func (p *Provisioner) SetSweepPage(n int32) { p.sweepPage = n }

// SetIssuerKeyCleanupTimeout shortens each issuer key's KMS cleanup deadline.
func (p *Provisioner) SetIssuerKeyCleanupTimeout(d time.Duration) { p.issuerKeyCleanupTimeout = d }
