package shared

import (
	. "goa.design/goa/v3/dsl"
)

var Slug = Type("Slug", String, func() {
	Pattern(`^[a-z]+(?:[a-z0-9_-]*[a-z0-9])?$`)
	MaxLength(40)
})
