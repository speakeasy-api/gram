// Package authz is a stand-in for server/internal/authz with just the
// shapes no-raw-authz-checks looks at.
package authz

type Scope string

type Check struct {
	Scope      Scope
	ResourceID string
	Dimensions map[string]string
}

func EnvironmentCheck(scope Scope, resourceID, projectID string) Check {
	return Check{Scope: scope, ResourceID: resourceID, Dimensions: map[string]string{"project_id": projectID}}
}

func MCPCheck(scope Scope, resourceID, projectID string) Check {
	return Check{Scope: scope, ResourceID: resourceID, Dimensions: map[string]string{"project_id": projectID}}
}

func InferenceExecuteCheck(projectID string) Check {
	return Check{Scope: "inference:execute", ResourceID: projectID}
}

type EnvironmentID string

type ProjectID string

type EnvironmentRef struct {
	id      EnvironmentID
	project ProjectID
}

func Environment(id EnvironmentID, project ProjectID) EnvironmentRef {
	return EnvironmentRef{id: id, project: project}
}

func (r EnvironmentRef) Read() Check {
	return EnvironmentCheck("environment:read", string(r.id), string(r.project))
}

// MCPCheck as a method is not the constructor the rule bans.
func (r EnvironmentRef) MCPCheck() Check {
	return Check{Scope: "mcp:read", ResourceID: string(r.id)}
}
