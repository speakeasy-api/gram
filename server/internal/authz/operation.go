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

// SyncDeviceAgent asks to sync device agents for the organization.
func (o OrganizationRef) SyncDeviceAgent() Operation {
	return newOperation("organization", "sync_device_agent", o.id, nil)
}

// IngestHooks asks to ingest hook events into one of the organization's
// projects. The operation always names the project, so a grant bounded to
// another project never matches. The caller must have verified that the
// project belongs to the organization.
func (o OrganizationRef) IngestHooks(projectID string) Operation {
	return newOperation("organization", "ingest_hooks", o.id, map[string]string{SelectorKeyProjectID: projectID})
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

// ReadToolLogs asks to read the project's individual tool calls and raw log
// records, which carry every member's tool arguments, results and prompts.
func (p ProjectRef) ReadToolLogs() Operation {
	return newOperation("project", "read_logs", p.id, nil)
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

// ReadOrOrganizationRead asks to read the project's organization or the
// project itself: either alternative allows, and a block on one does not
// stop the other. Organization read comes first, so a denial names it.
func (p ProjectRef) ReadOrOrganizationRead() Operation {
	return newAlternatives("project", "read_or_organization_read",
		Organization(p.organizationID).Read(),
		p.Read(),
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

// CallTool asks to connect to the server for one tool. A grant or block
// limited to other tools does not decide it. When disposition is not empty the
// request also names that disposition, so a grant or block limited to other
// dispositions does not decide it either. With an empty disposition, ordinary
// allows limited to a disposition still match, while blocks and direct
// exceptions limited to one match strictly and do not apply.
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

// MCPAuthClientRef names one shared upstream auth client of a project's MCP
// servers for typed operations. Every operation carries the client type and
// the project as dimensions, and grants on auth clients match strictly, so a
// grant bounded to another client type or project never matches.
type MCPAuthClientRef struct {
	projectID  string
	id         string
	clientType string
}

// MCPAuthClient names an upstream auth client. The caller must have verified
// that the client serves the project and has the given type.
func MCPAuthClient(projectID, clientID, clientType string) MCPAuthClientRef {
	return MCPAuthClientRef{projectID: projectID, id: clientID, clientType: clientType}
}

// Use asks to connect through the auth client.
func (a MCPAuthClientRef) Use() Operation {
	return newOperation("mcp_auth_client", "use", a.id, map[string]string{
		SelectorKeyClientType: a.clientType,
		SelectorKeyProjectID:  a.projectID,
	}).WithStrictMatch()
}

// InferenceRef names inference in one project for typed operations.
type InferenceRef struct {
	projectID string
}

// Inference names inference in a project. Inference grants are stored against
// every resource of the project, so the operation targets the project ID and
// carries it as a dimension.
func Inference(projectID string) InferenceRef {
	return InferenceRef{projectID: projectID}
}

// Execute asks to run inference in the project.
func (i InferenceRef) Execute() Operation {
	return newOperation("inference", "execute", i.projectID, map[string]string{SelectorKeyProjectID: i.projectID})
}

// ReadChats asks for a grant to read every chat session in the project, for
// views that aggregate or list sessions beyond the caller's own. Chat grants
// that name the project ID as their resource answer it.
func (p ProjectRef) ReadChats() Operation {
	return newOperation("chat", "read_grant", p.id, nil)
}

// WriteOrAdminister asks to change the project or to administer its
// organization. A block on one alternative does not stop the other.
func (p ProjectRef) WriteOrAdminister() Operation {
	return newAlternatives("project", "write_or_administer",
		p.Write(),
		Organization(p.organizationID).Administer(),
	)
}

// ReadOrEditMCPServer asks to read the project or to change one of its MCP
// servers. serverID may be the project ID to name every server of the
// project. A block on one alternative does not stop the other.
func (p ProjectRef) ReadOrEditMCPServer(serverID string) Operation {
	return newAlternatives("mcp_server", "project_read_or_edit",
		p.Read(),
		MCPServer(p.id, serverID).Edit(),
	)
}

// ChatRef names one chat session in its project for typed operations.
type ChatRef struct {
	id string
}

// Chat names a chat session. The caller must have loaded the chat and
// verified that it belongs to the request's project.
func Chat(chatID string) ChatRef {
	return ChatRef{id: chatID}
}

// Read asks for a grant to read the chat's transcript. Session owners and
// the owners of an agent-only session read without one; the caller decides
// ownership before asking.
func (c ChatRef) Read() Operation {
	return newOperation("chat", "read_grant", c.id, nil)
}

// Write asks for a grant to change or delete the chat. Session owners change
// their own sessions without one; the caller decides ownership before asking.
func (c ChatRef) Write() Operation {
	return newOperation("chat", "write_grant", c.id, nil)
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
		org.Read(), org.Administer(), org.SyncDeviceAgent(), org.IngestHooks("project"),
		project.Read(), project.Write(), project.PublishPlugin(), project.ReadOrOrganizationRead(),
		mcpServer.Read(), mcpServer.Edit(), mcpServer.Connect(), mcpServer.CallTool("tool", "read_only"),
		MCPAuthClient("project", "client", "type").Use(),
		project.ReadSkills(), project.WriteSkills(), Skill("project", "skill").Read(),
		project.WriteOrAdminister(), project.ReadOrEditMCPServer("server"),
		project.ReadToolLogs(),
		RiskPolicy("policy", RiskPolicyDimensions{ServerURL: "", ServerIdentity: ""}).Evaluate(),
		RiskPolicy("policy", RiskPolicyDimensions{ServerURL: "", ServerIdentity: ""}).Bypass(),
		Assistant("project", "assistant").Read(), Assistant("project", "assistant").Write(),
		environment.Read(), environment.Write(),
		Inference("project").Execute(),
		Chat("chat").Read(), Chat("chat").Write(),
		project.ReadChats(),
	}
}
