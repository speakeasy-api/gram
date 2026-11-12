package authz

import (
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
		return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, selectorMatch: selectorMatchNormal, representedUser: ""}
	}

	cases := map[string]struct{ ref, today Check }{
		"org read":           {Org(org).Read(), literal(ScopeOrgRead, org, nil)},
		"org admin":          {Org(org).Admin(), literal(ScopeOrgAdmin, org, nil)},
		"org device sync":    {Org(org).DeviceAgentSync(), literal(ScopeOrgDeviceAgentSync, org, nil)},
		"org hooks ingest":   {Org(org).HooksIngest(project), HooksIngestCheck(org, project)},
		"project read":       {Project(project).Read(), literal(ScopeProjectRead, project, nil)},
		"project write":      {Project(project).Write(), literal(ScopeProjectWrite, project, nil)},
		"plugin write":       {Project(project).PluginWrite(), literal(ScopePluginWrite, project, nil)},
		"log read":           {Project(project).LogRead(), literal(ScopeLogRead, project, nil)},
		"mcp read":           {MCPServer(id, project).Read(), MCPCheck(ScopeMCPRead, id, project)},
		"mcp write":          {MCPServer(id, project).Write(), MCPCheck(ScopeMCPWrite, id, project)},
		"mcp connect":        {MCPServer(id, project).Connect(), MCPCheck(ScopeMCPConnect, id, project)},
		"mcp create":         {Project(project).MCPServers().Create(), MCPCheck(ScopeMCPWrite, project, project)},
		"mcp tool call":      {MCPServer(id, project).ToolCall("tool", "destructive"), MCPToolCallCheck(id, MCPToolCallDimensions{Tool: "tool", Disposition: "destructive", ProjectID: project})},
		"environment read":   {Environment(id, project).Read(), EnvironmentCheck(ScopeEnvironmentRead, id, project)},
		"environment write":  {Environment(id, project).Write(), EnvironmentCheck(ScopeEnvironmentWrite, id, project)},
		"environment create": {Project(project).Environments().Create(), EnvironmentCheck(ScopeEnvironmentWrite, project, project)},
		"assistant read":     {Assistant(id, project).Read(), AssistantCheck(ScopeAssistantRead, id, project)},
		"assistant write":    {Assistant(id, project).Write(), AssistantCheck(ScopeAssistantWrite, id, project)},
		"assistant create":   {Project(project).Assistants().Create(), AssistantCheck(ScopeAssistantWrite, project, project)},
		"skill read":         {Skill(id).Read(), literal(ScopeSkillRead, id, nil)},
		"skill write":        {Skill(id).Write(), literal(ScopeSkillWrite, id, nil)},
		"chat read":          {Chat(id).Read(), ChatReadCheck(id)},
		"chat write":         {Chat(id).Write(), ChatWriteCheck(id)},
		"agent read":         {Agent(id).Read(), literal(ScopeAgentRead, id, nil)},
		"agent write":        {Agent(id).Write(), literal(ScopeAgentWrite, id, nil)},
		"agent authorize":    {Agent(id).Authorize(), literal(ScopeAgentAuthorize, id, nil)},
		"agent transfer":     {Agent(id).Transfer(), literal(ScopeAgentTransfer, id, nil)},
		"workload read":      {Workload(id).Read(), literal(ScopeWorkloadRead, id, nil)},
		"workload write":     {Workload(id).Write(), literal(ScopeWorkloadWrite, id, nil)},
	}
	for name, c := range cases {
		require.Equal(t, c.today, c.ref, name)
	}
}

// Every permission of a kind with a generated ref is reachable through it, so
// a permission added to the registry gets a method without hand work.
func TestRefs_CoverEveryPermission(t *testing.T) {
	t.Parallel()

	project := Project("project-1")
	reached := make(map[Scope]bool)
	for _, check := range []Check{
		Org("o").Read(), Org("o").Admin(), Org("o").DeviceAgentSync(), Org("o").HooksIngest("p"),
		project.Read(), project.Write(), project.PluginWrite(), project.LogRead(),
		MCPServer("m", "p").Read(), MCPServer("m", "p").Write(), MCPServer("m", "p").Connect(),
		Environment("e", "p").Read(), Environment("e", "p").Write(),
		Skill("s").Read(), Skill("s").Write(),
		Assistant("a", "p").Read(), Assistant("a", "p").Write(),
		Chat("c").Read(), Chat("c").Write(),
		Agent("a").Read(), Agent("a").Write(), Agent("a").Authorize(), Agent("a").Transfer(),
		Workload("w").Read(), Workload("w").Write(),
	} {
		reached[check.Scope] = true
	}
	handWritten := map[string]bool{ResourceKindMCPAuthClient: true, ResourceKindRiskPolicy: true, ResourceKindInference: true}
	for _, typ := range Model.Types {
		if handWritten[string(typ.Kind)] {
			continue
		}
		for _, p := range typ.Permissions {
			require.True(t, reached[typ.ScopeFor(p)], "%s has no ref method in this test; add it", typ.ScopeFor(p))
		}
	}
}

// A ref built without its project is refused at evaluation instead of
// matching project-bounded grants by omission.
func TestRefs_MissingProjectIsInvalid(t *testing.T) {
	t.Parallel()

	require.Error(t, validateInput(MCPServer("server-1", "").Write()))
	require.Error(t, validateInput(Environment("env-1", "").Read()))
	require.Error(t, validateInput(Assistant("assistant-1", "").Read()))
	require.Error(t, validateInput(Org("org-1").HooksIngest("")))
}
