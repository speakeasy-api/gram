package mcp

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// unresolvedToolKind stands in for the kind of a tool that failed before it
// resolved to a hosted member's tool. It is deliberately not a urn.ToolKind:
// the row keeps its "tools:" prefix and its gateway, member, and toolset
// attribution, and its URN source stays empty rather than naming a kind the
// tool may not have.
const unresolvedToolKind = "unresolved"

// gatewayDiscoveryToolPrefix starts the tool name of a member's row for a
// gateway discovery call that could not describe it. Discovery tools belong to
// the gateway, not the member, so the row must not share a name, URN or stats
// with a member tool of the same name. The URN keeps the member's own kind and
// source, which Tool logs needs to list the row.
const gatewayDiscoveryToolPrefix = "gateway_"

// metaFailureRow is how a failed gateway tool_call row is attributed.
type metaFailureRow struct {
	// unstamped leaves out the gateway's stamp, for a call the gateway
	// counts from another row.
	unstamped bool

	// source names the tool's source when its URN cannot carry one. Only a
	// discovery failure row on a hosted member sets it.
	source string
}

// metaToolCallAttemptKey carries a gateway member tool call's attempt into
// handleToolsCall.
type metaToolCallAttemptKey struct{}

// metaToolCallAttempt is one gateway member tool call. A member dispatch
// writes its own tool_call row once handleToolsCall resolves the tool; a call
// that fails before that point is logged from the attempt instead, so every
// failed call leaves exactly one row.
type metaToolCallAttempt struct {
	// start is when the gateway received the call.
	start time.Time

	// member is the member the call named, once it is found.
	member *metaMember

	// toolsetSlug is a hosted member's toolset, once its dispatch is built.
	toolsetSlug string

	// descriptor is the resolved tool, once handleToolsCall knows it.
	descriptor *gateway.ToolDescriptor

	// logged is set once handleToolsCall has committed to writing the
	// call's tool_call row.
	logged bool

	// proxied is set once a proxied member's tools/call is sent upstream,
	// where the remotemcp interceptor writes the row. A dial or session
	// failure before that is logged from the attempt.
	proxied bool

	// status is the HTTP status of a failure answered as an isError result.
	status int

	// message is what the caller was told about that failure.
	message string
}

// newMetaToolCallAttempt starts an attempt for a call received at start.
func newMetaToolCallAttempt(start time.Time) *metaToolCallAttempt {
	return &metaToolCallAttempt{
		start:       start,
		member:      nil,
		toolsetSlug: "",
		descriptor:  nil,
		logged:      false,
		proxied:     false,
		status:      0,
		message:     "",
	}
}

// markMetaToolCallProxied records that the call on ctx reached a proxied
// member's upstream, whose interceptor writes its row.
func markMetaToolCallProxied(ctx context.Context) {
	if attempt, ok := ctx.Value(metaToolCallAttemptKey{}).(*metaToolCallAttempt); ok && attempt != nil {
		attempt.proxied = true
	}
}

// failMetaToolCall records on the call on ctx a failure answered as an
// isError result.
func failMetaToolCall(ctx context.Context, status int, message string) {
	if attempt, ok := ctx.Value(metaToolCallAttemptKey{}).(*metaToolCallAttempt); ok && attempt != nil {
		attempt.fail(status, message)
	}
}

// fail records a failure answered as an isError result.
func (a *metaToolCallAttempt) fail(status int, message string) {
	a.status = status
	a.message = message
}

// metaMemberErrorStatus is the status a member failure answered as an isError
// result is logged with.
func metaMemberErrorStatus(err error) int {
	if _, ok := errors.AsType[memberCredentialError](err); ok {
		return http.StatusUnauthorized
	}
	if _, ok := errors.AsType[catalogConsentError](err); ok {
		return http.StatusForbidden
	}
	return http.StatusServiceUnavailable
}

// logMetaToolCallFailure writes the tool_call row for a gateway tool call that
// failed before a member dispatch could write one. It is logged in the
// member's project when the call reached a member, and in the gateway's
// otherwise; a call on an agent gateway that reached no member has no project
// to be logged under and is skipped.
func (s *Service) logMetaToolCallFailure(ctx context.Context, gate *metaGateContext, qualified string, arguments []byte, attempt *metaToolCallAttempt, handlerErr error) {
	params := metaToolCallFailureLogParams(ctx, gate, qualified, arguments, attempt, handlerErr, metaFailureRow{unstamped: false, source: ""})
	if params == nil {
		return
	}
	s.telemLogger.Log(ctx, *params)
}

// metaToolCallFailureLogParams builds the failed tool_call row for a gateway
// call, or nil when it has no project to be logged under.
func metaToolCallFailureLogParams(ctx context.Context, gate *metaGateContext, qualified string, arguments []byte, attempt *metaToolCallAttempt, handlerErr error, row metaFailureRow) *tm.LogParams {
	status, message := attempt.status, attempt.message
	if status == 0 {
		status, message = http.StatusInternalServerError, "unexpected error"
		if shareable, ok := errors.AsType[*oops.ShareableError](handlerErr); ok {
			status, message = shareable.HTTPStatus(ctx), shareable.Error()
		}
	}

	// The tool-call queries filter on the "tools:" prefix, so an unresolved
	// tool still needs a URN.
	toolInfo := tm.ToolInfo{
		ID:             "",
		URN:            "tools:" + unresolvedToolKind + ":" + gate.metaServerID.String() + ":" + qualified,
		Name:           qualified,
		ProjectID:      "",
		DeploymentID:   "",
		OrganizationID: gate.organizationID,
		FunctionID:     nil,
	}
	if gate.projectID != uuid.Nil {
		toolInfo.ProjectID = gate.projectID.String()
	}
	logAttrs := tm.HTTPLogAttributes{
		attr.EventSourceKey: string(tm.EventSourceToolCall),
		attr.ToolCallIDKey:  uuid.NewString(),
	}
	if !row.unstamped {
		logAttrs[attr.MetaMcpServerIDKey] = gate.metaServerID.String()
	}
	// ToolInfo sets the source from a URN it can parse, which wins over this.
	if row.source != "" {
		logAttrs[attr.ToolCallSourceKey] = row.source
	}
	if member := attempt.member; member != nil {
		toolInfo.ProjectID = member.projectID.String()
		logAttrs[attr.McpServerIDKey] = member.serverID.String()
		if _, toolName, err := metamcp.SplitQualifiedName(qualified); err == nil {
			toolInfo.Name = toolName
			toolInfo.URN = metaMemberToolURN(*member, toolName)
		}
		if attempt.toolsetSlug != "" {
			logAttrs.RecordToolsetSlug(attempt.toolsetSlug)
		}
	}
	if descriptor := attempt.descriptor; descriptor != nil {
		toolInfo.ID = descriptor.ID
		toolInfo.URN = descriptor.URN.String()
		toolInfo.Name = descriptor.Name
		toolInfo.ProjectID = descriptor.ProjectID
		toolInfo.DeploymentID = descriptor.DeploymentID
		toolInfo.OrganizationID = descriptor.OrganizationID
	}
	if toolInfo.ProjectID == "" {
		return nil
	}

	logAttrs[attr.OutcomeKey] = dialect.ToolCallOutcome(status, true)
	logAttrs[attr.ToolCallDurationKey] = time.Since(attempt.start).Seconds()
	logAttrs[attr.ErrorMessageKey] = message
	logAttrs.RecordStatusCode(status)
	logAttrs.RecordRequestBody(int64(len(arguments)))
	logAttrs.RecordRequestBodyContent(arguments)
	logAttrs.RecordResponseBody(int64(len(message)))
	logAttrs.RecordResponseBodyContent([]byte(message))
	logAttrs.RecordTraceContext(ctx)
	logAttrs.RecordAuthenticatedActor(ctx)
	if gate.chatID != "" {
		logAttrs[attr.GenAIConversationIDKey] = gate.chatID
	}
	if gate.externalUserID != "" {
		logAttrs[attr.ExternalUserIDKey] = gate.externalUserID
	}
	if gate.apiKeyID != "" {
		logAttrs[attr.APIKeyIDKey] = gate.apiKeyID
	}

	return &tm.LogParams{
		Timestamp:  time.Now(),
		ToolInfo:   toolInfo,
		UserInfo:   tm.UserInfoByID(gate.userID),
		Attributes: logAttrs,
	}
}

// metaMemberFailure is a member a discovery call named that could not be
// described. The call answers it as an isError result or a failed entry
// rather than as an error.
type metaMemberFailure struct {
	// member is the member that failed.
	member metaMember

	// status is the HTTP status the failure is logged with.
	status int

	// message is what the caller was told about the failure.
	message string

	// arguments is the part of the call's arguments that named this member,
	// logged as the row's request. A describe_tools can name tools of members
	// in other projects, and this member's row must not list them.
	arguments []byte
}

// newMetaMemberFailure records that member failed with err, whose message the
// caller was told, for a call whose arguments named it as arguments.
func newMetaMemberFailure(member metaMember, err error, message string, arguments []byte) metaMemberFailure {
	return metaMemberFailure{member: member, status: metaMemberErrorStatus(err), message: message, arguments: arguments}
}

// logMetaMemberDiscoveryFailure writes a failed tool_call row on a member that
// a gateway discovery call named and could not describe, so the member's
// failure counts include the call. It is shaped like a gateway tool call that
// failed before dispatch, with three differences:
//
//   - It is not stamped with the gateway. Gateway usage counts discovery calls
//     from their meta_discovery rows and executions from the tool_call rows
//     stamped with the gateway, so a stamp would count this call twice.
//   - It is written on its own trace, linked to the request's. Rows that share
//     a trace and a project are summarized as one call, and the call's
//     meta_discovery row would otherwise hide this one from tool logs. The
//     tracer that runs when OTel tracing is off ignores trace.WithNewRoot, so
//     the row then gets a fresh trace of its own.
//   - It is named as the gateway's discovery tool (gateway_describe_server),
//     not a tool of the member, so it never shares a URN or stats with a
//     member tool. Its URN keeps the member's kind and source, and a hosted
//     member, whose URN cannot carry one, gets its server slug as the source,
//     so Tool logs lists the row.
func (s *Service) logMetaMemberDiscoveryFailure(ctx context.Context, gate *metaGateContext, toolName string, start time.Time, failure metaMemberFailure) {
	parent := trace.SpanContextFromContext(ctx)
	ctx, span := s.tracer.Start(ctx, "mcp.meta.member_discovery_failure",
		trace.WithNewRoot(), trace.WithLinks(trace.LinkFromContext(ctx)))
	defer span.End()
	ctx = withOwnTrace(ctx, parent)

	attempt := newMetaToolCallAttempt(start)
	attempt.member = &failure.member
	attempt.fail(failure.status, failure.message)
	row := metaFailureRow{unstamped: true, source: ""}
	if failure.member.backend == metaMemberBackendHosted {
		row.source = failure.member.slug
	}
	qualified := metamcp.QualifyName(failure.member.slug, gatewayDiscoveryToolPrefix+toolName)
	params := metaToolCallFailureLogParams(ctx, gate, qualified, failure.arguments, attempt, nil, row)
	if params == nil {
		return
	}
	s.telemLogger.Log(ctx, *params)
}

// withOwnTrace keeps ctx's trace when it differs from parent's, and otherwise
// gives ctx a fresh random trace. A span started with trace.WithNewRoot gets a
// new trace from a real tracer; a no-op tracer keeps the parent's, which is
// empty when the request carried no trace.
func withOwnTrace(ctx context.Context, parent trace.SpanContext) context.Context {
	current := trace.SpanContextFromContext(ctx)
	if current.TraceID() != parent.TraceID() {
		return ctx
	}
	var traceID trace.TraceID
	var spanID trace.SpanID
	_, _ = rand.Read(traceID[:])
	_, _ = rand.Read(spanID[:])
	return trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: parent.TraceFlags(),
		TraceState: trace.TraceState{},
		Remote:     false,
	}))
}

// metaMemberToolURN is the synthetic URN of a member tool that failed before
// it resolved. Proxied members follow their upstream dispatch's URN, keyed on
// the stable backend id rather than the mutable slug.
func metaMemberToolURN(member metaMember, toolName string) string {
	switch {
	case member.backend == metaMemberBackendHosted:
		return "tools:" + unresolvedToolKind + ":" + member.serverID.String() + ":" + toolName
	case member.remoteServerID.Valid:
		return "tools:" + string(urn.ToolKindExternalMCP) + ":" + member.remoteServerID.UUID.String() + ":" + toolName
	default:
		return "tools:" + string(urn.ToolKindTunneledMCP) + ":" + member.tunneledServerID.UUID.String() + ":" + toolName
	}
}
