package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/otelpub"
)

const (
	// executeToolOperation is the GenAI semconv operation name of a tool call.
	executeToolOperation = "execute_tool"

	// toolsCallMethod is the MCP semconv method name of a tool call.
	toolsCallMethod = "tools/call"

	// toolErrorType is the MCP semconv error.type of a result that says isError.
	toolErrorType = "tool_error"
)

// toolCall is what the gateway knows about one hosted tool call. Its record
// is a GenAI semconv execute_tool log under the call id.
type toolCall struct {
	id    string
	start time.Time

	// organizationID and projectID are the tool's own, never the caller's claim.
	organizationID string
	projectID      string

	sessionID       string
	chatID          string
	toolName        string
	toolURN         string
	toolsetSlug     string
	mcpServerID     string
	metaMCPServerID string
	mcpURL          string
	clientName      string
	clientVersion   string
	externalUserID  string
	userID          string
	userEmail       string
}

func (c toolCall) record(end time.Time, statusCode int, resultIsError bool, failure *oops.ShareableError) log.Record {
	conversationID := c.sessionID
	if conversationID == "" {
		conversationID = c.chatID
	}
	attrs := []attribute.KeyValue{
		attr.GenAIOperationName(executeToolOperation),
		attr.McpMethodName(toolsCallMethod),
		attr.GenAIToolCallID(c.id),
		attr.HTTPResponseStatusCode(statusCode),
		attr.ToolCallDuration(end.Sub(c.start)),
	}
	for _, kv := range []attribute.KeyValue{
		attr.GenAIToolName(c.toolName),
		attr.GenAIConversationID(conversationID),
		attr.McpSessionID(c.sessionID),
		attr.ChatID(c.chatID),
		attr.UserID(c.externalUserID),
		attr.UserEmailKey.String(c.userEmail),
		attr.AuthUserID(c.userID),
		attr.ToolURN(c.toolURN),
		attr.ToolsetSlug(c.toolsetSlug),
		attr.McpServerID(c.mcpServerID),
		attr.MetaMcpServerID(c.metaMCPServerID),
		attr.McpURL(c.mcpURL),
		attr.McpClientName(c.clientName),
		attr.McpClientVersion(c.clientVersion),
		attr.ErrorTypeKey.String(toolCallErrorType(statusCode, resultIsError, failure)),
	} {
		if kv.Value.AsString() != "" {
			attrs = append(attrs, kv)
		}
	}
	if failure != nil {
		// A shareable error's text is what the client was told.
		attrs = append(attrs, attr.ErrorMessage(failure.Error()))
	}

	var record log.Record
	record.SetEventName(executeToolOperation)
	record.SetTimestamp(end)
	record.SetSeverity(log.SeverityInfo)
	for _, kv := range attrs {
		record.AddAttributes(log.KeyValueFromAttribute(kv))
	}
	return record
}

// publishToolCallRecord never fails the call: a lost record is only logged.
func publishToolCallRecord(ctx context.Context, logger *otelpub.Logger, slogger *slog.Logger, call toolCall, record log.Record) {
	ctx = otelpub.WithRecordID(otelpub.WithTenant(ctx, call.organizationID, call.projectID), call.id)
	if err := logger.Log(ctx, record); err != nil {
		slogger.WarnContext(ctx, "tool call record was not published", attr.SlogToolCallID(call.id), attr.SlogError(err))
	}
}

// toolCallErrorType is empty for a call the client read as a success.
func toolCallErrorType(statusCode int, resultIsError bool, failure *oops.ShareableError) string {
	switch {
	case toolCallOutcome(statusCode, resultIsError) == dialect.OutcomeOK:
		return ""
	case failure != nil && failure.Code != "":
		return string(failure.Code)
	case resultIsError:
		return toolErrorType
	default:
		return strconv.Itoa(statusCode)
	}
}

// toolCallOutcome follows the verdict the client reads as isError.
func toolCallOutcome(statusCode int, resultIsError bool) string {
	if resultIsError || statusCode < 200 || statusCode >= 300 {
		return dialect.OutcomeError
	}
	return dialect.OutcomeOK
}

// mcpResultIsError reads isError off a forwarded tool result document.
func mcpResultIsError(body []byte) bool {
	var result struct {
		IsError bool `json:"isError"`
	}
	return json.Unmarshal(body, &result) == nil && result.IsError
}

// toolCallCaller names the caller only when they belong to the tool's
// organization, whose data the records join.
func toolCallCaller(ctx context.Context, payload *mcpInputs, toolOrganizationID, email string) (userID, userEmail string) {
	if !payload.authenticated {
		return "", ""
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID != toolOrganizationID {
		return "", ""
	}
	return payload.userID, email
}
