package authz

import (
	"fmt"
	"maps"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/authz/schema"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Operation is one typed authorization request: a registered permission on a
// concrete resource instance, or a named set of complete alternatives. Build
// operations with the typed resource constructors, such as
// Project(organizationID, projectID).Read(); services never assemble scopes,
// resource kinds or selectors themselves.
//
// The zero Operation is invalid and every client method rejects it.
type Operation struct {
	permission schema.PermissionRef
	instance   operationInstance

	// alternatives is set for a named alternatives operation, whose options
	// are complete single-permission operations in declaration order.
	alternatives bool
	options      []Operation
}

type operationInstance struct {
	resourceID    string
	dimensions    map[string]string
	strict        bool
	facts         map[string]Fact
	requiredFacts []string
}

// newOperation is the only way an Operation is built, so every operation
// names a registered permission; NewClient verifies each one the typed
// constructors use.
func newOperation(typeName, permission, resourceID string, dimensions map[string]string) Operation {
	return Operation{
		permission: schema.PermissionRef{Type: typeName, Permission: permission},
		instance: operationInstance{
			resourceID:    resourceID,
			dimensions:    maps.Clone(dimensions),
			strict:        false,
			facts:         nil,
			requiredFacts: nil,
		},
		alternatives: false,
		options:      nil,
	}
}

func newAlternatives(typeName, name string, options ...Operation) Operation {
	return Operation{
		permission:   schema.PermissionRef{Type: typeName, Permission: name},
		instance:     operationInstance{resourceID: "", dimensions: nil, strict: false, facts: nil, requiredFacts: nil},
		alternatives: true,
		options:      slices.Clone(options),
	}
}

// Permission names the registered permission or alternatives the operation
// asks for.
func (o Operation) Permission() schema.PermissionRef { return o.permission }

// ResourceID is the concrete resource the operation targets, or "" for an
// alternatives operation, whose options each name their own resource.
func (o Operation) ResourceID() string { return o.instance.resourceID }

// WithStrictMatch makes ordinary allows match strictly: a grant constraint
// the request does not carry no longer matches. It mirrors
// Check.WithStrictSelectorMatch for callers that already required it.
func (o Operation) WithStrictMatch() Operation {
	o.instance.strict = true
	o.instance.dimensions = maps.Clone(o.instance.dimensions)
	o.options = slices.Clone(o.options)
	for i := range o.options {
		o.options[i] = o.options[i].WithStrictMatch()
	}
	return o
}

// WithFacts attaches the verified facts a fact-reading permission needs.
// required lists the facts that must resolve before any branch is decided.
func (o Operation) WithFacts(facts map[string]Fact, required ...string) Operation {
	o.instance.facts = maps.Clone(facts)
	o.instance.requiredFacts = slices.Clone(required)
	return o
}

func (o Operation) String() string {
	if o.alternatives {
		return fmt.Sprintf("%s.%s", o.permission.Type, o.permission.Permission)
	}
	return fmt.Sprintf("%s.%s(%s)", o.permission.Type, o.permission.Permission, o.instance.resourceID)
}

func (o Operation) request() PermissionRequest {
	return PermissionRequest{
		Permission:      o.permission,
		ResourceID:      o.instance.resourceID,
		Dimensions:      o.instance.dimensions,
		StrictMatch:     o.instance.strict,
		RepresentedUser: "",
		Actor:           urn.Principal{Type: "", ID: ""},
		Facts:           o.instance.facts,
		RequiredFacts:   o.instance.requiredFacts,
	}
}

// OrganizationRef names one organization for typed operations.
type OrganizationRef struct {
	id string
}

// Organization names the organization with the given ID. The caller must
// have verified the organization belongs to the request's tenant.
func Organization(organizationID string) OrganizationRef {
	return OrganizationRef{id: organizationID}
}

// Read asks to read the organization.
func (o OrganizationRef) Read() Operation {
	return newOperation("organization", "read", o.id, nil)
}

// Administer asks to administer the organization.
func (o OrganizationRef) Administer() Operation {
	return newOperation("organization", "administer", o.id, nil)
}

// ProjectRef names one project in its organization for typed operations.
type ProjectRef struct {
	organizationID string
	id             string
}

// Project names a project. The caller must have verified that the project
// belongs to the organization.
func Project(organizationID, projectID string) ProjectRef {
	return ProjectRef{organizationID: organizationID, id: projectID}
}

// Read asks to read the project.
func (p ProjectRef) Read() Operation {
	return newOperation("project", "read", p.id, nil)
}

// Write asks to change the project.
func (p ProjectRef) Write() Operation {
	return newOperation("project", "write", p.id, nil)
}

// PublishPlugin asks to publish a plugin from the project: either project
// plugin publication or administration of its organization, with a block on
// either alternative vetoing both.
func (p ProjectRef) PublishPlugin() Operation {
	return newAlternatives("project", "plugin_publication",
		newOperation("project", "publish_plugin", p.id, nil),
		Organization(p.organizationID).Administer(),
	)
}

// ReadSkills asks to read the project's skills. Skill grants are stored
// against the project ID, so the operation targets the project rather than
// one skill.
func (p ProjectRef) ReadSkills() Operation {
	return newOperation("skill", "read", p.id, nil)
}

// WriteSkills asks to change the project's skills as a collection, such as
// creating or importing one. Like ReadSkills it targets the project ID, the
// resource collection-wide skill grants are stored against.
func (p ProjectRef) WriteSkills() Operation {
	return newOperation("skill", "write", p.id, nil)
}

// SkillRef names one skill in its project for typed operations.
type SkillRef struct {
	projectID string
	id        string
}

// Skill names a skill. The caller must have verified that the skill belongs
// to the project.
func Skill(projectID, skillID string) SkillRef {
	return SkillRef{projectID: projectID, id: skillID}
}

// Read asks to read the skill. Skill grants are stored either against the
// project, covering every skill in it, or against one skill, so the
// operation passes with either; a block on either refuses it.
func (k SkillRef) Read() Operation {
	return newAlternatives("skill", "project_or_skill_read",
		newOperation("skill", "read", k.projectID, nil),
		newOperation("skill", "read", k.id, nil),
	)
}

// AssistantRef names one assistant in its project for typed operations.
// Every operation carries the project as a dimension, so a grant bounded to
// one project never matches an assistant in another.
type AssistantRef struct {
	projectID string
	id        string
}

// Assistant names an assistant. The caller must have verified that the
// assistant belongs to the project.
func Assistant(projectID, assistantID string) AssistantRef {
	return AssistantRef{projectID: projectID, id: assistantID}
}

// AssistantsInProject names the assistants of a project as a whole, for
// operations that are not about one existing assistant, such as creating
// one. Grants for these operations are stored with the project's ID as the
// resource, so the reference uses the project ID as the assistant ID and
// also carries it as the project dimension.
func AssistantsInProject(projectID string) AssistantRef {
	return AssistantRef{projectID: projectID, id: projectID}
}

func (a AssistantRef) dimensions() map[string]string {
	return map[string]string{SelectorKeyProjectID: a.projectID}
}

// Read asks to read the assistant.
func (a AssistantRef) Read() Operation {
	return newOperation("assistant", "read", a.id, a.dimensions())
}

// Write asks to change the assistant.
func (a AssistantRef) Write() Operation {
	return newOperation("assistant", "write", a.id, a.dimensions())
}

// MCPServerRef names one MCP server in its project for typed operations.
// Every operation carries the project as a dimension, so a grant or block
// bounded to one project never matches a server in another.
type MCPServerRef struct {
	projectID string
	id        string
}

// MCPServer names an MCP server (a toolset). The caller must have verified
// that the server belongs to the project.
func MCPServer(projectID, serverID string) MCPServerRef {
	return MCPServerRef{projectID: projectID, id: serverID}
}

// MCPServersInProject names the MCP servers of a project as a whole, for
// operations that are not about one existing server, such as listing or
// creating servers. Grants for these operations are stored with the
// project's ID as the resource, so the reference uses the project ID as the
// server ID and also carries it as the project dimension.
func MCPServersInProject(projectID string) MCPServerRef {
	return MCPServerRef{projectID: projectID, id: projectID}
}

func (m MCPServerRef) dimensions() map[string]string {
	return map[string]string{SelectorKeyProjectID: m.projectID}
}

// Read asks to inspect the server's configuration.
func (m MCPServerRef) Read() Operation {
	return newOperation("mcp_server", "read", m.id, m.dimensions())
}

// Edit asks to change the server's configuration.
func (m MCPServerRef) Edit() Operation {
	return newOperation("mcp_server", "edit", m.id, m.dimensions())
}

// Connect asks to connect to the server.
func (m MCPServerRef) Connect() Operation {
	return newOperation("mcp_server", "connect", m.id, m.dimensions())
}

// EnvironmentRef names one environment in its project for typed
// operations. Every operation carries the project as a dimension, so a grant
// or block bounded to one project never matches an environment in another.
type EnvironmentRef struct {
	projectID string
	id        string
}

// Environment names an environment. The caller must have verified that the
// environment belongs to the project.
func Environment(projectID, environmentID string) EnvironmentRef {
	return EnvironmentRef{projectID: projectID, id: environmentID}
}

// EnvironmentsInProject names the environments of a project as a whole, for
// operations that are not about one existing environment, such as creating
// one, or answering for a slug that matches none without revealing whether
// it exists. The project's ID stands in as the environment ID, so only a
// grant that covers every environment of the project matches.
func EnvironmentsInProject(projectID string) EnvironmentRef {
	return EnvironmentRef{projectID: projectID, id: projectID}
}

// Read asks to view the environment and its entries.
func (e EnvironmentRef) Read() Operation {
	return newOperation("environment", "read", e.id, map[string]string{SelectorKeyProjectID: e.projectID})
}

// Write asks to change, clone or delete the environment.
func (e EnvironmentRef) Write() Operation {
	return newOperation("environment", "write", e.id, map[string]string{SelectorKeyProjectID: e.projectID})
}

// CallTool asks to connect to the server for one tool, and for one of the
// tool's dispositions when disposition is not empty. A grant or block limited
// to other tools or dispositions does not decide it.
func (m MCPServerRef) CallTool(tool, disposition string) Operation {
	dimensions := m.dimensions()
	if tool != "" {
		dimensions[SelectorKeyTool] = tool
	}
	if disposition != "" {
		dimensions[SelectorKeyDisposition] = disposition
	}
	return newOperation("mcp_server", "connect", m.id, dimensions)
}

// typedOperations lists one instance of every operation the typed
// constructors build. NewClient rejects a registry that does not declare
// each of them, so a constructor can never name a missing permission at
// request time.
func typedOperations() []Operation {
	org := Organization("organization")
	project := Project("organization", "project")
	mcpServer := MCPServer("project", "server")
	environment := Environment("project", "environment")
	return []Operation{
		org.Read(), org.Administer(),
		project.Read(), project.Write(), project.PublishPlugin(),
		mcpServer.Read(), mcpServer.Edit(), mcpServer.Connect(), mcpServer.CallTool("tool", "read_only"),
		project.ReadSkills(), project.WriteSkills(), Skill("project", "skill").Read(),
		Assistant("project", "assistant").Read(), Assistant("project", "assistant").Write(),
		environment.Read(), environment.Write(),
	}
}
