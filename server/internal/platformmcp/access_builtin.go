package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

// builtinAccessDefinition accepts only the code-owned identity in this exact project.
func builtinAccessDefinition(projectID, serverID uuid.UUID) (builtinmcp.Definition, bool) {
	for _, definition := range builtinmcp.All() {
		if projectID != uuid.Nil && builtinmcp.ID(projectID, definition.Key) == serverID {
			return definition, true
		}
	}
	var empty builtinmcp.Definition
	return empty, false
}

func (s *AccessReadService) getBuiltinAccess(ctx context.Context, principal Principal, projectID, serverID uuid.UUID, definition builtinmcp.Definition) (GetMCPAccessOutput, error) {
	_, err := platformrepo.New(s.db).ResolvePlatformMCPProjectByID(ctx, platformrepo.ResolvePlatformMCPProjectByIDParams{OrganizationID: principal.OrganizationID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return GetMCPAccessOutput{}, ErrAccessMCPNotFound
	}
	if err != nil {
		return GetMCPAccessOutput{}, fmt.Errorf("resolve builtin access project: %w", err)
	}
	roles, err := s.roles.ListRoles(ctx, principal.OrganizationID)
	if err != nil {
		return GetMCPAccessOutput{}, fmt.Errorf("list builtin access roles: %w", err)
	}
	// Built-ins exist before their backing row is materialized. No row means
	// no offered credentials; all other upstream read failures remain errors.
	upstream, err := authclients.ReadServerAuthentication(ctx, s.db, principal.OrganizationID, serverID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return GetMCPAccessOutput{}, fmt.Errorf("read MCP upstream authentication: %w", err)
	}
	now := s.now().UTC()
	output := GetMCPAccessOutput{ProjectID: projectID.String(), MCP: MCPAccessTarget{
		ID: serverID.String(), Name: definition.Name, Backend: "builtin", Visibility: "private", AuthorizationMode: "rbac", AuthorizationSurface: "builtin_endpoint",
		AccessSummary: "Normal MCP connect grants apply to users and machines. Typed auth-client permissions apply when upstream auth enforcement is enabled; credential and owner ceilings and blocks still apply. No assignments or plugin distribution are changed. Tool access is not evaluated here.",
		ToolCatalog:   "unavailable", Tools: []MCPAccessTool{}, ToolsTruncated: false,
	}, Roles: []MCPRoleCoverage{}, SharedAuthClientRoles: []MCPRoleSharedAuthClients{}, ExpiresAt: now.Add(SubjectReferenceTTL).Format(time.RFC3339)}
	for _, role := range roles.Roles {
		grants, unevaluated := roleAuthzGrants(role)
		allowed, err := authz.GrantsAuthorize(grants, authz.MCPCheck(authz.ScopeMCPConnect, serverID.String(), projectID.String()))
		if err != nil {
			return GetMCPAccessOutput{}, fmt.Errorf("evaluate builtin role access: %w", err)
		}
		reference, err := s.references.Encode(principal, subjectKindAccessRole, role.ID, now)
		if err != nil {
			return GetMCPAccessOutput{}, err
		}
		if upstream != nil {
			authGrants, authUnevaluated := roleAuthzGrants(authClientRole(role))
			sharedClients, err := roleSharedAuthClientAccess(authGrants, upstream, projectID.String())
			if err != nil {
				return GetMCPAccessOutput{}, fmt.Errorf("evaluate auth client access for role %q: %w", role.Name, err)
			}
			if anyAuthClientRule(sharedClients) || authUnevaluated {
				output.SharedAuthClientRoles = append(output.SharedAuthClientRoles, MCPRoleSharedAuthClients{
					Name: role.Name, Reference: reference, AuthClients: sharedClients, UnevaluatedGrants: authUnevaluated,
				})
			}
		}
		version, err := accessRoleVersion(s.versionKey, role)
		if err != nil {
			return GetMCPAccessOutput{}, err
		}
		output.Roles = append(output.Roles, MCPRoleCoverage{
			MatchingPlugins: []MCPMatchingPlugin{}, Name: role.Name, Type: accessRoleType(role), MemberCount: NewSubjectCount(int64(role.MemberCount)), Reference: reference, Version: version,
			CanEnterServer: allowed, KnownToolAccess: "not_evaluated", AllowedKnownTools: []string{}, DispositionRules: []string{}, BlockedDispositions: []string{}, UnevaluatedGrants: unevaluated, AssignmentRules: nil, AssignmentEligible: false,
		})
	}
	return output, nil
}
