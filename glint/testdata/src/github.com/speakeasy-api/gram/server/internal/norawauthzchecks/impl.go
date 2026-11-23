package norawauthzchecks

import (
	"errors"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

func literal(project string) authz.Check {
	return authz.Check{Scope: "project:read", ResourceID: project} // want `replace the authz.Check literal: build authorization checks with the typed refs`
}

func zeroBesideError() (authz.Check, error) {
	return authz.Check{}, errors.New("no project")
}

func zeroWithoutError() (authz.Check, error) {
	return authz.Check{}, nil // want `replace the authz.Check literal: build authorization checks with the typed refs`
}

func zeroAlone() authz.Check {
	return authz.Check{} // want `replace the authz.Check literal: build authorization checks with the typed refs`
}

func allowed(env, project string) []authz.Check {
	ref := authz.Environment(authz.EnvironmentIDFrom(env), authz.ProjectIDFrom(project))
	return []authz.Check{
		ref.Read(),
		authz.InferenceExecuteCheck(project),
	}
}
