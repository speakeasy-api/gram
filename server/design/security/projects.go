package security

import (
	. "goa.design/goa/v3/dsl"
)

const ProjectSlugSecuritySchema = "project_slug"

var ProjectSlug = APIKeySecurity(ProjectSlugSecuritySchema, func() {
	Description("project slug header auth.")
})

var ProjectPayload = func() {
	APIKey(ProjectSlugSecuritySchema, "project_slug", String)
}

var ProjectHeader = func() {
	Header("project_slug:Gram-Project", String, "project header")
}
