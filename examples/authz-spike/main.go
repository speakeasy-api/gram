package main

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/speakeasy-api/gram/examples/authz-spike/authz"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// serverRow represents trusted data loaded from the application's resource store.
type serverRow struct {
	id             string
	projectID      string
	organizationID string
	tools          map[string]authz.Disposition
}

// AuthzTarget adapts a trusted row once, keeping ancestry out of handlers.
// Resource names the server; ancestors let broader MCP grants cover it without
// turning unrelated project permissions into tool-use authority.
func (s serverRow) AuthzTarget() authz.Target {
	return authz.Target{
		Resource: authz.ResourceRef{OrganizationID: s.organizationID, Kind: authz.MCPServer, ID: s.id},
		Ancestors: []authz.ResourceRef{
			{OrganizationID: s.organizationID, Kind: authz.Project, ID: s.projectID},
			{OrganizationID: s.organizationID, Kind: authz.Organization, ID: s.organizationID},
		},
	}
}

// service receives the authorization frontend through startup wiring.
// Neither handlers nor the resource store depend on the memory backend.
type service struct {
	authz        *authz.Client
	servers      map[string]serverRow
	projects     map[string]projectRow
	skills       map[string]skillRow
	environments map[string]environmentRow
	assistants   map[string]assistantRow
	plugins      map[string]pluginRow
}

// loadServer simulates an organization-scoped repository lookup, not authorization.
func (s *service) loadServer(ctx context.Context, organizationID, serverID string) (serverRow, error) {
	if err := ctx.Err(); err != nil {
		return serverRow{}, err
	}
	server, exists := s.servers[serverID]
	if !exists || server.organizationID != organizationID {
		return serverRow{}, fmt.Errorf("server not found")
	}
	return server, nil
}

// connectHandler models private MCP admission. The subject is supplied by trusted
// authentication middleware; serverID is request input. No disposition belongs
// to this request because establishing a connection does not invoke a tool.
func (s *service) connectHandler(ctx context.Context, subject authz.Subject, serverID string) (string, error) {
	server, err := s.loadServer(ctx, subject.Principal.OrganizationID, serverID)
	if err != nil {
		return "", err
	}
	decision, err := s.authz.DiscoverAccess(ctx, subject, authz.MCPConnect, server.AuthzTarget())
	if err != nil {
		return "", err
	}
	if decision.Outcome != authz.Allowed {
		return "", fmt.Errorf("%w: connection admission %s", authz.ErrDenied, decision.Outcome)
	}
	// This stands in for creating the connection. Admission is not a credential
	// permitting every later tool call; invokeToolHandler authorizes each call.
	return "connection established", nil
}

// invokeToolHandler models the boundary before protected tool execution.
// The request names a tool; its classification comes from stored metadata.
func (s *service) invokeToolHandler(ctx context.Context, subject authz.Subject, serverID, toolName string) (string, error) {
	server, err := s.loadServer(ctx, subject.Principal.OrganizationID, serverID)
	if err != nil {
		return "", err
	}
	disposition, exists := server.tools[toolName]
	if !exists {
		return "", fmt.Errorf("tool not found")
	}
	if err := s.authz.Require(ctx, subject, authz.Request{
		Permission: authz.MCPConnect,
		Target:     server.AuthzTarget(),
		Tool:       &authz.ToolFacts{Name: toolName, Disposition: disposition},
	}); err != nil {
		return "", err
	}
	// Only this point represents protected execution; real execution would
	// retain its separate risk, entitlement, and upstream credential checks.
	return fmt.Sprintf("tool %s executed", toolName), nil
}

// inspectServerHandler lets the service choose its endpoint's scope rather than
// using a generic business operation named "inspect" inside the authz package.
func (s *service) inspectServerHandler(ctx context.Context, subject authz.Subject, serverID string) (string, error) {
	server, err := s.loadServer(ctx, subject.Principal.OrganizationID, serverID)
	if err != nil {
		return "", err
	}
	if err := s.authz.Require(ctx, subject, authz.Request{
		Permission: authz.MCPRead,
		Target:     server.AuthzTarget(),
	}); err != nil {
		return "", err
	}
	return fmt.Sprintf("server %s belongs to project %s", server.id, server.projectID), nil
}

// run wires dependencies once, then exercises handlers as incoming requests.
// Reported refusals are example responses, not startup failures or test assertions.
func run(ctx context.Context) error {
	definitions := append(authz.MCPDefinitions(), authz.OrganizationDefinitions()...)
	definitions = append(definitions, authz.ProjectDefinitions()...)
	definitions = append(definitions, authz.SkillDefinitions()...)
	definitions = append(definitions, authz.EnvironmentDefinitions()...)
	definitions = append(definitions, authz.AssistantDefinitions()...)
	definitions = append(definitions, authz.PluginDefinitions()...)
	client, err := authz.NewClient(authz.NewMemoryBackend(), definitions)
	if err != nil {
		return err
	}
	if err := seedExamplePolicies(ctx, client); err != nil {
		return err
	}
	if err := seedAgentPolicies(ctx, client); err != nil {
		return err
	}
	if err := seedOrganizationProjectPolicies(ctx, client); err != nil {
		return err
	}
	if err := seedSkillPolicies(ctx, client); err != nil {
		return err
	}
	if err := seedEnvironmentPolicies(ctx, client); err != nil {
		return err
	}
	if err := seedAssistantPolicies(ctx, client); err != nil {
		return err
	}
	if err := seedPluginPolicies(ctx, client); err != nil {
		return err
	}
	if err := seedDelegationPolicies(ctx, client); err != nil {
		return err
	}
	app := &service{
		authz: client,
		projects: map[string]projectRow{
			"project-a": {id: "project-a", organizationID: "example-org"},
			"project-b": {id: "project-b", organizationID: "example-org"},
		},
		plugins: map[string]pluginRow{
			"plugin-a": {id: "plugin-a", projectID: "project-a", organizationID: "example-org"},
			"plugin-b": {id: "plugin-b", projectID: "project-b", organizationID: "example-org"},
		},
		assistants: map[string]assistantRow{
			"assistant-a": {id: "assistant-a", projectID: "project-a", organizationID: "example-org"},
			"assistant-b": {id: "assistant-b", projectID: "project-b", organizationID: "example-org"},
		},
		environments: map[string]environmentRow{
			"environment-a": {id: "environment-a", projectID: "project-a", organizationID: "example-org"},
			"environment-b": {id: "environment-b", projectID: "project-b", organizationID: "example-org"},
		},
		skills: map[string]skillRow{
			"skill-a": {id: "skill-a", projectID: "project-a", organizationID: "example-org"},
			"skill-b": {id: "skill-b", projectID: "project-b", organizationID: "example-org"},
		},
		servers: map[string]serverRow{
			"server-a": {
				id: "server-a", projectID: "project-a", organizationID: "example-org",
				tools: map[string]authz.Disposition{
					"lookup": authz.DispositionUnknown, "list": authz.ReadOnly,
					"list_documents": authz.ReadOnly, "delete_document": authz.Destructive,
				},
			},
		},
	}

	serverSpecificExample(ctx, app)
	projectGrantExample(ctx, app)
	organizationGrantExample(ctx, app)
	missingOrganizationExample(ctx, app)
	wrongProjectExample(ctx, app)
	opposingScopeExample(ctx, app)
	namedToolExclusionExample(ctx, app)
	readOnlyExample(ctx, app)
	projectReadOnlyFlowExample(ctx, app)
	blockedDeleteFlowExample(ctx, app)
	unclassifiedToolExample(ctx, app)
	inspectionExample(ctx, app)
	typedIdentityExample(ctx, app)
	agentGrantExample(ctx, app)
	organizationProjectExample(ctx, app)
	skillExample(ctx, app)
	environmentExample(ctx, app)
	assistantExample(ctx, app)
	pluginExample(ctx, app)
	alternativesExample(ctx, app)
	pluginReadExample(ctx, app)
	if err := collectionExample(ctx, app); err != nil {
		return err
	}
	if err := delegationExample(ctx, app); err != nil {
		return err
	}
	return revocationExample(ctx, app)
}

// listProjectsHandler models a bounded repository candidate page, not a full
// authorization-aware database scan. Only allowed IDs reach the simulated response.
func (s *service) listProjectsHandler(ctx context.Context, subject authz.Subject) (string, error) {
	ids := make([]string, 0, len(s.projects))
	for id, project := range s.projects {
		if project.organizationID == subject.Principal.OrganizationID {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids) // Stable candidate order stands in for repository ordering.
	if len(ids) > authz.MaxFilterCandidates {
		return "", fmt.Errorf("example project page exceeds candidate limit")
	}
	targets := make([]authz.Target, 0, len(ids))
	for _, id := range ids {
		targets = append(targets, s.projects[id].AuthzTarget())
	}
	allowed, err := s.authz.Filter(ctx, subject, authz.ProjectRead, targets)
	if err != nil {
		return "", err
	}
	visible := make([]string, 0, len(allowed))
	for _, resource := range allowed {
		visible = append(visible, resource.ID)
	}
	return fmt.Sprintf("visible projects: %v", visible), nil
}

// collectionExample shows filtering without per-row authorization calls, including
// empty visibility, opposing grants, fresh revocation, and all-or-error validation.
func collectionExample(ctx context.Context, app *service) error {
	for _, id := range []string{"project-editor", "project-write-blocked", "project-read-blocked", "org-admin-only", "project-list-reader"} {
		result, err := app.listProjectsHandler(ctx, exampleUserSession(id))
		report(id+": project collection", result, err)
	}
	subject := exampleUserSession("project-list-reader")
	if err := app.authz.ReplaceGrants(ctx, subject.Principal, nil); err != nil {
		return err
	}
	result, err := app.listProjectsHandler(ctx, subject)
	report("project collection after grant revocation", result, err)

	// A malformed later candidate cannot turn into a partially successful response.
	valid := app.projects["project-a"].AuthzTarget()
	malformed := app.projects["project-b"].AuthzTarget()
	malformed.Ancestors = nil
	_, err = app.authz.Filter(ctx, exampleUserSession("project-editor"), authz.ProjectRead,
		[]authz.Target{valid, malformed})
	report("malformed collection candidate", "filtered", err)
	_, err = app.authz.Filter(ctx, exampleAgentCredential("limited-agent", "user-key"), authz.ProjectRead, nil)
	report("empty collection still validates credential binding", "filtered", err)
	_, err = app.authz.Filter(ctx, exampleUserSession("project-editor"), authz.MCPConnect, nil)
	report("tool-only scope cannot filter resource reads", "filtered", err)
	return nil
}

// pluginRow supplies trusted bundle identity and ancestry, not member authority.
type pluginRow struct {
	id             string
	projectID      string
	organizationID string
}

// AuthzTarget supports exact-plugin and enclosing content grants in the spike.
func (p pluginRow) AuthzTarget() authz.Target {
	return authz.Target{
		Resource: authz.ResourceRef{OrganizationID: p.organizationID, Kind: authz.Plugin, ID: p.id},
		Ancestors: []authz.ResourceRef{
			{OrganizationID: p.organizationID, Kind: authz.Project, ID: p.projectID},
			{OrganizationID: p.organizationID, Kind: authz.Organization, ID: p.organizationID},
		},
	}
}

// loadPlugin simulates a tenant-scoped lookup without loading recipients or content.
func (s *service) loadPlugin(ctx context.Context, organizationID, pluginID string) (pluginRow, error) {
	if err := ctx.Err(); err != nil {
		return pluginRow{}, err
	}
	plugin, exists := s.plugins[pluginID]
	if !exists || plugin.organizationID != organizationID {
		return pluginRow{}, fmt.Errorf("plugin not found")
	}
	return plugin, nil
}

// readPluginMetadataHandler has two independent paths: organization read, or the
// exclusion-aware content-write alternatives. It exposes no recipients or members.
func (s *service) readPluginMetadataHandler(ctx context.Context, subject authz.Subject, pluginID string) (string, error) {
	plugin, err := s.loadPlugin(ctx, subject.Principal.OrganizationID, pluginID)
	if err != nil {
		return "", err
	}
	organization := organizationTarget(subject.Principal.OrganizationID)
	if err := s.authz.RequireAnyGroups(ctx, subject,
		authz.AlternativeGroup{Mode: authz.IndependentAlternatives, Requests: []authz.Request{
			{Permission: authz.OrgRead, Target: organization},
		}},
		authz.AlternativeGroup{Mode: authz.UnblockedAlternatives, Requests: []authz.Request{
			{Permission: authz.PluginWrite, Target: plugin.AuthzTarget()},
			{Permission: authz.OrgAdmin, Target: organization},
		}},
	); err != nil {
		return "", err
	}
	return "plugin metadata read authorized (no audience or member content exposed)", nil
}

// pluginReadExample demonstrates why independent read access must not inherit a
// content block, and why the write fallback cannot become a flat ordinary OR.
func pluginReadExample(ctx context.Context, app *service) {
	for _, id := range []string{"plugin-project-editor", "org-admin-only", "plugin-blocked-reader", "plugin-editor-admin-blocked", "plugin-specific-blocked", "server-user"} {
		result, err := app.readPluginMetadataHandler(ctx, exampleUserSession(id), "plugin-a")
		report(id+": plugin metadata read", result, err)
	}
	subject := exampleUserSession("plugin-editor-admin-blocked")
	plugin := app.plugins["plugin-a"]
	organization := organizationTarget(subject.Principal.OrganizationID)
	err := app.authz.RequireAny(ctx, subject,
		authz.Request{Permission: authz.OrgRead, Target: organization},
		authz.Request{Permission: authz.PluginWrite, Target: plugin.AuthzTarget()},
		authz.Request{Permission: authz.OrgAdmin, Target: organization},
	)
	report("flat OR incorrectly admits this metadata case", "allowed (contrast only, not a handler)", err)
	err = app.authz.RequireAnyGroups(ctx, exampleUserSession("org-admin-only"),
		authz.AlternativeGroup{Mode: authz.IndependentAlternatives, Requests: []authz.Request{
			{Permission: authz.OrgRead, Target: organization},
		}},
		authz.AlternativeGroup{Mode: authz.UnblockedAlternatives},
	)
	report("empty nested group despite another allowing path", "allowed", err)
}

// updatePluginHandler permits plugin write or org admin, with exclusions shared
// across both alternatives. It changes neither publication nor audiences.
func (s *service) updatePluginHandler(ctx context.Context, subject authz.Subject, pluginID string) (string, error) {
	plugin, err := s.loadPlugin(ctx, subject.Principal.OrganizationID, pluginID)
	if err != nil {
		return "", err
	}
	if err := s.authz.RequireAnyUnblocked(ctx, subject,
		authz.Request{Permission: authz.PluginWrite, Target: plugin.AuthzTarget()},
		authz.Request{Permission: authz.OrgAdmin, Target: organizationTarget(subject.Principal.OrganizationID)},
	); err != nil {
		return "", err
	}
	return "plugin content update authorized (no mutation persisted)", nil
}

// createPluginHandler permits project-parent plugin write or organization admin
// before a plugin ID exists; it creates no packages or audience assignments.
func (s *service) createPluginHandler(ctx context.Context, subject authz.Subject, projectID string) (string, error) {
	project, err := s.loadProject(ctx, subject.Principal.OrganizationID, projectID)
	if err != nil {
		return "", err
	}
	if err := s.authz.RequireAnyUnblocked(ctx, subject,
		authz.Request{Permission: authz.PluginWrite, Target: project.AuthzTarget(), Creation: true},
		authz.Request{Permission: authz.OrgAdmin, Target: organizationTarget(subject.Principal.OrganizationID)},
	); err != nil {
		return "", err
	}
	return "plugin creation authorized (no plugin persisted)", nil
}

// pluginExample covers content boundaries without granting access to the bundled
// MCP servers or skills, or treating bundle membership as execution permission.
func pluginExample(ctx context.Context, app *service) {
	subject := exampleUserSession("plugin-project-editor")
	result, err := app.createPluginHandler(ctx, subject, "project-a")
	report("project plugin write: create", result, err)
	result, err = app.updatePluginHandler(ctx, subject, "plugin-a")
	report("project plugin write: update", result, err)
	result, err = app.updatePluginHandler(ctx, subject, "plugin-b")
	report("plugin grant for another project: update", result, err)
	result, err = app.createPluginHandler(ctx, subject, "project-b")
	report("plugin grant for another project: create", result, err)
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "list")
	report("plugin write alone: invoke bundled MCP tool", result, err)
	result, err = app.updateSkillHandler(ctx, subject, "skill-a")
	report("plugin write alone: edit referenced skill", result, err)

	subject = exampleUserSession("plugin-specific-editor")
	result, err = app.updatePluginHandler(ctx, subject, "plugin-a")
	report("exact plugin write: update", result, err)
	result, err = app.createPluginHandler(ctx, subject, "project-a")
	report("exact plugin write: create sibling", result, err)
	subject = exampleUserSession("plugin-org-editor")
	result, err = app.updatePluginHandler(ctx, subject, "plugin-b")
	report("organization plugin write: another project", result, err)
	result, err = app.createPluginHandler(ctx, subject, "project-b")
	report("organization plugin write: create", result, err)

	subject = exampleUserSession("plugin-specific-blocked")
	result, err = app.updatePluginHandler(ctx, subject, "plugin-a")
	report("exact plugin block: update", result, err)
	result, err = app.createPluginHandler(ctx, subject, "project-a")
	report("exact plugin block leaves sibling creation", result, err)
	subject = exampleUserSession("plugin-project-blocked")
	result, err = app.createPluginHandler(ctx, subject, "project-a")
	report("project plugin block: create", result, err)
	result, err = app.updatePluginHandler(ctx, subject, "plugin-a")
	report("project plugin block: update", result, err)
	subject = exampleUserSession("project-editor")
	result, err = app.updatePluginHandler(ctx, subject, "plugin-a")
	report("project write alone: edit plugin", result, err)
}

// alternativesExample compares branch-local OR with exclusion-aware OR and shows
// that invalid questions fail even when another branch could allow the operation.
// The ordinary-OR comparison is not a complete plugin inventory handler.
func alternativesExample(ctx context.Context, app *service) {
	subject := exampleUserSession("org-admin-only")
	result, err := app.updatePluginHandler(ctx, subject, "plugin-a")
	report("plugin edit via org admin fallback", result, err)
	result, err = app.createPluginHandler(ctx, subject, "project-a")
	report("plugin creation via org admin fallback", result, err)

	subject = exampleUserSession("plugin-blocked-admin")
	result, err = app.updatePluginHandler(ctx, subject, "plugin-a")
	report("plugin block defeats org admin fallback", result, err)
	subject = exampleUserSession("plugin-editor-admin-blocked")
	result, err = app.updatePluginHandler(ctx, subject, "plugin-a")
	report("org admin block defeats plugin write alternative", result, err)

	plugin := app.plugins["plugin-a"] // Trusted fixture, not request ancestry.
	requests := []authz.Request{
		{Permission: authz.OrgRead, Target: organizationTarget("example-org")},
		{Permission: authz.PluginWrite, Target: plugin.AuthzTarget()},
	}
	subject = exampleUserSession("plugin-blocked-reader")
	err = app.authz.RequireAny(ctx, subject, requests...)
	report("ordinary OR: org read survives plugin write block", "allowed", err)
	err = app.authz.RequireAnyUnblocked(ctx, subject, requests...)
	report("unblocked OR: plugin write block defeats org read alternative", "allowed", err)

	// The second branch may allow via org admin, but the tool-specific opposing
	// grant in the first branch cannot be resolved without classification.
	subject = exampleUserSession("uncertain-tool-admin")
	requests = []authz.Request{
		{Permission: authz.MCPConnect, Target: app.servers["server-a"].AuthzTarget(),
			Tool: &authz.ToolFacts{Name: "lookup", Disposition: authz.DispositionUnknown}},
		{Permission: authz.OrgAdmin, Target: organizationTarget("example-org")},
	}
	err = app.authz.RequireAnyUnblocked(ctx, subject, requests...)
	report("unresolved exclusion defeats another branch's allow", "allowed", err)
	err = app.authz.RequireAny(ctx, subject, requests...)
	report("ordinary OR: independent admin branch allows", "allowed", err)

	err = app.authz.RequireAny(ctx, exampleUserSession("server-user"), requests[1])
	report("OR with no matching allow", "allowed", err)
	err = app.authz.RequireAnyUnblocked(ctx, subject)
	report("empty OR", "allowed", err)
	requests[0].Permission = "unknown:permission"
	err = app.authz.RequireAny(ctx, subject, requests...)
	report("invalid alternative despite another branch's allow", "allowed", err)
}

// seedPluginPolicies provisions scope examples, not authorized role edits.
func seedPluginPolicies(ctx context.Context, client *authz.Client) error {
	organization := organizationTarget("example-org").Resource
	project := authz.ResourceRef{OrganizationID: "example-org", Kind: authz.Project, ID: "project-a"}
	plugin := authz.ResourceRef{OrganizationID: "example-org", Kind: authz.Plugin, ID: "plugin-a"}
	for id, grants := range map[string][]authz.Grant{
		"plugin-project-editor":  {{Permission: authz.PluginWrite, Anchor: project}},
		"plugin-specific-editor": {{Permission: authz.PluginWrite, Anchor: plugin}},
		"plugin-org-editor":      {{Permission: authz.PluginWrite, Anchor: organization}},
		"plugin-specific-blocked": {
			{Permission: authz.PluginWrite, Anchor: project},
			{Permission: authz.PluginBlockedWrite, Anchor: plugin},
		},
		"plugin-project-blocked": {
			{Permission: authz.PluginWrite, Anchor: organization},
			{Permission: authz.PluginBlockedWrite, Anchor: project},
		},
		"plugin-blocked-admin": {
			{Permission: authz.OrgAdmin, Anchor: organization},
			{Permission: authz.PluginBlockedWrite, Anchor: plugin},
		},
		"plugin-editor-admin-blocked": {
			{Permission: authz.PluginWrite, Anchor: project},
			{Permission: authz.OrgBlockedAdmin, Anchor: organization},
		},
		"plugin-blocked-reader": {
			{Permission: authz.OrgRead, Anchor: organization},
			{Permission: authz.PluginBlockedWrite, Anchor: plugin},
		},
		"uncertain-tool-admin": {
			{Permission: authz.OrgAdmin, Anchor: organization},
			{Permission: authz.MCPBlockedConnect,
				Anchor:      authz.ResourceRef{OrganizationID: "example-org", Kind: authz.MCPServer, ID: "server-a"},
				Constraints: authz.Constraints{Disposition: authz.ReadOnly}},
		},
	} {
		if err := client.ReplaceGrants(ctx, exampleUserSession(id).Principal, grants); err != nil {
			return err
		}
	}
	return nil
}

// assistantRow supplies trusted configuration identity and enclosing boundaries.
// It does not describe the runtime's executing principal or credentials.
type assistantRow struct {
	id             string
	projectID      string
	organizationID string
}

// AuthzTarget adapts stored ancestry for exact and enclosing assistant grants.
func (a assistantRow) AuthzTarget() authz.Target {
	return authz.Target{
		Resource: authz.ResourceRef{OrganizationID: a.organizationID, Kind: authz.Assistant, ID: a.id},
		Ancestors: []authz.ResourceRef{
			{OrganizationID: a.organizationID, Kind: authz.Project, ID: a.projectID},
			{OrganizationID: a.organizationID, Kind: authz.Organization, ID: a.organizationID},
		},
	}
}

// loadAssistant simulates a tenant-scoped configuration lookup, not authorization.
func (s *service) loadAssistant(ctx context.Context, organizationID, assistantID string) (assistantRow, error) {
	if err := ctx.Err(); err != nil {
		return assistantRow{}, err
	}
	assistant, exists := s.assistants[assistantID]
	if !exists || assistant.organizationID != organizationID {
		return assistantRow{}, fmt.Errorf("assistant not found")
	}
	return assistant, nil
}

// readAssistantHandler gates configuration reads, not conversation transcripts.
func (s *service) readAssistantHandler(ctx context.Context, subject authz.Subject, assistantID string) (string, error) {
	assistant, err := s.loadAssistant(ctx, subject.Principal.OrganizationID, assistantID)
	if err != nil {
		return "", err
	}
	if err := s.authz.Require(ctx, subject, authz.Request{
		Permission: authz.AssistantRead, Target: assistant.AuthzTarget(),
	}); err != nil {
		return "", err
	}
	return "assistant configuration read authorized", nil
}

// updateAssistantHandler checks configuration write before simulated mutation.
// Attachment authorization and runtime authority remain separate domain gates.
func (s *service) updateAssistantHandler(ctx context.Context, subject authz.Subject, assistantID string) (string, error) {
	assistant, err := s.loadAssistant(ctx, subject.Principal.OrganizationID, assistantID)
	if err != nil {
		return "", err
	}
	if err := s.authz.Require(ctx, subject, authz.Request{
		Permission: authz.AssistantWrite, Target: assistant.AuthzTarget(),
	}); err != nil {
		return "", err
	}
	return "assistant update authorized (no mutation persisted)", nil
}

// createAssistantHandler authorizes against the project parent before an
// assistant ID exists. It performs no workload identity or runtime provisioning.
func (s *service) createAssistantHandler(ctx context.Context, subject authz.Subject, projectID string) (string, error) {
	project, err := s.loadProject(ctx, subject.Principal.OrganizationID, projectID)
	if err != nil {
		return "", err
	}
	if err := s.authz.Require(ctx, subject, authz.Request{
		Permission: authz.AssistantWrite, Target: project.AuthzTarget(), Creation: true,
	}); err != nil {
		return "", err
	}
	return "assistant creation authorized (no assistant persisted)", nil
}

// assistantExample demonstrates configuration scopes, parent creation,
// directional exclusions, and isolation from project and tool permissions.
func assistantExample(ctx context.Context, app *service) {
	subject := exampleUserSession("assistant-project-editor")
	result, err := app.createAssistantHandler(ctx, subject, "project-a")
	report("project assistant write: create", result, err)
	result, err = app.readAssistantHandler(ctx, subject, "assistant-a")
	report("assistant write implies assistant read", result, err)
	result, err = app.updateAssistantHandler(ctx, subject, "assistant-a")
	report("project assistant write: update", result, err)
	result, err = app.readAssistantHandler(ctx, subject, "assistant-b")
	report("assistant grant for another project: read", result, err)
	result, err = app.createAssistantHandler(ctx, subject, "project-b")
	report("assistant grant for another project: create", result, err)
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "list")
	report("assistant write alone: invoke MCP tool", result, err)

	subject = exampleUserSession("assistant-specific-editor")
	result, err = app.updateAssistantHandler(ctx, subject, "assistant-a")
	report("exact assistant write: update", result, err)
	result, err = app.createAssistantHandler(ctx, subject, "project-a")
	report("exact assistant write: create sibling", result, err)

	subject = exampleUserSession("assistant-org-reader")
	result, err = app.readAssistantHandler(ctx, subject, "assistant-b")
	report("organization assistant read: another project", result, err)
	result, err = app.updateAssistantHandler(ctx, subject, "assistant-a")
	report("assistant read alone: update", result, err)
	result, err = app.createAssistantHandler(ctx, subject, "project-a")
	report("assistant read alone: create", result, err)

	subject = exampleUserSession("assistant-write-blocked")
	result, err = app.readAssistantHandler(ctx, subject, "assistant-a")
	report("blocked assistant write retains read", result, err)
	result, err = app.updateAssistantHandler(ctx, subject, "assistant-a")
	report("blocked assistant write: update", result, err)
	subject = exampleUserSession("assistant-read-blocked")
	result, err = app.readAssistantHandler(ctx, subject, "assistant-a")
	report("blocked assistant read: read", result, err)
	result, err = app.updateAssistantHandler(ctx, subject, "assistant-a")
	report("blocked assistant read also blocks write", result, err)
	subject = exampleUserSession("assistant-project-blocked")
	result, err = app.createAssistantHandler(ctx, subject, "project-a")
	report("project assistant block: create", result, err)

	for _, id := range []string{"project-editor", "org-admin-only", "skill-project-editor"} {
		result, err = app.readAssistantHandler(ctx, exampleUserSession(id), "assistant-a")
		report(id+" without assistant grants: read", result, err)
	}
}

// seedAssistantPolicies provisions synthetic scope assignments, not role defaults.
func seedAssistantPolicies(ctx context.Context, client *authz.Client) error {
	organization := organizationTarget("example-org").Resource
	project := authz.ResourceRef{OrganizationID: "example-org", Kind: authz.Project, ID: "project-a"}
	assistant := authz.ResourceRef{OrganizationID: "example-org", Kind: authz.Assistant, ID: "assistant-a"}
	for id, grants := range map[string][]authz.Grant{
		"assistant-project-editor":  {{Permission: authz.AssistantWrite, Anchor: project}},
		"assistant-specific-editor": {{Permission: authz.AssistantWrite, Anchor: assistant}},
		"assistant-org-reader":      {{Permission: authz.AssistantRead, Anchor: organization}},
		"assistant-write-blocked": {
			{Permission: authz.AssistantWrite, Anchor: project},
			{Permission: authz.AssistantBlockedWrite, Anchor: assistant},
		},
		"assistant-read-blocked": {
			{Permission: authz.AssistantWrite, Anchor: project},
			{Permission: authz.AssistantBlockedRead, Anchor: assistant},
		},
		"assistant-project-blocked": {
			{Permission: authz.AssistantWrite, Anchor: organization},
			{Permission: authz.AssistantBlockedWrite, Anchor: project},
		},
	} {
		if err := client.ReplaceGrants(ctx, exampleUserSession(id).Principal, grants); err != nil {
			return err
		}
	}
	return nil
}

// environmentRow carries trusted identity and ancestry, without storing secrets
// in this authorization-only showcase.
type environmentRow struct {
	id             string
	projectID      string
	organizationID string
}

// AuthzTarget supplies the environment and its enclosing policy boundaries.
func (e environmentRow) AuthzTarget() authz.Target {
	return authz.Target{
		Resource: authz.ResourceRef{OrganizationID: e.organizationID, Kind: authz.Environment, ID: e.id},
		Ancestors: []authz.ResourceRef{
			{OrganizationID: e.organizationID, Kind: authz.Project, ID: e.projectID},
			{OrganizationID: e.organizationID, Kind: authz.Organization, ID: e.organizationID},
		},
	}
}

// loadEnvironment simulates a tenant-scoped lookup, not access to secret values.
func (s *service) loadEnvironment(ctx context.Context, organizationID, environmentID string) (environmentRow, error) {
	if err := ctx.Err(); err != nil {
		return environmentRow{}, err
	}
	environment, exists := s.environments[environmentID]
	if !exists || environment.organizationID != organizationID {
		return environmentRow{}, fmt.Errorf("environment not found")
	}
	return environment, nil
}

// readEnvironmentHandler gates secret-bearing reads on the environment scope.
// Successful output contains no real or synthetic secret values.
func (s *service) readEnvironmentHandler(ctx context.Context, subject authz.Subject, environmentID string) (string, error) {
	environment, err := s.loadEnvironment(ctx, subject.Principal.OrganizationID, environmentID)
	if err != nil {
		return "", err
	}
	if err := s.authz.Require(ctx, subject, authz.Request{
		Permission: authz.EnvironmentRead, Target: environment.AuthzTarget(),
	}); err != nil {
		return "", err
	}
	return "environment read authorized (no secret values loaded)", nil
}

// updateEnvironmentHandler checks an existing environment before simulated work.
func (s *service) updateEnvironmentHandler(ctx context.Context, subject authz.Subject, environmentID string) (string, error) {
	environment, err := s.loadEnvironment(ctx, subject.Principal.OrganizationID, environmentID)
	if err != nil {
		return "", err
	}
	if err := s.authz.Require(ctx, subject, authz.Request{
		Permission: authz.EnvironmentWrite, Target: environment.AuthzTarget(),
	}); err != nil {
		return "", err
	}
	return "environment update authorized (no mutation persisted)", nil
}

// createEnvironmentHandler requires environment write covering the project parent,
// not project write or write on a different existing environment.
func (s *service) createEnvironmentHandler(ctx context.Context, subject authz.Subject, projectID string) (string, error) {
	project, err := s.loadProject(ctx, subject.Principal.OrganizationID, projectID)
	if err != nil {
		return "", err
	}
	if err := s.authz.Require(ctx, subject, authz.Request{
		Permission: authz.EnvironmentWrite, Target: project.AuthzTarget(), Creation: true,
	}); err != nil {
		return "", err
	}
	return "environment creation authorized (no environment persisted)", nil
}

// environmentExample demonstrates secret-access isolation, creation boundaries,
// enclosing grants, and directional exclusions through ordinary handlers.
func environmentExample(ctx context.Context, app *service) {
	subject := exampleUserSession("environment-project-editor")
	result, err := app.createEnvironmentHandler(ctx, subject, "project-a")
	report("project environment write: create", result, err)
	result, err = app.readEnvironmentHandler(ctx, subject, "environment-a")
	report("environment write implies secret-bearing read", result, err)
	result, err = app.updateEnvironmentHandler(ctx, subject, "environment-a")
	report("project environment write: update", result, err)
	result, err = app.readEnvironmentHandler(ctx, subject, "environment-b")
	report("environment grant for another project: read", result, err)
	result, err = app.createEnvironmentHandler(ctx, subject, "project-b")
	report("environment grant for another project: create", result, err)

	subject = exampleUserSession("environment-specific-editor")
	result, err = app.updateEnvironmentHandler(ctx, subject, "environment-a")
	report("exact environment write: update", result, err)
	result, err = app.createEnvironmentHandler(ctx, subject, "project-a")
	report("exact environment write: create sibling", result, err)

	subject = exampleUserSession("environment-org-reader")
	result, err = app.readEnvironmentHandler(ctx, subject, "environment-b")
	report("organization environment read: another project", result, err)
	result, err = app.updateEnvironmentHandler(ctx, subject, "environment-a")
	report("environment read alone: update", result, err)
	result, err = app.createEnvironmentHandler(ctx, subject, "project-a")
	report("environment read alone: create", result, err)

	subject = exampleUserSession("environment-write-blocked")
	result, err = app.readEnvironmentHandler(ctx, subject, "environment-a")
	report("blocked environment write retains read", result, err)
	result, err = app.updateEnvironmentHandler(ctx, subject, "environment-a")
	report("blocked environment write: update", result, err)
	subject = exampleUserSession("environment-read-blocked")
	result, err = app.readEnvironmentHandler(ctx, subject, "environment-a")
	report("blocked environment read: read", result, err)
	result, err = app.updateEnvironmentHandler(ctx, subject, "environment-a")
	report("blocked environment read also blocks write", result, err)
	subject = exampleUserSession("environment-project-blocked")
	result, err = app.createEnvironmentHandler(ctx, subject, "project-a")
	report("project environment block: create", result, err)

	for _, id := range []string{"project-editor", "project-metadata-reader", "org-admin-only", "skill-project-editor"} {
		result, err = app.readEnvironmentHandler(ctx, exampleUserSession(id), "environment-a")
		report(id+" without environment grants: read", result, err)
	}
	result, err = app.createEnvironmentHandler(ctx, exampleUserSession("project-editor"), "project-a")
	report("project write alone: create environment", result, err)
}

// seedEnvironmentPolicies provisions scope examples, not authorized grant edits.
func seedEnvironmentPolicies(ctx context.Context, client *authz.Client) error {
	organization := organizationTarget("example-org").Resource
	project := authz.ResourceRef{OrganizationID: "example-org", Kind: authz.Project, ID: "project-a"}
	environment := authz.ResourceRef{OrganizationID: "example-org", Kind: authz.Environment, ID: "environment-a"}
	for id, grants := range map[string][]authz.Grant{
		"environment-project-editor":  {{Permission: authz.EnvironmentWrite, Anchor: project}},
		"environment-specific-editor": {{Permission: authz.EnvironmentWrite, Anchor: environment}},
		"environment-org-reader":      {{Permission: authz.EnvironmentRead, Anchor: organization}},
		"environment-write-blocked": {
			{Permission: authz.EnvironmentWrite, Anchor: project},
			{Permission: authz.EnvironmentBlockedWrite, Anchor: environment},
		},
		"environment-read-blocked": {
			{Permission: authz.EnvironmentWrite, Anchor: project},
			{Permission: authz.EnvironmentBlockedRead, Anchor: environment},
		},
		"environment-project-blocked": {
			{Permission: authz.EnvironmentWrite, Anchor: organization},
			{Permission: authz.EnvironmentBlockedWrite, Anchor: project},
		},
	} {
		if err := client.ReplaceGrants(ctx, exampleUserSession(id).Principal, grants); err != nil {
			return err
		}
	}
	return nil
}

// skillRow adapts trusted skill identity and ancestry, not request-supplied parents.
type skillRow struct {
	id             string
	projectID      string
	organizationID string
}

// AuthzTarget lets exact-skill and enclosing skill grants cover the same resource.
func (s skillRow) AuthzTarget() authz.Target {
	return authz.Target{
		Resource: authz.ResourceRef{OrganizationID: s.organizationID, Kind: authz.Skill, ID: s.id},
		Ancestors: []authz.ResourceRef{
			{OrganizationID: s.organizationID, Kind: authz.Project, ID: s.projectID},
			{OrganizationID: s.organizationID, Kind: authz.Organization, ID: s.organizationID},
		},
	}
}

// loadSkill simulates an organization-scoped lookup without granting access.
func (s *service) loadSkill(ctx context.Context, organizationID, skillID string) (skillRow, error) {
	if err := ctx.Err(); err != nil {
		return skillRow{}, err
	}
	skill, exists := s.skills[skillID]
	if !exists || skill.organizationID != organizationID {
		return skillRow{}, fmt.Errorf("skill not found")
	}
	return skill, nil
}

// readSkillHandler checks the skill family, independently of project metadata access.
func (s *service) readSkillHandler(ctx context.Context, subject authz.Subject, skillID string) (string, error) {
	skill, err := s.loadSkill(ctx, subject.Principal.OrganizationID, skillID)
	if err != nil {
		return "", err
	}
	if err := s.authz.Require(ctx, subject, authz.Request{
		Permission: authz.SkillRead, Target: skill.AuthzTarget(),
	}); err != nil {
		return "", err
	}
	return "skill read authorized", nil
}

// updateSkillHandler demonstrates the spike's resource-level edit gate, not full
// parity with production endpoints that still require project-wide skill access.
func (s *service) updateSkillHandler(ctx context.Context, subject authz.Subject, skillID string) (string, error) {
	skill, err := s.loadSkill(ctx, subject.Principal.OrganizationID, skillID)
	if err != nil {
		return "", err
	}
	if err := s.authz.Require(ctx, subject, authz.Request{
		Permission: authz.SkillWrite, Target: skill.AuthzTarget(),
	}); err != nil {
		return "", err
	}
	return "skill update authorized (no mutation persisted)", nil
}

// createSkillHandler checks skill write against the trusted project parent.
// No future skill ID exists, so an exact-existing-skill grant cannot authorize it.
func (s *service) createSkillHandler(ctx context.Context, subject authz.Subject, projectID string) (string, error) {
	project, err := s.loadProject(ctx, subject.Principal.OrganizationID, projectID)
	if err != nil {
		return "", err
	}
	if err := s.authz.Require(ctx, subject, authz.Request{
		Permission: authz.SkillWrite, Target: project.AuthzTarget(), Creation: true,
	}); err != nil {
		return "", err
	}
	return "skill creation authorized (no skill persisted)", nil
}

// skillExample exercises parent creation, exact and enclosing anchors, directional
// exclusions, wrong-project denial, and isolation from project/MCP permissions.
func skillExample(ctx context.Context, app *service) {
	subject := exampleUserSession("skill-project-editor")
	result, err := app.createSkillHandler(ctx, subject, "project-a")
	report("project skill write: create", result, err)
	result, err = app.readSkillHandler(ctx, subject, "skill-a")
	report("skill write implies skill read", result, err)
	result, err = app.updateSkillHandler(ctx, subject, "skill-a")
	report("project skill write: update", result, err)
	result, err = app.readSkillHandler(ctx, subject, "skill-b")
	report("skill grant for another project: read", result, err)
	result, err = app.createSkillHandler(ctx, subject, "project-b")
	report("skill grant for another project: create", result, err)
	result, err = app.inspectProjectHandler(ctx, subject, "project-a")
	report("skill write alone: inspect project", result, err)
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "list")
	report("skill write alone: invoke MCP tool", result, err)

	subject = exampleUserSession("skill-specific-editor")
	result, err = app.updateSkillHandler(ctx, subject, "skill-a")
	report("exact skill write: update", result, err)
	result, err = app.createSkillHandler(ctx, subject, "project-a")
	report("exact skill write: create sibling", result, err)

	subject = exampleUserSession("skill-org-reader")
	result, err = app.readSkillHandler(ctx, subject, "skill-b")
	report("organization skill read: another project", result, err)
	result, err = app.createSkillHandler(ctx, subject, "project-a")
	report("skill read alone: create", result, err)

	subject = exampleUserSession("skill-write-blocked")
	result, err = app.readSkillHandler(ctx, subject, "skill-a")
	report("blocked skill write retains read", result, err)
	result, err = app.updateSkillHandler(ctx, subject, "skill-a")
	report("blocked skill write: update", result, err)

	subject = exampleUserSession("skill-read-blocked")
	result, err = app.readSkillHandler(ctx, subject, "skill-a")
	report("blocked skill read: read", result, err)
	result, err = app.updateSkillHandler(ctx, subject, "skill-a")
	report("blocked skill read also blocks write", result, err)
	result, err = app.createSkillHandler(ctx, subject, "project-a")
	report("exact skill block does not block sibling creation", result, err)

	subject = exampleUserSession("skill-project-blocked")
	result, err = app.createSkillHandler(ctx, subject, "project-a")
	report("project skill block: create", result, err)
	subject = exampleUserSession("project-editor")
	result, err = app.readSkillHandler(ctx, subject, "skill-a")
	report("project write alone: read skill", result, err)
}

// seedSkillPolicies provisions synthetic grants, not an authorized editing API.
func seedSkillPolicies(ctx context.Context, client *authz.Client) error {
	organization := organizationTarget("example-org").Resource
	project := authz.ResourceRef{OrganizationID: "example-org", Kind: authz.Project, ID: "project-a"}
	skill := authz.ResourceRef{OrganizationID: "example-org", Kind: authz.Skill, ID: "skill-a"}
	for id, grants := range map[string][]authz.Grant{
		"skill-project-editor":  {{Permission: authz.SkillWrite, Anchor: project}},
		"skill-specific-editor": {{Permission: authz.SkillWrite, Anchor: skill}},
		"skill-org-reader":      {{Permission: authz.SkillRead, Anchor: organization}},
		"skill-write-blocked": {
			{Permission: authz.SkillWrite, Anchor: project},
			{Permission: authz.SkillBlockedWrite, Anchor: skill},
		},
		"skill-read-blocked": {
			{Permission: authz.SkillWrite, Anchor: project},
			{Permission: authz.SkillBlockedRead, Anchor: skill},
		},
		"skill-project-blocked": {
			{Permission: authz.SkillWrite, Anchor: organization},
			{Permission: authz.SkillBlockedWrite, Anchor: project},
		},
	} {
		if err := client.ReplaceGrants(ctx, exampleUserSession(id).Principal, grants); err != nil {
			return err
		}
	}
	return nil
}

// organizationTarget adapts the authenticated tenant boundary. An organization
// target names itself and has no ancestors; it does not invent a future project ID.
func organizationTarget(organizationID string) authz.Target {
	return authz.Target{Resource: authz.ResourceRef{
		OrganizationID: organizationID, Kind: authz.Organization, ID: organizationID,
	}}
}

// projectRow supplies trusted project ancestry for existing-project operations.
type projectRow struct {
	id             string
	organizationID string
}

// AuthzTarget supplies the project identity and its canonical tenant ancestor.
func (p projectRow) AuthzTarget() authz.Target {
	return authz.Target{
		Resource: authz.ResourceRef{OrganizationID: p.organizationID, Kind: authz.Project, ID: p.id},
		Ancestors: []authz.ResourceRef{
			{OrganizationID: p.organizationID, Kind: authz.Organization, ID: p.organizationID},
		},
	}
}

// loadProject simulates a tenant-scoped repository lookup, not permission to read.
func (s *service) loadProject(ctx context.Context, organizationID, projectID string) (projectRow, error) {
	if err := ctx.Err(); err != nil {
		return projectRow{}, err
	}
	project, exists := s.projects[projectID]
	if !exists || project.organizationID != organizationID {
		return projectRow{}, fmt.Errorf("project not found")
	}
	return project, nil
}

// createProjectHandler checks organization administration before simulated
// creation, matching the current domain gate rather than inventing project:create.
func (s *service) createProjectHandler(ctx context.Context, subject authz.Subject) (string, error) {
	if err := s.authz.Require(ctx, subject, authz.Request{
		Permission: authz.OrgAdmin, Target: organizationTarget(subject.Principal.OrganizationID),
	}); err != nil {
		return "", err
	}
	return "project creation authorized (no project persisted)", nil
}

// inspectOrganizationHandler demonstrates admin satisfying organization read.
func (s *service) inspectOrganizationHandler(ctx context.Context, subject authz.Subject) (string, error) {
	if err := s.authz.Require(ctx, subject, authz.Request{
		Permission: authz.OrgRead, Target: organizationTarget(subject.Principal.OrganizationID),
	}); err != nil {
		return "", err
	}
	return "organization metadata inspected", nil
}

// inspectProjectHandler requires project read independently of organization admin.
func (s *service) inspectProjectHandler(ctx context.Context, subject authz.Subject, projectID string) (string, error) {
	project, err := s.loadProject(ctx, subject.Principal.OrganizationID, projectID)
	if err != nil {
		return "", err
	}
	if err := s.authz.Require(ctx, subject, authz.Request{
		Permission: authz.ProjectRead, Target: project.AuthzTarget(),
	}); err != nil {
		return "", err
	}
	return "project inspected", nil
}

// updateProjectHandler checks write on an existing project before simulated work.
func (s *service) updateProjectHandler(ctx context.Context, subject authz.Subject, projectID string) (string, error) {
	project, err := s.loadProject(ctx, subject.Principal.OrganizationID, projectID)
	if err != nil {
		return "", err
	}
	if err := s.authz.Require(ctx, subject, authz.Request{
		Permission: authz.ProjectWrite, Target: project.AuthzTarget(),
	}); err != nil {
		return "", err
	}
	return "project update authorized (no mutation persisted)", nil
}

// organizationProjectExample distinguishes administration from per-project access,
// checks directional exclusions, and shows that project grants do not imply MCP use.
func organizationProjectExample(ctx context.Context, app *service) {
	subject := exampleUserSession("org-admin-only")
	result, err := app.createProjectHandler(ctx, subject)
	report("org admin: create project", result, err)
	result, err = app.inspectOrganizationHandler(ctx, subject)
	report("org admin implies org read", result, err)
	result, err = app.inspectProjectHandler(ctx, subject, "project-a")
	report("org admin alone: inspect project", result, err)

	subject = exampleUserSession("project-editor")
	result, err = app.inspectProjectHandler(ctx, subject, "project-a")
	report("project write implies project read", result, err)
	result, err = app.updateProjectHandler(ctx, subject, "project-a")
	report("project write: update", result, err)
	result, err = app.createProjectHandler(ctx, subject)
	report("project write alone: create project", result, err)
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "list")
	report("project write alone: invoke MCP tool", result, err)

	subject = exampleUserSession("project-write-blocked")
	result, err = app.inspectProjectHandler(ctx, subject, "project-a")
	report("blocked project write retains read", result, err)
	result, err = app.updateProjectHandler(ctx, subject, "project-a")
	report("blocked project write: update", result, err)
	subject = exampleUserSession("project-read-blocked")
	result, err = app.updateProjectHandler(ctx, subject, "project-a")
	report("blocked project read also blocks write", result, err)

	subject = exampleUserSession("org-admin-blocked")
	result, err = app.inspectOrganizationHandler(ctx, subject)
	report("blocked org admin retains org read", result, err)
	result, err = app.createProjectHandler(ctx, subject)
	report("blocked org admin: create project", result, err)
	subject = exampleUserSession("org-read-blocked")
	result, err = app.createProjectHandler(ctx, subject)
	report("blocked org read also blocks admin", result, err)
}

// seedOrganizationProjectPolicies provisions fixtures, not authorized role edits.
func seedOrganizationProjectPolicies(ctx context.Context, client *authz.Client) error {
	organization := organizationTarget("example-org").Resource
	project := authz.ResourceRef{OrganizationID: "example-org", Kind: authz.Project, ID: "project-a"}
	for id, grants := range map[string][]authz.Grant{
		"org-admin-only":          {{Permission: authz.OrgAdmin, Anchor: organization}},
		"project-editor":          {{Permission: authz.ProjectWrite, Anchor: project}},
		"project-metadata-reader": {{Permission: authz.ProjectRead, Anchor: project}},
		"project-list-reader":     {{Permission: authz.ProjectRead, Anchor: organization}},
		"project-write-blocked": {
			{Permission: authz.ProjectWrite, Anchor: organization},
			{Permission: authz.ProjectBlockedWrite, Anchor: project},
		},
		"project-read-blocked": {
			{Permission: authz.ProjectWrite, Anchor: organization},
			{Permission: authz.ProjectBlockedRead, Anchor: project},
		},
		"org-admin-blocked": {
			{Permission: authz.OrgAdmin, Anchor: organization},
			{Permission: authz.OrgBlockedAdmin, Anchor: organization},
		},
		"org-read-blocked": {
			{Permission: authz.OrgAdmin, Anchor: organization},
			{Permission: authz.OrgBlockedRead, Anchor: organization},
		},
	} {
		if err := client.ReplaceGrants(ctx, exampleUserSession(id).Principal, grants); err != nil {
			return err
		}
	}
	return nil
}

// exampleUserSession stands in for trusted human-session middleware output.
// It constructs fixture data; it does not authenticate a real session.
func exampleUserSession(id string) authz.Subject {
	return authz.Subject{
		Delegation:     authz.Delegation{Kind: authz.DirectExecution},
		Principal:      authz.Principal{OrganizationID: "example-org", Kind: authz.UserPrincipal, ID: id},
		Authentication: authz.Authentication{Kind: authz.SessionAuthentication, ID: "example-session-" + id},
	}
}

// exampleAgentCredential stands in for authenticated agent-credential middleware
// output with a default represented user. The backend checks the full stored
// principal/delegator binding; the helper does not authenticate either party.
func exampleAgentCredential(agentID, credentialID string) authz.Subject {
	return authz.Subject{
		Delegation: authz.Delegation{Kind: authz.DelegatedExecution,
			Delegator: authz.Principal{OrganizationID: "example-org", Kind: authz.UserPrincipal, ID: "example-delegator"}},
		Principal:      authz.Principal{OrganizationID: "example-org", Kind: authz.AgentPrincipal, ID: agentID},
		Authentication: authz.Authentication{Kind: authz.CredentialAuthentication, ID: credentialID},
	}
}

// serverSpecificExample shows a grant anchored to the exact server authorizing
// connection and invocation independently, without a project-wide grant.
func serverSpecificExample(ctx context.Context, app *service) {
	subject := exampleUserSession("server-user")
	result, err := app.connectHandler(ctx, subject, "server-a")
	report("server-specific grant: connect", result, err)
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "lookup")
	report("server-specific grant: invoke", result, err)
}

// projectGrantExample shows a project-anchored MCP scope covering a child server
// through trusted ancestry. This is not inheritance from project:read.
func projectGrantExample(ctx context.Context, app *service) {
	subject := exampleUserSession("project-user")
	result, err := app.invokeToolHandler(ctx, subject, "server-a", "lookup")
	report("project-wide MCP grant", result, err)
}

// organizationGrantExample shows organization-wide MCP use without wildcard IDs.
// The same grant can cover servers across projects within this organization.
func organizationGrantExample(ctx context.Context, app *service) {
	subject := exampleUserSession("organization-user")
	result, err := app.invokeToolHandler(ctx, subject, "server-a", "lookup")
	report("organization-wide MCP grant", result, err)
}

// missingOrganizationExample shows a malformed adapter output being rejected
// structurally, even when the user has an otherwise sufficient organization grant.
// Ordinary handlers use AuthzTarget, which always includes the organization.
func missingOrganizationExample(ctx context.Context, app *service) {
	subject := exampleUserSession("organization-user")
	server, err := app.loadServer(ctx, subject.Principal.OrganizationID, "server-a")
	if err != nil {
		report("missing organization ancestor", "", err)
		return
	}
	target := server.AuthzTarget()
	target.Ancestors = target.Ancestors[:1] // Deliberately omit the organization.
	err = app.authz.Require(ctx, subject, authz.Request{
		Permission: authz.MCPConnect, Target: target,
		Tool: &authz.ToolFacts{Name: "lookup", Disposition: authz.DispositionUnknown},
	})
	report("missing organization ancestor", "authorization completed", err)
}

// wrongProjectExample shows that an MCP grant for project B cannot authorize a
// server in project A, even though the scope and organization match.
func wrongProjectExample(ctx context.Context, app *service) {
	subject := exampleUserSession("other-project-user")
	result, err := app.invokeToolHandler(ctx, subject, "server-a", "lookup")
	report("another project's grant", result, err)
}

// opposingScopeExample shows a server-wide opposing scope subtracting access
// supplied by a project-wide grant, refusing both admission and invocation.
// This example does not model direct-user exception precedence.
func opposingScopeExample(ctx context.Context, app *service) {
	subject := exampleUserSession("blocked-user")
	result, err := app.connectHandler(ctx, subject, "server-a")
	report("opposing scope: connect", result, err)
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "lookup")
	report("opposing scope: invoke", result, err)
}

// namedToolExclusionExample shows why admission and invocation are different
// questions: blocking lookup leaves usable policy for list, so connection and
// list succeed while lookup is refused.
func namedToolExclusionExample(ctx context.Context, app *service) {
	subject := exampleUserSession("tool-limited-user")
	result, err := app.connectHandler(ctx, subject, "server-a")
	report("tool-specific block: connect", result, err)
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "lookup")
	report("tool-specific block: lookup", result, err)
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "list")
	report("tool-specific block: list", result, err)
}

// readOnlyExample shows that connection needs no disposition, while a concrete
// invocation must satisfy the grant's classification constraint. The unclassified
// tool is refused; the tool classified read_only executes.
func readOnlyExample(ctx context.Context, app *service) {
	subject := exampleUserSession("read-only-user")
	result, err := app.connectHandler(ctx, subject, "server-a")
	report("read-only grant: connect without disposition", result, err)
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "lookup")
	report("read-only grant: unclassified invocation", result, err)
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "list")
	report("read-only grant: classified invocation", result, err)
}

// projectReadOnlyFlowExample follows a project-anchored read-only grant through
// the invocation handler: list_documents matches both ancestry and classification;
// delete_document fails classification, while lookup has insufficient facts.
func projectReadOnlyFlowExample(ctx context.Context, app *service) {
	subject := exampleUserSession("project-reader")
	result, err := app.invokeToolHandler(ctx, subject, "server-a", "list_documents")
	report("project read-only flow: list_documents", result, err)
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "delete_document")
	report("project read-only flow: delete_document", result, err)
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "lookup")
	report("project read-only flow: unclassified lookup", result, err)
}

// blockedDeleteFlowExample follows separate admission and execution checks:
// connection succeeds because usable policy remains, delete_document is refused
// by its named opposing grant, and list_documents is still permitted.
func blockedDeleteFlowExample(ctx context.Context, app *service) {
	subject := exampleUserSession("delete-blocked-user")
	result, err := app.connectHandler(ctx, subject, "server-a")
	report("blocked-delete flow: connect", result, err)
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "delete_document")
	report("blocked-delete flow: delete_document", result, err)
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "list_documents")
	report("blocked-delete flow: list_documents", result, err)
}

// unclassifiedToolExample shows that unknown disposition does not automatically
// deny execution: an unconstrained grant permits the unclassified tool.
func unclassifiedToolExample(ctx context.Context, app *service) {
	subject := exampleUserSession("server-user")
	result, err := app.invokeToolHandler(ctx, subject, "server-a", "lookup")
	report("unconstrained grant: unclassified invocation", result, err)
}

// inspectionExample shows the handler owning its scope choice: mcp:read permits
// this inspection endpoint, whereas mcp:connect alone does not.
func inspectionExample(ctx context.Context, app *service) {
	subject := exampleUserSession("inspector")
	result, err := app.inspectServerHandler(ctx, subject, "server-a")
	report("inspection with mcp:read", result, err)
	subject = exampleUserSession("server-user")
	result, err = app.inspectServerHandler(ctx, subject, "server-a")
	report("inspection with only mcp:connect", result, err)
}

// typedIdentityExample shows principal type and authentication kind as independent
// inputs. Equal user/agent IDs have separate policies, users can use credentials,
// and an unknown authentication kind or mismatched credential binding is refused.
func typedIdentityExample(ctx context.Context, app *service) {
	subject := authz.Subject{
		Delegation:     authz.Delegation{Kind: authz.DirectExecution},
		Principal:      authz.Principal{OrganizationID: "example-org", Kind: authz.UserPrincipal, ID: "server-user"},
		Authentication: authz.Authentication{Kind: authz.CredentialAuthentication, ID: "user-key"},
	}
	result, err := app.invokeToolHandler(ctx, subject, "server-a", "lookup")
	report("user authenticated with a credential", result, err)

	subject = exampleAgentCredential("server-user", "same-id-agent-key")
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "lookup")
	report("same ID, agent policy: lookup", result, err)
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "list")
	report("same ID, agent policy: list", result, err)

	subject = exampleAgentCredential("limited-agent", "user-key")
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "list")
	report("credential bound to another principal", result, err)

	subject = exampleUserSession("server-user")
	subject.Authentication.Kind = ""
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "lookup")
	report("missing authentication kind", result, err)

	subject = exampleAgentCredential("limited-agent", "limited-agent-key")
	subject.Authentication = authz.Authentication{Kind: authz.SessionAuthentication, ID: "example-session"}
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "list")
	report("unsupported agent human-session combination", result, err)
}

// agentGrantExample evaluates the agent's assigned policy through the ordinary
// handler, without loading owner permissions or a separate credential policy.
func agentGrantExample(ctx context.Context, app *service) {
	subject := exampleAgentCredential("limited-agent", "limited-agent-key")
	result, err := app.invokeToolHandler(ctx, subject, "server-a", "list")
	report("agent grant: list", result, err)
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "lookup")
	report("agent grant: lookup", result, err)
}

// revocationExample shows a fresh check refusing the next invocation after the
// last grant is removed, even though this subject previously established access.
// It does not demonstrate cancellation of already-running work.
func revocationExample(ctx context.Context, app *service) error {
	subject := exampleUserSession("server-user")
	// A fixture edit simulates committed revocation. This is not a management
	// handler: editor authority will be shown with the role-management slice.
	if err := app.authz.ReplaceGrants(ctx, subject.Principal, nil); err != nil {
		return err
	}
	result, err := app.invokeToolHandler(ctx, subject, "server-a", "lookup")
	report("next invocation after last grant revoked", result, err)
	return nil
}

func report(label, result string, err error) {
	if err != nil {
		fmt.Printf("%s: refused (%v)\n", label, err)
		return
	}
	fmt.Printf("%s: %s\n", label, result)
}

// seedExamplePolicies is fixture provisioning, separate from request handlers.
// These synthetic writes do not demonstrate role or audience editing authority.
func seedExamplePolicies(ctx context.Context, client *authz.Client) error {
	organization := authz.ResourceRef{OrganizationID: "example-org", Kind: authz.Organization, ID: "example-org"}
	server := authz.ResourceRef{OrganizationID: "example-org", Kind: authz.MCPServer, ID: "server-a"}
	project := authz.ResourceRef{OrganizationID: "example-org", Kind: authz.Project, ID: "project-a"}
	otherProject := authz.ResourceRef{OrganizationID: "example-org", Kind: authz.Project, ID: "project-b"}
	policies := map[string][]authz.Grant{
		"organization-user":  {{Permission: authz.MCPConnect, Anchor: organization, Constraints: authz.Constraints{}}},
		"server-user":        {{Permission: authz.MCPConnect, Anchor: server, Constraints: authz.Constraints{}}},
		"project-user":       {{Permission: authz.MCPConnect, Anchor: project, Constraints: authz.Constraints{}}},
		"other-project-user": {{Permission: authz.MCPConnect, Anchor: otherProject, Constraints: authz.Constraints{}}},
		"blocked-user": {
			{Permission: authz.MCPConnect, Anchor: project, Constraints: authz.Constraints{}},
			{Permission: authz.MCPBlockedConnect, Anchor: server, Constraints: authz.Constraints{}},
		},
		"tool-limited-user": {
			{Permission: authz.MCPConnect, Anchor: server, Constraints: authz.Constraints{}},
			{Permission: authz.MCPBlockedConnect, Anchor: server, Constraints: authz.Constraints{Tool: "lookup", Disposition: authz.DispositionUnknown}},
		},
		"read-only-user": {{Permission: authz.MCPConnect, Anchor: server, Constraints: authz.Constraints{Tool: "", Disposition: authz.ReadOnly}}},
		"project-reader": {{Permission: authz.MCPConnect, Anchor: project, Constraints: authz.Constraints{Disposition: authz.ReadOnly}}},
		"delete-blocked-user": {
			{Permission: authz.MCPConnect, Anchor: server, Constraints: authz.Constraints{}},
			{Permission: authz.MCPBlockedConnect, Anchor: server, Constraints: authz.Constraints{Tool: "delete_document"}},
		},
		"inspector": {{Permission: authz.MCPRead, Anchor: server, Constraints: authz.Constraints{}}},
	}
	for id, grants := range policies {
		if err := client.ReplaceGrants(ctx, authz.Principal{OrganizationID: "example-org", Kind: authz.UserPrincipal, ID: id}, grants); err != nil {
			return err
		}
	}
	return nil
}

// seedAgentPolicies provisions synthetic agents and credential bindings.
// These fixtures do not implement creation authority, containment, or lifecycle.
func seedAgentPolicies(ctx context.Context, client *authz.Client) error {
	server := authz.ResourceRef{OrganizationID: "example-org", Kind: authz.MCPServer, ID: "server-a"}
	listOnly := []authz.Grant{{Permission: authz.MCPConnect, Anchor: server, Constraints: authz.Constraints{Tool: "list"}}}
	for _, id := range []string{"limited-agent", "server-user"} {
		principal := authz.Principal{OrganizationID: "example-org", Kind: authz.AgentPrincipal, ID: id}
		if err := client.ReplaceGrants(ctx, principal, listOnly); err != nil {
			return err
		}
	}
	for credentialID, principal := range map[string]authz.Principal{
		"limited-agent-key": {OrganizationID: "example-org", Kind: authz.AgentPrincipal, ID: "limited-agent"},
		"same-id-agent-key": {OrganizationID: "example-org", Kind: authz.AgentPrincipal, ID: "server-user"},
		"user-key":          {OrganizationID: "example-org", Kind: authz.UserPrincipal, ID: "server-user"},
	} {
		var err error
		if principal.Kind == authz.AgentPrincipal {
			delegator := authz.Principal{OrganizationID: "example-org", Kind: authz.UserPrincipal, ID: "example-delegator"}
			err = client.BindDelegatedCredential(ctx, principal, delegator, credentialID)
		} else {
			err = client.BindCredential(ctx, principal, credentialID)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// seedDelegationPolicies provisions authenticated execution-context fixtures.
// Binding is not proof of safe issuance, user authentication, or owner eligibility.
func seedDelegationPolicies(ctx context.Context, client *authz.Client) error {
	organization := organizationTarget("example-org").Resource
	project := authz.ResourceRef{OrganizationID: "example-org", Kind: authz.Project, ID: "project-a"}
	server := authz.ResourceRef{OrganizationID: "example-org", Kind: authz.MCPServer, ID: "server-a"}
	broad := []authz.Grant{
		{Permission: authz.MCPConnect, Anchor: organization},
		{Permission: authz.ProjectRead, Anchor: organization},
		{Permission: authz.PluginWrite, Anchor: organization},
	}
	agent := authz.Principal{OrganizationID: "example-org", Kind: authz.AgentPrincipal, ID: "delegation-agent"}
	if err := client.ReplaceGrants(ctx, agent, broad); err != nil {
		return err
	}
	for id, grants := range map[string][]authz.Grant{
		"example-delegator": broad,
		"delegator-list": {
			{Permission: authz.MCPConnect, Anchor: server, Constraints: authz.Constraints{Tool: "list"}},
			{Permission: authz.ProjectRead, Anchor: project},
		},
		"delegator-lookup": {{Permission: authz.MCPConnect, Anchor: server, Constraints: authz.Constraints{Tool: "lookup"}}},
	} {
		if err := client.ReplaceGrants(ctx, exampleUserSession(id).Principal, grants); err != nil {
			return err
		}
	}
	if err := client.BindDelegatedCredential(ctx, agent, exampleUserSession("delegator-list").Principal, "delegation-list-key"); err != nil {
		return err
	}
	limited := authz.Principal{OrganizationID: "example-org", Kind: authz.AgentPrincipal, ID: "limited-agent"}
	return client.BindDelegatedCredential(ctx, limited, exampleUserSession("delegator-lookup").Principal, "delegation-disjoint-key")
}

// delegationExample bounds the executing agent by the represented user, not its
// owner, and exercises concrete, discovery, collection, and composed checks.
func delegationExample(ctx context.Context, app *service) error {
	subject := exampleAgentCredential("delegation-agent", "delegation-list-key")
	subject.Delegation.Delegator = exampleUserSession("delegator-list").Principal
	result, err := app.invokeToolHandler(ctx, subject, "server-a", "list")
	report("delegator and agent both permit list", result, err)
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "lookup")
	report("broad agent cannot exceed delegator", result, err)
	result, err = app.listProjectsHandler(ctx, subject)
	report("delegated collection uses common project access", result, err)
	result, err = app.updatePluginHandler(ctx, subject, "plugin-a")
	report("delegator also bounds exclusion-aware alternatives", result, err)
	result, err = app.readPluginMetadataHandler(ctx, subject, "plugin-a")
	report("delegator also bounds grouped alternatives", result, err)

	disjoint := exampleAgentCredential("limited-agent", "delegation-disjoint-key")
	disjoint.Delegation.Delegator = exampleUserSession("delegator-lookup").Principal
	result, err = app.connectHandler(ctx, disjoint, "server-a")
	report("list-only agent and lookup-only delegator cannot connect", result, err)

	spoofed := subject
	spoofed.Delegation.Delegator = exampleUserSession("example-delegator").Principal
	result, err = app.invokeToolHandler(ctx, spoofed, "server-a", "lookup")
	report("credential cannot substitute a more powerful delegator", result, err)
	spoofed = subject
	spoofed.Delegation = authz.Delegation{Kind: authz.DirectExecution}
	result, err = app.invokeToolHandler(ctx, spoofed, "server-a", "lookup")
	report("agent cannot omit delegation to bypass the bound", result, err)

	if err := app.authz.ReplaceGrants(ctx, subject.Delegation.Delegator, nil); err != nil {
		return err
	}
	result, err = app.invokeToolHandler(ctx, subject, "server-a", "list")
	report("same delegated credential after delegator revocation", result, err)
	return nil
}
