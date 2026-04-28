package activities

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

type GetAllWorkOSLinkedOrganizations struct {
	logger *slog.Logger
	db     *pgxpool.Pool
}

func NewGetAllWorkOSLinkedOrganizations(logger *slog.Logger, db *pgxpool.Pool) *GetAllWorkOSLinkedOrganizations {
	return &GetAllWorkOSLinkedOrganizations{
		logger: logger.With(attr.SlogComponent("get_all_workos_linked_organizations")),
		db:     db,
	}
}

func (g *GetAllWorkOSLinkedOrganizations) Do(ctx context.Context) ([]string, error) {
	workosOrgIDs, err := orgrepo.New(g.db).ListOrganizationsWithWorkosID(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list workos linked organizations").Log(ctx, g.logger)
	}
	return workosOrgIDs, nil
}
