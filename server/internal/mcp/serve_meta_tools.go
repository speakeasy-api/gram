// Meta MCP drill-down tools over gateway members. Members dispatch through
// their own upstream sessions (serve_meta_proxy.go).

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
)

// metaMemberError surfaces as an isError tool result naming only the
// affected member.
type metaMemberError struct {
	message string
}

func (e *metaMemberError) Error() string { return e.message }

// handleMetaDescribeServerCall: one member's catalog as qualified names and
// descriptions, without schemas (describe_tools' job). A member that cannot be
// described is answered as an isError result and returned as the failure.
func (s *Service) handleMetaDescribeServerCall(
	ctx context.Context,
	logger *slog.Logger,
	gate *metaGateContext,
	members []metaMember,
	req *rawRequest,
	argsRaw json.RawMessage,
) (json.RawMessage, *metaMemberFailure, error) {
	var args struct {
		Server string `json:"server"`
	}
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			return nil, nil, oops.E(oops.CodeInvalid, err, "invalid describe_server arguments").LogWarn(ctx, logger)
		}
	}
	if args.Server == "" {
		return nil, nil, oops.E(oops.CodeInvalid, nil, "server is required").LogWarn(ctx, logger)
	}

	if message, hosted := hostedMemberRefusal(gate, args.Server); hosted {
		body, err := marshalMetaToolError(ctx, logger, req.ID, message)
		return body, nil, err
	}
	member, ok := s.findNamedMember(ctx, gate, members, args.Server)
	if !ok {
		return nil, nil, unknownMetaMemberError(ctx, logger, gate, args.Server)
	}
	catalog, err := s.describeMetaMember(ctx, logger, gate, member)
	if err != nil {
		if _, ok := errors.AsType[*metaMemberError](err); ok {
			// describe_server names one member, so its arguments are all this
			// member's.
			failure := newMetaMemberFailure(member, err, s.memberFailureMessage(ctx, logger, gate, member, err), argsRaw)
			body, marshalErr := marshalMetaToolError(ctx, logger, req.ID, failure.message)
			return body, &failure, marshalErr
		}
		return nil, nil, err
	}

	described := make([]metamcp.DescribedTool, 0, len(catalog.entries))
	for _, entry := range catalog.entries {
		described = append(described, metamcp.DescribedTool{
			Name:        metamcp.QualifyName(member.slug, entry.Name),
			Description: entry.Description,
		})
	}

	structured, err := json.Marshal(metamcp.DescribeServerResult{
		Server: metamcp.ListedServer{
			Slug:      member.slug,
			Name:      member.name,
			SortOrder: int(member.sortOrder),
			Status:    s.memberStatus(ctx, member),
		},
		Tools: described,
	})
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "serialize describe_server result").LogError(ctx, logger)
	}
	body, err := marshalMetaToolCallResult(ctx, logger, req.ID, structured)
	return body, nil, err
}

// handleMetaDescribeToolsCall: full input schemas for the requested
// qualified names; misses land in not_found rather than being dropped. Members
// that cannot be described land in failed and are returned as failures.
func (s *Service) handleMetaDescribeToolsCall(
	ctx context.Context,
	logger *slog.Logger,
	gate *metaGateContext,
	members []metaMember,
	req *rawRequest,
	argsRaw json.RawMessage,
) (json.RawMessage, []metaMemberFailure, error) {
	var args struct {
		Tools []string `json:"tools"`
	}
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			return nil, nil, oops.E(oops.CodeInvalid, err, "invalid describe_tools arguments").LogWarn(ctx, logger)
		}
	}
	if len(args.Tools) == 0 {
		return nil, nil, oops.E(oops.CodeInvalid, nil, "tools is required").LogWarn(ctx, logger)
	}

	// Group by member so each member is described once.
	type memberRequest struct {
		member    metaMember
		toolNames []string
	}
	if len(args.Tools) > metamcp.MaxDescribeTools {
		return nil, nil, oops.E(oops.CodeInvalid, nil, "tools accepts at most %d names per call", metamcp.MaxDescribeTools).LogWarn(ctx, logger)
	}

	requestsBySlug := map[string]*memberRequest{}
	order := []string{}
	notFound := []string{}
	failed := []metamcp.FailedServer{}
	// A repeated name would otherwise emit one full schema per occurrence,
	// so a single request can amplify far beyond the body cap.
	seen := make(map[string]struct{}, len(args.Tools))
	for _, qualified := range args.Tools {
		if _, dup := seen[qualified]; dup {
			continue
		}
		seen[qualified] = struct{}{}
		serverSlug, toolName, err := metamcp.SplitQualifiedName(qualified)
		if err != nil {
			return nil, nil, oops.E(oops.CodeInvalid, err, "invalid tool name: must be of the form serverslug--toolname").LogWarn(ctx, logger)
		}
		if message, hosted := hostedMemberRefusal(gate, serverSlug); hosted {
			if !slices.ContainsFunc(failed, func(f metamcp.FailedServer) bool { return f.Server == serverSlug }) {
				failed = append(failed, metamcp.FailedServer{Server: serverSlug, Message: message})
			}
			continue
		}
		member, ok := s.findNamedMember(ctx, gate, members, serverSlug)
		if !ok {
			notFound = append(notFound, qualified)
			continue
		}
		mr, ok := requestsBySlug[serverSlug]
		if !ok {
			mr = &memberRequest{member: member, toolNames: nil}
			requestsBySlug[serverSlug] = mr
			order = append(order, serverSlug)
		}
		mr.toolNames = append(mr.toolNames, toolName)
	}

	described := []metamcp.SchemaTool{}
	var failures []metaMemberFailure
	for _, slug := range order {
		mr := requestsBySlug[slug]
		catalog, err := s.describeMetaMember(ctx, logger, gate, mr.member)
		if err != nil {
			// A member outage degrades only that member: its names land in
			// failed while every other member is still described.
			if _, ok := errors.AsType[*metaMemberError](err); ok {
				failure := newMetaMemberFailure(mr.member, err, s.memberFailureMessage(ctx, logger, gate, mr.member, err), describeToolsMemberArguments(mr.member.slug, mr.toolNames))
				failures = append(failures, failure)
				failed = append(failed, metamcp.FailedServer{Server: mr.member.slug, Message: failure.message})
				continue
			}
			// Members already found unreachable keep their failed rows.
			return nil, failures, err
		}
		for _, toolName := range mr.toolNames {
			entry, ok := catalog.byName[toolName]
			if !ok {
				notFound = append(notFound, metamcp.QualifyName(mr.member.slug, toolName))
				continue
			}
			described = append(described, metamcp.SchemaTool{
				Title:        entry.Title,
				OutputSchema: entry.OutputSchema,
				Icons:        entry.Icons,
				Execution:    entry.Execution,
				Meta:         entry.Meta,
				Name:         metamcp.QualifyName(mr.member.slug, entry.Name),
				Description:  entry.Description,
				InputSchema:  entry.InputSchema,
				Annotations:  entry.Annotations,
			})
		}
	}

	structured, err := json.Marshal(metamcp.DescribeToolsResult{
		Tools:    described,
		NotFound: notFound,
		Failed:   failed,
	})
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "serialize describe_tools result").LogError(ctx, logger)
	}
	body, err := marshalMetaToolCallResult(ctx, logger, req.ID, structured)
	return body, failures, err
}

// describeToolsMemberArguments is the describe_tools arguments narrowed to the
// names of one member, so a failed row on that member lists only its own
// tools.
func describeToolsMemberArguments(slug string, toolNames []string) []byte {
	qualified := make([]string, 0, len(toolNames))
	for _, toolName := range toolNames {
		qualified = append(qualified, metamcp.QualifyName(slug, toolName))
	}
	arguments, err := json.Marshal(map[string][]string{"tools": qualified})
	if err != nil {
		return nil
	}
	return arguments
}

// handleMetaExecuteToolCall routes execute_tool into the member's dispatch.
func (s *Service) handleMetaExecuteToolCall(
	ctx context.Context,
	logger *slog.Logger,
	gate *metaGateContext,
	members []metaMember,
	req *rawRequest,
	argsRaw json.RawMessage,
	meta *mcprequests.WireMeta,
) (json.RawMessage, error) {
	start := time.Now()
	qualified, arguments, err := processExecuteToolCall(ctx, logger, argsRaw)
	if err != nil {
		s.logMetaToolCallFailure(ctx, gate, metamcp.ToolExecuteTool, argsRaw, newMetaToolCallAttempt(start), err)
		return nil, err
	}

	return s.executeMetaMemberTool(ctx, logger, gate, members, req, qualified, arguments, meta)
}

// executeMetaMemberTool is shared by Direct calls, Progressive execute_tool,
// and Code Mode. A call that fails before its member dispatch writes the
// member's tool_call row is logged here, so every failed call leaves one row
// stamped with this gateway.
func (s *Service) executeMetaMemberTool(ctx context.Context, logger *slog.Logger, gate *metaGateContext, members []metaMember, req *rawRequest, qualified string, arguments json.RawMessage, meta *mcprequests.WireMeta) (json.RawMessage, error) {
	attempt := newMetaToolCallAttempt(time.Now())
	body, err := s.dispatchMetaMemberTool(context.WithValue(ctx, metaToolCallAttemptKey{}, attempt), logger, gate, members, req, qualified, arguments, meta, attempt)
	if attempt.proxied || (err == nil && attempt.status == 0) {
		return body, err
	}
	s.logMetaToolCallFailure(ctx, gate, qualified, arguments, attempt, err)
	return body, err
}

// dispatchMetaMemberTool routes one member tool call, recording on attempt
// how far it got and why it failed.
func (s *Service) dispatchMetaMemberTool(ctx context.Context, logger *slog.Logger, gate *metaGateContext, members []metaMember, req *rawRequest, qualified string, arguments json.RawMessage, meta *mcprequests.WireMeta, attempt *metaToolCallAttempt) (json.RawMessage, error) {
	serverSlug, toolName, err := metamcp.SplitQualifiedName(qualified)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid tool name: must be of the form serverslug--toolname").LogWarn(ctx, logger)
	}
	if message, hosted := hostedMemberRefusal(gate, serverSlug); hosted {
		if invocation, ok := ctx.Value(metaCodeInvocationKey{}).(*metaCodeInvocation); ok {
			invocation.rejected = true
		}
		attempt.fail(http.StatusNotFound, message)
		return marshalMetaToolError(ctx, logger, req.ID, message)
	}
	// Resolved before the cached-view check so the named server's check is
	// recorded on both paths; an unknown server is still refused after it.
	member, ok := s.findNamedMember(ctx, gate, members, serverSlug)
	if gate.catalog != nil && gate.catalogRef.Mode == toolfilter.CatalogModeCached {
		found := false
		for _, tool := range gate.catalog.Tools {
			if tool.Name == qualified {
				found = true
				break
			}
		}
		if !found {
			// Absence from this cached view does not prove consent excluded a real
			// tool: misspelled, removed, or unknown member tools are absent too.
			attempt.fail(http.StatusForbidden, remotemcp.ToolNotApprovedMessage)
			return marshalMetaToolError(ctx, logger, req.ID, remotemcp.ToolNotApprovedMessage)
		}
	}
	if !ok {
		return nil, unknownMetaMemberError(ctx, logger, gate, serverSlug)
	}
	attempt.member = &member
	if err := s.validateCatalogMember(ctx, gate, member, catalogCheckRecorded); err != nil {
		if _, ok := errors.AsType[*metaMemberError](err); ok {
			message := s.callFailureMessage(ctx, logger, gate, member, err)
			attempt.fail(metaMemberErrorStatus(err), message)
			return marshalMetaMemberFailure(ctx, logger, req.ID, err, message)
		}
		if denied, ok := errors.AsType[*oops.ShareableError](err); ok && (denied.Code == oops.CodeForbidden || denied.Code == oops.CodeNotFound) {
			return nil, err
		}
		return nil, oops.E(oops.CodeUnavailable, err, "could not verify the approved server; retry shortly").LogError(ctx, logger)
	}
	return s.executeProxiedMemberTool(ctx, logger, gate, member, req, toolName, arguments, meta)
}

// memberCatalog is one member's permission-filtered tool inventory.
type memberCatalog struct {
	entries    []*toolListEntry
	byName     map[string]*toolListEntry
	incomplete bool
}

// marshalMetaToolCallResult wraps structuredContent in the meta MCP's
// tools/call envelope with a mirroring text chunk.
func marshalMetaToolCallResult(ctx context.Context, logger *slog.Logger, id mcpjsonrpc.ID, structured []byte) (json.RawMessage, error) {
	chunk, err := json.Marshal(contentChunk[string, json.RawMessage]{
		Type:     "text",
		MimeType: nil,
		Text:     string(structured),
		Data:     nil,
		Meta:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize tool result content").LogError(ctx, logger)
	}
	bs, err := json.Marshal(&result[toolCallResult]{
		ID: id,
		Result: toolCallResult{
			Content:           []json.RawMessage{chunk},
			StructuredContent: structured,
			IsError:           false,
		},
		serverIdentity: serverInfoMetaServer,
		cacheHints:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tools/call response").LogError(ctx, logger)
	}
	return bs, nil
}

// marshalMetaMemberFailure is marshalMetaToolError for a member failure that
// records, for Code Mode, whether connecting the member is its remedy.
func marshalMetaMemberFailure(ctx context.Context, logger *slog.Logger, id mcpjsonrpc.ID, err error, message string) (json.RawMessage, error) {
	if invocation, ok := ctx.Value(metaCodeInvocationKey{}).(*metaCodeInvocation); ok {
		_, credential := errors.AsType[memberCredentialError](err)
		_, stale := errors.AsType[memberStaleChoiceError](err)
		invocation.credential = credential && !stale
		invocation.rejected = invocation.rejected || stale
	}
	return marshalMetaToolError(ctx, logger, id, message)
}

// marshalMetaToolError emits a member-scoped failure as an isError tool
// result rather than a protocol error.
func marshalMetaToolError(ctx context.Context, logger *slog.Logger, id mcpjsonrpc.ID, message string) (json.RawMessage, error) {
	if invocation, ok := ctx.Value(metaCodeInvocationKey{}).(*metaCodeInvocation); ok {
		invocation.message = message
		return nil, invocation.failure()
	}
	return marshalToolErrorResult(ctx, logger, id, message, serverInfoMetaServer)
}

// marshalToolErrorResult serializes a tools/call result with isError set and
// one text block, under the given server identity.
func marshalToolErrorResult(ctx context.Context, logger *slog.Logger, id mcpjsonrpc.ID, message string, identity serverInfo) (json.RawMessage, error) {
	chunk, err := json.Marshal(contentChunk[string, json.RawMessage]{
		Type:     "text",
		MimeType: nil,
		Text:     message,
		Data:     nil,
		Meta:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize tool error content").LogError(ctx, logger)
	}
	bs, err := json.Marshal(&result[toolCallResult]{
		ID: id,
		Result: toolCallResult{
			Content:           []json.RawMessage{chunk},
			StructuredContent: nil,
			IsError:           true,
		},
		serverIdentity: identity,
		cacheHints:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tools/call response").LogError(ctx, logger)
	}
	return bs, nil
}

// hostedMemberRefusal is the tool error for a call naming one of a stored
// gateway's hosted members, which gateways never serve.
func hostedMemberRefusal(gate *metaGateContext, slug string) (string, bool) {
	if !slices.Contains(gate.hostedMemberSlugs, slug) {
		return "", false
	}
	return fmt.Sprintf("server %q is hosted by Speakeasy, and hosted servers can't be used through a gateway. Connect to the server's own MCP URL instead.", slug), true
}

// unknownMetaMemberError says when the server is one the caller hid, so the
// model can point them at the connect page instead of calling it missing.
func unknownMetaMemberError(ctx context.Context, logger *slog.Logger, gate *metaGateContext, slug string) error {
	if _, hidden := findMetaMember(gate.hiddenMembers, slug); hidden {
		if gate.connectURL != "" {
			return oops.E(oops.CodeNotFound, nil, "server %q is hidden by the user; they can show it again at %s", slug, gate.connectURL).LogWarn(ctx, logger)
		}
		return oops.E(oops.CodeNotFound, nil, "server %q is hidden by the user", slug).LogWarn(ctx, logger)
	}
	return oops.E(oops.CodeNotFound, nil, "unknown server %q; call list_servers to see available servers", slug).LogWarn(ctx, logger)
}
