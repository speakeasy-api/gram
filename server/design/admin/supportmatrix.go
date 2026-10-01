package admin

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

// The support matrix is code: server/internal/supportmatrix/matrix.yaml,
// embedded in the server and changed by pull request. This API only reads it.

var SupportFact = Type("SupportFact", func() {
	Attribute("status", String, func() {
		Meta("struct:tag:json", "status")
		Enum("supported", "partial", "unimplemented", "impossible", "na", "unknown")
	})
	Attribute("note", String, func() { Meta("struct:tag:json", "note"); MaxLength(10000) })
	Attribute("verify", Boolean, func() { Meta("struct:tag:json", "verify") })
	Required("status", "note", "verify")
})
var SupportAccounts = Type("SupportAccounts", func() {
	Description("Which account types can use a method on a platform.")
	for _, account := range []string{"personal", "team", "enterprise"} {
		Attribute(account, String, func() {
			Meta("struct:tag:json", account)
			Enum("supported", "unsupported", "unknown")
		})
	}
	Required("personal", "team", "enterprise")
})
var SupportOS = Type("SupportOS", func() {
	Description("What is known per operating system; an absent one is unknown.")
	for _, os := range []string{"mac", "windows", "linux"} {
		Attribute(os, String, func() {
			Meta("struct:tag:json", os+",omitempty")
			Enum("supported", "verify")
		})
	}
})
var SupportPlatformSupport = Type("SupportPlatformSupport", func() {
	Description("One method on one platform: whether it applies, who can use it, and one cell per capability when it applies.")
	Attribute("platform", String, func() { Meta("struct:tag:json", "platform") })
	Attribute("applicability", String, func() { Meta("struct:tag:json", "applicability"); Enum("unknown", "applicable", "na") })
	Attribute("accounts", SupportAccounts, func() { Meta("struct:tag:json", "accounts") })
	Attribute("os", SupportOS, func() { Meta("struct:tag:json", "os,omitempty") })
	Attribute("note", String, func() { Meta("struct:tag:json", "note"); MaxLength(10000) })
	Attribute("cells", MapOf(String, SupportFact), func() {
		Description("One fact per capability when the method applies; empty otherwise, meaning not applicable or unknown everywhere.")
		Meta("struct:tag:json", "cells")
	})
	Required("platform", "applicability", "accounts", "note", "cells")
})
var SupportMethod = Type("SupportMethod", func() {
	Attribute("id", String, func() { Meta("struct:tag:json", "id") })
	Attribute("name", String, func() { Meta("struct:tag:json", "name") })
	Attribute("vendor", String, func() { Meta("struct:tag:json", "vendor") })
	Attribute("plans", String, func() { Meta("struct:tag:json", "plans") })
	Attribute("claims", MapOf(String, SupportFact), func() {
		Description("What the method delivers per capability, platform aside.")
		Meta("struct:tag:json", "claims")
	})
	Attribute("platforms", ArrayOf(SupportPlatformSupport), func() {
		Description("Every platform once, in the matrix's platform order.")
		Meta("struct:tag:json", "platforms")
	})
	Required("id", "name", "vendor", "plans", "claims", "platforms")
})
var SupportPlatform = Type("SupportPlatform", func() {
	Attribute("id", String, func() { Meta("struct:tag:json", "id") })
	Attribute("name", String, func() { Meta("struct:tag:json", "name") })
	Attribute("vendor", String, func() { Meta("struct:tag:json", "vendor") })
	Attribute("family", String, func() { Meta("struct:tag:json", "family") })
	Attribute("surface", String, func() { Meta("struct:tag:json", "surface") })
	Required("id", "name", "vendor", "family", "surface")
})
var SupportCapability = Type("SupportCapability", func() {
	Attribute("id", String, func() { Meta("struct:tag:json", "id") })
	Attribute("name", String, func() { Meta("struct:tag:json", "name") })
	Attribute("group", String, func() { Meta("struct:tag:json", "group") })
	Required("id", "name", "group")
})
var SupportMatrix = Type("SupportMatrix", func() {
	Attribute("capabilities", ArrayOf(SupportCapability), func() { Meta("struct:tag:json", "capabilities") })
	Attribute("platforms", ArrayOf(SupportPlatform), func() { Meta("struct:tag:json", "platforms") })
	Attribute("methods", ArrayOf(SupportMethod), func() { Meta("struct:tag:json", "methods") })
	Attribute("revision", String, func() {
		Description("Hex SHA-256 of the deployed matrix file.")
		Meta("struct:tag:json", "revision")
	})
	Required("capabilities", "platforms", "methods", "revision")
})

func supportMatrixMethods() {
	Method("getSupportMatrix", func() {
		Meta("openapi:operationId", "adminGetSupportMatrix")
		Meta("openapi:extension:x-speakeasy-name-override", "getSupportMatrix")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminGetSupportMatrix"}`)
		Description("Read the support matrix the server was built with. It is code, changed by pull request: server/internal/supportmatrix/matrix.yaml.")
		Payload(func() { security.AdminAuthPayload() })
		Result(SupportMatrix)
		HTTP(func() { GET("/admin/supportMatrix.get"); Response(StatusOK) })
	})
}
