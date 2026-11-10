package internalsessionissuers

import (
	"context"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// tokenPurgeTimeout bounds credential cleanup after the issuer is fenced.
const tokenPurgeTimeout = 10 * time.Second

// EMAIssuerTokenPurger erases retained vendor credentials independently of KMS.
// It is the only purge seam: wire it on the Service, which also runs without
// provisioning. Cleanup is best effort after authoritative revocation; cleanup
// failure must never roll back containment.
type EMAIssuerTokenPurger interface {
	PurgeEMAIssuerTokens(context.Context, string) error
}

// SetEMAIssuerTokenPurger installs local credential cleanup for containment.
func (s *Service) SetEMAIssuerTokenPurger(purger EMAIssuerTokenPurger) {
	s.tokenPurger = purger
}

func (s *Service) purgeIssuerTokens(ctx context.Context, organizationID string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tokenPurgeTimeout)
	defer cancel()
	if s.tokenPurger != nil {
		if err := s.tokenPurger.PurgeEMAIssuerTokens(ctx, organizationID); err != nil {
			s.logger.ErrorContext(ctx, "issuer vendor credential cleanup failed after containment", attr.SlogOrganizationID(organizationID), attr.SlogError(err))
		}
		return
	}
	// TODO(AIM-523): wire the purger with the vendor-token cache.
	s.logger.DebugContext(ctx, "issuer vendor credential cleanup is not configured", attr.SlogOrganizationID(organizationID))
}
