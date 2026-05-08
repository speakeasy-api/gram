package workos

import (
	"context"
	"fmt"

	"github.com/workos/workos-go/v6/pkg/organizations"
)

// Organization represents a WorkOS organization with the fields used by Gram.
type Organization struct {
	ID        string
	Name      string
	CreatedAt string
	UpdatedAt string
}

// GetOrganization fetches a WorkOS organization by id.
func (wc *Client) GetOrganization(ctx context.Context, orgID string) (*Organization, error) {
	o, err := wc.orgs.GetOrganization(ctx, organizations.GetOrganizationOpts{Organization: orgID})
	if err != nil {
		return nil, fmt.Errorf("get organization: %w", err)
	}

	return &Organization{
		ID:        o.ID,
		Name:      o.Name,
		CreatedAt: o.CreatedAt,
		UpdatedAt: o.UpdatedAt,
	}, nil
}
