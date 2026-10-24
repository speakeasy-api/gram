// Package gatewayscope resolves a gateway to the identity its sessions are
// recorded under, for the session list and the Platform session tool.
package gatewayscope

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ResourceIdentity is the identity of the live gateway gatewayID in projectID.
// A missing gateway, and one deleted or in another project, is not found.
func ResourceIdentity(ctx context.Context, db metamcprepo.DBTX, projectID uuid.UUID, gatewayID string) (pgtype.Text, error) {
	id, err := uuid.Parse(gatewayID)
	if err != nil {
		return pgtype.Text{}, oops.E(oops.CodeBadRequest, err, "invalid meta_mcp_server_id")
	}
	gateway, err := metamcprepo.New(db).GetMetaMCPServerByIDAndProjectID(ctx, metamcprepo.GetMetaMCPServerByIDAndProjectIDParams{
		ID:        id,
		ProjectID: projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.Text{}, oops.E(oops.CodeNotFound, err, "gateway not found")
	}
	if err != nil {
		return pgtype.Text{}, fmt.Errorf("load gateway: %w", err)
	}
	return pgtype.Text{String: "meta_mcp_server:" + gateway.ID.String(), Valid: true}, nil
}
