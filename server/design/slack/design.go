package slack

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

var _ = Service("slack", func() {
	Description("Auth and interactions for the Gram Slack App.")
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("callback", func() {
		Description("Handles the authentication callback.")

		NoSecurity()

		Payload(func() {
			Attribute("state", String, "The state parameter from the callback")
			Attribute("code", String, "The code parameter from the callback")
			Required("state", "code")
		})

		Result(func() {
			Attribute("location", String, "The URL to redirect to after authentication")
			Required("location")
		})

		HTTP(func() {
			GET("/rpc/slack.callback")
			Param("state")
			Param("code")

			Response(StatusTemporaryRedirect, func() {
				Header("location:Location", String, func() {
				})
			})
		})

		Meta("openapi:operationId", "slackCallback")
		Meta("openapi:extension:x-speakeasy-name-override", "slackCallback")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"disabled": true}`)
	})

	Method("login", func() {
		Description("Proxies to auth login through speakeasy oidc.")

		Payload(func() {
			security.SessionPayload()
			APIKey(constants.ProjectSlugSecuritySchema, "project_slug", String)
			Attribute("return_url", String, "The dashboard location to return too")
		})

		Result(func() {
			Attribute("location", String, "The URL to redirect to after authentication")
			Required("location")
		})

		HTTP(func() {
			GET("/rpc/{project_slug}/slack.login")
			Param("project_slug")
			Param("return_url")
			security.SessionHeader()
			Response(StatusTemporaryRedirect, func() {
				Header("location:Location", String, func() {
				})
			})
		})

		Meta("openapi:operationId", "slackLogin")
		Meta("openapi:extension:x-speakeasy-name-override", "slackLogin")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"disabled": true}`)
	})

	Method("getSlackConnection", func() {
		Description("get slack connection for an organization and project.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(GetSlackConnectionResult)

		HTTP(func() {
			GET("/rpc/slack.getConnection")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "getSlackConnection")
		Meta("openapi:extension:x-speakeasy-name-override", "getSlackConnection")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "getSlackConnection"}`)
	})

	Method("updateSlackConnection", func() {
		Description("update slack connection for an organization and project.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("default_toolset_slug", String, "The default toolset slug for this Slack connection")
			Required("default_toolset_slug")
		})
		Result(GetSlackConnectionResult)

		HTTP(func() {
			POST("/rpc/slack.updateConnection")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "updateSlackConnection")
		Meta("openapi:extension:x-speakeasy-name-override", "updateSlackConnection")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "updateSlackConnection"}`)
	})

	Method("deleteSlackConnection", func() {
		Description("delete slack connection for an organization and project.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/slack.deleteConnection")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "deleteSlackConnection")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteSlackConnection")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "deleteSlackConnection"}`)
	})
})

var GetSlackConnectionResult = Type("GetSlackConnectionResult", func() {
	Attribute("slack_team_name", String, "The name of the connected Slack team")
	Attribute("slack_team_id", String, "The ID of the connected Slack team")
	Attribute("default_toolset_slug", String, "The default toolset slug for this Slack connection")
	Attribute("created_at", String, func() {
		Description("When the toolset was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the toolset was last updated.")
		Format(FormatDateTime)
	})

	Required("slack_team_name", "slack_team_id", "default_toolset_slug", "created_at", "updated_at")
})
