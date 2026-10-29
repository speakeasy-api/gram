package declarative

import (
	"fmt"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/authz"
	s "github.com/speakeasy-api/gram/server/internal/authz/declarative/relschema"
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
	typeOrganization = "organization"
	typeProject      = "project"
	typeMCPServer    = "mcp_server"
)

// MCPServerPermission is a permission declared on the mcp_server type.
type MCPServerPermission string

const (
	MCPServerRead    MCPServerPermission = "read"
	MCPServerEdit    MCPServerPermission = "edit"
	MCPServerConnect MCPServerPermission = "connect"
)

func mcpServerTypes() []*s.Type {
	const (
		org     = typeOrganization
		project = typeProject
		server  = typeMCPServer
	)
	root := s.AncestorRel(org, "root")
	anywhere := s.AnchoredAt(server, project, org)
	toolLimited := s.ConstrainedBy(authz.SelectorKeyTool, authz.SelectorKeyDisposition)

	return []*s.Type{
		s.NewType(org, s.StoredKind(authz.ResourceKindOrg),
			// root administers everything except auth clients, as the legacy
			// engine's expansion does.
			s.Grant("root", s.StoredAs(string(authz.ScopeRoot)), s.StoredKindAs(authz.ResourceKindWildcard), s.AnchoredAt(org)),
		),

		s.NewType(project, s.Parent(org), s.StoredKind(authz.ResourceKindProject)),

		s.NewType(server, s.Parent(project), s.StoredKind(authz.ResourceKindMCP),
			s.Grant("viewer", s.StoredAs(string(authz.ScopeMCPRead)), anywhere, s.NarrowableBy(project),
				s.Describe("View the server's configuration")),
			s.Grant("editor", s.StoredAs(string(authz.ScopeMCPWrite)), anywhere, s.NarrowableBy(project),
				s.Describe("Change the server's configuration")),
			// A connection grant names one server. Project- and
			// organization-wide rows are honored until they are migrated.
			s.Grant("connector", s.StoredAs(string(authz.ScopeMCPConnect)), s.AnchoredAt(server), s.LegacyAnchoredAt(project, org),
				s.NarrowableBy(project), toolLimited,
				s.Describe("Connect to the server and call its tools")),

			s.Block("read_blocked", s.StoredAs(string(authz.ScopeMCPBlockedRead)), anywhere, s.NarrowableBy(project)),
			s.Block("edit_blocked", s.StoredAs(string(authz.ScopeMCPBlockedWrite)), anywhere, s.NarrowableBy(project)),
			s.Block("connect_blocked", s.StoredAs(string(authz.ScopeMCPBlockedConnect)), anywhere, s.NarrowableBy(project), toolLimited),

			s.Permission(string(MCPServerRead), s.Union(s.Rel("viewer"), s.Rel("editor"), root), s.ExcludedBy("read_blocked")),
			s.Permission(string(MCPServerEdit), s.Union(s.Rel("editor"), root), s.ExcludedBy("edit_blocked")),
			// No edge from read or edit: configuration authority is not use.
			s.Permission(string(MCPServerConnect), s.Union(s.Rel("connector"), root), s.ExcludedBy("connect_blocked")),
		),
	}
}

// legacyScope is the stored scope whose legacy check a permission replaces,
// used when the schema is not active for an organization and to label
// challenge logs.
func (p MCPServerPermission) legacyScope() (authz.Scope, error) {
	switch p {
	case MCPServerRead:
		return authz.ScopeMCPRead, nil
	case MCPServerEdit:
		return authz.ScopeMCPWrite, nil
	case MCPServerConnect:
		return authz.ScopeMCPConnect, nil
	default:
		return "", fmt.Errorf("unknown mcp server permission %q", p)
	}
}

// mcpServerSchemaScopes are the stored scopes the schema reads. Every other
// registered scope is declared out of it.
var mcpServerSchemaScopes = []authz.Scope{
	authz.ScopeRoot,
	authz.ScopeMCPRead,
	authz.ScopeMCPWrite,
	authz.ScopeMCPConnect,
	authz.ScopeMCPBlockedRead,
	authz.ScopeMCPBlockedWrite,
	authz.ScopeMCPBlockedConnect,
}

var mcpServerSchema = mustCompileMCPServerSchema()

func mustCompileMCPServerSchema() *s.Schema {
	sc, err := compileMCPServerSchema()
	if err != nil {
		panic(fmt.Sprintf("compile mcp server relationship schema: %v", err))
	}
	return sc
}

func compileMCPServerSchema() (*s.Schema, error) {
	registered := authz.RegisteredScopes()
	known := make([]string, 0, len(registered))
	var outOfSchema []string
	for _, scope := range registered {
		known = append(known, string(scope))
		if !slices.Contains(mcpServerSchemaScopes, scope) {
			outOfSchema = append(outOfSchema, string(scope))
		}
	}

	sc, err := s.Compile(s.Definition{
		Types:        mcpServerTypes(),
		AncestorKeys: map[string]string{authz.SelectorKeyProjectID: typeProject},
		Root:         typeOrganization,
		KnownScopes:  known,
		OutOfSchema:  outOfSchema,
	})
	if err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	return sc, nil
}
