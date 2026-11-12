// Package environments carries the production import path of a migrated
// package, so the rule's default package list is exercised.
package environments

import "github.com/speakeasy-api/gram/server/internal/authz"

func raw(env, project string) authz.Check {
	return authz.EnvironmentCheck("environment:read", env, project) // want `replace authz.EnvironmentCheck: build authorization checks`
}
