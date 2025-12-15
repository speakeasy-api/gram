package projects

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("projects", func() {
	Description("Manages projects in Gram.")

	Security(security.ByKey, func() {
		Scope("producer")
	})
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("createProject", func() {
		Description("Create a new project.")

		Payload(func() {
			Extend(CreateProjectForm)
			security.ByKeyPayload()
			security.SessionPayload()
		})
		Result(CreateProjectResult)

		HTTP(func() {
			POST("/rpc/projects.create")
			security.ByKeyHeader()
			security.SessionHeader()
		})

		Meta("openapi:operationId", "createProject")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateProject"}`)
	})

	Method("listProjects", func() {
		Description("List all projects for an organization.")

		Payload(ListProjectsPayload)
		Result(ListProjectsResult)

		HTTP(func() {
			GET("/rpc/projects.list")
			security.ByKeyHeader()
			security.SessionHeader()

			Param("organization_id")
		})

		Meta("openapi:operationId", "listProjects")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListProjects"}`)
	})

	Method("setLogo", func() {
		Description("Uploads a logo for a project.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.ProjectSlug, security.Session)

		Payload(func() {
			Extend(SetProjectLogoForm)
			security.ByKeyPayload()
			security.ProjectPayload()
			security.SessionPayload()
		})
		Result(SetProjectLogoResult)

		HTTP(func() {
			POST("/rpc/projects.setLogo")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "setProjectLogo")
		Meta("openapi:extension:x-speakeasy-name-override", "setLogo")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "setProjectLogo"}`)
	})

	Method("listAllowedOrigins", func() {
		Description("List allowed origins for a project.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.ProjectSlug, security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.ProjectPayload()
			security.SessionPayload()
		})
		Result(ListAllowedOriginsResult)

		HTTP(func() {
			GET("/rpc/projects.listAllowedOrigins")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "listAllowedOrigins")
		Meta("openapi:extension:x-speakeasy-name-override", "listAllowedOrigins")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListAllowedOrigins"}`)
	})

	Method("upsertAllowedOrigin", func() {
		Description("Upsert an allowed origin for a project.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.ProjectSlug, security.Session)

		Payload(func() {
			Extend(UpsertAllowedOriginForm)
			security.ByKeyPayload()
			security.ProjectPayload()
			security.SessionPayload()
		})
		Result(UpsertAllowedOriginResult)

		HTTP(func() {
			POST("/rpc/projects.upsertAllowedOrigin")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "upsertAllowedOrigin")
		Meta("openapi:extension:x-speakeasy-name-override", "upsertAllowedOrigin")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpsertAllowedOrigin"}`)
	})
})

var CreateProjectForm = Type("CreateProjectForm", func() {
	Required("organization_id", "name")
	security.ByKeyPayload()
	security.SessionPayload()

	Attribute("organization_id", String, "The ID of the organization to create the project in")
	Attribute("name", String, "The name of the project", func() { MaxLength(40) })
})

var CreateProjectResult = Type("CreateProjectResult", func() {
	Required("project")

	Attribute("project", shared.Project, "The created project")
})

var ListProjectsPayload = Type("ListProjectsPayload", func() {
	Required("organization_id")
	security.ByKeyPayload()
	security.SessionPayload()

	Attribute("organization_id", String, "The ID of the organization to list projects for")
})

var ListProjectsResult = Type("ListProjectsResult", func() {
	Required("projects")

	Attribute("projects", ArrayOf(shared.ProjectEntry), "The list of projects")
})

var SetProjectLogoForm = Type("SetProjectLogoForm", func() {
	Required("asset_id")

	Attribute("asset_id", String, "The ID of the asset")
})

var SetProjectLogoResult = Type("SetProjectLogoResult", func() {
	Required("project")

	Attribute("project", shared.Project, "The updated project with the new logo")
})

var AllowedOrigin = Type("AllowedOrigin", func() {
	Required("id", "project_id", "origin", "status", "created_at", "updated_at")

	Attribute("id", String, "The ID of the allowed origin")
	Attribute("project_id", String, "The ID of the project")
	Attribute("origin", String, "The origin URL")
	Attribute("status", String, "The status of the allowed origin")
	Attribute("created_at", String, func() {
		Description("The creation date of the allowed origin.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("The last update date of the allowed origin.")
		Format(FormatDateTime)
	})
})

var ListAllowedOriginsResult = Type("ListAllowedOriginsResult", func() {
	Required("allowed_origins")

	Attribute("allowed_origins", ArrayOf(AllowedOrigin), "The list of allowed origins")
})

var UpsertAllowedOriginForm = Type("UpsertAllowedOriginForm", func() {
	Required("origin")

	Attribute("origin", String, "The origin URL to upsert", func() {
		MaxLength(500)
		MinLength(1)
	})
	Attribute("status", String, "The status of the allowed origin (defaults to 'pending')", func() {
		Default("pending")
	})
})

var UpsertAllowedOriginResult = Type("UpsertAllowedOriginResult", func() {
	Required("allowed_origin")

	Attribute("allowed_origin", AllowedOrigin, "The upserted allowed origin")
})
