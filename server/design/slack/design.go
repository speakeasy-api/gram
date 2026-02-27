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

	// --- Slack Apps CRUD ---

	Method("createSlackApp", func() {
		Description("Create a new Slack app and generate its manifest.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("name", String, "Display name for the Slack app", func() {
				MinLength(1)
				MaxLength(60)
			})
			Attribute("toolset_ids", ArrayOf(String), "Toolset IDs to attach to this app")
			Attribute("system_prompt", String, "System prompt for the Slack app")
			Attribute("icon_asset_id", String, "Asset ID for the app icon", func() {
				Format(FormatUUID)
			})
			Required("name", "toolset_ids")
		})

		Result(CreateSlackAppResult)

		HTTP(func() {
			POST("/rpc/slack-apps.create")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "createSlackApp")
		Meta("openapi:extension:x-speakeasy-name-override", "createSlackApp")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "createSlackApp"}`)
	})

	Method("listSlackApps", func() {
		Description("List Slack apps for a project.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListSlackAppsResult)

		HTTP(func() {
			GET("/rpc/slack-apps.list")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "listSlackApps")
		Meta("openapi:extension:x-speakeasy-name-override", "listSlackApps")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "listSlackApps"}`)
	})

	Method("getSlackApp", func() {
		Description("Get details of a specific Slack app.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The Slack app ID", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		Result(SlackAppResult)

		HTTP(func() {
			GET("/rpc/slack-apps.get")
			security.SessionHeader()
			security.ProjectHeader()
			Param("id")
		})

		Meta("openapi:operationId", "getSlackApp")
		Meta("openapi:extension:x-speakeasy-name-override", "getSlackApp")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "getSlackApp"}`)
	})

	Method("configureSlackApp", func() {
		Description("Store Slack credentials (client ID, client secret, signing secret) for an app.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The Slack app ID", func() {
				Format(FormatUUID)
			})
			Attribute("slack_client_id", String, "Slack app Client ID")
			Attribute("slack_client_secret", String, "Slack app Client Secret")
			Attribute("slack_signing_secret", String, "Slack app Signing Secret")
			Required("id", "slack_client_id", "slack_client_secret", "slack_signing_secret")
		})

		Result(SlackAppResult)

		HTTP(func() {
			POST("/rpc/slack-apps.configure")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "configureSlackApp")
		Meta("openapi:extension:x-speakeasy-name-override", "configureSlackApp")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "configureSlackApp"}`)
	})

	Method("updateSlackApp", func() {
		Description("Update a Slack app's settings.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The Slack app ID", func() {
				Format(FormatUUID)
			})
			Attribute("name", String, "New display name for the Slack app", func() {
				MinLength(1)
				MaxLength(60)
			})
			Attribute("system_prompt", String, "System prompt for the Slack app")
			Attribute("icon_asset_id", String, "Asset ID for the app icon", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		Result(SlackAppResult)

		HTTP(func() {
			PUT("/rpc/slack-apps.update")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "updateSlackApp")
		Meta("openapi:extension:x-speakeasy-name-override", "updateSlackApp")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "updateSlackApp"}`)
	})

	Method("deleteSlackApp", func() {
		Description("Soft-delete a Slack app.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The Slack app ID", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		HTTP(func() {
			DELETE("/rpc/slack-apps.delete")
			security.SessionHeader()
			security.ProjectHeader()
			Param("id")
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteSlackApp")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteSlackApp")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "deleteSlackApp"}`)
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

// --- Slack Apps types ---

var SlackAppResult = Type("SlackAppResult", func() {
	Attribute("id", String, "The Slack app ID", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "Display name of the Slack app")
	Attribute("status", String, "Current status: unconfigured, active")
	Attribute("slack_client_id", String, "The Slack app Client ID")
	Attribute("system_prompt", String, "System prompt for the Slack app")
	Attribute("icon_asset_id", String, "Asset ID for the app icon")
	Attribute("slack_team_id", String, "The connected Slack workspace ID")
	Attribute("slack_team_name", String, "The connected Slack workspace name")
	Attribute("toolset_ids", ArrayOf(String), "Attached toolset IDs")
	Attribute("redirect_url", String, "OAuth callback URL for this app")
	Attribute("request_url", String, "Event subscription URL for this app")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "name", "status", "toolset_ids", "created_at", "updated_at")
})

var CreateSlackAppResult = Type("CreateSlackAppResult", func() {
	Attribute("app", SlackAppResult, "The created Slack app")
	Required("app")
})

var ListSlackAppsResult = Type("ListSlackAppsResult", func() {
	Attribute("items", ArrayOf(SlackAppResult), "List of Slack apps")
	Required("items")
})
