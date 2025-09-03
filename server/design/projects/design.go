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
