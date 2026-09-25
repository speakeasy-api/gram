package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// GatewayInventory is shared by dashboard review and consent. It reads current
// permissions and credentials; it never executes a member tool.
func (s *Service) GatewayInventory(ctx context.Context, gatewayID, projectID uuid.UUID, subject urn.SessionSubject) (*toolfilter.FrozenToolset, error) {
	server, err := metamcprepo.New(s.db).GetMetaMCPServerByIDAndProjectID(ctx, metamcprepo.GetMetaMCPServerByIDAndProjectIDParams{ID: gatewayID, ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("load gateway inventory: %w", err)
	}
	ctx, err = s.authz.PrepareContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("prepare gateway review: %w", err)
	}
	if server.Visibility != "public" {
		if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, gatewayID.String(), projectID.String())); err != nil {
			return nil, fmt.Errorf("authorize gateway review: %w", err)
		}
	}
	tokens, err := s.remoteChallengeMgr.ResolveAvailableAccessTokens(ctx, projectID, server.OrganizationID, server.UserSessionIssuerID.UUID, subject)
	if err != nil {
		return nil, fmt.Errorf("load review credentials: %w", err)
	}
	gate := &metaGateContext{projectID: projectID, metaServerID: gatewayID, agentID: uuid.Nil, organizationID: server.OrganizationID, tokens: tokens, toolSelection: nil, frozen: nil, discoveryMode: metamcp.DiscoveryModeDirect, authenticated: false, sessionID: uuid.NewString(), chatID: "", userID: "", externalUserID: "", apiKeyID: "", protocolVersion: mcpversions.Resolution{Declared: "", InEffect: metaMemberUpstreamProtocolVersion}}
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil {
		gate.authenticated = authCtx.ActiveOrganizationID == server.OrganizationID
		gate.userID, gate.externalUserID, gate.apiKeyID = authCtx.UserID, authCtx.ExternalUserID, authCtx.APIKeyID
	}
	ctx, members, err := s.resolveMetaMemberSnapshot(ctx, s.logger, gatewayID, projectID)
	if err != nil {
		return nil, err
	}
	return s.captureGatewayToolset(ctx, s.logger, gate, members)
}

func memberRouting(member metaMember, upstream string) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s/%s/%s/%s", member.projectID, member.toolsetID.UUID, member.remoteServerID.UUID, member.tunneledServerID.UUID, member.environmentID.UUID, member.toolVariationsGroupID.UUID, member.remoteSessionIssuerID.UUID, upstream)
}

func hostedMemberRouting(member metaMember, inputs *mcpInputs) string {
	variation := ""
	if inputs.toolVariationsGroupID != nil {
		variation = inputs.toolVariationsGroupID.String()
	}
	return memberRouting(member, "") + "/" + inputs.environment + "/" + variation
}

type frozenHostedExecutionKey struct{}
type frozenHostedExecution struct {
	approved *toolfilter.FrozenToolset
	member   metaMember
	routing  string
}

// Validate the exact definition and URN that the hosted dispatcher resolved.
// Environment and variation are pinned in its inputs before this check.
func validateFrozenHostedTool(ctx context.Context, tool *types.Tool, plan *gateway.ToolCallPlan) error {
	check, ok := ctx.Value(frozenHostedExecutionKey{}).(frozenHostedExecution)
	if !ok {
		return nil
	}
	base, err := conv.ToBaseTool(tool)
	if err != nil || plan == nil || plan.Descriptor == nil || base.ID != plan.Descriptor.ID {
		return oops.E(oops.CodeForbidden, nil, "tool execution changed during validation; review this connection")
	}
	entry := toolToListEntry(tool)
	if entry != nil {
		current, err := frozenMemberTool(check.member, check.routing, entry)
		if err != nil {
			return err
		}
		if check.approved.Allows(current) {
			return nil
		}
	}
	return oops.E(oops.CodeForbidden, nil, "tool is not in the approved frozen toolset or has changed; review this connection")
}

func frozenMemberTool(member metaMember, routing string, entry *toolListEntry) (toolfilter.FrozenTool, error) {
	if member.backend == metaMemberBackendHosted && entry.routingIdentity == "" {
		return toolfilter.FrozenTool{}, fmt.Errorf("hosted tool has no stable routing identity")
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return toolfilter.FrozenTool{}, fmt.Errorf("encode reviewed definition: %w", err)
	}
	tool, err := toolfilter.NewFrozenTool(metamcp.QualifyName(member.slug, entry.Name), member.serverID, routing+"/"+entry.routingIdentity, raw)
	if err != nil {
		return toolfilter.FrozenTool{}, fmt.Errorf("fingerprint member tool: %w", err)
	}
	return tool, nil
}

func (s *Service) captureGatewayToolset(ctx context.Context, logger *slog.Logger, gate *metaGateContext, members []metaMember) (*toolfilter.FrozenToolset, error) {
	snapshot := &toolfilter.FrozenToolset{Tools: make([]toolfilter.FrozenTool, 0)}
	names := map[string]bool{}
	size := 0
	for _, member := range members {
		if gate.frozen != nil && !frozenIncludesMember(gate.frozen, member.serverID) {
			continue
		}
		catalog, err := s.describeMetaMember(ctx, logger, gate, member)
		if err != nil {
			var denied *oops.ShareableError
			if errors.As(err, &denied) && (denied.Code == oops.CodeNotFound || denied.Code == oops.CodeForbidden) {
				continue
			}
			return nil, oops.E(oops.CodeUnavailable, err, "gateway tool inventory is incomplete; try again").LogWarn(ctx, logger)
		}
		if catalog.incomplete {
			return nil, oops.E(oops.CodeUnavailable, nil, "gateway tool inventory contains invalid definitions; try again").LogWarn(ctx, logger)
		}
		for _, entry := range catalog.entries {
			tool, err := frozenMemberTool(member, catalog.routingIdentity, entry)
			if err != nil {
				return nil, err
			}
			if names[tool.Name] {
				return nil, oops.E(oops.CodeConflict, nil, "gateway contains an ambiguous tool name")
			}
			names[tool.Name] = true
			snapshot.Tools = append(snapshot.Tools, tool)
			size += len(tool.Definition)
			if len(snapshot.Tools) > 10000 || size > 16<<20 {
				return nil, oops.E(oops.CodeRequestTooLarge, nil, "gateway inventory exceeds its review limit")
			}
		}
	}
	return snapshot, nil
}

func (s *Service) filterFrozenMemberCatalog(ctx context.Context, gate *metaGateContext, member metaMember, catalog *memberCatalog) (*memberCatalog, error) {
	if gate.frozen == nil {
		return catalog, nil
	}
	filtered := &memberCatalog{entries: make([]*toolListEntry, 0), byName: map[string]*toolListEntry{}, routingIdentity: catalog.routingIdentity, incomplete: catalog.incomplete}
	for _, entry := range catalog.entries {
		current, err := frozenMemberTool(member, catalog.routingIdentity, entry)
		if err != nil {
			return nil, err
		}
		if gate.frozen.Allows(current) {
			filtered.entries = append(filtered.entries, entry)
			filtered.byName[entry.Name] = entry
		}
	}
	return filtered, nil
}

func frozenIncludesMember(snapshot *toolfilter.FrozenToolset, id uuid.UUID) bool {
	for _, tool := range snapshot.Tools {
		if tool.MemberID == id {
			return true
		}
	}
	return false
}
