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

func (wc *Client) ListOrganizations(ctx context.Context) ([]Organization, error) {
	var out []Organization
	var after string

	for {
		resp, err := wc.orgs.ListOrganizations(ctx, organizations.ListOrganizationsOpts{
			Domains: nil,
			Limit:   100,
			Order:   "",
			Before:  "",
			After:   after,
		})
		if err != nil {
			return nil, fmt.Errorf("list organizations: %w", err)
		}

		for _, o := range resp.Data {
			out = append(out, Organization{
				ID:         o.ID,
				Name:       o.Name,
				ExternalID: o.ExternalID,
				CreatedAt:  o.CreatedAt,
				UpdatedAt:  o.UpdatedAt,
			})
		}

		if resp.ListMetadata.After == "" {
			return out, nil
		}
		after = resp.ListMetadata.After
	}
}
