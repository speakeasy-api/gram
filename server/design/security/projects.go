package security

import (
	"fmt"

	"github.com/speakeasy-api/gram/internal/auth"
	. "goa.design/goa/v3/dsl"
)

var ProjectSlug = APIKeySecurity(auth.ProjectSlugSecuritySchema, func() {
	Description("project slug header auth.")
})

var ProjectPayload = func() {
	APIKey(auth.ProjectSlugSecuritySchema, "project_slug_input", String)
}

var ProjectHeader = func() {
	Header(fmt.Sprintf("project_slug_input:%s", auth.ProjectHeader), String, "project header")
}

var ProjectPayloadNamed = func(name string) {
	APIKey(auth.ProjectSlugSecuritySchema, name, String)
}

var ProjectParam = func(name string) {
	Param(name)
}
