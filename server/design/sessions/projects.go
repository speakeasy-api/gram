package sessions

import (
	. "goa.design/goa/v3/dsl"
)

var ProjectSlug = func() {
	Attribute("project_slug", String, "The project the action belongs too")
}

var ProjectPayload = Type("ProjectPayload", func() {
	ProjectSlug()
	Required("project_slug")
})

var ProjectHeader = func() {
	Header("project_slug:Gram-Project", String, "project header")
}
