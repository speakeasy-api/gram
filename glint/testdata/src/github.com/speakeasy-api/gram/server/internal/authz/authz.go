// Package authz is a stand-in for server/internal/authz with just the
// shapes no-raw-authz-checks looks at.
package authz

type Scope string

type Check struct {
	Scope      Scope
	ResourceID string
	Dimensions map[string]string
}

func InferenceExecuteCheck(projectID string) Check {
	return Check{Scope: "inference:execute", ResourceID: projectID}
}

type EnvironmentID struct{ id string }

func EnvironmentIDFrom(id string) EnvironmentID { return EnvironmentID{id: id} }

type ProjectID struct{ id string }

func ProjectIDFrom(id string) ProjectID { return ProjectID{id: id} }

type EnvironmentRef struct {
	id      EnvironmentID
	project ProjectID
}

func Environment(id EnvironmentID, project ProjectID) EnvironmentRef {
	return EnvironmentRef{id: id, project: project}
}

func (r EnvironmentRef) Read() Check {
	return Check{Scope: "environment:read", ResourceID: r.id.id, Dimensions: map[string]string{"project_id": r.project.id}}
}
