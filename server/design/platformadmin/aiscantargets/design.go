// Package aiscantargets declares the platform-administrator surface for the
// Shadow AI scan target catalog. The catalog is global, so the service is
// gated on the users.admin entitlement rather than an organization role.
package aiscantargets

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Mirrors aitargets.Validate and the device agent's validator.
const (
	maxSignatureEntries = 16
	targetIDPattern     = `^[a-z0-9][a-z0-9-]{0,63}$`
	bundleIDPattern     = `^[A-Za-z0-9._-]{1,128}$`
	binaryPattern       = `^[A-Za-z0-9._-]{1,64}$`
	processNamePattern  = `^[A-Za-z0-9 ._-]{1,64}$`
	plistKeyPattern     = `^[A-Za-z0-9]{1,64}$`
	configDirPattern    = `^~/[^\\]+$`
)

var Signatures = Type("AiScanTargetSignatures", func() {
	Description("On-device footprints that identify one target. Any install signature hit reports the target as installed; a process-name hit reports it as running.")
	Attribute("bundle_ids", ArrayOf(String, func() { Pattern(bundleIDPattern) }), "macOS CFBundleIdentifier values matched against app bundles under /Applications and ~/Applications.", func() {
		MaxLength(maxSignatureEntries)
	})
	Attribute("binaries", ArrayOf(String, func() { Pattern(binaryPattern) }), "Bare command names resolved on the device PATH; never a path.", func() {
		MaxLength(maxSignatureEntries)
	})
	Attribute("config_dirs", ArrayOf(String, func() {
		Pattern(configDirPattern)
		MaxLength(256)
	}), "Home-relative directories (~/...) whose existence marks the tool as installed.", func() {
		MaxLength(maxSignatureEntries)
	})
	Attribute("process_names", ArrayOf(String, func() { Pattern(processNamePattern) }), "Exact process names checked for the running signal.", func() {
		MaxLength(maxSignatureEntries)
	})
	Required("bundle_ids", "binaries", "config_dirs", "process_names")
})

var Target = Type("AiScanTarget", func() {
	Description("One entry of the Shadow AI scan target catalog.")
	Attribute("id", String, "Stable catalog id that agents report and detections key on. Never reused for a different tool.", func() {
		Pattern(targetIDPattern)
	})
	Attribute("display_name", String, "Human-readable name shown in the dashboard.", func() {
		MinLength(1)
		MaxLength(128)
	})
	Attribute("category", String, "Target category: harness (an AI coding tool) or local_model (a local model runtime).", func() {
		Enum("harness", "local_model")
	})
	Attribute("signatures", Signatures)
	Attribute("version_plist_key", String, "Info.plist key to read the installed version from on a bundle match; defaults to CFBundleShortVersionString when omitted.", func() {
		Pattern(plistKeyPattern)
	})
	Attribute("enabled", Boolean, "Disabled targets stay in the catalog for history but are not served to agents.")
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
	Required("id", "display_name", "category", "signatures", "enabled", "created_at", "updated_at")
})

var Revision = Type("AiScanCatalogRevision", func() {
	Description("One recorded change to the catalog. The highest revision is the list_version agents echo on scan receipts.")
	Attribute("revision", Int)
	Attribute("target_id", String)
	Attribute("action", String, "What changed: seed, upsert, enable, disable, or delete.")
	Attribute("actor_user_id", String)
	Attribute("actor_email", String)
	Attribute("reason", String)
	Attribute("target_before", Any, "Target state before the change, absent on creation.")
	Attribute("target_after", Any, "Target state after the change, absent on deletion.")
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Required("revision", "target_id", "action", "created_at")
})

var ListResult = Type("ListAiScanTargetsResult", func() {
	Attribute("list_version", Int, "Current catalog revision; the value agents echo as target_list_version once they receive this list.")
	Attribute("etag", String, "Fingerprint of the served list; changes whenever the enabled set changes.")
	Attribute("targets", ArrayOf(Target), "Every non-deleted target, enabled or not, ordered by id.")
	Required("list_version", "etag", "targets")
})

var MutationResult = Type("AiScanTargetMutationResult", func() {
	Attribute("list_version", Int, "Catalog revision after the change.")
	Attribute("target", Target)
	Required("list_version", "target")
})

var DeleteResult = Type("DeleteAiScanTargetResult", func() {
	Attribute("list_version", Int, "Catalog revision after the change.")
	Required("list_version")
})

var ListRevisionsResult = Type("ListAiScanCatalogRevisionsResult", func() {
	Attribute("revisions", ArrayOf(Revision), "Most recent changes first.")
	Required("revisions")
})

var _ = Service("platformAiScanTargets", func() {
	Description("Platform-administrator management of the Shadow AI scan target catalog served to every enrolled device agent. Requires a current users.admin entitlement on an ordinary Gram session.")
	Security(security.Session)
	shared.DeclareErrorResponses()
	Error(string(oops.CodeUnavailable), func() {
		Description(oops.CodeUnavailable.UserMessage())
		Fault()
	})
	HTTP(func() {
		shared.DeclareHTTPErrorResponses()
		Response(string(oops.CodeUnavailable), StatusServiceUnavailable, func() { ContentType("application/json") })
	})

	Method("list", func() {
		Description("List every target in the catalog together with the current list version.")
		Payload(func() { security.SessionPayload() })
		Result(ListResult)
		HTTP(func() {
			GET("/rpc/platformAiScanTargets.list")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listPlatformAiScanTargets")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "PlatformAiScanTargetsList"}`)
	})

	Method("upsert", func() {
		Description("Create a target or replace an existing one wholesale. Revives a previously deleted id. Bumps the list version.")
		Payload(func() {
			security.SessionPayload()
			Attribute("id", String, func() { Pattern(targetIDPattern) })
			Attribute("display_name", String, func() {
				MinLength(1)
				MaxLength(128)
			})
			Attribute("category", String, func() { Enum("harness", "local_model") })
			Attribute("signatures", Signatures)
			Attribute("version_plist_key", String, func() { Pattern(plistKeyPattern) })
			Attribute("enabled", Boolean, "Whether the target is served to agents.", func() { Default(true) })
			Attribute("reason", String, "Why the change is being made; recorded on the revision.", func() { MaxLength(1000) })
			Required("id", "display_name", "category", "signatures")
		})
		Result(MutationResult)
		HTTP(func() {
			POST("/rpc/platformAiScanTargets.upsert")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "upsertPlatformAiScanTarget")
		Meta("openapi:extension:x-speakeasy-name-override", "upsert")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "PlatformAiScanTargetsUpsert"}`)
	})

	Method("setEnabled", func() {
		Description("Enable or disable a target without deleting it. Bumps the list version.")
		Payload(func() {
			security.SessionPayload()
			Attribute("id", String, func() { Pattern(targetIDPattern) })
			Attribute("enabled", Boolean)
			Attribute("reason", String, func() { MaxLength(1000) })
			Required("id", "enabled")
		})
		Result(MutationResult)
		HTTP(func() {
			POST("/rpc/platformAiScanTargets.setEnabled")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "setPlatformAiScanTargetEnabled")
		Meta("openapi:extension:x-speakeasy-name-override", "setEnabled")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "PlatformAiScanTargetsSetEnabled"}`)
	})

	Method("delete", func() {
		Description("Soft-delete a target. Existing detections keep the id; agents stop probing for it. Bumps the list version.")
		Payload(func() {
			security.SessionPayload()
			Attribute("id", String, func() { Pattern(targetIDPattern) })
			Attribute("reason", String, func() { MaxLength(1000) })
			Required("id")
		})
		Result(DeleteResult)
		HTTP(func() {
			POST("/rpc/platformAiScanTargets.delete")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "deletePlatformAiScanTarget")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "PlatformAiScanTargetsDelete"}`)
	})

	Method("listRevisions", func() {
		Description("List the most recent catalog changes, newest first.")
		Payload(func() {
			security.SessionPayload()
			Attribute("limit", Int, func() {
				Minimum(1)
				Maximum(200)
				Default(50)
			})
		})
		Result(ListRevisionsResult)
		HTTP(func() {
			GET("/rpc/platformAiScanTargets.listRevisions")
			security.SessionHeader()
			Param("limit")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listPlatformAiScanCatalogRevisions")
		Meta("openapi:extension:x-speakeasy-name-override", "listRevisions")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "PlatformAiScanTargetsListRevisions"}`)
	})
})
