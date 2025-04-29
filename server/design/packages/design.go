package projects

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/design/security"
	"github.com/speakeasy-api/gram/design/shared"
)

var _ = Service("packages", func() {
	Description("Manages packages in Gram.")

	Method("createPackage", func() {
		Description("Create a new package for a project.")

		Security(security.Session, security.ProjectSlug)

		Payload(CreatePackageForm)
		Result(CreatePackageResult)

		HTTP(func() {
			POST("/rpc/packages.create")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "createPackage")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreatePackage"}`)
	})

	Method("listVersions", func() {
		Description("List published versions of a package.")

		Security(security.Session, security.ProjectSlug)

		Payload(ListVersionsForm)
		Result(ListVersionsResult)

		HTTP(func() {
			GET("/rpc/packages.listVersions")
			Param("name")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "listVersions")
		Meta("openapi:extension:x-speakeasy-name-override", "listVersions")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListVersions"}`)
	})

	Method("publish", func() {
		Description("Publish a new version of a package.")

		Security(security.Session, security.ProjectSlug)

		Payload(PublishPackageForm)
		Result(PublishPackageResult)

		HTTP(func() {
			POST("/rpc/packages.publish")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "publish")
		Meta("openapi:extension:x-speakeasy-name-override", "publish")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "PublishPackage"}`)
	})
})

var CreatePackageForm = Type("CreatePackageForm", func() {
	Required("name")

	Attribute("name", String, func() {
		Description("The name of the package")
		MaxLength(100)
		Pattern(shared.SlugPattern)
	})
	Attribute("title", String, "The title of the package", func() {
		MaxLength(100)
	})
	Attribute("summary", String, "The summary of the package", func() {
		MaxLength(80)
	})
	Attribute("keywords", ArrayOf(String), "The keywords of the package", func() {
		MaxLength(5)
	})

	security.SessionPayload()
	security.ProjectPayload()
})

var CreatePackageResult = Type("CreatePackageResult", func() {
	Required("package")

	Attribute("package", shared.ProjectPackage, "The newly created package")
})

var ListVersionsForm = Type("ListVersionsForm", func() {
	Required("name")

	Attribute("name", String, "The name of the package")
	security.SessionPayload()
	security.ProjectPayload()
})

var ListVersionsResult = Type("ListVersionsResult", func() {
	Required("package", "versions")

	Attribute("package", shared.ProjectPackage)
	Attribute("versions", ArrayOf(shared.PackageVersion))
})

var PublishPackageForm = Type("PublishPackageForm", func() {
	Required("name", "version", "deployment_id", "visibility")

	Attribute("name", String, "The name of the package")
	Attribute("version", String, "The new semantic version of the package to publish")
	Attribute("deployment_id", String, "The deployment ID to associate with the package version")
	Attribute("visibility", String, "The visibility of the package version", func() {
		Enum("public", "private")
	})

	security.SessionPayload()
	security.ProjectPayload()
})

var PublishPackageResult = Type("PublishPackageResult", func() {
	Required("package", "version")

	Attribute("package", shared.ProjectPackage, "The published package")
	Attribute("version", shared.PackageVersion, "The published package version")
})
