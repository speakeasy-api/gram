package slack

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("slack", func() {
	Description("Auth and interactions for the Gram Slack App.")
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	// --- Slack Apps CRUD ---

	Method("createSlackApp", func() {
		Description("Create a new Slack app and generate its manifest.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("name", String, "Display name for the Slack app", func() {
				MinLength(1)
				MaxLength(36)
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
				MaxLength(36)
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
