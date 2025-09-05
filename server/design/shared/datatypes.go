package shared

import (
	. "goa.design/goa/v3/dsl"
)

const SlugPattern = `^[a-z0-9]+(?:[a-z0-9_-]*[a-z0-9])?$`

var Slug = Type("Slug", String, func() {
	Description("A short url-friendly label that uniquely identifies a resource.")
	Pattern(SlugPattern)
	MaxLength(40)
})
