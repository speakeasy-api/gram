package design

import (
	. "goa.design/goa/v3/dsl"
	"goa.design/goa/v3/expr"

	_ "github.com/speakeasy-api/gram/server/design/about"
	_ "github.com/speakeasy-api/gram/server/design/access"
	_ "github.com/speakeasy-api/gram/server/design/admin"
	_ "github.com/speakeasy-api/gram/server/design/assets"
	_ "github.com/speakeasy-api/gram/server/design/assistants"
	_ "github.com/speakeasy-api/gram/server/design/auditlogs"
	_ "github.com/speakeasy-api/gram/server/design/auth"
	_ "github.com/speakeasy-api/gram/server/design/chat"
	_ "github.com/speakeasy-api/gram/server/design/chatsessions"
	_ "github.com/speakeasy-api/gram/server/design/collections"
	_ "github.com/speakeasy-api/gram/server/design/deployments"
	_ "github.com/speakeasy-api/gram/server/design/domains"
	_ "github.com/speakeasy-api/gram/server/design/environments"
	_ "github.com/speakeasy-api/gram/server/design/externalmcp"
	_ "github.com/speakeasy-api/gram/server/design/functions"
	_ "github.com/speakeasy-api/gram/server/design/hooks"
	_ "github.com/speakeasy-api/gram/server/design/instances"
	_ "github.com/speakeasy-api/gram/server/design/integrations"
	_ "github.com/speakeasy-api/gram/server/design/keys"
	_ "github.com/speakeasy-api/gram/server/design/mcpendpoints"
	_ "github.com/speakeasy-api/gram/server/design/mcpmetadata"
	_ "github.com/speakeasy-api/gram/server/design/mcpservers"
	_ "github.com/speakeasy-api/gram/server/design/organizations"
	_ "github.com/speakeasy-api/gram/server/design/packages"
	_ "github.com/speakeasy-api/gram/server/design/plugins"
	_ "github.com/speakeasy-api/gram/server/design/productfeatures"
	_ "github.com/speakeasy-api/gram/server/design/projects"
	_ "github.com/speakeasy-api/gram/server/design/remotemcp"
	_ "github.com/speakeasy-api/gram/server/design/resources"
	_ "github.com/speakeasy-api/gram/server/design/risk"
	_ "github.com/speakeasy-api/gram/server/design/slack"
	_ "github.com/speakeasy-api/gram/server/design/telemetry"
	_ "github.com/speakeasy-api/gram/server/design/templates"
	_ "github.com/speakeasy-api/gram/server/design/tools"
	_ "github.com/speakeasy-api/gram/server/design/toolsets"
	_ "github.com/speakeasy-api/gram/server/design/triggers"
	_ "github.com/speakeasy-api/gram/server/design/usage"
	_ "github.com/speakeasy-api/gram/server/design/usersessionclients"
	_ "github.com/speakeasy-api/gram/server/design/usersessionconsents"
	_ "github.com/speakeasy-api/gram/server/design/usersessionissuers"
	_ "github.com/speakeasy-api/gram/server/design/usersessions"
	_ "github.com/speakeasy-api/gram/server/design/variations"
)

var _ = API("gram", func() {
	Title("Gram API Description")
	Description("Gram is the tools platform for AI agents")
	Meta("openapi:example", "false")
	Randomizer(expr.NewDeterministicRandomizer())

	Server("gram", func() {
		Host("production", func() {
			Description("Gram production API base URL")
			URI("https://app.getgram.ai")
		})
	})
})
