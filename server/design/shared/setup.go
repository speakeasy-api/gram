package shared

import . "goa.design/goa/v3/dsl"

var SetupWorkstream = Type("SetupWorkstream", func() {
	Meta("struct:pkg:path", "types")
	Description("Canonical onboarding workstream. Task keys define task display order; the containing workstreams array defines workstream display order.")
	Attribute("id", String, "Stable workstream identifier used for assignment.")
	Attribute("title", String, "Workstream display title.")
	Attribute("task_keys", ArrayOf(String), "Ordered task keys available in this response. Hidden tasks are included only in authorized responses.")
	Required("id", "title", "task_keys")
})
