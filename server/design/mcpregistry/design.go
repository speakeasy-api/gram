package mcpregistry

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var discoveryPage = Type("RegistryDiscoveryPage", func() {
	Attribute("servers", ArrayOf(Any), "Complete registry record envelopes, including server and _meta extensions")
	Attribute("metadata", discoveryMetadata)
	Required("servers", "metadata")
})

var _ = Service("registryDiscovery", func() {
	Description("Authenticated discovery-only preview. Current records only: no version history, incremental synchronization or mirror guarantees. Uses Gram credentials, not generic-client OAuth. Discovery errors use the pinned standard error envelope; authorization uses Gram security.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() { Scope("producer") })
	shared.DeclareErrorResponses()
	Error("discovery_not_found", discoveryErrorType)
	Error("discovery_bad_request", discoveryErrorType)

	for _, operation := range []struct{ name, path string }{
		{"discoverServers", "/v0.1/servers"},
		{"discoverVersions", "/v0.1/servers/{serverName}/versions"},
		{"discoverVersion", "/v0.1/servers/{serverName}/versions/{version}"},
	} {
		Method(operation.name, func() {
			Payload(func() {
				security.SessionPayload()
				security.ByKeyPayload()
				security.ProjectPayload()
				Attribute("include_deleted", Boolean, func() { Default(false) })
				Attribute("updated_since", String, "Unsupported: any supplied value returns 400")
				if operation.name == "discoverServers" {
					Attribute("search", String, "Search query; at most 1024 UTF-8 bytes, enforced by the server (not a character count).")
					Attribute("version", String, "Version filter; at most 1024 UTF-8 bytes, enforced by the server (not a character count).")
					Attribute("cursor", String, func() { MaxLength(8192) })
					Attribute("limit", Int32, func() { Default(25); Minimum(1); Maximum(100) })
				} else {
					Attribute("serverName", String)
					Required("serverName")
				}
				if operation.name == "discoverVersion" {
					Attribute("version", String)
					Required("version")
				}
			})
			if operation.name == "discoverVersion" {
				Result(Any)
			} else {
				Result(discoveryPage)
			}
			HTTP(func() {
				GET(operation.path)
				security.SessionHeader()
				security.ByKeyHeader()
				security.ProjectHeader()
				Param("include_deleted")
				Param("updated_since")
				if operation.name == "discoverServers" {
					Param("search")
					Param("version")
					Param("cursor")
					Param("limit")
				}

				Response("discovery_not_found", StatusNotFound, func() { Body(func() { Attribute("error"); Required("error") }) })
				Response("discovery_bad_request", StatusBadRequest, func() { Body(func() { Attribute("error"); Required("error") }) })
				Response(StatusOK)
			})
			Meta("openapi:operationId", operation.name)
			Meta("openapi:extension:x-speakeasy-name-override", operation.name)
		})
	}
})

var discoveryErrorType = Type("RegistryDiscoveryError", func() {
	ErrorName("name", String)
	Attribute("error", String, func() { Meta("struct:field:name", "Message") })
	Required("name", "error")
})

var discoveryMetadata = Type("RegistryDiscoveryMetadata", func() {
	Attribute("count", Int)
	Attribute("nextCursor", String, func() { Meta("struct:tag:json", "nextCursor,omitempty") })
	Required("count")
})
