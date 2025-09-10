package activities

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type GetAllOrganizations struct {
	logger *slog.Logger
	repo   *repo.Queries
}

func NewGetAllOrganizations(logger *slog.Logger, db *pgxpool.Pool) *GetAllOrganizations {
	return &GetAllOrganizations{
		logger: logger.With(attr.SlogComponent("get-all-organizations")),
		repo:   repo.New(db),
	}
}

func (g *GetAllOrganizations) Do(ctx context.Context) ([]string, error) {
	orgs, err := g.repo.GetAllOrganizationsWithToolsets(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get all organization").Log(ctx, g.logger)
	}

	orgIDs := make([]string, len(orgs))
	for i, org := range orgs {
		orgIDs[i] = org.ID
	}

	g.logger.InfoContext(ctx, "retrieved all organization IDs successfully")
	return orgIDs, nil
}
