// Package gatewayscope resolves a gateway to the identity its sessions are
// recorded under, for the session list, its facets and the Platform session
// tool.
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

// Gateway is the session scope of one gateway.
type Gateway struct {
	// ResourceIdentity is the identity the gateway's sessions are recorded
	// under.
	ResourceIdentity pgtype.Text

	// IssuerID is the sign-in server the gateway issues sessions from now.
	IssuerID uuid.NullUUID
}

// Resolve scopes sessions to the live gateway gatewayID in projectID. A
// missing gateway, and one deleted or in another project, is not found.
func Resolve(ctx context.Context, db metamcprepo.DBTX, projectID uuid.UUID, gatewayID string) (Gateway, error) {
	id, err := uuid.Parse(gatewayID)
	if err != nil {
		return Gateway{}, oops.E(oops.CodeBadRequest, err, "invalid meta_mcp_server_id")
	}
	gateway, err := metamcprepo.New(db).GetMetaMCPServerByIDAndProjectID(ctx, metamcprepo.GetMetaMCPServerByIDAndProjectIDParams{
		ID:        id,
		ProjectID: projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Gateway{}, oops.E(oops.CodeNotFound, err, "gateway not found")
	}
	if err != nil {
		return Gateway{}, fmt.Errorf("load gateway: %w", err)
	}
	return Gateway{
		ResourceIdentity: pgtype.Text{String: "meta_mcp_server:" + gateway.ID.String(), Valid: true},
		IssuerID:         gateway.UserSessionIssuerID,
	}, nil
}
