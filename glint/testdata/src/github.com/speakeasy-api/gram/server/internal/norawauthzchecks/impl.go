package norawauthzchecks

import "github.com/speakeasy-api/gram/server/internal/authz"

func literal(project string) authz.Check {
	return authz.Check{Scope: "project:read", ResourceID: project} // want `replace the authz.Check literal: build authorization checks with the typed refs`
}

func constructors(env, project string) []authz.Check {
	return []authz.Check{
		authz.EnvironmentCheck("environment:read", env, project), // want `replace authz.EnvironmentCheck: build authorization checks`
		authz.MCPCheck("mcp:read", env, project),                 // want `replace authz.MCPCheck: build authorization checks`
	}
}

func allowed(env, project string) []authz.Check {
	ref := authz.Environment(authz.EnvironmentID(env), authz.ProjectID(project))
	return []authz.Check{
		ref.Read(),
		ref.MCPCheck(),
		authz.InferenceExecuteCheck(project),
	}
}
