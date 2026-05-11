package activities

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type ListWorkOSOrganizations struct {
	logger *slog.Logger
	workos WorkOSBackfillClient
}

func NewListWorkOSOrganizations(logger *slog.Logger, workosClient WorkOSBackfillClient) *ListWorkOSOrganizations {
	return &ListWorkOSOrganizations{
		logger: logger.With(attr.SlogComponent("list_workos_organizations")),
		workos: workosClient,
	}
}

func (l *ListWorkOSOrganizations) Do(ctx context.Context) ([]string, error) {
	orgs, err := l.workos.ListOrganizations(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list WorkOS organizations").Log(ctx, l.logger)
	}

	orgIDs := make([]string, 0, len(orgs))
	for _, org := range orgs {
		if org.ID != "" {
			orgIDs = append(orgIDs, org.ID)
		}
	}

	return orgIDs, nil
}
