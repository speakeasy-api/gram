package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	"github.com/speakeasy-api/gram/server/internal/oops"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// handleSkillResourceRead answers resources/read for a served skill's
// SKILL.md with its stored content, exactly the bytes the entry's digest
// covers. The read is answered from the skill distribution rather than a
// toolset resource: it invokes nothing, so it is neither metered nor billed.
// A successful read is recorded as an activation of the served version, best
// effort, counted on the skill read metric, and written to the tool-usage
// logs as a load of the named skill; a URI the caller is not
// served is the extension's invalid-params error. uri is the skill URI the
// request named, already known to have the shape of a served skill; now is
// when the read is recorded as seen.
func handleSkillResourceRead(
	ctx context.Context,
	logger *slog.Logger,
	db *pgxpool.Pool,
	resolver *mcpskills.Resolver,
	metrics *mcpmetrics.Metrics,
	telemLogger *tm.Logger,
	clientInfoStore sessionClientInfoStore,
	payload *mcpInputs,
	req *rawRequest,
	uri string,
	now time.Time,
) (json.RawMessage, error) {
	scope, err := hostedSkillScope(ctx, logger, db, payload)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to resolve skill recipient").LogError(ctx, logger)
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
		ProjectID: payload.projectID,
		Entry:     *entry,
		UserID:    payload.userID,
		SessionID: payload.sessionID,
		SeenAt:    now,
	}); err != nil {
		logger.WarnContext(ctx, "failed to record skill read",
			attr.SlogError(err),
			attr.SlogProjectID(payload.projectID.String()),
			attr.SlogResourceURI(entry.URI()),
		)
	}
	metrics.RecordSkillRead(ctx, mcpmetrics.SurfaceHosting, mcpmetrics.SkillOriginDistribution)
	logSkillRead(ctx, logger, telemLogger, clientInfoStore, payload, *entry, now)

	// Whether the URI resolves at all depends on the caller's plugin
	// assignments, so the content is private to the caller.
	return marshalSkillRead(ctx, logger, req.ID, *entry, serverInfoHostedToolset)
}

// skillToolName is the tool name a skill activation is logged under. The
// telemetry_logs skill_name column materializes from the "skill" argument of
// rows carrying this name, which is how hook-reported Skill tool calls are
// already recognized as skill usage; a load over MCP joins them under the
// same column.
const skillToolName = "Skill"

// logSkillRead writes a served skill read to the tool-usage logs, so it shows
// next to hook-reported skill activations on the Logs and MCP & Tools pages.
// The row is a resource_read, which keeps it apart from a hook call, and it
// carries the server it was served through and the version served.
func logSkillRead(
	ctx context.Context,
	logger *slog.Logger,
	telemLogger *tm.Logger,
	clientInfoStore sessionClientInfoStore,
	payload *mcpInputs,
	entry mcpskills.Entry,
	now time.Time,
) {
	if telemLogger == nil {
		return
	}

	// Only the name is recorded as the argument: the content is not tool IO
	// the caller sent, and the scrub keeps exactly this shape for Skill rows.
	// The scrub reads the tool name from the row's own attributes, before the
	// tool info is merged in, so the name is stamped here too.
	arguments, err := json.Marshal(map[string]string{"skill": entry.Name})
	if err != nil {
		logger.WarnContext(ctx, "failed to encode skill read log arguments", attr.SlogError(err))
		return
	}

	logAttrs := tm.HTTPLogAttributes{}
	logAttrs[attr.EventSourceKey] = string(tm.EventSourceResourceRead)
	logAttrs[attr.ToolNameKey] = skillToolName
	logAttrs[attr.GenAIToolCallArgumentsKey] = string(arguments)
	logAttrs[attr.ResourceURIKey] = entry.URI()
	logAttrs[attr.McpSkillIDKey] = entry.SkillID.String()
	logAttrs[attr.McpSkillVersionIDKey] = entry.VersionID.String()
	logAttrs[attr.McpSkillDigestKey] = entry.Digest()
	logAttrs.RecordStatusCode(http.StatusOK)
	logAttrs.RecordResponseBody(int64(entry.Size()))
	logAttrs.RecordTraceContext(ctx)
	logAttrs.RecordAuthenticatedActor(ctx)
	if payload.chatID != "" {
		logAttrs[attr.GenAIConversationIDKey] = payload.chatID
	}
	if payload.externalUserID != "" {
		logAttrs[attr.ExternalUserIDKey] = payload.externalUserID
	}
	if payload.apiKeyID != "" {
		logAttrs[attr.APIKeyIDKey] = payload.apiKeyID
	}
	logAttrs.RecordToolsetSlug(payload.toolset)
	recordServingLogAttrs(logAttrs, payload.servingServerID(), payload.mcpEndpointID)
	if payload.metaMcpServerID != "" {
		logAttrs[attr.MetaMcpServerIDKey] = payload.metaMcpServerID
	}
	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		logAttrs.RecordMCPURL(requestContext.Host + requestContext.ReqURL)
	}
	clientIdentity, _ := resolveClientIdentity(ctx, logger, clientInfoStore, payload, nil)
	logAttrs.RecordMCPClient(clientIdentity.Name, clientIdentity.Version)

	telemLogger.Log(ctx, tm.LogParams{
		Timestamp: now,
		ToolInfo: tm.ToolInfo{
			ID:             entry.SkillID.String(),
			URN:            urn.NewSkill(entry.SkillID).String(),
			Name:           skillToolName,
			ProjectID:      payload.projectID.String(),
			DeploymentID:   "",
			OrganizationID: payload.organizationID,
			FunctionID:     nil,
		},
		UserInfo:   tm.UserInfoByID(payload.userID),
		Attributes: logAttrs,
	})
}
