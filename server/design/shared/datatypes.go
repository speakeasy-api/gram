package shared

import (
	. "goa.design/goa/v3/dsl"
)

var Slug = Type("Slug", String, func() {
	Description("A short url-friendly label that uniquely identifies a resource.")
	Pattern(`^[a-z]+(?:[a-z0-9_-]*[a-z0-9])?$`)
	MaxLength(40)
})
