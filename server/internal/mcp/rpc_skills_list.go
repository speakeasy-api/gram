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
	var params skillsListParams
	if len(req.Params) > 0 && string(req.Params) != "null" {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "failed to parse skills/list request").LogWarn(ctx, logger)
		}
	}

	// The listing is never split across pages, so no cursor this surface
	// issued can come back. One that does is a param naming a page that does
	// not exist.
	if params.Cursor != "" {
		return nil, oops.E(oops.CodeInvalid, nil, "unknown cursor").LogWarn(ctx, logger)
	}

	scope, err := hostedSkillScope(ctx, db, payload)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to resolve skill recipient").LogError(ctx, logger)
	}

	entries, err := resolver.List(ctx, scope)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list skills").LogError(ctx, logger)
	}

	skills := make([]mcpskills.Skill, 0, len(entries))
	for _, entry := range entries {
		skills = append(skills, entry.Wire())
	}

	bs, err := json.Marshal(result[skillsListResult]{
		ID: req.ID,
		Result: skillsListResult{
			Skills:     skills,
			NextCursor: nil,
		},
		serverIdentity: serverInfoHostedToolset,
		// The listing is filtered by the caller's plugin assignments, so no
		// shared cache may serve it to a second caller.
		cacheHints: cacheHintsCallerVarying,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize skills/list response").LogError(ctx, logger)
	}

	return bs, nil
}
