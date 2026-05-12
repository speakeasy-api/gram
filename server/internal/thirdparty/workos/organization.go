package workos

import (
	"context"
	"fmt"

	"github.com/workos/workos-go/v6/pkg/organizations"
)

// Organization represents a WorkOS organization with the fields used by Gram.
type Organization struct {
	ID         string
	Name       string
	ExternalID string
	CreatedAt  string
	UpdatedAt  string
}

// GetOrganization fetches a WorkOS organization by id.
func (wc *Client) GetOrganization(ctx context.Context, orgID string) (*Organization, error) {
	o, err := wc.orgs.GetOrganization(ctx, organizations.GetOrganizationOpts{Organization: orgID})
	if err != nil {
		return nil, fmt.Errorf("get organization: %w", err)
	}

	return &Organization{
		ID:         o.ID,
		Name:       o.Name,
		ExternalID: o.ExternalID,
		CreatedAt:  o.CreatedAt,
		UpdatedAt:  o.UpdatedAt,
	}, nil
}

// EnsureOrgExternalID sets the WorkOS organization's external_id to gramOrgID
// if it is not already set. Returns an error if the existing external_id
// doesn't match (indicates a data inconsistency that needs investigation).
func (wc *Client) EnsureOrgExternalID(ctx context.Context, workosOrgID, gramOrgID string) error {
	org, err := wc.GetOrganization(ctx, workosOrgID)
	if err != nil {
		return fmt.Errorf("get workos organization: %w", err)
	}

	if org.ExternalID == gramOrgID {
		return nil
	}
	if org.ExternalID != "" {
		return fmt.Errorf("workos org %s external_id mismatch: got %q, want %q", workosOrgID, org.ExternalID, gramOrgID)
	}

	_, err = wc.orgs.UpdateOrganization(ctx, organizations.UpdateOrganizationOpts{
		Organization:                     workosOrgID,
		Name:                             "",
		AllowProfilesOutsideOrganization: false,
		Domains:                          nil,
		DomainData:                       nil,
		ExternalID:                       gramOrgID,
		StripeCustomerID:                 "",
		Metadata:                         nil,
	})
	if err != nil {
		return fmt.Errorf("set workos org external_id: %w", err)
	}

	return nil
}
