package triggers

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("triggers", func() {
	Description("Manage project trigger instances and static trigger definitions.")

	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("listTriggerDefinitions", func() {
		Description("List static trigger definitions available to a project.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListTriggerDefinitionsResult)

		HTTP(func() {
			GET("/rpc/triggers.definitions.list")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listTriggerDefinitions")
		Meta("openapi:extension:x-speakeasy-name-override", "listDefinitions")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "TriggerDefinitions"}`)
	})

	Method("listTriggerInstances", func() {
		Description("List trigger instances for the current project.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListTriggerInstancesResult)

		HTTP(func() {
			GET("/rpc/triggers.list")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listTriggerInstances")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Triggers"}`)
	})

	Method("getTriggerInstance", func() {
		Description("Get a trigger instance by ID.")

		Payload(func() {
			Attribute("id", String, "The trigger instance ID.", func() {
				Format(FormatUUID)
			})
			Required("id")

			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(shared.TriggerInstance)

		HTTP(func() {
			GET("/rpc/triggers.get")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getTriggerInstance")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Trigger"}`)
	})

	Method("createTriggerInstance", func() {
		Description("Create a trigger instance.")

		Payload(func() {
			Extend(CreateTriggerInstanceForm)
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(shared.TriggerInstance)

		HTTP(func() {
			POST("/rpc/triggers.create")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createTriggerInstance")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateTrigger"}`)
	})

	Method("updateTriggerInstance", func() {
		Description("Update a trigger instance.")

		Payload(func() {
			Extend(UpdateTriggerInstanceForm)
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(shared.TriggerInstance)

		HTTP(func() {
			POST("/rpc/triggers.update")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateTriggerInstance")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateTrigger"}`)
	})

	Method("deleteTriggerInstance", func() {
		Description("Delete a trigger instance.")

		Payload(func() {
			Attribute("id", String, "The trigger instance ID.", func() {
				Format(FormatUUID)
			})
			Required("id")

			security.SessionPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/triggers.delete")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteTriggerInstance")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteTrigger"}`)
	})

	Method("pauseTriggerInstance", func() {
		Description("Pause a trigger instance.")

		Payload(func() {
			Attribute("id", String, "The trigger instance ID.", func() {
				Format(FormatUUID)
			})
			Required("id")

			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(shared.TriggerInstance)

		HTTP(func() {
			POST("/rpc/triggers.pause")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "pauseTriggerInstance")
		Meta("openapi:extension:x-speakeasy-name-override", "pause")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "PauseTrigger"}`)
	})

	Method("resumeTriggerInstance", func() {
		Description("Resume a trigger instance.")

		Payload(func() {
			Attribute("id", String, "The trigger instance ID.", func() {
				Format(FormatUUID)
			})
			Required("id")

			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(shared.TriggerInstance)

		HTTP(func() {
			POST("/rpc/triggers.resume")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "resumeTriggerInstance")
		Meta("openapi:extension:x-speakeasy-name-override", "resume")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ResumeTrigger"}`)
	})
})

var CreateTriggerInstanceForm = Type("CreateTriggerInstanceForm", func() {
	Required("definition_slug", "name", "target_kind", "target_ref", "target_display", "config")

	Attribute("definition_slug", String, "The trigger definition slug.")
	Attribute("name", String, "The trigger instance name.")
	Attribute("environment_id", String, "The linked environment ID.", func() {
		Format(FormatUUID)
	})
	Attribute("target_kind", String, "The trigger target kind.", func() {
		Enum("assistant", "noop")
	})
	Attribute("target_ref", String, "The opaque target reference.")
	Attribute("target_display", String, "The user-facing target display value.")
	Attribute("config", MapOf(String, Any), "The trigger config payload.")
	Attribute("status", String, "Optional initial status.", func() {
		Enum("active", "paused")
	})
})

var UpdateTriggerInstanceForm = Type("UpdateTriggerInstanceForm", func() {
	Required("id")

	Attribute("id", String, "The trigger instance ID.", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "The trigger instance name.")
	Attribute("environment_id", String, "The linked environment ID.", func() {
		Format(FormatUUID)
	})
	Attribute("target_kind", String, "The trigger target kind.", func() {
		Enum("assistant", "noop")
	})
	Attribute("target_ref", String, "The opaque target reference.")
	Attribute("target_display", String, "The user-facing target display value.")
	Attribute("config", MapOf(String, Any), "The trigger config payload.")
	Attribute("status", String, "The trigger status.", func() {
		Enum("active", "paused")
	})
})

var ListTriggerDefinitionsResult = Type("ListTriggerDefinitionsResult", func() {
	Attribute("definitions", ArrayOf(shared.TriggerDefinition), "The available trigger definitions.")
	Required("definitions")
})

var ListTriggerInstancesResult = Type("ListTriggerInstancesResult", func() {
	Attribute("triggers", ArrayOf(shared.TriggerInstance), "The trigger instances for the current project.")
	Required("triggers")
})
