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
	"maps"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/hostingstate"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
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
		return nil, unknownMetaMemberError(ctx, logger, gate, args.Server)
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

	return s.executeMetaMemberTool(ctx, logger, gate, members, req, qualified, arguments, meta)
}

// executeMetaMemberTool is shared by Direct calls and Progressive execute_tool.
func (s *Service) executeMetaMemberTool(ctx context.Context, logger *slog.Logger, gate *metaGateContext, members []metaMember, req *rawRequest, qualified string, arguments json.RawMessage, meta *mcprequests.WireMeta) (json.RawMessage, error) {
	serverSlug, toolName, err := metamcp.SplitQualifiedName(qualified)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid tool name: must be of the form serverslug--toolname").LogWarn(ctx, logger)
	}
	if gate.catalog != nil && gate.catalogRef.Mode == toolfilter.CatalogModeCached {
		found := false
		for _, tool := range gate.catalog.Tools {
			if tool.Name == qualified {
				found = true
				break
			}
		}
		if !found {
			return marshalMetaToolError(ctx, logger, req.ID, remotemcp.ToolNotApprovedMessage)
		}
	}
	member, ok := findMetaMember(members, serverSlug)
	if !ok {
		return nil, unknownMetaMemberError(ctx, logger, gate, serverSlug)
	}
	if err := s.validateCatalogMember(ctx, gate, member); err != nil {
		if memberErr, ok := errors.AsType[*metaMemberError](err); ok {
			return marshalMetaToolError(ctx, logger, req.ID, memberErr.message)
		}
		if denied, ok := errors.AsType[*oops.ShareableError](err); ok && (denied.Code == oops.CodeForbidden || denied.Code == oops.CodeNotFound) {
			return nil, err
		}
		return nil, oops.E(oops.CodeUnavailable, err, "could not verify the approved server; retry shortly").LogError(ctx, logger)
	}
	if member.backend != metaMemberBackendHosted {
		return s.executeProxiedMemberTool(ctx, logger, gate, member, req, toolName, arguments, meta)
	}

	toolset, hosting, inputs, err := s.buildMemberDispatch(ctx, logger, gate, member)
	if err != nil {
		if memberErr, ok := errors.AsType[*metaMemberError](err); ok {
			return marshalMetaToolError(ctx, logger, req.ID, memberErr.message)
		}
		return nil, err
	}

	// Pre-dispatch security check so an unsatisfied member surfaces as a
	// member-scoped result (as in serveToolsetResolved). Members keep their
	// toolset's hosting publicness.
	satisfied, err := s.checkToolsetSecurity(ctx, toolset, hosting.Public, inputs)
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
		Method:  mcpversions.MethodToolsCall,
		Params:  params,
	}

	// The authoritative route fronts a meta MCP, not the hosted member's
	// mcp_server. Meta resources are outside the mcp_tool_execution contract,
	// so do not report the synthetic member dispatch as hosted coverage.
	body, err := handleToolsCall(ctx, logger, s.metrics, nil, s.authz, s.guardianPolicy, s.db, s.env,
		inputs, syntheticReq, s.toolProxy, s.billingTracker, s.billingRepository, &s.toolsetCache,
		s.telemLogger, s.toolCallLogger, s.vectorToolStore, s.mcpMetadataRepo, s.auditLogger,
		s.platformExtras, s.sessionClientInfo, s.scanEvaluator)
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
			// An unknown tool or invalid call refused before dispatch is one
			// the program can correct.
			if invocation, ok := ctx.Value(metaCodeInvocationKey{}).(*metaCodeInvocation); ok && !invocation.dispatched &&
				(shareable.Code == oops.CodeNotFound || shareable.Code == oops.CodeInvalid) {
				invocation.rejected = true
			}
			return marshalMetaToolError(ctx, logger, req.ID,
				fmt.Sprintf("server %q failed to execute %q: %s", member.slug, toolName, deepest.Error()))
		}
		return nil, err
	}
	return body, nil
}

// memberCatalog is one member's permission-filtered tool inventory.
type memberCatalog struct {
	entries    []*toolListEntry
	byName     map[string]*toolListEntry
	incomplete bool
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
	toolset, hosting, _, err := s.loadMemberToolset(ctx, logger, gate, member)
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

	catalog := &memberCatalog{entries: nil, byName: map[string]*toolListEntry{}, incomplete: false}
	duplicates := map[string]bool{}
	for _, tool := range described.Tools {
		// External-MCP passthrough tools are excluded from the meta MCP
		// catalog (toolToListEntry returns nil); they belong to the
		// proxied-member runtime.
		if tool != nil && conv.IsProxyTool(tool) {
			continue
		}
		entry := toolToListEntry(tool)
		if entry == nil || entry.Name == "" {
			catalog.incomplete = true
			continue
		}
		if _, exists := catalog.byName[entry.Name]; exists {
			catalog.incomplete = true
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
	if gate.authenticated && !hosting.Public {
		allowed, err := filterToolsByMCPConnect(ctx, logger, s.authz, toolset.ID.String(), toolset.ProjectID.String(), slices.Collect(maps.Values(catalog.byName)))
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, &metaMemberError{message: fmt.Sprintf("server %q did not answer in time", member.slug)}
		}
		if err != nil {
			return nil, err
		}
		catalog.byName = make(map[string]*toolListEntry, len(allowed))
		for _, entry := range allowed {
			catalog.byName[entry.Name] = entry
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
) (*toolsets_repo.Toolset, hostingstate.Hosting, *mcpInputs, error) {
	toolset, hosting, projectID, err := s.loadMemberToolset(ctx, logger, gate, member)
	if err != nil {
		return nil, hostingstate.NotHosted(), nil, err
	}

	// A hosted member records no upstream resource, so its credential is the
	// token map entry keyed by its own derived remote_session_issuer: a token
	// consented for a specific member must never reach another member's
	// tools. No entry degrades to no token rather than an error, so partially
	// connected meta sessions keep hosted members callable.
	tokens, err := s.resolveMemberTokens(ctx, gate, member)
	if err != nil {
		return nil, hostingstate.NotHosted(), nil, err
	}
	hostedTokens, err := hostedMemberTokens(tokens, member)
	if err != nil {
		return nil, hostingstate.NotHosted(), nil, err
	}
	tokenInputs, err := appendRemoteSessionTokenInputs(nil, hostedTokens)
	if err != nil {
		return nil, hostingstate.NotHosted(), nil, oops.E(oops.CodeUnexpected, err, "resolve upstream tokens for meta MCP member").LogError(ctx, logger)
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
				return nil, hostingstate.NotHosted(), nil, oops.E(oops.CodeUnexpected, err, "load member environment").LogError(ctx, logger)
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
		organizationID:        toolset.OrganizationID,
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
		mcpEndpointID:         nil,
		attributionServerID:   nil,
		// Meta members keep their toolset-keyed per-tool checks; the meta
		// surface's RBAC model is outside the wrapper-governance cutover.
		// Publicness is the hosting state the member was admitted under.
		wrapperRBACResourceID:    "",
		wrapperIsPublic:          new(hosting.Public),
		metaMcpServerID:          gate.metaServerID.String(),
		clientInfoScope:          metaClientInfoScope(gate.metaServerID),
		skipProxyTools:           true,
		toolsetID:                uuid.NullUUID{UUID: toolset.ID, Valid: true},
		toolsetIsPublic:          new(hosting.Public),
		tags:                     nil,
		protocolVersion:          gate.protocolVersion,
		identityCoverageRecorded: false,
		toolSelection:            nil,
		skillsOverMCP:            nil,
	}
	return toolset, hosting, inputs, nil
}

// hostedMemberTokens narrows a gate's token map to what a hosted member may
// receive: the entry keyed by the member's own derived remote_session_issuer,
// and only when its grant is unqualified. The tunneled arm of
// routeMetaMemberToken applies the same rule. A lone-token fallback would
// forward a sibling's bearer once partial resolution leaves gaps in the map,
// and a resource-qualified token is audience-bound to the remote upstream it
// was consented for; both cases yield no token. A self client whose
// credential could not be obtained yields the member-scoped remedy instead,
// so the member's tools never run without the credential it holds.
func hostedMemberTokens(tokens map[uuid.UUID]remotesessions.UpstreamToken, member metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	if !member.remoteSessionIssuerID.Valid {
		return nil, nil
	}
	entry, ok := tokens[member.remoteSessionIssuerID.UUID]
	if !ok || entry.Resource != "" {
		return nil, nil
	}
	if err := entry.ClientCredentialErr; err != nil {
		return nil, metaMemberClientCredentialError(member, err)
	}
	return map[uuid.UUID]remotesessions.UpstreamToken{member.remoteSessionIssuerID.UUID: entry}, nil
}

// loadMemberToolset loads the member's toolset row, applying the member
// endpoint's own privacy rule for non-public toolsets.
func (s *Service) loadMemberToolset(
	ctx context.Context,
	logger *slog.Logger,
	gate *metaGateContext,
	member metaMember,
) (*toolsets_repo.Toolset, hostingstate.Hosting, uuid.UUID, error) {
	if !member.toolsetID.Valid {
		return nil, hostingstate.NotHosted(), uuid.Nil, oops.E(oops.CodeUnexpected, nil, "hosted member %q has no toolset", member.slug).LogError(ctx, logger)
	}
	toolset, err := toolsets_repo.New(s.db).GetToolsetByIDAndProject(ctx, toolsets_repo.GetToolsetByIDAndProjectParams{
		ID: member.toolsetID.UUID,
		// The member's own project: an agent gateway's members are not all in
		// one, and a mismatch here reads as "not servable" rather than failing
		// loudly, so it would silently hide members.
		ProjectID: member.projectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, hostingstate.NotHosted(), uuid.Nil, &metaMemberError{message: fmt.Sprintf("server %q is not currently servable", member.slug)}
	case err != nil:
		return nil, hostingstate.NotHosted(), uuid.Nil, oops.E(oops.CodeUnexpected, err, "load member toolset").LogError(ctx, logger)
	}
	hosting, _, err := hostedmcp.LoadServerState(ctx, s.db, toolset)
	if err != nil {
		return nil, hostingstate.NotHosted(), uuid.Nil, oops.E(oops.CodeUnexpected, err, "load member hosting state").LogError(ctx, logger)
	}
	// Match direct wrapper dispatch: only the canonical wrapper gates on the
	// toolset's hosting state. Independently configured servers use their own
	// visibility, already checked during member admission.
	if member.serverID == toolset.ID && !hosting.Enabled {
		return nil, hostingstate.NotHosted(), uuid.Nil, &metaMemberError{message: fmt.Sprintf("server %q is not currently servable", member.slug)}
	}
	allowed, err := s.memberToolsetConnectAllowed(ctx, s.newMandatoryShadow(ctx, toolset.OrganizationID), mandatorySurfaceGatewayMember, gate.authenticated, &toolset, hosting.Public, gate.publicMembersNeedConnect(ctx))
	if err != nil {
		return nil, hostingstate.NotHosted(), uuid.Nil, oops.E(oops.CodeUnexpected, err, "check toolset-level authz").LogError(ctx, logger)
	}
	if !allowed {
		// Unauthorized reads as nonexistent (as in ServeToolsetResolved).
		return nil, hostingstate.NotHosted(), uuid.Nil, oops.E(oops.CodeNotFound, nil, "unknown server %q; call list_servers to see available servers", member.slug).LogWarn(ctx, logger)
	}
	return &toolset, hosting, toolset.ProjectID, nil
}

// memberToolsetConnectAllowed applies a non-public toolset's own mcp:connect
// gate, matching ServeToolsetResolved's connection check. connectPublic
// applies it to a public toolset as well, for a delegated caller at a stored
// gateway. Grants context was prepared when the member snapshot was resolved.
func (s *Service) memberToolsetConnectAllowed(ctx context.Context, shadow *mandatoryShadow, shadowSurface string, authenticated bool, toolset *toolsets_repo.Toolset, public, connectPublic bool) (bool, error) {
	connect := authz.MCPCheck(authz.ScopeMCPConnect, toolset.ID.String(), toolset.ProjectID.String())
	visibility := mandatoryVisibility(public)
	if public && !connectPublic {
		shadow.recordServer(ctx, shadowSurface, true, visibility, connect)
		return true, nil
	}
	if !authenticated {
		shadow.recordServer(ctx, shadowSurface, false, visibility, connect)
		return false, nil
	}
	err := s.authz.Require(ctx, connect)
	shadow.shadowRequireServer(ctx, shadowSurface, err, visibility, connect)
	if err != nil {
		if oopsErr, ok := errors.AsType[*oops.ShareableError](err); ok && oopsErr.Code == oops.CodeForbidden {
			return false, nil
		}
		return false, fmt.Errorf("check toolset-level authz: %w", err)
	}
	return true, nil
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
