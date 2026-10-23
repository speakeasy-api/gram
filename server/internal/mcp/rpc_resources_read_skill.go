package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// handleSkillResourceRead answers resources/read for a served skill's
// SKILL.md with its stored content, exactly the bytes the entry's digest
// covers. The read is answered from the skill distribution rather than a
// toolset resource: it invokes nothing, so it is neither metered nor billed.
// A successful read is recorded as an activation of the served version, best
// effort, and counted on the skill read metric; a URI the caller is not
// served is the extension's invalid-params error. uri is the skill URI the
// request named, already known to have the shape of a served skill; now is
// when the read is recorded as seen.
func handleSkillResourceRead(
	ctx context.Context,
	logger *slog.Logger,
	db *pgxpool.Pool,
	resolver *mcpskills.Resolver,
	metrics *mcpmetrics.Metrics,
	payload *mcpInputs,
	req *rawRequest,
	uri string,
	now time.Time,
) (json.RawMessage, error) {
	scope, ok := hostedSkillScope(ctx, logger, db, payload)

	if !ok {
		return nil, skillNotFound(ctx, logger, mcpskills.ErrSkillNotFound, uri)
	}
	entry, err := resolver.Get(ctx, scope, uri)
	if errors.Is(err, mcpskills.ErrSkillNotFound) {
		return nil, skillNotFound(ctx, logger, err, uri)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to read skill").LogError(ctx, logger)
	}

	// The activation is bookkeeping about the read, not part of it: a
	// failure to record it is logged and the content is still served.
	if _, err := resolver.RecordRead(ctx, mcpskills.Read{
		Entry:     *entry,
		UserID:    scope.UserID,
		SessionID: payload.sessionID,
		SeenAt:    now,
	}); err != nil {
		logger.WarnContext(ctx, "failed to record skill read",
			attr.SlogError(err),
			attr.SlogProjectID(entry.ProjectID.String()),
			attr.SlogResourceURI(entry.URI()),
		)
	}
	metrics.RecordSkillRead(ctx, mcpmetrics.SurfaceHosting, mcpmetrics.SkillOriginDistribution)

	// Whether the URI resolves at all depends on the caller's organization
	// membership, so the content is private to the caller.
	return marshalSkillRead(ctx, logger, req.ID, *entry, serverInfoHostedToolset)
}
