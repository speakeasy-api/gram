// Package environments carries a production import path outside authz,
// package, so the rule's default reach is exercised.
package environments

import "github.com/speakeasy-api/gram/server/internal/authz"

func raw(env, project string) authz.Check {
	return authz.Check{Scope: "environment:read", ResourceID: env, Dimensions: map[string]string{"project_id": project}} // want `replace the authz.Check literal: build authorization checks`
}
