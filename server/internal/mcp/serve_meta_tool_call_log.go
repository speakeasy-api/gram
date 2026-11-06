package mcp

import (
	"context"
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
	params := metaToolCallFailureLogParams(ctx, gate, qualified, arguments, attempt, handlerErr)
	if params == nil {
		return
	}
	s.telemLogger.Log(ctx, *params)
}

// metaToolCallFailureLogParams builds the failed tool_call row for a gateway
// call, or nil when it has no project to be logged under.
func metaToolCallFailureLogParams(ctx context.Context, gate *metaGateContext, qualified string, arguments []byte, attempt *metaToolCallAttempt, handlerErr error) *tm.LogParams {
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
		attr.EventSourceKey:     string(tm.EventSourceToolCall),
		attr.ToolCallIDKey:      uuid.NewString(),
		attr.MetaMcpServerIDKey: gate.metaServerID.String(),
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
}

// newMetaMemberFailure records that member failed with err, whose message the
// caller was told.
func newMetaMemberFailure(member metaMember, err error, message string) metaMemberFailure {
	return metaMemberFailure{member: member, status: metaMemberErrorStatus(err), message: message}
}

// logMetaMemberDiscoveryFailure writes a failed tool_call row on a member that
// a gateway discovery call named and could not describe, so the member's
// failure counts include the call. It is shaped like a gateway tool call that
// failed before dispatch, with two differences:
//
//   - It is not stamped with the gateway. Gateway usage counts discovery calls
//     from their meta_discovery rows and executions from the tool_call rows
//     stamped with the gateway, so a stamp would count this call twice.
//   - It is written on its own trace, linked to the request's. Rows that share
//     a trace and a project are summarized as one call, and the call's
//     meta_discovery row would otherwise hide this one from tool logs.
func (s *Service) logMetaMemberDiscoveryFailure(ctx context.Context, gate *metaGateContext, toolName string, arguments []byte, start time.Time, failure metaMemberFailure) {
	ctx, span := s.tracer.Start(ctx, "mcp.meta.member_discovery_failure",
		trace.WithNewRoot(), trace.WithLinks(trace.LinkFromContext(ctx)))
	defer span.End()

	attempt := newMetaToolCallAttempt(start)
	attempt.member = &failure.member
	attempt.fail(failure.status, failure.message)
	params := metaToolCallFailureLogParams(ctx, gate, metamcp.QualifyName(failure.member.slug, toolName), arguments, attempt, nil)
	if params == nil {
		return
	}
	delete(params.Attributes, attr.MetaMcpServerIDKey)
	s.telemLogger.Log(ctx, *params)
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
