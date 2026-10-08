package shared

import . "goa.design/goa/v3/dsl"

var NetworkAccessMode = Type("NetworkAccessMode", String, func() {
	Description("The network surfaces through which a Speakeasy-hosted MCP server may be reached.")
	Enum("public_only", "dual", "private_only")
	Meta("struct:pkg:path", "types")
})
