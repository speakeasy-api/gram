package authz

import (
	"github.com/google/uuid"

	"testing"

	"github.com/stretchr/testify/require"
)

// Each typed request builds the same check as the constructor or literal the
// handlers use today, so migrating a handler to refs changes nothing it
// enforces.
func TestRefs_MatchTodaysChecks(t *testing.T) {
	t.Parallel()

	const (
		org     = "org-1"
		project = "project-1"
		id      = "resource-1"
	)
	literal := func(scope Scope, resourceID string, dimensions map[string]string) Check {
		return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, selectorMatch: selectorMatchNormal, representedUser: "", probe: false, anyOf: nil, operation: operationDefault, organization: None[OrgID]()}
	}

	cases := map[string]struct{ ref, today Check }{
		"org read":           {Org(OrgIDFrom(org)).Read(), literal(ScopeOrgRead, org, nil)},
		"org admin":          {Org(OrgIDFrom(org)).Admin(), literal(ScopeOrgAdmin, org, nil)},
		"org device sync":    {Org(OrgIDFrom(org)).DeviceAgentSync(), literal(ScopeOrgDeviceAgentSync, org, nil)},
		"org hooks ingest":   {Org(OrgIDFrom(org)).HooksIngest(ProjectIDFrom(project)), HooksIngestCheck(org, project)},
		"project read":       {Project(ProjectIDFrom(project)).Read(), literal(ScopeProjectRead, project, nil)},
		"project write":      {Project(ProjectIDFrom(project)).Write(), literal(ScopeProjectWrite, project, nil)},
		"plugin write":       {Project(ProjectIDFrom(project)).PluginWrite(), literal(ScopePluginWrite, project, nil)},
		"log read":           {Project(ProjectIDFrom(project)).LogRead(), literal(ScopeLogRead, project, nil)},
		"mcp read":           {MCPServer(MCPServerIDFrom(id), ProjectIDFrom(project)).Read(), MCPCheck(ScopeMCPRead, id, project)},
		"mcp write":          {MCPServer(MCPServerIDFrom(id), ProjectIDFrom(project)).Write(), MCPCheck(ScopeMCPWrite, id, project)},
		"mcp connect":        {MCPServer(MCPServerIDFrom(id), ProjectIDFrom(project)).Connect(), MCPCheck(ScopeMCPConnect, id, project)},
		"mcp create":         {Project(ProjectIDFrom(project)).MCPServers().Create(), MCPCheck(ScopeMCPWrite, project, project)},
		"mcp tool call":      {MCPServer(MCPServerIDFrom(id), ProjectIDFrom(project)).ToolCall("tool", "destructive"), MCPToolCallCheck(id, MCPToolCallDimensions{Tool: "tool", Disposition: "destructive", ProjectID: project})},
		"environment read":   {Environment(EnvironmentIDFrom(id), ProjectIDFrom(project)).Read(), EnvironmentCheck(ScopeEnvironmentRead, id, project)},
		"environment write":  {Environment(EnvironmentIDFrom(id), ProjectIDFrom(project)).Write(), EnvironmentCheck(ScopeEnvironmentWrite, id, project)},
		"environments read":  {Project(ProjectIDFrom(project)).Environments().Read(), EnvironmentCheck(ScopeEnvironmentRead, project, project)},
		"environments write": {Project(ProjectIDFrom(project)).Environments().Write(), EnvironmentCheck(ScopeEnvironmentWrite, project, project)},
		"environment create": {Project(ProjectIDFrom(project)).Environments().Create(), EnvironmentCheck(ScopeEnvironmentWrite, project, project)},
		"assistant read":     {Assistant(AssistantIDFrom(id), ProjectIDFrom(project)).Read(), AssistantCheck(ScopeAssistantRead, id, project)},
		"assistant write":    {Assistant(AssistantIDFrom(id), ProjectIDFrom(project)).Write(), AssistantCheck(ScopeAssistantWrite, id, project)},
		"assistant create":   {Project(ProjectIDFrom(project)).Assistants().Create(), AssistantCheck(ScopeAssistantWrite, project, project)},
		"skill read":         {Skill(SkillIDFrom(id)).Read(), literal(ScopeSkillRead, id, nil)},
		"skill write":        {Skill(SkillIDFrom(id)).Write(), literal(ScopeSkillWrite, id, nil)},
		"chat read":          {Chat(ChatIDFrom(id)).Read(), ChatReadCheck(id)},
		"chat write":         {Chat(ChatIDFrom(id)).Write(), ChatWriteCheck(id)},
		"agent read":         {Agent(AgentIDFrom(id)).Read(), literal(ScopeAgentRead, id, nil)},
		"agent write":        {Agent(AgentIDFrom(id)).Write(), literal(ScopeAgentWrite, id, nil)},
		"agent authorize":    {Agent(AgentIDFrom(id)).Authorize(), literal(ScopeAgentAuthorize, id, nil)},
		"agent transfer":     {Agent(AgentIDFrom(id)).Transfer(), literal(ScopeAgentTransfer, id, nil)},
		"workload read":      {Workload(WorkloadIDFrom(id)).Read(), literal(ScopeWorkloadRead, id, nil)},
		"workload write":     {Workload(WorkloadIDFrom(id)).Write(), literal(ScopeWorkloadWrite, id, nil)},
	}
	for name, c := range cases {
		require.Equal(t, c.today, c.ref, name)
	}
}

// Every permission of a kind with a generated ref is reachable through it, so
// a permission added to the registry gets a method without hand work.
func TestRefs_CoverEveryPermission(t *testing.T) {
	t.Parallel()

	project := Project(ProjectIDFrom("project-1"))
	reached := make(map[Scope]bool)
	for _, check := range []Check{
		Org(OrgIDFrom("o")).Read(), Org(OrgIDFrom("o")).Admin(), Org(OrgIDFrom("o")).DeviceAgentSync(), Org(OrgIDFrom("o")).HooksIngest(ProjectIDFrom("p")),
		project.Read(), project.Write(), project.PluginWrite(), project.LogRead(),
		MCPServer(MCPServerIDFrom("m"), ProjectIDFrom("p")).Read(), MCPServer(MCPServerIDFrom("m"), ProjectIDFrom("p")).Write(), MCPServer(MCPServerIDFrom("m"), ProjectIDFrom("p")).Connect(),
		Environment(EnvironmentIDFrom("e"), ProjectIDFrom("p")).Read(), Environment(EnvironmentIDFrom("e"), ProjectIDFrom("p")).Write(),
		Skill(SkillIDFrom("s")).Read(), Skill(SkillIDFrom("s")).Write(),
		Assistant(AssistantIDFrom("a"), ProjectIDFrom("p")).Read(), Assistant(AssistantIDFrom("a"), ProjectIDFrom("p")).Write(),
		Chat(ChatIDFrom("c")).Read(), Chat(ChatIDFrom("c")).Write(),
		Agent(AgentIDFrom("a")).Read(), Agent(AgentIDFrom("a")).Write(), Agent(AgentIDFrom("a")).Authorize(), Agent(AgentIDFrom("a")).Transfer(),
		Workload(WorkloadIDFrom("w")).Read(), Workload(WorkloadIDFrom("w")).Write(),
	} {
		reached[check.Scope] = true
	}
	handWritten := map[string]bool{ResourceKindMCPAuthClient: true, ResourceKindRiskPolicy: true, ResourceKindInference: true}
	for _, entry := range scopeCatalog {
		if handWritten[entry.ResourceType] || scopeVisibilityByScope[entry.Scope] != scopeVisibilityUserVisible {
			continue
		}
		require.True(t, reached[entry.Scope], "%s has no ref method in this test; add it", entry.Scope)
	}
}

// A ref built without its project is refused at evaluation instead of
// matching project-bounded grants by omission.
func TestRefs_MissingProjectIsInvalid(t *testing.T) {
	t.Parallel()

	require.Error(t, validateInput(MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("")).Write()))
	require.Error(t, validateInput(Environment(EnvironmentIDFrom("env-1"), ProjectIDFrom("")).Read()))
	require.Error(t, validateInput(Assistant(AssistantIDFrom("assistant-1"), ProjectIDFrom("")).Read()))
	require.Error(t, validateInput(Org(OrgIDFrom("org-1")).HooksIngest(ProjectIDFrom(""))))
}

// A ref built from uuids builds the same check as one built from their
// strings.
func TestRefs_UUIDAndStringIDsBuildTheSameCheck(t *testing.T) {
	t.Parallel()
	env := uuid.New()
	project := uuid.New()
	require.Equal(t,
		Environment(EnvironmentIDFrom(env.String()), ProjectIDFrom(project.String())).Read(),
		Environment(EnvironmentIDOf(env), ProjectIDOf(project)).Read(),
	)
	require.Equal(t, Project(ProjectIDFrom(project.String())).Write(), Project(ProjectIDOf(project)).Write())
}

// An id that cannot name a resource is absent, and every check built from it
// is refused at evaluation whatever the caller holds.
func TestRefs_AbsentIDsAreRefused(t *testing.T) {
	t.Parallel()

	project := ProjectIDFrom("project-1")
	absent := map[string]Check{
		"zero project id":        Project(ProjectID{}).Read(),
		"nil uuid":               Project(ProjectIDOf(uuid.Nil)).Read(),
		"empty string":           Project(ProjectIDFrom("")).Read(),
		"wildcard":               Project(ProjectIDFrom(WildcardResource)).Read(),
		"reserved marker":        Project(ProjectIDFrom(SelectorValueRepresentedUser)).Read(),
		"whitespace":             Project(ProjectIDFrom("project 1")).Read(),
		"empty resource":         Environment(EnvironmentIDFrom(""), project).Read(),
		"nil uuid parent":        Environment(EnvironmentIDFrom("env-1"), ProjectIDOf(uuid.Nil)).Read(),
		"wildcard parent":        MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom(WildcardResource)).Write(),
		"empty extra dimension":  Org(OrgIDFrom("org-1")).HooksIngest(ProjectID{}),
		"collection of no one":   Project(ProjectID{}).Environments().Create(),
		"mcp collection, no one": Project(ProjectIDOf(uuid.Nil)).MCPServers().Create(),
	}
	root := []Grant{{PrincipalUrn: "user:u1", Scope: ScopeRoot, Selector: Selector{SelectorKeyResourceKind: ResourceKindWildcard, SelectorKeyResourceID: WildcardResource}, Inferred: false}}
	for name, check := range absent {
		require.ErrorIs(t, validateInput(check), ErrInvalidCheck, name)
		allowed, err := GrantsAuthorize(root, check)
		require.ErrorIs(t, err, ErrInvalidCheck, name)
		require.False(t, allowed, name)
	}
}
