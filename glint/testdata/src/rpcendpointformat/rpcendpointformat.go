package rpcendpointformat

import (
	. "goa.design/goa/v3/dsl"
)

// A local GET that shadows nothing here but confirms the analyzer resolves the
// goa DSL package by type, not by function name.
func routes() {
	// Valid: /rpc/<namespace>.<verb> with lowercase-led alphanumeric segments.
	GET("/rpc/access.listRoles")
	POST("/rpc/access.createRole")
	PUT("/rpc/environments.setSourceLink")
	DELETE("/rpc/environments.deleteToolsetLink")
	GET("/rpc/assets.serveOpenAPIv3")
	POST("/rpc/assets.uploadOpenAPIv3")

	// Invalid: dotted sub-paths (more than two segments).
	GET("/rpc/access.shadowMcp.requests.approve") // want `format RPC endpoint "/rpc/access.shadowMcp.requests.approve" as /rpc/<namespace>.<verb> with lowercase-led alphanumeric segments`
	POST("/rpc/risk.customRules.create")          // want `format RPC endpoint "/rpc/risk.customRules.create" as /rpc/<namespace>.<verb> with lowercase-led alphanumeric segments`
	GET("/rpc/risk.results.byChat")               // want `format RPC endpoint "/rpc/risk.results.byChat" as /rpc/<namespace>.<verb> with lowercase-led alphanumeric segments`

	// Invalid: extra path segments separated by slashes.
	POST("/rpc/hooks.otel/v1/logs") // want `format RPC endpoint "/rpc/hooks.otel/v1/logs" as /rpc/<namespace>.<verb> with lowercase-led alphanumeric segments`

	// Invalid: missing verb segment.
	GET("/rpc/access") // want `format RPC endpoint "/rpc/access" as /rpc/<namespace>.<verb> with lowercase-led alphanumeric segments`

	// Invalid: empty segment.
	GET("/rpc/access.") // want `format RPC endpoint "/rpc/access." as /rpc/<namespace>.<verb> with lowercase-led alphanumeric segments`

	// Invalid: namespace does not start with a lowercase letter.
	GET("/rpc/Access.listRoles") // want `format RPC endpoint "/rpc/Access.listRoles" as /rpc/<namespace>.<verb> with lowercase-led alphanumeric segments`

	// Not an RPC path: out of scope, no diagnostic.
	GET("/health")

	// Not a route function: out of scope even with an /rpc/ argument.
	Path("/rpc/access.shadowMcp.requests.approve")
}
