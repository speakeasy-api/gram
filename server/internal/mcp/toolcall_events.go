package mcp

import (
	"context"
	"encoding/json"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/otel/otelpub"
)

// toolCallTenant is the organization and project a tool call belongs to,
// which is the tool's own, never the caller's claim.
type toolCallTenant struct {
	organizationID string
	projectID      string
}

// toolCallIdentity is what both records of a tool call say about it: which
// call, on which session, which tool and server, from which client, by whom.
// An unknown value leaves its attribute off the record.
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

// toolCallEvents emits the two records that describe one tool call the
// gateway runs: gram.tool_call.started before the tool runs and
// gram.tool_call.completed once it has returned. Both are published under
// the tool call id as their record id, so they pair up in agent_events as
// the tool_call and the tool_call_result of one event.
//
// Each Emit publishes synchronously and waits for the Pub/Sub ack, so a
// call pays one publish before the tool runs and one more before its
// response is written. A publish that fails is logged by otelpub, and the
// tool call is never failed over it.
type toolCallEvents struct {
	emitter log.Logger
	tenant  toolCallTenant
	callID  string
	base    []log.KeyValue
	start   time.Time
	now     func() time.Time
}

func newToolCallEvents(
	emitter log.Logger,
	tenant toolCallTenant,
	identity toolCallIdentity,
	now func() time.Time,
) *toolCallEvents {
	return &toolCallEvents{
		emitter: emitter,
		tenant:  tenant,
		callID:  identity.callID,
		base:    logAttributes(identity.attributes()...),
		start:   now(),
		now:     now,
	}
}

// started says the tool is about to run.
func (e *toolCallEvents) started(ctx context.Context) {
	var record log.Record
	record.SetEventName(dialect.GramToolCallStartedEvent)
	record.SetTimestamp(e.start)
	record.SetSeverity(log.SeverityInfo)
	record.AddAttributes(e.base...)
	e.emit(ctx, record)
}

// completed says how the call went: the outcome by the rule that marks the
// tool result isError for the client, the status behind it, how long the
// call took, and, when the gateway answered with a failure of its own, the
// message the client was given. A tool that returned its own error
// document has an error outcome and no message: the result is the record
// of it, not a message about it.
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

// emit publishes one record under the call's tenant and id. The context is
// detached from the request's cancellation so a client that goes away
// mid-call still gets its completed record.
func (e *toolCallEvents) emit(ctx context.Context, record log.Record) {
	ctx = otelpub.WithTenant(context.WithoutCancel(ctx), e.tenant.organizationID, e.tenant.projectID)
	ctx = otelpub.WithRecordID(ctx, e.callID)
	e.emitter.Emit(ctx, record)
}

func toolCallCompletedAttributes(statusCode int, resultIsError bool, duration time.Duration, failure *oops.ShareableError) []attribute.KeyValue {
	out := []attribute.KeyValue{
		attr.OutcomeKey.String(toolCallOutcome(statusCode, resultIsError)),
		attr.HTTPResponseStatusCodeKey.Int(statusCode),
		attr.ToolCallDuration(duration),
	}
	if failure != nil {
		// A shareable error's text is what the client was told, so it
		// carries nothing the row may not hold.
		out = append(out, attr.ErrorMessage(failure.Error()))
	}
	return out
}

// toolCallOutcome is how the call went, in the vocabulary agent_events
// stores: the verdict the client reads as isError, so the row and the
// client never disagree about whether the call failed. For a tool the
// gateway formats the result of, that is its status; for a passthrough
// result the gateway forwards untouched, it is the upstream's own isError.
func toolCallOutcome(statusCode int, resultIsError bool) string {
	if resultIsError || statusCode < 200 || statusCode >= 300 {
		return dialect.OutcomeError
	}
	return dialect.OutcomeOK
}

// mcpResultIsError reads the isError verdict off a tool result document the
// gateway forwards untouched. Anything that is not such a document carries
// no verdict.
func mcpResultIsError(body []byte) bool {
	var result struct {
		IsError bool `json:"isError"`
	}
	return json.Unmarshal(body, &result) == nil && result.IsError
}

// toolCallCaller names the Speakeasy user behind a call only when the caller
// belongs to the tool's organization, whose data the records join, which
// is also as far as the telemetry row's hydration resolves a user. An
// outside caller on a public MCP is identified by its external user id
// alone.
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
