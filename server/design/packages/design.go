package projects

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/design/security"
	"github.com/speakeasy-api/gram/design/shared"
)

var _ = Service("packages", func() {
	Description("Manages packages in Gram.")
	shared.DeclareErrorResponses()

	Method("createPackage", func() {
		Description("Create a new package for a project.")

		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(CreatePackageForm)

			security.SessionPayload()
			security.ProjectPayload()
		})
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

	Method("updatePackage", func() {
		Description("Update package details.")

		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(UpdatePackageForm)

			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(UpdatePackageResult)

		Error("not_modified", func() {
			Required("location")
			Attribute("location", String)
		})

		HTTP(func() {
			PUT("/rpc/packages.update")
			security.SessionHeader()
			security.ProjectHeader()

			Response(StatusOK)
			Response("not_modified", StatusNotModified, func() {
				ContentType("application/json")
			})
		})

		Meta("openapi:operationId", "updatePackage")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdatePackage"}`)
	})

	Method("listVersions", func() {
		Description("List published versions of a package.")

		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(ListVersionsForm)

			security.SessionPayload()
			security.ProjectPayload()
		})
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

		Payload(func() {
			Extend(PublishPackageForm)

			security.SessionPayload()
			security.ProjectPayload()
		})
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

var UpdatePackageForm = Type("UpdatePackageForm", func() {
	Required("id")

	Attribute("id", String, "The id of the package to update", func() {
		MaxLength(50)
	})

	Attribute("title", String, "The title of the package", func() {
		MaxLength(100)
	})
	Attribute("summary", String, "The summary of the package", func() {
		MaxLength(80)
	})
	Attribute("url", String, "External URL for the package owner", func() {
		MaxLength(100)
	})
	Attribute("keywords", ArrayOf(String), "The keywords of the package", func() {
		MaxLength(5)
	})
	Attribute("image_asset_id", String, "The asset ID of the image to show for this package", func() {
		MaxLength(50)
	})
})

var UpdatePackageResult = Type("UpdatePackageResult", func() {
	Required("package")

	Attribute("package", shared.ProjectPackage, "The newly created package")
})

var CreatePackageForm = Type("CreatePackageForm", func() {
	Required("name", "title", "summary")

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
	Attribute("url", String, "External URL for the package owner", func() {
		MaxLength(100)
	})
	Attribute("keywords", ArrayOf(String), "The keywords of the package", func() {
		MaxLength(5)
	})
	Attribute("image_asset_id", String, "The asset ID of the image to show for this package", func() {
		MaxLength(50)
	})
})

var CreatePackageResult = Type("CreatePackageResult", func() {
	Required("package")

	Attribute("package", shared.ProjectPackage, "The newly created package")
})

var ListVersionsForm = Type("ListVersionsForm", func() {
	Required("name")

	Attribute("name", String, "The name of the package")
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
})

var PublishPackageResult = Type("PublishPackageResult", func() {
	Required("package", "version")

	Attribute("package", shared.ProjectPackage, "The published package")
	Attribute("version", shared.PackageVersion, "The published package version")
})
