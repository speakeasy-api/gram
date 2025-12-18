package security

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/auth/constants"
	. "goa.design/goa/v3/dsl"
)

var ProjectSlug = APIKeySecurity(constants.ProjectSlugSecuritySchema, func() {
	Description("project slug header auth.")
})

var ProjectPayload = func() {
	APIKey(constants.ProjectSlugSecuritySchema, "project_slug_input", String)
}

var ProjectHeader = func() {
	Header(fmt.Sprintf("project_slug_input:%s", constants.ProjectHeader), String, "project header")
}

var ProjectPayloadNamed = func(name string) {
	APIKey(constants.ProjectSlugSecuritySchema, name, String)
}

var ProjectParam = func(name string) {
	Param(name)
}
