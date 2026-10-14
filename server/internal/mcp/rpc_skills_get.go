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
// extension requires, so an unassigned skill is indistinguishable from a
// nonexistent one.
func handleSkillsGet(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, resolver *mcpskills.Resolver, payload *mcpInputs, req *rawRequest) (json.RawMessage, error) {
	var params skillsGetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "failed to parse skills/get request").LogWarn(ctx, logger)
	}

	if params.URI == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "skill URI is required").LogWarn(ctx, logger)
	}

	scope, err := hostedSkillScope(ctx, db, payload)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to resolve skill recipient").LogError(ctx, logger)
	}

	entry, err := resolver.Get(ctx, scope, params.URI)
	if errors.Is(err, mcpskills.ErrSkillNotFound) {
		return nil, skillNotFound(ctx, logger, err, params.URI)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get skill").LogError(ctx, logger)
	}

	bs, err := json.Marshal(result[skillsGetResult]{
		ID:             req.ID,
		Result:         skillsGetResult{Skill: entry.Wire()},
		serverIdentity: serverInfoHostedToolset,
		// Whether the URI resolves at all depends on the caller's plugin
		// assignments, so the entry is private to the caller.
		cacheHints: cacheHintsCallerVarying,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize skills/get response").LogError(ctx, logger)
	}

	return bs, nil
}
