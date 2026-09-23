package sigint

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("sigint", func() {
	Description("Manage project-scoped custom signals and sensors. Signals are reusable live definitions; sensors attach an ordered set of signals in multi-label, exclusive, or ordered-score mode.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() { Scope("producer") })
	shared.DeclareErrorResponses()

	Method("createSignal", func() {
		Description("Create a reusable custom signal. Names are trimmed and must contain 1 to 200 characters. Description and classifier criteria are optional authoring metadata; an empty string clears either field.")
		Payload(func() {
			Extend(CreateSignalForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(SigintSignal)
		HTTP(func() {
			POST("/rpc/sigint.createSignal")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "createSigintSignal")
		Meta("openapi:extension:x-speakeasy-name-override", "createSignal")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"CreateSigintSignal"}`)
	})

	Method("getSignal", func() {
		Description("Get an active custom signal by ID in the selected project.")
		Payload(func() {
			Attribute("id", String, "Custom signal ID", func() { Format(FormatUUID) })
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(SigintSignal)
		HTTP(func() {
			GET("/rpc/sigint.getSignal")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getSigintSignal")
		Meta("openapi:extension:x-speakeasy-name-override", "getSignal")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"GetSigintSignal"}`)
	})

	Method("listSignals", func() {
		Description("List active custom signals in deterministic ID order. The cursor is the final ID from the previous page.")
		Payload(func() {
			Attribute("cursor", String, "Signal ID after which to continue", func() { Format(FormatUUID) })
			Attribute("limit", Int, "Page size", func() {
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(ListSignalsResult)
		HTTP(func() {
			GET("/rpc/sigint.listSignals")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		shared.CursorPagination()
		Meta("openapi:operationId", "listSigintSignals")
		Meta("openapi:extension:x-speakeasy-name-override", "listSignals")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"SigintSignals"}`)
	})

	Method("updateSignal", func() {
		Description("Partially update a custom signal. Omitted fields and JSON null leave existing values unchanged. Supplying an empty optional-text value clears it. A supplied name is trimmed and must contain 1 to 200 characters.")
		Payload(func() {
			Extend(UpdateSignalForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(SigintSignal)
		HTTP(func() {
			POST("/rpc/sigint.updateSignal")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "updateSigintSignal")
		Meta("openapi:extension:x-speakeasy-name-override", "updateSignal")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"UpdateSigintSignal"}`)
	})

	Method("deleteSignal", func() {
		Description("Soft-delete a custom signal and detach it from every sensor in the selected project. Surviving sensor signal order is compacted; sensors may remain incomplete drafts.")
		Payload(func() {
			Attribute("id", String, "Custom signal ID", func() { Format(FormatUUID) })
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(SigintSignal)
		HTTP(func() {
			DELETE("/rpc/sigint.deleteSignal")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "deleteSigintSignal")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteSignal")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"DeleteSigintSignal"}`)
	})

	Method("createSensor", func() {
		Description("Create a sensor. multi_label permits independently applicable labels, exclusive makes attached signals compete and permits at most 255, and ordered_score uses ordered levels and permits at most 10. Empty and one-signal sensors are valid drafts. Omitted signal_ids creates an empty sensor; [] is explicitly empty. Repeated, deleted, missing, or cross-project signal IDs are rejected atomically.")
		Payload(func() {
			Extend(CreateSensorForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(SigintSensor)
		HTTP(func() {
			POST("/rpc/sigint.createSensor")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "createSigintSensor")
		Meta("openapi:extension:x-speakeasy-name-override", "createSensor")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"CreateSigintSensor"}`)
	})

	Method("getSensor", func() {
		Description("Get an active sensor with its ordered signal_ids. The collection is always returned and is [] when no signals are attached.")
		Payload(func() {
			Attribute("id", String, "Sensor ID", func() { Format(FormatUUID) })
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(SigintSensor)
		HTTP(func() {
			GET("/rpc/sigint.getSensor")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getSigintSensor")
		Meta("openapi:extension:x-speakeasy-name-override", "getSensor")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"GetSigintSensor"}`)
	})

	Method("listSensors", func() {
		Description("List active sensors in deterministic ID order with each sensor's ordered signal_ids from one consistent database snapshot. The collection is [] for an unattached sensor.")
		Payload(func() {
			Attribute("cursor", String, "Sensor ID after which to continue", func() { Format(FormatUUID) })
			Attribute("limit", Int, "Page size", func() {
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(ListSensorsResult)
		HTTP(func() {
			GET("/rpc/sigint.listSensors")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		shared.CursorPagination()
		Meta("openapi:operationId", "listSigintSensors")
		Meta("openapi:extension:x-speakeasy-name-override", "listSensors")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"SigintSensors"}`)
	})

	Method("updateSensor", func() {
		Description("Partially update a sensor. Omitted fields and JSON null preserve stored values. Empty optional text clears it. Omitted signal_ids preserves membership, while [] clears it and a non-empty array replaces and reorders it atomically. Mode changes validate the final membership: exclusive permits at most 255 signals and ordered_score at most 10; incomplete drafts remain valid.")
		Payload(func() {
			Extend(UpdateSensorForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(SigintSensor)
		HTTP(func() {
			POST("/rpc/sigint.updateSensor")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "updateSigintSensor")
		Meta("openapi:extension:x-speakeasy-name-override", "updateSensor")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"UpdateSigintSensor"}`)
	})

	Method("deleteSensor", func() {
		Description("Soft-delete a sensor and its memberships. Shared custom signals remain active.")
		Payload(func() {
			Attribute("id", String, "Sensor ID", func() { Format(FormatUUID) })
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(SigintSensor)
		HTTP(func() {
			DELETE("/rpc/sigint.deleteSensor")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "deleteSigintSensor")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteSensor")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"DeleteSigintSensor"}`)
	})
})

var SigintSensorMode = Type("SigintSensorMode", String, func() {
	Description("How a sensor interprets its ordered signals: independently applicable labels, an exclusive choice, or ordered score levels.")
	Enum("multi_label", "exclusive", "ordered_score")
	Meta("struct:pkg:path", "types")
})

var SigintSignal = Type("SigintSignal", func() {
	Meta("struct:pkg:path", "types")
	Description("A reusable project-scoped custom signal.")
	Attribute("id", String, "Signal ID", func() { Format(FormatUUID) })
	Attribute("project_id", String, "Owning project ID", func() { Format(FormatUUID) })
	Attribute("name", String, "Display name")
	Attribute("description", String, "Optional author-facing description")
	Attribute("classifier_criteria", String, "Optional criteria describing when this signal applies")
	Attribute("created_at", String, "Creation time", func() { Format(FormatDateTime) })
	Attribute("updated_at", String, "Last update time", func() { Format(FormatDateTime) })
	Required("id", "project_id", "name", "created_at", "updated_at")
})

var SigintSensor = Type("SigintSensor", func() {
	Meta("struct:pkg:path", "types")
	Description("A project-scoped sensor with ordered live references to custom signals. signal_ids is always present, including when empty.")
	Attribute("id", String, "Sensor ID", func() { Format(FormatUUID) })
	Attribute("project_id", String, "Owning project ID", func() { Format(FormatUUID) })
	Attribute("name", String, "Display name")
	Attribute("description", String, "Optional author-facing description")
	Attribute("instructions", String, "Optional framing instructions for classification")
	Attribute("mode", SigintSensorMode)
	Attribute("signal_ids", ArrayOf(String, func() { Format(FormatUUID) }), "Ordered signal IDs. In ordered_score mode the zero-based array index is the score level.")
	Attribute("created_at", String, "Creation time", func() { Format(FormatDateTime) })
	Attribute("updated_at", String, "Last update time", func() { Format(FormatDateTime) })
	Required("id", "project_id", "name", "mode", "signal_ids", "created_at", "updated_at")
})

var CreateSignalForm = Type("CreateSigintSignalForm", func() {
	Attribute("name", String, "Display name; trimmed before enforcing the 1 to 200 character limit")
	Attribute("description", String, "Optional description; empty stores no value")
	Attribute("classifier_criteria", String, "Optional classifier criteria; empty stores no value")
	Required("name")
})

var UpdateSignalForm = Type("UpdateSigintSignalForm", func() {
	Attribute("id", String, "Signal ID", func() { Format(FormatUUID) })
	Attribute("name", String, "Replacement display name; trimmed before enforcing the 1 to 200 character limit")
	Attribute("description", String, "Replacement description; empty clears it")
	Attribute("classifier_criteria", String, "Replacement classifier criteria; empty clears it")
	Required("id")
})

var CreateSensorForm = Type("CreateSigintSensorForm", func() {
	Attribute("name", String, "Display name; trimmed before enforcing the 1 to 200 character limit")
	Attribute("description", String, "Optional description; empty stores no value")
	Attribute("instructions", String, "Optional classification instructions; empty stores no value")
	Attribute("mode", SigintSensorMode)
	Attribute("signal_ids", ArrayOf(String, func() { Format(FormatUUID) }), "Initial ordered signal IDs. Omitted and [] both create an empty sensor.")
	Required("name", "mode")
})

var UpdateSensorForm = Type("UpdateSigintSensorForm", func() {
	Attribute("id", String, "Sensor ID", func() { Format(FormatUUID) })
	Attribute("name", String, "Replacement display name; trimmed before enforcing the 1 to 200 character limit")
	Attribute("description", String, "Replacement description; empty clears it")
	Attribute("instructions", String, "Replacement instructions; empty clears it")
	Attribute("mode", SigintSensorMode)
	Attribute("signal_ids", ArrayOf(String, func() { Format(FormatUUID) }), "Authoritative ordered signal IDs. Omit or send null to preserve; send [] to clear.")
	Required("id")
})

var ListSignalsResult = Type("ListSigintSignalsResult", func() {
	Attribute("signals", ArrayOf(SigintSignal), "Signals in this page")
	Attribute("next_cursor", String, "Final returned signal ID when another page exists", func() { Format(FormatUUID) })
	Required("signals")
})

var ListSensorsResult = Type("ListSigintSensorsResult", func() {
	Attribute("sensors", ArrayOf(SigintSensor), "Sensors in this page")
	Attribute("next_cursor", String, "Final returned sensor ID when another page exists", func() { Format(FormatUUID) })
	Required("sensors")
})
