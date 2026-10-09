package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/otelpub"
)

// toolCallTenant is the tool's own organization and project, never the caller's claim.
type toolCallTenant struct {
	organizationID string
	projectID      string
}

// toolCallIdentity is what both records of a tool call say about it.
type toolCallIdentity struct {
	callID          string
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

func (id toolCallIdentity) attributes() []attribute.KeyValue {
	out := []attribute.KeyValue{attr.ToolCallID(id.callID)}
	for _, kv := range []attribute.KeyValue{
		attr.SessionID(id.sessionID),
		attr.ChatID(id.chatID),
		attr.ToolName(id.toolName),
		attr.ToolURN(id.toolURN),
		attr.ToolsetSlug(id.toolsetSlug),
		attr.McpServerID(id.mcpServerID),
		attr.MetaMcpServerID(id.metaMCPServerID),
		attr.McpURL(id.mcpURL),
		attr.McpClientName(id.clientName),
		attr.McpClientVersion(id.clientVersion),
		attr.ExternalUserID(id.externalUserID),
		attr.UserID(id.userID),
		attr.UserEmailKey.String(id.userEmail),
	} {
		if kv.Value.AsString() != "" {
			out = append(out, kv)
		}
	}
	return out
}

// toolCallEvents emits the started and completed records of one tool call,
// both under the call id so they pair up in agent_events.
type toolCallEvents struct {
	logger *otelpub.Logger
	log    *slog.Logger
	tenant toolCallTenant
	callID string
	base   []log.KeyValue
	start  time.Time
	now    func() time.Time
}

func newToolCallEvents(
	logger *otelpub.Logger,
	slogger *slog.Logger,
	tenant toolCallTenant,
	identity toolCallIdentity,
	now func() time.Time,
) *toolCallEvents {
	return &toolCallEvents{
		logger: logger,
		log:    slogger,
		tenant: tenant,
		callID: identity.callID,
		base:   logAttributes(identity.attributes()...),
		start:  now(),
		now:    now,
	}
}

func (e *toolCallEvents) started(ctx context.Context) {
	var record log.Record
	record.SetEventName(dialect.GramToolCallStartedEvent)
	record.SetTimestamp(e.start)
	record.SetSeverity(log.SeverityInfo)
	record.AddAttributes(e.base...)
	e.emit(ctx, record)
}

func (e *toolCallEvents) completed(ctx context.Context, statusCode int, resultIsError bool, failure *oops.ShareableError) {
	end := e.now()
	var record log.Record
	record.SetEventName(dialect.GramToolCallCompletedEvent)
	record.SetTimestamp(end)
	record.SetSeverity(log.SeverityInfo)
	record.AddAttributes(e.base...)
	record.AddAttributes(logAttributes(toolCallCompletedAttributes(statusCode, resultIsError, end.Sub(e.start), failure)...)...)
	e.emit(ctx, record)
}

// emit never fails the call: a lost record is only logged.
func (e *toolCallEvents) emit(ctx context.Context, record log.Record) {
	if err := e.logger.Log(otelpub.WithRecordID(otelpub.WithTenant(ctx, e.tenant.organizationID, e.tenant.projectID), e.callID), record); err != nil {
		e.log.WarnContext(ctx, "tool call record was not published", attr.SlogToolCallID(e.callID), attr.SlogError(err))
	}
}

func toolCallCompletedAttributes(statusCode int, resultIsError bool, duration time.Duration, failure *oops.ShareableError) []attribute.KeyValue {
	out := []attribute.KeyValue{
		attr.OutcomeKey.String(toolCallOutcome(statusCode, resultIsError)),
		attr.HTTPResponseStatusCodeKey.Int(statusCode),
		attr.ToolCallDuration(duration),
	}
	if failure != nil {
		// A shareable error's text is what the client was told.
		out = append(out, attr.ErrorMessage(failure.Error()))
	}
	return out
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

func logAttributes(attrs ...attribute.KeyValue) []log.KeyValue {
	out := make([]log.KeyValue, 0, len(attrs))
	for _, kv := range attrs {
		out = append(out, log.KeyValueFromAttribute(kv))
	}
	return out
}
