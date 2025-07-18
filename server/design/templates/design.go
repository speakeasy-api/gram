package templates

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("templates", func() {
	Description("Manages re-usable prompt templates and higher-order tools for a project.")

	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("createTemplate", func() {
		Description("Create a new prompt template.")

		Payload(func() {
			Extend(CreatePromptTemplateForm)

			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(CreatePromptTemplateResult)

		HTTP(func() {
			POST("/rpc/templates.create")

			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "createTemplate")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateTemplate"}`)
	})

	Method("updateTemplate", func() {
		Description("Update a prompt template.")

		Payload(func() {
			Extend(UpdatePromptTemplateForm)

			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(UpdatePromptTemplateResult)

		HTTP(func() {
			POST("/rpc/templates.update")

			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "updateTemplate")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateTemplate"}`)
	})

	Method("getTemplate", func() {
		Description("Get prompt template by its ID or name.")

		Security(security.Session, security.ProjectSlug)
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("consumer")
		})

		Payload(func() {
			Attribute("id", String, "The ID of the prompt template")
			Attribute("name", String, "The name of the prompt template")

			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(GetPromptTemplateResult)

		HTTP(func() {
			GET("/rpc/templates.get")
			Param("id")
			Param("name")

			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "getTemplate")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Template"}`)
	})

	Method("listTemplates", func() {
		Description("List available prompt template.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(ListPromptTemplatesResult)

		HTTP(func() {
			GET("/rpc/templates.list")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "listTemplates")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Templates"}`)
	})

	Method("deleteTemplate", func() {
		Description("Delete prompt template by its ID or name.")

		Payload(func() {
			Attribute("id", String, "The ID of the prompt template")
			Attribute("name", String, "The name of the prompt template")

			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/templates.delete")
			Param("id")
			Param("name")

			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "deleteTemplate")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteTemplate"}`)
	})

	Method("renderTemplateByID", func() {
		Description("Render a prompt template by ID with provided input data.")

		// This allows us to accept multiple key scopes for a particular method
		// One downside of this is it does duplicate the same security scheme into the openapi spec
		Security(security.Session, security.ProjectSlug)
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("consumer")
		})

		Payload(func() {
			Required("id", "arguments")
			Attribute("id", String, "The ID of the prompt template to render")
			Attribute("arguments", MapOf(String, Any), "The input data to render the template with")

			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(RenderTemplateResult)

		HTTP(func() {
			POST("/rpc/templates.render")
			Param("id")

			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "renderTemplateByID")
		Meta("openapi:extension:x-speakeasy-name-override", "renderByID")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RenderTemplateByID", "type": "query"}`)
	})

	Method("renderTemplate", func() {
		Description("Render a prompt template directly with all template fields provided.")

		// This allows us to accept multiple key scopes for a particular method
		// One downside of this is it does duplicate the same security scheme into the openapi spec
		Security(security.Session, security.ProjectSlug)
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("consumer")
		})

		Payload(func() {
			Required("prompt", "arguments", "engine", "kind")
			Attribute("prompt", String, "The template content to render")
			Attribute("arguments", MapOf(String, Any), "The input data to render the template with")
			Attribute("engine", String, func() {
				Description("The template engine")
				Enum("mustache")
			})
			Attribute("kind", String, func() {
				Description("The kind of prompt the template is used for")
				Enum("prompt", "higher_order_tool")
			})

			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(RenderTemplateResult)

		HTTP(func() {
			POST("/rpc/templates.renderDirect")

			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "renderTemplate")
		Meta("openapi:extension:x-speakeasy-name-override", "render")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RenderTemplate", "type": "query"}`)
	})
})

var CreatePromptTemplateForm = Type("CreatePromptTemplateForm", func() {
	Required("name", "prompt", "engine", "kind")

	Attribute("name", shared.Slug, "The name of the prompt template")
	Attribute("prompt", String, "The template content")

	Attribute("description", String, "The description of the prompt template")
	Attribute("arguments", String, func() {
		Description("The JSON Schema defining the placeholders found in the prompt template")
		Format(FormatJSON)
	})
	Attribute("engine", String, func() {
		Description("The template engine")
		Enum("mustache")
	})
	Attribute("kind", String, func() {
		Description("The kind of prompt the template is used for")
		Enum("prompt", "higher_order_tool")
	})
	Attribute("tools_hint", ArrayOf(String), func() {
		Description("The suggested tool names associated with the prompt template")
		MaxLength(20)
	})
})

var CreatePromptTemplateResult = Type("CreatePromptTemplateResult", func() {
	Required("template")

	Attribute("template", shared.PromptTemplate, "The created prompt template")
})

var UpdatePromptTemplateForm = Type("UpdatePromptTemplateForm", func() {
	Required("id")

	Attribute("id", String, "The ID of the prompt template to update")
	Attribute("prompt", String, "The template content")
	Attribute("description", String, "The description of the prompt template")
	Attribute("arguments", String, func() {
		Description("The JSON Schema defining the placeholders found in the prompt template")
		Format(FormatJSON)
	})
	Attribute("engine", String, func() {
		Description("The template engine")
		Enum("mustache")
	})
	Attribute("kind", String, func() {
		Description("The kind of prompt the template is used for")
		Enum("prompt", "higher_order_tool")
	})
	Attribute("tools_hint", ArrayOf(String), func() {
		Description("The suggested tool names associated with the prompt template")
		MaxLength(20)
	})
})

var UpdatePromptTemplateResult = Type("UpdatePromptTemplateResult", func() {
	Required("template")

	Attribute("template", shared.PromptTemplate, "The updated prompt template")
})

var GetPromptTemplateResult = Type("GetPromptTemplateResult", func() {
	Required("template")

	Attribute("template", shared.PromptTemplate, "The created prompt template")
})

var ListPromptTemplatesResult = Type("ListPromptTemplatesResult", func() {
	Required("templates")

	Attribute("templates", ArrayOf(shared.PromptTemplate), "The created prompt template")
})

var RenderTemplateResult = Type("RenderTemplateResult", func() {
	Required("prompt")

	Attribute("prompt", String, "The rendered prompt")
})
