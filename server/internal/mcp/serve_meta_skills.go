// Skills over MCP on gateways: a stored gateway and the organization gateway
// serve the skills linked to them plus the skills linked to every member
// server the caller reaches through them, behind the skills-over-mcp flag.

package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

// metaSkillsEnabled reports whether the gateway serves the Skills extension
// to this request. Agent gateways serve no skills: links cannot name them,
// and their members are what an agent key reaches, not a signed-in person.
func (s *Service) metaSkillsEnabled(ctx context.Context, gate *metaGateContext) bool {
	if gate.agentID != uuid.Nil {
		return false
	}
	if gate.skillsOverMCP == nil {
		enabled := s.skillsOverMCPEnabledForOrganization(ctx, gate.organizationID)
		gate.skillsOverMCP = &enabled
	}
	return *gate.skillsOverMCP
}

// metaExtensions lists the extension capability labels the gateway declares
// to this request, which is also the set whose methods dispatch admits.
func (s *Service) metaExtensions(ctx context.Context, gate *metaGateContext) []string {
	if !s.metaSkillsEnabled(ctx, gate) {
		return nil
	}
	return []string{mcpversions.ExtensionSkills}
}

// metaSkillMethod reports whether the request is answered from the skill
// distribution: an extension method, or a read of a skill URI. It does not
// consult the flag; callers check metaSkillsEnabled as well.
func metaSkillMethod(req *rawRequest) bool {
	if mcpversions.KnownExtensionMethod(req.Method) {
		return true
	}
	if req.Method != mcpversions.MethodResourcesRead {
		return false
	}
	_, ok := skillResourceURI(req.Params)
	return ok
}

// metaSkillScope resolves the surface and caller a gateway skills request is
// answered for. The surface is the gateway itself, and its members are the
// servers the caller is served through it: admitted, not hidden, and, for a
// consented connection, approved with at least one selected tool. A skill
// linked to any of them is inherited by the gateway.
//
// The caller rules are the hosted surface's (see hostedSkillScope): only a
// validated user session, or a request no MCP credential stamped, names a
// recipient. Any other caller has no scope, which serves nothing without
// resolving members.
func (s *Service) metaSkillScope(ctx context.Context, logger *slog.Logger, metaServerID uuid.UUID, gate *metaGateContext) (mcpskills.Scope, bool, error) {
	var scope mcpskills.Scope
	if !gate.authenticated || gate.apiKeyID != "" || gate.userID == "" || !actsAsUser(ctx) {
		return scope, false, nil
	}

	target := mcpskills.MetaMCPServerTarget(metaServerID)
	if gate.organizationGateway {
		target = mcpskills.OrganizationGatewayTarget()
	}

	_, members, err := s.resolveGateMemberSnapshot(ctx, logger, gate, metaServerID, gate.projectID)
	if err != nil {
		return scope, false, err
	}
	members = catalogSelectedMembers(gate, members)
	memberIDs := make([]uuid.UUID, 0, len(members))
	for _, member := range members {
		memberIDs = append(memberIDs, member.serverID)
	}

	return mcpskills.Scope{
		OrganizationID:     gate.organizationID,
		Target:             target,
		MemberMCPServerIDs: memberIDs,
		UserID:             gate.userID,
	}, true, nil
}

// listMetaSkills answers skills/list on a gateway.
func (s *Service) listMetaSkills(ctx context.Context, logger *slog.Logger, metaServerID uuid.UUID, gate *metaGateContext, req *rawRequest) (json.RawMessage, error) {
	if _, err := parseSkillsListParams(ctx, logger, req); err != nil {
		return nil, err
	}
	scope, ok, err := s.metaSkillScope(ctx, logger, metaServerID, gate)
	if err != nil {
		return nil, err
	}
	return listServedSkills(ctx, logger, s.skillResolver, scope, ok, req, serverInfoMetaServer)
}

// getMetaSkill answers skills/get on a gateway.
func (s *Service) getMetaSkill(ctx context.Context, logger *slog.Logger, metaServerID uuid.UUID, gate *metaGateContext, req *rawRequest) (json.RawMessage, error) {
	params, err := parseSkillsGetParams(ctx, logger, req)
	if err != nil {
		return nil, err
	}
	scope, ok, err := s.metaSkillScope(ctx, logger, metaServerID, gate)
	if err != nil {
		return nil, err
	}
	return getServedSkill(ctx, logger, s.skillResolver, scope, ok, req, params.URI, serverInfoMetaServer)
}

// readMetaSkill answers resources/read of a served skill's SKILL.md on a
// gateway, recorded and logged as a hosted read is, under the gateway.
func (s *Service) readMetaSkill(ctx context.Context, logger *slog.Logger, metaServerID uuid.UUID, gate *metaGateContext, req *rawRequest, uri string) (json.RawMessage, error) {
	scope, ok, err := s.metaSkillScope(ctx, logger, metaServerID, gate)
	if err != nil {
		return nil, err
	}
	// The read's attribution for the logs: the gateway, its session and the
	// caller. No single member served it, so no server is recorded.
	attribution := &mcpInputs{ //nolint:exhaustruct // only the attribution fields matter here
		projectID:       gate.projectID,
		organizationID:  gate.organizationID,
		authenticated:   gate.authenticated,
		sessionID:       gate.sessionID,
		chatID:          gate.chatID,
		userID:          gate.userID,
		externalUserID:  gate.externalUserID,
		apiKeyID:        gate.apiKeyID,
		metaMcpServerID: gate.metaServerID.String(),
		clientInfoScope: metaClientInfoScope(gate.metaServerID),
		protocolVersion: gate.protocolVersion,
	}
	return readServedSkill(ctx, logger, s.skillResolver, s.metrics, s.telemLogger, s.sessionClientInfo, attribution, scope, ok, req, uri, time.Now(), mcpmetrics.SurfaceMeta, serverInfoMetaServer)
}
