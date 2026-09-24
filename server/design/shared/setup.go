package shared

import . "goa.design/goa/v3/dsl"

var SetupWorkstream = Type("SetupWorkstream", func() {
	Meta("struct:pkg:path", "types")
	Description("Canonical onboarding workstream. Task keys define task display order; the containing workstreams array defines workstream display order.")
	Attribute("id", String, "Stable workstream identifier used for assignment.")
	Attribute("title", String, "Workstream display title.")
	Attribute("task_keys", ArrayOf(String), "Ordered task keys, limited to the tasks present in the same response. Hidden tasks appear only when the reader is authorized to see them.")
	Required("id", "title", "task_keys")
})
