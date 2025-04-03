package security

import (
	. "goa.design/goa/v3/dsl"
)

var ProjectPayload = func() {
	Attribute("project_slug", String, "The project the action belongs too")
	Required("project_slug")
}

var ProjectHeader = func() {
	Header("project_slug:Gram-Project", String, "project header")
}
