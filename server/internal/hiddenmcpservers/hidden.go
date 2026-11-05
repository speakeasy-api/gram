// Package hiddenmcpservers stores the MCP servers each user hid.
package hiddenmcpservers

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/hiddenmcpservers/repo"
)

// ErrServerNotFound reports that no live server with that id is in the
// organization.
var ErrServerNotFound = errors.New("mcp server not found in organization")

// HiddenServerIDs is the set of servers the user hid in the organization.
func HiddenServerIDs(ctx context.Context, db repo.DBTX, organizationID, userID string) (map[uuid.UUID]bool, error) {
	ids, err := repo.New(db).ListHiddenMCPServerIDs(ctx, repo.ListHiddenMCPServerIDsParams{OrganizationID: organizationID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("list hidden mcp servers: %w", err)
	}
	hidden := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		hidden[id] = true
	}
	return hidden, nil
}

// Hide hides a live server in the organization for the user.
func Hide(ctx context.Context, db repo.DBTX, organizationID, userID string, serverID uuid.UUID) error {
	_, err := repo.New(db).HideMCPServer(ctx, repo.HideMCPServerParams{OrganizationID: organizationID, UserID: userID, McpServerID: serverID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrServerNotFound
	case err != nil:
		return fmt.Errorf("hide mcp server: %w", err)
	}
	return nil
}

// Unhide shows a hidden server again for the user.
func Unhide(ctx context.Context, db repo.DBTX, organizationID, userID string, serverID uuid.UUID) error {
	if _, err := repo.New(db).UnhideMCPServer(ctx, repo.UnhideMCPServerParams{OrganizationID: organizationID, UserID: userID, McpServerID: serverID}); err != nil {
		return fmt.Errorf("unhide mcp server: %w", err)
	}
	return nil
}
