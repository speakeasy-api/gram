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

// outOfSchemaScopes is every registered scope the schema deliberately does not
// read, each with the reason. It is written out rather than derived: a scope
// registered in authz/scopes.go appears in neither this list nor a schema
// grant until someone decides where it belongs, and Compile refuses to start
// until they do.
var outOfSchemaScopes = map[authz.Scope]string{
	authz.ScopeOrgRead:                 "organization checks stay on the legacy engine",
	authz.ScopeOrgBlockedRead:          "organization checks stay on the legacy engine",
	authz.ScopeOrgAdmin:                "organization checks stay on the legacy engine",
	authz.ScopeOrgBlockedAdmin:         "organization checks stay on the legacy engine",
	authz.ScopeProjectRead:             "project checks stay on the legacy engine",
	authz.ScopeProjectBlockedRead:      "project checks stay on the legacy engine",
	authz.ScopeProjectWrite:            "project checks stay on the legacy engine",
	authz.ScopeProjectBlockedWrite:     "project checks stay on the legacy engine",
	authz.ScopeMCPAuth:                 "auth client use is checked by the legacy engine",
	authz.ScopeMCPBlockedAuth:          "auth client use is checked by the legacy engine",
	authz.ScopeEnvironmentRead:         "not migrated",
	authz.ScopeEnvironmentBlockedRead:  "not migrated",
	authz.ScopeEnvironmentWrite:        "not migrated",
	authz.ScopeEnvironmentBlockedWrite: "not migrated",
	authz.ScopeSkillRead:               "not migrated",
	authz.ScopeSkillBlockedRead:        "not migrated",
	authz.ScopeSkillWrite:              "not migrated",
	authz.ScopeSkillBlockedWrite:       "not migrated",
	authz.ScopePluginWrite:             "not migrated",
	authz.ScopePluginBlockedWrite:      "not migrated",
	authz.ScopeAssistantRead:           "not migrated",
	authz.ScopeAssistantBlockedRead:    "not migrated",
	authz.ScopeAssistantWrite:          "not migrated",
	authz.ScopeAssistantBlockedWrite:   "not migrated",
	authz.ScopeRiskPolicyEvaluate:      "not migrated",
	authz.ScopeRiskPolicyBypass:        "not migrated",
	authz.ScopeRiskPolicyBlock:         "not migrated",
	authz.ScopeInferenceExecute:        "not migrated",
	authz.ScopeLogRead:                 "not migrated",
	// Agent management is deliberately not a relation on sessions: managing
	// an agent never grants its transcripts.
	authz.ScopeAgentRead:            "agent management confers nothing on sessions",
	authz.ScopeAgentWrite:           "agent management confers nothing on sessions",
	authz.ScopeAgentAuthorize:       "agent management confers nothing on sessions",
	authz.ScopeAgentTransfer:        "agent management confers nothing on sessions",
	authz.ScopeWorkloadRead:         "not migrated",
	authz.ScopeWorkloadBlockedRead:  "not migrated",
	authz.ScopeWorkloadWrite:        "not migrated",
	authz.ScopeWorkloadBlockedWrite: "not migrated",
	authz.ScopeOrgDeviceAgentSync:   "transport scope, not a resource permission",
	authz.ScopeOrgHooksIngest:       "transport scope, not a resource permission",
}

var mcpServerSchema = mustCompileMCPServerSchema()

func mustCompileMCPServerSchema() *s.Schema {
	sc, err := compileSchema(authz.RegisteredScopes())
	if err != nil {
		panic(fmt.Sprintf("compile relationship schema: %v", err))
	}
	return sc
}

// compileSchema compiles every declared type against the registered scopes.
// Each registered scope must be read by a grant or listed in
// outOfSchemaScopes, and every listed scope must be registered.
func compileSchema(registered []authz.Scope) (*s.Schema, error) {
	known := make([]string, 0, len(registered))
	for _, scope := range registered {
		known = append(known, string(scope))
	}
	outOfSchema := make([]string, 0, len(outOfSchemaScopes))
	for scope := range outOfSchemaScopes {
		if !slices.Contains(registered, scope) {
			return nil, fmt.Errorf("out-of-schema scope %q is not registered", scope)
		}
		outOfSchema = append(outOfSchema, string(scope))
	}
	slices.Sort(outOfSchema)

	sc, err := s.Compile(s.Definition{
		Types:        slices.Concat(mcpServerTypes(), chatTypes()),
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
