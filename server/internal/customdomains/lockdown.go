package customdomains

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/customdomains/repo"
)

// Lockdown is an organization's custom-domain lockdown: whether its MCP
// traffic is refused on the platform hosts, and the domain it must use.
type Lockdown struct {
	// LockedDown is true when the organization's custom domain carries an IP
	// allowlist. It holds whether or not the domain is serving yet, so a
	// pending allowlisted domain still keeps the platform hosts closed.
	LockedDown bool

	// ServingHost is the custom domain's host once it is verified and
	// activated, and empty before then. Addresses quoted to clients use it:
	// an unactivated domain is refused by the custom-domain middleware, so
	// quoting it would hand out an address that cannot work either.
	ServingHost string
}

// OrganizationLockdown reads the organization's custom-domain lockdown. An
// organization without a custom domain, or with one that has no IP allowlist,
// is not locked down.
func OrganizationLockdown(ctx context.Context, db repo.DBTX, organizationID string) (Lockdown, error) {
	domain, err := repo.New(db).GetCustomDomainByOrganization(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Lockdown{LockedDown: false, ServingHost: ""}, nil
	}
	if err != nil {
		return Lockdown{}, fmt.Errorf("load organization custom domain: %w", err)
	}
	if len(domain.IpAllowlist) == 0 {
		return Lockdown{LockedDown: false, ServingHost: ""}, nil
	}

	lockdown := Lockdown{LockedDown: true, ServingHost: ""}
	if domain.Verified && domain.Activated {
		lockdown.ServingHost = domain.Domain
	}
	return lockdown, nil
}
