package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// handleSkillsList answers skills/list with every skill the endpoint serves
// to the caller, in one page. An anonymous caller receives an empty listing.
func handleSkillsList(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, resolver *mcpskills.Resolver, payload *mcpInputs, req *rawRequest) (json.RawMessage, error) {
	_, err := parseSkillsListParams(ctx, logger, req)
	if err != nil {
		return nil, err
	}

	scope, ok := hostedSkillScope(ctx, logger, db, payload)

	var entries []mcpskills.Entry
	if ok {
		entries, err = resolver.List(ctx, scope)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list skills").LogError(ctx, logger)
	}

	// The listing depends on the caller's organization membership, so no
	// shared cache may serve it to a second caller.
	return marshalSkillsList(ctx, logger, req.ID, entries, serverInfoHostedToolset)
}
