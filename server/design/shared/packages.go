package shared

import (
	. "goa.design/goa/v3/dsl"
)

var ProjectPackage = Type("Package", func() {
	Required("id", "name", "project_id", "organization_id", "created_at", "updated_at")

	Attribute("id", String, "The ID of the package")
	Attribute("project_id", String, "The ID of the project that owns the package")
	Attribute("organization_id", String, "The ID of the organization that owns the package")
	Attribute("name", String, "The name of the package")
	Attribute("title", String, "The title of the package")
	Attribute("summary", String, "The summary of the package")
	Attribute("keywords", ArrayOf(String), "The keywords of the package")
	Attribute("image_asset_id", String, "The asset ID of the image to show for this package")
	Attribute("latest_version", String, "The latest version of the package")
	Attribute("created_at", String, "The creation date of the package", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "The last update date of the package", func() {
		Format(FormatDateTime)
	})
	Attribute("deleted_at", String, "The deletion date of the package", func() {
		Format(FormatDateTime)
	})
})

var PackageVersion = Type("PackageVersion", func() {
	Required("id", "package_id", "deployment_id", "visibility", "semver", "created_at")

	Attribute("id", String, "The ID of the package version")
	Attribute("package_id", String, "The ID of the package that the version belongs to")
	Attribute("deployment_id", String, "The ID of the deployment that the version belongs to")
	Attribute("visibility", String, "The visibility of the package version")
	Attribute("semver", String, "The semantic version value")
	Attribute("created_at", String, "The creation date of the package version", func() {
		Format(FormatDateTime)
	})
})
