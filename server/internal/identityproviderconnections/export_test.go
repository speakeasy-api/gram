package identityproviderconnections

import (
	"context"

	"github.com/google/uuid"
)

// PublicationAbsent exposes the uncertain-commit reconciliation to black-box tests.
func (p *Provisioner) PublicationAbsent(ctx context.Context, organizationID string, connectionID uuid.UUID, resourceName string) (bool, error) {
	return p.publicationAbsent(ctx, organizationID, connectionID, resourceName)
}
