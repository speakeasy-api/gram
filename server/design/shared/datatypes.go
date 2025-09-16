package shared

import (
	"github.com/speakeasy-api/gram/server/internal/constants"
	. "goa.design/goa/v3/dsl"
)

var Slug = Type("Slug", String, func() {
	Description("A short url-friendly label that uniquely identifies a resource.")
	Pattern(constants.SlugPattern)
	MaxLength(40)
})
