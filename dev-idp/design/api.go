// Package design declares the Goa API for the dev-idp management surface.
//
// This API is intentionally separate from the production gram API. It lives
// under the top-level dev-idp/ project so its design and generated code
// cannot leak into the production binary.
//
// Every method in every service here is unauthenticated. dev-idp is a
// localhost-only tool.
package design

import (
	"goa.design/goa/v3/expr"

	. "goa.design/goa/v3/dsl"
)

var _ = API("gram-dev-idp", func() {
	Title("Gram dev-idp")
	Description("Management API for the local-development IDP that backs Gram's auth flows in tests and dev. Permanently unauthenticated.")
	Meta("openapi:example", "false")
	Randomizer(expr.NewDeterministicRandomizer())
})
