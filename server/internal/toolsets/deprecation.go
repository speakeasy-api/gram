package toolsets

import (
	"context"
	"errors"
	"log/slog"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// ErrHostingFieldsMoved rejects toolset writes to MCP hosting fields, which belong to the MCP server and its endpoints.
var ErrHostingFieldsMoved = errors.New("toolset MCP hosting fields are managed through mcpServers.update and mcpEndpoints")

// deprecatedHostingFields lists the MCP hosting fields an updateToolset request sets.
func deprecatedHostingFields(payload *gen.UpdateToolsetPayload) []string {
	var fields []string
	if payload.McpEnabled != nil {
		fields = append(fields, "mcp_enabled")
	}
	if payload.McpSlug != nil {
		fields = append(fields, "mcp_slug")
	}
	if payload.McpIsPublic != nil {
		fields = append(fields, "mcp_is_public")
	}
	if payload.NetworkAccessMode != nil {
		fields = append(fields, "network_access_mode")
	}
	if payload.CustomDomainID != nil {
		fields = append(fields, "custom_domain_id")
	}
	return fields
}

func logDeprecatedHostingUse(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, fields []string) {
	userAgent, refererHost := "", ""
	if requestContext, ok := contextvalues.GetRequestContext(ctx); ok && requestContext != nil {
		userAgent, refererHost = requestContext.UserAgent, requestContext.RefererHost
	}
	attrs := []slog.Attr{
		attr.SlogEvent("toolsets:deprecated-mcp-hosting-api"),
		attr.SlogHTTPRequestHeaderUserAgent(userAgent),
		attr.SlogHTTPReferrerHost(refererHost),
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogUserID(authCtx.UserID),
	}
	if len(fields) > 0 {
		attrs = append(attrs, attr.SlogDeprecatedFields(fields))
	}
	if authCtx.APIKeyID != "" {
		attrs = append(attrs, attr.SlogAPIKeyID(authCtx.APIKeyID))
	}
	logger.LogAttrs(ctx, slog.LevelWarn, "deprecated toolset MCP hosting API used; migrate to mcpServers and mcpEndpoints", attrs...)
}
