package workos

import (
	"context"
	"fmt"
	"time"

	"github.com/workos/workos-go/v6/pkg/organizations"
)

type Organization struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	ExternalID string            `json:"external_id"`
	Metadata   map[string]string `json:"metadata"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

func (wc *Client) GetOrganizationByGramID(ctx context.Context, gramOrgID string) (Organization, error) {
	org, err := wc.orgs.GetOrganizationByExternalID(ctx, organizations.GetOrganizationByExternalIDOpts{
		ExternalID: gramOrgID,
	})
	if err != nil {
		return Organization{}, fmt.Errorf("get organization by external id: %w", err)
	}

	createdAt, err := time.Parse(time.RFC3339, org.CreatedAt)
	if err != nil {
		return Organization{}, fmt.Errorf("parse organization created at: %w", err)
	}
	updatedAt, err := time.Parse(time.RFC3339, org.UpdatedAt)
	if err != nil {
		return Organization{}, fmt.Errorf("parse organization updated at: %w", err)
	}

	return Organization{
		ID:         org.ID,
		Name:       org.Name,
		ExternalID: org.ExternalID,
		Metadata:   org.Metadata,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
	}, nil
}
