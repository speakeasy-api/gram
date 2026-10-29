package authz

import (
	"fmt"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/authz/relschema"
)

// The relationship schema for MCP servers. It is the one place that decides
// what configuring a server allows and what using one requires:
//
//   - read is mcp:read, mcp:write or root, minus a matching mcp:blocked_read
//   - edit is mcp:write or root, minus a matching mcp:blocked_write
//   - connect is mcp:connect or root, minus a matching mcp:blocked_connect
//
// Neither read nor edit confers connect: managing a server never grants use
// of it. A matching block withdraws an allow whatever principal holds either
// side; a direct grant does not outrank an inherited block here.
//
// A connection grant names one server. Rows that grant mcp:connect over a
// project or the organization are honored as legacy broad grants until they
// are migrated to server-specific audiences, and say so in every trace.
//
// Every stored scope is either read by this schema or declared out of it, so
// a new scope cannot be skipped by accident: Compile fails at startup.
const (
	relTypeOrganization = "organization"
	relTypeProject      = "project"
	relTypeMCPServer    = "mcp_server"
)

// MCPServerPermission is a permission declared on the mcp_server type.
type MCPServerPermission string

const (
	MCPServerRead    MCPServerPermission = "read"
	MCPServerEdit    MCPServerPermission = "edit"
	MCPServerConnect MCPServerPermission = "connect"
)

// legacyScope is the stored scope whose legacy check a permission replaces,
// used when the schema is not active for an organization and to label
// challenge logs.
func (p MCPServerPermission) legacyScope() (Scope, error) {
	switch p {
	case MCPServerRead:
		return ScopeMCPRead, nil
	case MCPServerEdit:
		return ScopeMCPWrite, nil
	case MCPServerConnect:
		return ScopeMCPConnect, nil
	default:
		return "", fmt.Errorf("unknown mcp server permission %q", p)
	}
}

var mcpServerSchemaScopes = []Scope{
	ScopeRoot,
	ScopeMCPRead,
	ScopeMCPWrite,
	ScopeMCPConnect,
	ScopeMCPBlockedRead,
	ScopeMCPBlockedWrite,
	ScopeMCPBlockedConnect,
}

var mcpServerSchema = mustCompileMCPServerSchema()

func mustCompileMCPServerSchema() *relschema.Schema {
	sc, err := compileMCPServerSchema()
	if err != nil {
		panic(fmt.Sprintf("compile mcp server relationship schema: %v", err))
	}
	return sc
}

func compileMCPServerSchema() (*relschema.Schema, error) {
	serverAnchors := []string{relschema.Self, relTypeProject, relTypeOrganization}
	toolConstraints := []string{SelectorKeyTool, SelectorKeyDisposition}
	project := []string{relTypeProject}

	organization := &relschema.Type{
		Name:       relTypeOrganization,
		StoredKind: ResourceKindOrg,
		Grants: map[string]*relschema.Grant{
			// root administers everything except auth clients, as the legacy
			// engine's expansion does.
			"root": {Name: "root", StoredScope: string(ScopeRoot), StoredKind: ResourceKindWildcard, Anchors: []string{relschema.Self}},
		},
		Perms: map[string]*relschema.Perm{},
	}
	projectType := &relschema.Type{
		Name:       relTypeProject,
		Parent:     relTypeOrganization,
		StoredKind: ResourceKindProject,
		Grants:     map[string]*relschema.Grant{},
		Perms:      map[string]*relschema.Perm{},
	}
	root := relschema.AncestorRel(relTypeOrganization, "root")
	server := &relschema.Type{
		Name:       relTypeMCPServer,
		Parent:     relTypeProject,
		StoredKind: ResourceKindMCP,
		Grants: map[string]*relschema.Grant{
			"viewer": {Name: "viewer", StoredScope: string(ScopeMCPRead), Anchors: serverAnchors, Narrowable: project},
			"editor": {Name: "editor", StoredScope: string(ScopeMCPWrite), Anchors: serverAnchors, Narrowable: project},
			"connector": {
				Name: "connector", StoredScope: string(ScopeMCPConnect),
				Anchors:       []string{relschema.Self},
				LegacyAnchors: []string{relTypeProject, relTypeOrganization},
				Narrowable:    project,
				Constraints:   toolConstraints,
			},
			"read_blocked":    {Name: "read_blocked", StoredScope: string(ScopeMCPBlockedRead), Anchors: serverAnchors, Narrowable: project, Block: true},
			"edit_blocked":    {Name: "edit_blocked", StoredScope: string(ScopeMCPBlockedWrite), Anchors: serverAnchors, Narrowable: project, Block: true},
			"connect_blocked": {Name: "connect_blocked", StoredScope: string(ScopeMCPBlockedConnect), Anchors: serverAnchors, Narrowable: project, Constraints: toolConstraints, Block: true},
		},
		Perms: map[string]*relschema.Perm{
			string(MCPServerRead): {
				Name: string(MCPServerRead),
				Expr: relschema.ExcludedBy(relschema.Union(relschema.Rel("viewer"), relschema.Rel("editor"), root), "read_blocked"),
			},
			string(MCPServerEdit): {
				Name: string(MCPServerEdit),
				Expr: relschema.ExcludedBy(relschema.Union(relschema.Rel("editor"), root), "edit_blocked"),
			},
			// No edge from read or edit: configuration authority is not use.
			string(MCPServerConnect): {
				Name: string(MCPServerConnect),
				Expr: relschema.ExcludedBy(relschema.Union(relschema.Rel("connector"), root), "connect_blocked"),
			},
		},
	}

	known := make([]string, 0, len(scopeVisibilityByScope))
	var outOfSchema []string
	for _, scope := range RegisteredScopes() {
		known = append(known, string(scope))
		if !slices.Contains(mcpServerSchemaScopes, scope) {
			outOfSchema = append(outOfSchema, string(scope))
		}
	}

	sc, err := relschema.Compile(relschema.Definition{
		Types:        []*relschema.Type{organization, projectType, server},
		AncestorKeys: map[string]string{SelectorKeyProjectID: relTypeProject},
		Root:         relTypeOrganization,
		KnownScopes:  known,
		OutOfSchema:  outOfSchema,
	})
	if err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	return sc, nil
}
