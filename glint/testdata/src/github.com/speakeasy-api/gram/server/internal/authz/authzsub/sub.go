// Package authzsub stands in for a package under internal/authz. Only
// internal/authz itself is exempt, so the rule still applies here.
package authzsub

import "github.com/speakeasy-api/gram/server/internal/authz"

func check(project string) authz.Check {
	return authz.Check{Scope: "project:read", ResourceID: project} // want `replace the authz.Check literal: build authorization checks with the typed refs`
}
