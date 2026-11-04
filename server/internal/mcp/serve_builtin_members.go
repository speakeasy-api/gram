package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func builtinCandidate(projectID uuid.UUID, projectSlug string, definition builtinmcp.Definition) metaMemberCandidate {
	//nolint:exhaustruct // Synthetic members have no persisted backend references.
	return metaMemberCandidate{builtinKey: definition.Key, serverID: builtinmcp.ID(projectID, definition.Key), projectID: projectID, projectSlug: projectSlug, slug: "builtin_" + definition.Key, name: definition.Name, visibility: mcpservers.VisibilityPrivate}
}

func (s *Service) storedBuiltinCandidates(ctx context.Context, gatewayID, projectID uuid.UUID) ([]metaMemberCandidate, error) {
	// Built-ins are private members. An anonymous public gateway still serves
	// its public members; absence of a caller hides only these candidates.
	organizationID, err := builtinMemberOrganization(ctx)
	if err != nil {
		return nil, nil
	}
	rows, err := metamcprepo.New(s.db).ListBuiltinMetaMCPMembers(ctx, metamcprepo.ListBuiltinMetaMCPMembersParams{MetaMcpServerID: uuid.NullUUID{UUID: gatewayID, Valid: true}, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}})
	if err != nil {
		return nil, fmt.Errorf("resolve built-in member: %w", err)
	}
	candidates := make([]metaMemberCandidate, 0, len(rows))
	for _, row := range rows {
		definition, ok := builtinmcp.Lookup(row.BuiltinKey)
		if !ok {
			continue
		}
		if err := builtinmcp.EnsureServer(ctx, s.db, organizationID, projectID, definition.Key, uuid.NullUUID{UUID: uuid.Nil, Valid: false}); err != nil {
			return nil, fmt.Errorf("ensure built-in member: %w", err)
		}
		candidate := builtinCandidate(projectID, "", definition)
		candidate.environmentID = row.EnvironmentID
		candidate.sortOrder = row.SortOrder
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func (s *Service) derivedBuiltinCandidates(ctx context.Context, organizationID string, scopeProjectID uuid.UUID) ([]metaMemberCandidate, error) {
	if _, err := builtinMemberOrganization(ctx); err != nil {
		return nil, nil
	}
	// A synthetic member has no endpoint row to apply organization lockdown.
	// A blocked origin makes these candidates unavailable, not the entire
	// gateway consent page. Gateway ingress and consent actions independently
	// enforce lockdown; discovery must not bypass those checks or acquire tools.
	if err := s.enforceAgentGatewayCustomDomainLockdown(ctx, s.logger, organizationID); err != nil {
		var denied *oops.ShareableError
		if errors.As(err, &denied) && denied.Code == oops.CodeForbidden {
			return nil, nil
		}
		return nil, fmt.Errorf("resolve built-in member: %w", err)
	}
	projects, err := projectsrepo.New(s.db).ListProjectsByOrganization(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("resolve built-in member: %w", err)
	}
	candidates := make([]metaMemberCandidate, 0, len(projects))
	for _, project := range projects {
		if scopeProjectID != uuid.Nil && project.ID != scopeProjectID {
			continue
		}
		for _, definition := range builtinmcp.All() {
			if err := builtinmcp.EnsureServer(ctx, s.db, organizationID, project.ID, definition.Key, uuid.NullUUID{UUID: uuid.Nil, Valid: false}); err != nil {
				return nil, fmt.Errorf("ensure built-in member: %w", err)
			}
			candidates = append(candidates, builtinCandidate(project.ID, project.Slug, definition))
		}
	}
	return candidates, nil
}

func (s *Service) describeBuiltinMember(ctx context.Context, gate *metaGateContext, member metaMember) (*memberCatalog, error) {
	definition, ok := builtinmcp.Lookup(member.builtinKey)
	if !ok {
		return nil, oops.C(oops.CodeNotFound)
	}
	tools, err := s.builtinToolset(ctx, definition, member.projectID)
	if err != nil {
		return nil, fmt.Errorf("resolve built-in member: %w", err)
	}
	catalog := &memberCatalog{entries: make([]*toolListEntry, 0, len(tools.Tools)), byName: map[string]*toolListEntry{}, incomplete: false}
	for _, tool := range tools.Tools {
		model := tool.Executor.Descriptor().ToTool(member.projectID)
		entry := toolToListEntry(model)
		if entry == nil {
			continue
		}
		catalog.entries = append(catalog.entries, entry)
		catalog.byName[entry.Name] = entry
	}
	slices.SortFunc(catalog.entries, func(a, b *toolListEntry) int { return strings.Compare(a.Name, b.Name) })
	return catalog, nil
}

func (s *Service) executeBuiltinMember(ctx context.Context, gate *metaGateContext, member metaMember, req *rawRequest, toolName string, arguments json.RawMessage, meta *mcprequests.WireMeta) (json.RawMessage, error) {
	catalog, err := s.describeBuiltinMember(ctx, gate, member)
	if err != nil {
		return nil, fmt.Errorf("resolve built-in member: %w", err)
	}
	if _, ok := catalog.byName[toolName]; !ok {
		return nil, oops.C(oops.CodeNotFound)
	}
	environment, handled, err := s.builtinMemberAuthEnvironment(ctx, gate.organizationID, member)
	if err == nil && !handled {
		environment, err = s.builtinEnvironment(ctx, member.projectID, member.builtinKey, member.environmentID, "", nil)
	}
	if err != nil {
		return marshalMetaToolError(ctx, s.logger, req.ID, fmt.Sprintf("server %q has no usable credential", member.slug))
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID != gate.organizationID {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	bound := *authCtx
	bound.ProjectID = &member.projectID
	bound.ProjectSlug = &member.projectSlug
	ctx = contextvalues.SetAuthContext(ctx, &bound)
	definition, _ := builtinmcp.Lookup(member.builtinKey)
	tools, err := s.builtinToolset(ctx, definition, member.projectID)
	if err != nil {
		return nil, fmt.Errorf("resolve built-in member: %w", err)
	}
	params, err := json.Marshal(toolsCallParams{Name: toolName, Arguments: arguments, Meta: meta})
	if err != nil {
		return nil, fmt.Errorf("resolve built-in member: %w", err)
	}
	forwarded := *req
	forwarded.Params = params
	ctx = s.memberAttributionContext(ctx, s.logger, gate)
	return s.callPlatformToolsetToolWithConfig(ctx, &bound, tools, &forwarded, gate.chatID, &platformCallConfig{environment: environment, serverID: member.serverID.String(), metaServerID: gate.metaServerID.String()})
}

// builtinMemberAuth checks readiness after typed member choices have resolved.
func (s *Service) builtinMemberAuth(ctx context.Context, member metaMember) (bool, bool, error) {
	organizationID, err := builtinMemberOrganization(ctx)
	if err != nil {
		return false, false, err
	}
	_, handled, err := s.builtinMemberAuthEnvironment(ctx, organizationID, member)
	if handled || err != nil {
		return err == nil, false, err
	}
	return s.builtinLegacyMemberAuth(ctx, member)
}

// builtinMemberCapability runs before choices resolve. Enforced built-ins stay
// eligible even without an environment; the selected client decides readiness.
func (s *Service) builtinMemberCapability(ctx context.Context, member metaMember) (bool, bool, error) {
	organizationID, err := builtinMemberOrganization(ctx)
	if err != nil {
		return false, false, err
	}
	mode, err := s.authClientMode(ctx, organizationID)
	if err != nil {
		return false, false, err
	}
	if mode == feature.AuthClientResolverEnforced {
		return true, false, nil
	}
	return s.builtinLegacyMemberAuth(ctx, member)
}

func builtinMemberOrganization(ctx context.Context) (string, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return "", oops.C(oops.CodeUnauthorized)
	}
	return authCtx.ActiveOrganizationID, nil
}

func (s *Service) builtinLegacyMemberAuth(ctx context.Context, member metaMember) (bool, bool, error) {
	_, err := s.builtinEnvironment(ctx, member.projectID, member.builtinKey, member.environmentID, "", nil)
	if err == nil {
		return true, false, nil
	}
	var failure *oops.ShareableError
	if errors.As(err, &failure) && failure.Code == oops.CodeBadRequest {
		return false, false, nil
	}
	return false, false, err
}

// Keep existing ordinary identifiers intact. Synthetic members choose the first
// unused suffix within their project, before cross-project qualification.
func appendBuiltinCandidates(candidates, builtins []metaMemberCandidate) []metaMemberCandidate {
	type identity struct {
		projectID uuid.UUID
		slug      string
	}
	used := make(map[identity]bool, len(candidates)+len(builtins))
	for _, candidate := range candidates {
		used[identity{candidate.projectID, candidate.slug}] = true
	}
	for _, candidate := range builtins {
		base := candidate.slug
		for suffix := 2; used[identity{candidate.projectID, candidate.slug}]; suffix++ {
			candidate.slug = fmt.Sprintf("%s_%d", base, suffix)
		}
		used[identity{candidate.projectID, candidate.slug}] = true
		candidates = append(candidates, candidate)
	}
	return candidates
}
