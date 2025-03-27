package design

import (
	. "goa.design/goa/v3/dsl"

	_ "github.com/speakeasy-api/gram/design/auth"
	_ "github.com/speakeasy-api/gram/design/deployments"
	_ "github.com/speakeasy-api/gram/design/system"
)

var _ = API("gram", func() {
	Title("Gram API Description")
	Description("Gram is the tools platform for AI agents")
})
