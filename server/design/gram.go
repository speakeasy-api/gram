package design

import (
	. "goa.design/goa/v3/dsl"
	_ "goa.design/plugins/v3/otel"

	_ "github.com/speakeasy-api/gram/design/assets"
	_ "github.com/speakeasy-api/gram/design/auth"
	_ "github.com/speakeasy-api/gram/design/chat"
	_ "github.com/speakeasy-api/gram/design/deployments"
	_ "github.com/speakeasy-api/gram/design/environments"
	_ "github.com/speakeasy-api/gram/design/instances"
	_ "github.com/speakeasy-api/gram/design/integrations"
	_ "github.com/speakeasy-api/gram/design/keys"
	_ "github.com/speakeasy-api/gram/design/mcp"
	_ "github.com/speakeasy-api/gram/design/packages"
	_ "github.com/speakeasy-api/gram/design/projects"
	_ "github.com/speakeasy-api/gram/design/slack"
	_ "github.com/speakeasy-api/gram/design/tools"
	_ "github.com/speakeasy-api/gram/design/toolsets"
)

var _ = API("gram", func() {
	Title("Gram API Description")
	Description("Gram is the tools platform for AI agents")
	Meta("openapi:example", "false")
})
