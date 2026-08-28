// Meta MCP drill-down tools over hosted (toolset-backed) members. Execution
// reuses handleToolsCall, so billing, guardian, RBAC, audit, and telemetry
// apply as on direct calls. Proxied members dispatch through their own
// upstream sessions (serve_meta_proxy.go).

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// metaMemberError surfaces as an isError tool result naming only the
// affected member.
type metaMemberError struct {
	message string
}

func (e *metaMemberError) Error() string { return e.message }

// handleMetaDescribeServerCall: one member's catalog as qualified names and
// descriptions, without schemas (describe_tools' job).
func (s *Service) handleMetaDescribeServerCall(
	ctx context.Context,
	logger *slog.Logger,
	gate *metaGateContext,
	members []metaMember,
	req *rawRequest,
	argsRaw json.RawMessage,
) (json.RawMessage, error) {
	var args struct {
		Server string `json:"server"`
	}
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid describe_server arguments").LogWarn(ctx, logger)
		}
	}
	if args.Server == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "server is required").LogWarn(ctx, logger)
	}

	member, ok := findMetaMember(members, args.Server)
	if !ok {
		return nil, oops.E(oops.CodeNotFound, nil, "unknown server %q; call list_servers to see available servers", args.Server).LogWarn(ctx, logger)
	}
	catalog, err := s.describeMetaMember(ctx, logger, gate, member)
	if err != nil {
		if memberErr, ok := errors.AsType[*metaMemberError](err); ok {
			return marshalMetaToolError(ctx, logger, req.ID, memberErr.message)
		}
		return nil, err
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
		return nil, oops.E(oops.CodeUnexpected, err, "serialize describe_server result").LogError(ctx, logger)
	}
	return marshalMetaToolCallResult(ctx, logger, req.ID, structured)
}

// handleMetaDescribeToolsCall: full input schemas for the requested
// qualified names; misses land in not_found rather than being dropped.
func (s *Service) handleMetaDescribeToolsCall(
	ctx context.Context,
	logger *slog.Logger,
	gate *metaGateContext,
	members []metaMember,
	req *rawRequest,
	argsRaw json.RawMessage,
) (json.RawMessage, error) {
	var args struct {
		Tools []string `json:"tools"`
	}
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid describe_tools arguments").LogWarn(ctx, logger)
		}
	}
	if len(args.Tools) == 0 {
		return nil, oops.E(oops.CodeInvalid, nil, "tools is required").LogWarn(ctx, logger)
	}

	// Group by member so each toolset is described once.
	type memberRequest struct {
		member    metaMember
		toolNames []string
	}
	if len(args.Tools) > metamcp.MaxDescribeTools {
		return nil, oops.E(oops.CodeInvalid, nil, "tools accepts at most %d names per call", metamcp.MaxDescribeTools).LogWarn(ctx, logger)
	}

	requestsBySlug := map[string]*memberRequest{}
	order := []string{}
	notFound := []string{}
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
			return nil, oops.E(oops.CodeInvalid, err, "invalid tool name: must be of the form serverslug--toolname").LogWarn(ctx, logger)
		}
		member, ok := findMetaMember(members, serverSlug)
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
	failed := []metamcp.FailedServer{}
	for _, slug := range order {
		mr := requestsBySlug[slug]
		catalog, err := s.describeMetaMember(ctx, logger, gate, mr.member)
		if err != nil {
			// A member outage degrades only that member: its names land in
			// failed while every other member is still described.
			if memberErr, ok := errors.AsType[*metaMemberError](err); ok {
				failed = append(failed, metamcp.FailedServer{Server: mr.member.slug, Message: memberErr.message})
				continue
			}
			return nil, err
		}
		for _, toolName := range mr.toolNames {
			entry, ok := catalog.byName[toolName]
			if !ok {
				notFound = append(notFound, metamcp.QualifyName(mr.member.slug, toolName))
				continue
			}
			described = append(described, metamcp.SchemaTool{
				Name:        metamcp.QualifyName(mr.member.slug, entry.Name),
				Description: entry.Description,
				InputSchema: entry.InputSchema,
				Annotations: entry.Annotations,
			})
		}
	}

	structured, err := json.Marshal(metamcp.DescribeToolsResult{
		Tools:    described,
		NotFound: notFound,
		Failed:   failed,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize describe_tools result").LogError(ctx, logger)
	}
	return marshalMetaToolCallResult(ctx, logger, req.ID, structured)
}

// handleMetaExecuteToolCall routes into the member's hosted dispatch. The
// synthetic tools/call carries the outer request ID, so the dispatch envelope
// answers directly; the result's _meta identity stays the member's.
func (s *Service) handleMetaExecuteToolCall(
	ctx context.Context,
	logger *slog.Logger,
	gate *metaGateContext,
	members []metaMember,
	req *rawRequest,
	argsRaw json.RawMessage,
	meta *mcprequests.WireMeta,
) (json.RawMessage, error) {
	qualified, arguments, err := processExecuteToolCall(ctx, logger, argsRaw)
	if err != nil {
		return nil, err
	}

	serverSlug, toolName, err := metamcp.SplitQualifiedName(qualified)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid tool name: must be of the form serverslug--toolname").LogWarn(ctx, logger)
	}
	member, ok := findMetaMember(members, serverSlug)
	if !ok {
		return nil, oops.E(oops.CodeNotFound, nil, "unknown server %q; call list_servers to see available servers", serverSlug).LogWarn(ctx, logger)
	}
	if member.backend != metaMemberBackendHosted {
		return s.executeProxiedMemberTool(ctx, logger, gate, member, req, toolName, arguments, meta)
	}

	toolset, inputs, err := s.buildMemberDispatch(ctx, logger, gate, member)
	if err != nil {
		if memberErr, ok := errors.AsType[*metaMemberError](err); ok {
			return marshalMetaToolError(ctx, logger, req.ID, memberErr.message)
		}
		return nil, err
	}

	// Pre-dispatch security check so an unsatisfied member surfaces as a
	// member-scoped result (as in ServeToolsetResolved).
	satisfied, err := s.checkToolsetSecurity(ctx, toolset, inputs)
	if err != nil {
		return nil, err
	}
	if !satisfied {
		return marshalMetaToolError(ctx, logger, req.ID,
			fmt.Sprintf("server %q requires authentication that this meta MCP session does not satisfy", member.slug))
	}

	// The outer request's _meta rides along: the member dispatch serves the
	// same client, so its telemetry (client identity, protocol declaration)
	// must see the metadata a direct call would carry.
	params, err := json.Marshal(toolsCallParams{
		Name:      toolName,
		Arguments: arguments,
		Meta:      meta,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "marshal member tool call params").LogError(ctx, logger)
	}
	syntheticReq := &rawRequest{
		JSONRPC: "2.0",
		ID:      req.ID,
		Method:  "tools/call",
		Params:  params,
	}

	body, err := handleToolsCall(ctx, logger, s.metrics, s.authz, s.guardianPolicy, s.db, s.env,
		inputs, syntheticReq, s.toolProxy, s.billingTracker, s.billingRepository, &s.toolsetCache,
		s.telemLogger, s.vectorToolStore, s.temporal, s.mcpMetadataRepo, s.auditLogger,
		s.platformExtras, s.sessionClientInfo)
	if err != nil {
		// A member's execution failure must degrade that member, not the
		// call: on a multi-member gateway an aborted JSON-RPC call reads as
		// a gateway fault. Internal faults — unexpected errors and server
		// invariant violations — keep the error envelope. The innermost
		// shareable message carries the actionable cause (every
		// ShareableError public is written to be client-safe).
		if shareable, ok := errors.AsType[*oops.ShareableError](err); ok &&
			shareable.Code != oops.CodeUnexpected && shareable.Code != oops.CodeInvariantViolation {
			deepest := shareable
			for inner := errors.Unwrap(error(deepest)); inner != nil; inner = errors.Unwrap(inner) {
				if innerShareable, ok := errors.AsType[*oops.ShareableError](inner); ok {
					deepest = innerShareable
				}
			}
			return marshalMetaToolError(ctx, logger, req.ID,
				fmt.Sprintf("server %q failed to execute %q: %s", member.slug, toolName, deepest.Error()))
		}
		return nil, err
	}
	return body, nil
}

// memberCatalog is one hosted member's described tool inventory.
type memberCatalog struct {
	entries []*toolListEntry
	byName  map[string]*toolListEntry
}

// describeMemberToolset loads the member's catalog via the same model view
// its own tools/list uses. Duplicate names are dropped from byName so they
// fail deterministically.
func (s *Service) describeMemberToolset(
	ctx context.Context,
	logger *slog.Logger,
	gate *metaGateContext,
	member metaMember,
) (*memberCatalog, error) {
	toolset, _, err := s.loadMemberToolset(ctx, logger, gate, member)
	if err != nil {
		return nil, err
	}

	var variationsGroupID *uuid.UUID
	if member.toolVariationsGroupID.Valid {
		id := member.toolVariationsGroupID.UUID
		variationsGroupID = &id
	} else if toolset.ToolVariationsGroupID.Valid {
		id := toolset.ToolVariationsGroupID.UUID
		variationsGroupID = &id
	}

	described, err := mv.DescribeToolset(ctx, logger, s.db, mv.ProjectID(toolset.ProjectID), mv.ToolsetSlug(conv.ToLower(toolset.Slug)), &s.toolsetCache, variationsGroupID, s.platformExtras...)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "describe member toolset").LogError(ctx, logger)
	}

	// The consent-screen tool selection narrows describe exactly as
	// buildMemberDispatch narrows execute, so the two surfaces agree on the
	// session's visible tools (parity with rpc_tools_list.go).
	if gate.toolSelection != nil {
		described.Tools = toolfilter.FilterToolsBySelection(described.Tools, gate.toolSelection)
	}

	catalog := &memberCatalog{entries: nil, byName: map[string]*toolListEntry{}}
	duplicates := map[string]bool{}
	for _, tool := range described.Tools {
		// External-MCP passthrough tools are excluded from the meta MCP
		// catalog (toolToListEntry returns nil); they belong to the
		// proxied-member runtime.
		entry := toolToListEntry(tool)
		if entry == nil {
			continue
		}
		if _, exists := catalog.byName[entry.Name]; exists {
			duplicates[entry.Name] = true
			continue
		}
		catalog.byName[entry.Name] = entry
	}
	// Duplicated names are dropped so describe never presents an ambiguous
	// name; handleToolsCall refuses the same names on the execute side.
	for name := range duplicates {
		delete(catalog.byName, name)
	}

	// Parity with the direct surface's tools/list: private authenticated
	// toolsets filter per-tool mcp:connect with tools/call's dimensions, so
	// describe never discloses a tool the member endpoint would hide.
	if gate.authenticated && !toolset.McpIsPublic {
		for name, entry := range catalog.byName {
			err := s.authz.Require(ctx, authz.MCPToolCallCheck(toolset.ID.String(), authz.MCPToolCallDimensions{
				Tool:        name,
				Disposition: dispositionFromAnnotations(entry.Annotations),
				ProjectID:   toolset.ProjectID.String(),
			}))
			if err != nil {
				var oopsErr *oops.ShareableError
				if errors.As(err, &oopsErr) && oopsErr.Code == oops.CodeForbidden {
					delete(catalog.byName, name)
					continue
				}
				return nil, oops.E(oops.CodeUnexpected, err, "check tool-level authz for describe").LogError(ctx, logger)
			}
		}
	}

	for _, tool := range described.Tools {
		entry := toolToListEntry(tool)
		if entry == nil {
			continue
		}
		if kept, ok := catalog.byName[entry.Name]; ok {
			catalog.entries = append(catalog.entries, kept)
		}
	}
	return catalog, nil
}

// buildMemberDispatch assembles mcpInputs for one member as
// ServeToolsetResolved would, with meta-surface decisions: static mode, no
// header env vars or ?tags=, environment = member environment_id then
// toolset default, tokens from the meta gate.
func (s *Service) buildMemberDispatch(
	ctx context.Context,
	logger *slog.Logger,
	gate *metaGateContext,
	member metaMember,
) (*toolsets_repo.Toolset, *mcpInputs, error) {
	toolset, projectID, err := s.loadMemberToolset(ctx, logger, gate, member)
	if err != nil {
		return nil, nil, err
	}

	// A hosted member records no upstream resource, so only a lone
	// unqualified token can be its credential: a token consented for a
	// specific member must never reach another member's tools, and several
	// tokens are unroutable. Both degrade to no token rather than an error,
	// so multi-credential meta sessions keep hosted members callable.
	tokenInputs, err := appendRemoteSessionTokenInputs(nil, hostedMemberTokens(gate.tokens))
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "resolve upstream tokens for meta MCP member").LogError(ctx, logger)
	}

	var environment string
	if gate.authenticated {
		environment = conv.PtrValOr(conv.FromPGText[string](toolset.DefaultEnvironmentSlug), "")
		if member.environmentID.Valid {
			row, err := environmentsrepo.New(s.db).GetEnvironmentByID(ctx, environmentsrepo.GetEnvironmentByIDParams{
				ID:        member.environmentID.UUID,
				ProjectID: projectID,
			})
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				// Dangling environment: keep the toolset default.
			case err != nil:
				return nil, nil, oops.E(oops.CodeUnexpected, err, "load member environment").LogError(ctx, logger)
			default:
				environment = row.Slug
			}
		}
	}

	var variationsGroupID *uuid.UUID
	if member.toolVariationsGroupID.Valid {
		id := member.toolVariationsGroupID.UUID
		variationsGroupID = &id
	} else if toolset.ToolVariationsGroupID.Valid {
		id := toolset.ToolVariationsGroupID.UUID
		variationsGroupID = &id
	}

	serverID := member.serverID
	inputs := &mcpInputs{
		projectID:             projectID,
		toolset:               toolset.Slug,
		environment:           environment,
		mcpEnvVariables:       nil,
		oauthTokenInputs:      tokenInputs,
		authenticated:         gate.authenticated,
		sessionID:             gate.sessionID,
		chatID:                gate.chatID,
		mode:                  ToolModeStatic,
		userID:                gate.userID,
		externalUserID:        gate.externalUserID,
		apiKeyID:              gate.apiKeyID,
		toolVariationsGroupID: variationsGroupID,
		mcpServerID:           &serverID,
		skipProxyTools:        true,
		tags:                  nil,
		protocolVersion:       gate.protocolVersion,
		toolSelection:         gate.toolSelection,
	}
	return toolset, inputs, nil
}

// hostedMemberTokens narrows a gate's token map to what a hosted member may
// receive: exactly one token with no recorded resource. The tunneled arm of
// routeMetaMemberToken applies the same rule, so on a mixed gateway one
// unqualified credential reaches both; recording synthetic tunnel resources
// (AIM-87 follow-up) is what will split them. A resource-qualified
// token belongs to the member it was consented for, and several tokens are
// unroutable without a scheme-to-issuer mapping; both cases yield no token.
func hostedMemberTokens(tokens map[uuid.UUID]remotesessions.UpstreamToken) map[uuid.UUID]remotesessions.UpstreamToken {
	if len(tokens) != 1 {
		return nil
	}
	for _, entry := range tokens {
		if entry.Resource != "" {
			return nil
		}
	}
	return tokens
}

// loadMemberToolset loads the member's toolset row, applying the member
// endpoint's own privacy rule for non-public toolsets.
func (s *Service) loadMemberToolset(
	ctx context.Context,
	logger *slog.Logger,
	gate *metaGateContext,
	member metaMember,
) (*toolsets_repo.Toolset, uuid.UUID, error) {
	if !member.toolsetID.Valid {
		return nil, uuid.Nil, oops.E(oops.CodeUnexpected, nil, "hosted member %q has no toolset", member.slug).LogError(ctx, logger)
	}
	toolset, err := toolsets_repo.New(s.db).GetToolsetByIDAndProject(ctx, toolsets_repo.GetToolsetByIDAndProjectParams{
		ID:        member.toolsetID.UUID,
		ProjectID: gate.projectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, uuid.Nil, &metaMemberError{message: fmt.Sprintf("server %q is not currently servable", member.slug)}
	case err != nil:
		return nil, uuid.Nil, oops.E(oops.CodeUnexpected, err, "load member toolset").LogError(ctx, logger)
	}
	if !toolset.McpIsPublic && !gate.authenticated {
		// Unauthorized reads as nonexistent (as in ServeToolsetResolved).
		return nil, uuid.Nil, oops.E(oops.CodeNotFound, nil, "unknown server %q; call list_servers to see available servers", member.slug).LogWarn(ctx, logger)
	}
	if !toolset.McpIsPublic && gate.authenticated {
		// Toolset-level mcp:connect gate, matching ServeToolsetResolved's
		// connection check; the meta MCP maps a denial to nonexistent rather
		// than Forbidden, per its unauthorized-reads-as-nonexistent rule.
		// Grants context was prepared in resolveMetaMemberSnapshot.
		if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, toolset.ID.String(), toolset.ProjectID.String())); err != nil {
			var oopsErr *oops.ShareableError
			if errors.As(err, &oopsErr) && oopsErr.Code == oops.CodeForbidden {
				return nil, uuid.Nil, oops.E(oops.CodeNotFound, nil, "unknown server %q; call list_servers to see available servers", member.slug).LogWarn(ctx, logger)
			}
			return nil, uuid.Nil, oops.E(oops.CodeUnexpected, err, "check toolset-level authz").LogError(ctx, logger)
		}
	}
	return &toolset, toolset.ProjectID, nil
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

// marshalMetaToolError emits a member-scoped failure as an isError tool
// result rather than a protocol error.
func marshalMetaToolError(ctx context.Context, logger *slog.Logger, id mcpjsonrpc.ID, message string) (json.RawMessage, error) {
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
		serverIdentity: serverInfoMetaServer,
		cacheHints:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tools/call response").LogError(ctx, logger)
	}
	return bs, nil
}
