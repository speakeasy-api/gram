package shared

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/internal/constants"
)

var Slug = Type("Slug", String, func() {
	Description("A short url-friendly label that uniquely identifies a resource.")
	Pattern(constants.SlugPattern)
	MaxLength(40)
})

var URN = Type("URN", String, func() {
	Meta("struct:field:type", "urn.Tool", "github.com/speakeasy-api/gram/server/internal/urn")
})
