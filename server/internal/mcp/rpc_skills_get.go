package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// handleSkillsGet answers skills/get with the entry for one served skill. A
// URI that names no skill served to this caller, including every URI an
// anonymous caller asks for, is answered with the invalid-params error the
// extension requires, so an unlinked skill is indistinguishable from a
// nonexistent one.
func handleSkillsGet(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, resolver *mcpskills.Resolver, payload *mcpInputs, req *rawRequest) (json.RawMessage, error) {
	params, err := parseSkillsGetParams(ctx, logger, req)
	if err != nil {
		return nil, err
	}

	scope, ok := hostedSkillScope(ctx, logger, db, payload)

	if !ok {
		return nil, skillNotFound(ctx, logger, mcpskills.ErrSkillNotFound, params.URI)
	}
	entry, err := resolver.Get(ctx, scope, params.URI)
	if errors.Is(err, mcpskills.ErrSkillNotFound) {
		return nil, skillNotFound(ctx, logger, err, params.URI)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get skill").LogError(ctx, logger)
	}

	// Whether the URI resolves at all depends on the caller's organization
	// membership, so the entry is private to the caller.
	return marshalSkillsGet(ctx, logger, req.ID, *entry, serverInfoHostedToolset)
}
