package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// implicitGatewayPlatformSlugs is the fixed, ordered set of built-in toolsets
// eligible to appear as synthetic Project Gateway members. Research tools are
// deliberately excluded.
var implicitGatewayPlatformSlugs = []string{
	platformtools.AssistantsPlatformToolsetSlug,
	platformtools.PlatformMCPReadToolsetSlug,
	platformtools.SpeakeasySlackPlatformToolsetSlug,
}

// platformGatewayCandidates produces synthetic Project Gateway candidates for
// built-in platform toolsets. Built-ins are private members addressed by
// PlatformToolsetID(slug) and appear only for agent/workload callers whose
// policy grants mcp:connect on that built-in and who satisfy the existing
// managed-only rules.
func (s *Service) platformGatewayCandidates(ctx context.Context, projectID uuid.UUID) ([]metaMemberCandidate, error) {
	if len(s.platformToolsets) == 0 || projectID == uuid.Nil {
		return nil, nil
	}
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	if !ok || (actor.Type != urn.PrincipalTypeAgent && actor.Type != urn.PrincipalTypeWorkload) {
		return nil, nil
	}
	scopedCtx := withMemberProjectAuthContext(ctx, projectID)
	authCtx, ok := contextvalues.GetAuthContext(scopedCtx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, nil
	}

	candidates := make([]metaMemberCandidate, 0, len(implicitGatewayPlatformSlugs))
	for _, slug := range implicitGatewayPlatformSlugs {
		if _, registered := s.platformToolsets[slug]; !registered {
			continue
		}
		if err := s.authorizePlatformToolset(scopedCtx, slug, authCtx); err != nil {
			if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && shareable.Code == oops.CodeNotFound {
				continue
			}
			return nil, err
		}
		candidates = append(candidates, metaMemberCandidate{
			serverID:                   platformtools.PlatformToolsetID(slug),
			projectID:                  projectID,
			projectSlug:                "",
			slug:                       slug,
			name:                       platformtools.PlatformToolsetName(slug),
			sortOrder:                  0,
			visibility:                 mcpservers.VisibilityPrivate,
			toolsetID:                  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			remoteServerID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			tunneledServerID:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			platformToolsetSlug:        slug,
			environmentID:              uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			toolVariationsGroupID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			upstreamIssuer:             upstreamIssuerKeys{column: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, association: associationUpstreamIssuer{Provider: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Providers: 0}, known: false},
			tunneledResourceIdentifier: "",
			feedbackToolEnabled:        false,
		})
	}
	return candidates, nil
}

// describePlatformMember builds the permission-filtered tool catalog of a
// synthetic built-in Project Gateway member.
func (s *Service) describePlatformMember(
	ctx context.Context,
	logger *slog.Logger,
	gate *metaGateContext,
	member metaMember,
) (*memberCatalog, error) {
	toolset, ok := s.platformToolsets[member.platformToolsetSlug]
	if !ok {
		return nil, oops.E(oops.CodeNotFound, nil, "platform toolset not found")
	}
	origCtx := withMemberProjectAuthContext(ctx, member.projectID)
	origAuthCtx, ok := contextvalues.GetAuthContext(origCtx)
	if !ok || origAuthCtx == nil || origAuthCtx.ProjectID == nil {
		return nil, oops.E(oops.CodeUnauthorized, nil, "no project auth context").LogError(ctx, logger)
	}
	if err := s.authorizePlatformToolset(origCtx, member.platformToolsetSlug, origAuthCtx); err != nil {
		return nil, err
	}

	actingCtx := origCtx
	if toolset.ActingContext != nil {
		var err error
		actingCtx, err = toolset.ActingContext(origCtx)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "prepare platform toolset request").LogError(ctx, logger)
		}
	}
	actingAuthCtx, ok := contextvalues.GetAuthContext(actingCtx)
	if !ok || actingAuthCtx == nil || actingAuthCtx.ProjectID == nil {
		return nil, oops.E(oops.CodeUnauthorized, nil, "no project auth context").LogError(ctx, logger)
	}

	featureCache := map[string]bool{}
	grantedScopes := s.grantedPlatformToolScopes(actingCtx, actingAuthCtx, toolset.Tools)

	entries := make([]*toolListEntry, 0, len(toolset.Tools))
	for _, extra := range toolset.Tools {
		if extra.Executor == nil {
			continue
		}
		if !s.platformToolFeatureAvailable(actingCtx, actingAuthCtx.ActiveOrganizationID, extra.RequiredFeature, featureCache) {
			continue
		}
		if extra.RequiredScope != "" && !grantedScopes[extra.RequiredScope] {
			continue
		}
		entry := toolToListEntry(platformtools.DescriptorForContext(actingCtx, extra.Executor.Descriptor()).ToTool(*actingAuthCtx.ProjectID))
		if entry != nil && entry.Name != "" {
			entries = append(entries, entry)
		}
	}

	if gate.authenticated {
		var err error
		entries, err = filterToolsByMCPConnect(origCtx, logger, s.authz, member.serverID.String(), member.projectID.String(), entries)
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, memberDiscoveryError{metaMemberError: &metaMemberError{message: fmt.Sprintf("server %q did not answer in time", member.slug)}, status: 0, retryable: false, retryAfter: 0}
		}
		if err != nil {
			return nil, err
		}
	}

	catalog := &memberCatalog{
		entries:      entries,
		byName:       make(map[string]*toolListEntry, len(entries)),
		local:        nil,
		incomplete:   false,
		instructions: "",
	}
	for _, entry := range entries {
		catalog.byName[entry.Name] = entry
	}
	return catalog, nil
}

// executePlatformMemberTool dispatches a gateway tool call to an implicit
// built-in platform toolset member.
func (s *Service) executePlatformMemberTool(
	ctx context.Context,
	logger *slog.Logger,
	gate *metaGateContext,
	member metaMember,
	req *rawRequest,
	toolName string,
	arguments json.RawMessage,
	meta *mcprequests.WireMeta,
	attempt *metaToolCallAttempt,
) (json.RawMessage, error) {
	toolset, ok := s.platformToolsets[member.platformToolsetSlug]
	if !ok {
		message := fmt.Sprintf("server %q failed to execute %q: platform toolset not found", member.slug, toolName)
		attempt.fail(http.StatusNotFound, message)
		return marshalMetaToolError(ctx, logger, req.ID, message)
	}
	origCtx := withMemberProjectAuthContext(ctx, member.projectID)
	origAuthCtx, ok := contextvalues.GetAuthContext(origCtx)
	if !ok || origAuthCtx == nil || origAuthCtx.ProjectID == nil {
		return nil, oops.E(oops.CodeUnauthorized, nil, "no project auth context").LogError(ctx, logger)
	}
	if err := s.authorizePlatformToolset(origCtx, member.platformToolsetSlug, origAuthCtx); err != nil {
		if shareable, ok := errors.AsType[*oops.ShareableError](err); ok &&
			shareable.Code != oops.CodeUnexpected && shareable.Code != oops.CodeInvariantViolation {
			message := fmt.Sprintf("server %q failed to execute %q: %s", member.slug, toolName, oops.Innermost(shareable).Error())
			attempt.fail(shareable.HTTPStatus(ctx), message)
			return marshalMetaToolError(ctx, logger, req.ID, message)
		}
		return nil, err
	}

	var matched platformtools.ExternalTool
	var found bool
	for _, extra := range toolset.Tools {
		if extra.Executor != nil && extra.Executor.Descriptor().Name == toolName {
			matched = extra
			found = true
			break
		}
	}
	if !found {
		if invocation, ok := ctx.Value(metaCodeInvocationKey{}).(*metaCodeInvocation); ok && !invocation.dispatched {
			invocation.rejected = true
		}
		message := fmt.Sprintf("server %q failed to execute %q: tool not found", member.slug, toolName)
		attempt.fail(http.StatusNotFound, message)
		return marshalMetaToolError(ctx, logger, req.ID, message)
	}

	var disposition string
	if ann := matched.Executor.Descriptor().Annotations; ann != nil {
		disposition, _ = authz.DispositionFromHints(ann.ReadOnlyHint, ann.DestructiveHint, ann.IdempotentHint, ann.OpenWorldHint)
	} else {
		disposition, _ = authz.DispositionFromHints(nil, nil, nil, nil)
	}
	if attempt.dispositions == memberToolReadOnly && disposition != authz.DispositionReadOnly {
		message := readOnlyToolRefusal(toolName)
		attempt.fail(http.StatusForbidden, message)
		return marshalMetaToolError(ctx, logger, req.ID, message)
	}
	if gate.authenticated {
		if err := s.authz.Require(origCtx, authz.MCPServer(authz.MCPServerIDOf(member.serverID), authz.ProjectIDOf(member.projectID)).ToolCall(toolName, disposition)); err != nil {
			if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && shareable.Code == oops.CodeForbidden {
				message := fmt.Sprintf("server %q failed to execute %q: %s", member.slug, toolName, oops.Innermost(shareable).Error())
				attempt.fail(http.StatusForbidden, message)
				return marshalMetaToolError(ctx, logger, req.ID, message)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "check platform member tool authz").LogError(ctx, logger)
		}
	}

	actingCtx := origCtx
	if toolset.ActingContext != nil {
		var err error
		actingCtx, err = toolset.ActingContext(origCtx)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "prepare platform toolset request").LogError(ctx, logger)
		}
	}
	actingAuthCtx, ok := contextvalues.GetAuthContext(actingCtx)
	if !ok || actingAuthCtx == nil || actingAuthCtx.ProjectID == nil {
		return nil, oops.E(oops.CodeUnauthorized, nil, "no project auth context").LogError(ctx, logger)
	}

	attempt.toolsetSlug = toolset.Slug
	params, err := json.Marshal(toolsCallParams{
		Name:      toolName,
		Arguments: arguments,
		Meta:      meta,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "marshal platform member tool call params").LogError(ctx, logger)
	}
	syntheticReq := &rawRequest{
		JSONRPC: "2.0",
		ID:      req.ID,
		Method:  mcpversions.MethodToolsCall,
		Params:  params,
	}

	body, err := s.callPlatformToolsetTool(actingCtx, actingAuthCtx, toolset, syntheticReq, gate.chatID, platformToolCallOptions{
		metaServerID:   uuid.NullUUID{UUID: gate.metaServerID, Valid: gate.metaServerID != uuid.Nil},
		serverID:       uuid.NullUUID{UUID: member.serverID, Valid: member.serverID != uuid.Nil},
		serverIdentity: serverInfoMetaServer,
		attempt:        attempt,
	})
	if err != nil {
		if shareable, ok := errors.AsType[*oops.ShareableError](err); ok &&
			shareable.Code != oops.CodeUnexpected && shareable.Code != oops.CodeInvariantViolation {
			if invocation, ok := ctx.Value(metaCodeInvocationKey{}).(*metaCodeInvocation); ok && !invocation.dispatched &&
				(shareable.Code == oops.CodeNotFound || shareable.Code == oops.CodeInvalid) {
				invocation.rejected = true
			}
			message := fmt.Sprintf("server %q failed to execute %q: %s", member.slug, toolName, oops.Innermost(shareable).Error())
			attempt.fail(shareable.HTTPStatus(ctx), message)
			return marshalMetaToolError(ctx, logger, req.ID, message)
		}
		return nil, err
	}
	s.recordMemberServed(ctx, gate, member, uuid.Nil)
	return body, nil
}
