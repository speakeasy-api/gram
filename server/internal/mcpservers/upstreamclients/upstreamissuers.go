package upstreamclients

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
)

// UpstreamIssuers holds the distinct live providers one MCP server reaches
// through its live client associations.
type UpstreamIssuers struct {
	ids []uuid.UUID
}

// Sole returns the server's provider when it has exactly one, and an invalid
// value when it has none or several.
func (u UpstreamIssuers) Sole() uuid.NullUUID {
	if len(u.ids) != 1 {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}
	return uuid.NullUUID{UUID: u.ids[0], Valid: true}
}

// Several reports whether the server has more than one provider, which an
// invalid Sole alone cannot tell apart from having none.
func (u UpstreamIssuers) Several() bool {
	return len(u.ids) > 1
}

// ServerUpstreamIssuers reads the providers of one MCP server in projectID.
func ServerUpstreamIssuers(ctx context.Context, dbtx repo.DBTX, projectID, serverID uuid.UUID) (UpstreamIssuers, error) {
	ids, err := repo.New(dbtx).ListMCPServerUpstreamIssuers(ctx, repo.ListMCPServerUpstreamIssuersParams{
		McpServerID: serverID,
		ProjectID:   projectID,
	})
	if err != nil {
		return UpstreamIssuers{ids: nil}, fmt.Errorf("list mcp server upstream issuers: %w", err)
	}
	return UpstreamIssuers{ids: ids}, nil
}
