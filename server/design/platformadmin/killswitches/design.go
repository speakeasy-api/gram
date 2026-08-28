// Package killswitches declares the main-server platform break-glass transport
// for the generic killswitch lifecycle. It is intentionally separate from the
// customer API and accepts an explicit target organization.
package killswitches

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

var Definition = Type("PlatformKillswitchDefinition", func() {
	Required("key", "principal_kinds", "resource_kinds", "failure_policy", "default_external_note", "enforcement_owner", "identity_contract", "surfaces", "transport_adapters")
	Attribute("key", String)
	Attribute("principal_kinds", ArrayOf(String))
	Attribute("resource_kinds", ArrayOf(String))
	Attribute("failure_policy", String)
	Attribute("default_external_note", String)
	Attribute("enforcement_owner", String)
	Attribute("identity_contract", String)
	Attribute("surfaces", ArrayOf(String))
	Attribute("transport_adapters", ArrayOf(String))
})

var MutationResult = Type("PlatformKillswitchMutationResult", func() {
	Required("prescription_id", "version", "state", "replayed")
	Attribute("prescription_id", String, func() { Format(FormatUUID) })
	Attribute("version", Int64)
	Attribute("state", String)
	Attribute("replayed", Boolean)
})

var Prescription = Type("PlatformKillswitchPrescription", func() {
	Required("id", "organization_id", "definition", "principal_kind", "principal_key", "resource_kind", "version", "state", "resource_scope", "selected_resource_keys", "starts_at", "internal_note", "external_note")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("organization_id", String)
	Attribute("definition", String)
	Attribute("principal_kind", String)
	Attribute("principal_key", String)
	Attribute("resource_kind", String)
	Attribute("version", Int64)
	Attribute("state", String)
	Attribute("resource_scope", String)
	Attribute("selected_resource_keys", ArrayOf(String))
	Attribute("starts_at", String, func() { Format(FormatDateTime) })
	Attribute("expires_at", String, func() { Format(FormatDateTime) })
	Attribute("activated_at", String, func() { Format(FormatDateTime) })
	Attribute("superseded_at", String, func() { Format(FormatDateTime) })
	Attribute("internal_note", String)
	Attribute("external_note", String)
})

func declareLifecycleErrors() {
	Error("operation_conflict", func() { Description("The operation ID was already used for a different request.") })
	Error("version_conflict", func() { Description("The prescription changed after the supplied expected version.") })

}

func declareUnavailable() {
	Error(string(oops.CodeUnavailable), func() {
		Description(oops.CodeUnavailable.UserMessage())
		Fault()
	})

}

func desiredVersionPayload() {
	Attribute("resource_scope", String, func() { Enum("all", "selected") })
	Attribute("selected_resource_inputs", ArrayOf(String), func() { MaxLength(1000) })
	Attribute("start_mode", String, func() { Enum("now", "at") })
	Attribute("starts_at", String, func() { Format(FormatDateTime) })
	Attribute("expires_at", String, func() { Format(FormatDateTime) })
	Attribute("internal_note", String, func() { MaxLength(4000) })
	Attribute("external_note", String, func() { MaxLength(500) })
	Required("resource_scope", "start_mode", "internal_note", "external_note")
}

func mutationIdentityPayload() {
	Attribute("organization_id", String)
	Attribute("operation_id", String, func() { Format(FormatUUID) })
	Required("organization_id", "operation_id")
}

func prescriptionIdentityPayload() {
	Attribute("organization_id", String)
	Attribute("prescription_id", String, func() { Format(FormatUUID) })
	Required("organization_id", "prescription_id")
}

var _ = Service("platformKillswitches", func() {
	Description("Platform break-glass access to generic killswitch lifecycle operations on the main server. Requires a current users.admin entitlement on an ordinary Gram session.")
	Security(security.Session)
	shared.DeclareErrorResponses()
	declareLifecycleErrors()
	declareUnavailable()
	HTTP(func() {
		shared.DeclareHTTPErrorResponses()
		Response("operation_conflict", StatusConflict, func() { ContentType("application/json") })
		Response("version_conflict", StatusConflict, func() { ContentType("application/json") })
		Response(string(oops.CodeUnavailable), StatusServiceUnavailable, func() { ContentType("application/json") })
	})

	Method("listDefinitions", func() {
		Payload(func() { security.SessionPayload() })
		Result(func() {
			Required("definitions")
			Attribute("definitions", ArrayOf(Definition))
		})
		HTTP(func() {
			GET("/rpc/platformKillswitches.listDefinitions")
			security.SessionHeader()
			Response(StatusOK)
		})
	})

	Method("activatePrescription", func() {
		Payload(func() {
			security.SessionPayload()
			mutationIdentityPayload()
			Attribute("prescription_id", String, func() { Format(FormatUUID) })
			Attribute("expected_version", Int64)
			Attribute("definition", String)
			Attribute("principal_kind", String)
			Attribute("principal_input", String)
			Attribute("resource_kind", String)
			desiredVersionPayload()
		})
		Result(MutationResult)
		HTTP(func() {
			POST("/rpc/platformKillswitches.activatePrescription")
			security.SessionHeader()
			Response(StatusOK)
		})
	})

	Method("changePrescription", func() {
		Payload(func() {
			security.SessionPayload()
			mutationIdentityPayload()
			Attribute("prescription_id", String, func() { Format(FormatUUID) })
			Attribute("expected_version", Int64)
			desiredVersionPayload()
			Required("prescription_id", "expected_version")
		})
		Result(MutationResult)
		HTTP(func() {
			POST("/rpc/platformKillswitches.changePrescription")
			security.SessionHeader()
			Response(StatusOK)
		})
	})

	Method("deactivatePrescription", func() {
		Payload(func() {
			security.SessionPayload()
			mutationIdentityPayload()
			Attribute("prescription_id", String, func() { Format(FormatUUID) })
			Attribute("expected_version", Int64)
			Required("prescription_id", "expected_version")
		})
		Result(MutationResult)
		HTTP(func() {
			POST("/rpc/platformKillswitches.deactivatePrescription")
			security.SessionHeader()
			Response(StatusOK)
		})
	})

	Method("getPrescription", func() {
		Payload(func() {
			security.SessionPayload()
			prescriptionIdentityPayload()
		})
		Result(Prescription)
		HTTP(func() {
			GET("/rpc/platformKillswitches.getPrescription")
			Param("organization_id")
			Param("prescription_id")
			security.SessionHeader()
			Response(StatusOK)
		})
	})

	Method("listPrescriptions", func() {
		Payload(func() {
			security.SessionPayload()
			Attribute("organization_id", String)
			Attribute("limit", Int32, func() {
				Minimum(1)
				Maximum(100)
			})
			Attribute("after_id", String, func() { Format(FormatUUID) })
			Required("organization_id")
		})
		Result(func() {
			Required("prescriptions")
			Attribute("prescriptions", ArrayOf(Prescription))
			Attribute("next_after_id", String, func() { Format(FormatUUID) })
		})
		HTTP(func() {
			GET("/rpc/platformKillswitches.listPrescriptions")
			Param("organization_id")
			Param("limit")
			Param("after_id")
			security.SessionHeader()
			Response(StatusOK)
		})
	})
})
