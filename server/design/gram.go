package design

import (
	. "goa.design/goa/v3/dsl"
	_ "goa.design/plugins/v3/otel"

	_ "github.com/speakeasy-api/gram/server/design/about"
	_ "github.com/speakeasy-api/gram/server/design/assets"
	_ "github.com/speakeasy-api/gram/server/design/auth"
	_ "github.com/speakeasy-api/gram/server/design/chat"
	_ "github.com/speakeasy-api/gram/server/design/deployments"
	_ "github.com/speakeasy-api/gram/server/design/domains"
	_ "github.com/speakeasy-api/gram/server/design/environments"
	_ "github.com/speakeasy-api/gram/server/design/instances"
	_ "github.com/speakeasy-api/gram/server/design/integrations"
	_ "github.com/speakeasy-api/gram/server/design/keys"
	_ "github.com/speakeasy-api/gram/server/design/packages"
	_ "github.com/speakeasy-api/gram/server/design/projects"
	_ "github.com/speakeasy-api/gram/server/design/slack"
	_ "github.com/speakeasy-api/gram/server/design/templates"
	_ "github.com/speakeasy-api/gram/server/design/tools"
	_ "github.com/speakeasy-api/gram/server/design/toolsets"
	_ "github.com/speakeasy-api/gram/server/design/usage"
	_ "github.com/speakeasy-api/gram/server/design/variations"
)

var _ = API("gram", func() {
	Title("Gram API Description")
	Description("Gram is the tools platform for AI agents")
	Meta("openapi:example", "false")
})
