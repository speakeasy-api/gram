package repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type GetOrganizationMCPServerParams struct {
	ID             uuid.UUID
	OrganizationID string
}

func (q *Queries) GetOrganizationMCPServer(ctx context.Context, arg GetOrganizationMCPServerParams) (ListOrganizationMCPServersByIDRow, error) {
	rows, err := q.ListOrganizationMCPServersByID(ctx, ListOrganizationMCPServersByIDParams{
		OrganizationID: arg.OrganizationID, Ids: []uuid.UUID{arg.ID}, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	if err != nil {
		return ListOrganizationMCPServersByIDRow{}, fmt.Errorf("query organization MCP server: %w", err)
	}
	if len(rows) == 0 {
		return ListOrganizationMCPServersByIDRow{}, pgx.ErrNoRows
	}
	return rows[0], nil
}
