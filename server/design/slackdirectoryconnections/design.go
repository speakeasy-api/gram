package slackdirectoryconnections

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/oops"
	. "goa.design/goa/v3/dsl"
)

var Connection = Type("SlackDirectoryConnection", func() {
	Description("An organization Slack workspace authorization. Authorization does not indicate directory sync or invocation protection.")
	Attribute("id", String, "Durable organization workspace connection ID.", func() { Format(FormatUUID) })
	Attribute("workspace_id", String, "Verified Slack workspace ID.")
	Attribute("workspace_name", String, "Workspace display name from Slack.")
	Attribute("status", String, "Authorization status, independent of directory sync.", func() { Enum("connected", "disconnected", "reconnect_required") })
	Attribute("generation", String, "Connection generation used to reject stale changes.", func() { Format(FormatUUID) })
	Attribute("granted_scopes", ArrayOf(String), "Permissions granted by Slack.")
	Attribute("last_error_code", String, "Bounded authorization failure code, when known.")
	Attribute("disconnected_at", String, "When credentials were removed.", func() { Format(FormatDateTime) })
	Attribute("member_count", Int64, "Members observed in the last complete snapshot, including guests and bots.")
	Attribute("directory_status", String, "Whether a snapshot belongs to the usable current authorization.", func() { Enum("never_synced", "current", "stale") })
	Attribute("sync_status", String, "Latest workflow state. Unknown means progress could not be checked.", func() { Enum("idle", "queued", "running", "retrying", "failed", "unknown") })
	Attribute("sync_phase", String, "Bounded progress phase while syncing.")
	Attribute("sync_pages", Int, "Completed pages in the current attempt.")
	Attribute("sync_members", Int, "Verified members fetched in the current attempt.")
	Attribute("last_sync_started_at", String, "Start of the latest attempted sync.", func() { Format(FormatDateTime) })
	Attribute("last_sync_failed_at", String, "Most recent failed attempt; may precede a successful sync.", func() { Format(FormatDateTime) })
	Attribute("last_full_sync_succeeded_at", String, "Last complete directory publication, possibly from an earlier authorization.", func() { Format(FormatDateTime) })
	Attribute("updated_at", String, "Last connection change timestamp.", func() { Format(FormatDateTime) })
	Required("id", "workspace_id", "workspace_name", "status", "generation", "granted_scopes", "updated_at", "member_count", "directory_status", "sync_status")
})

var Member = Type("SlackDirectoryMember", func() {
	Attribute("id", String, "Durable membership ID.", func() { Format(FormatUUID) })
	Attribute("connection_id", String, "Connection that observed this workspace member.", func() { Format(FormatUUID) })
	Attribute("workspace_id", String, "Verified member workspace.")
	Attribute("workspace_name", String, "Workspace display name.")
	Attribute("slack_user_id", String, "Slack workspace user ID.")
	Attribute("display_name", String, "Optional observed display name.")
	Attribute("email", String, "Observed email; never evidence of a confirmed Gram identity.")
	Attribute("status", String, "Observed account state.", func() { Enum("active", "deactivated", "invited", "unknown") })
	Attribute("member_type", String, "Observed account type.", func() { Enum("person", "guest", "single_channel_guest", "bot", "unknown") })
	Attribute("last_seen_at", String, "Latest published observation.", func() { Format(FormatDateTime) })
	Attribute("observed_in_last_sync", Boolean, "Whether this row was present in its workspace's last complete snapshot.")
	Required("id", "connection_id", "workspace_id", "workspace_name", "slack_user_id", "status", "member_type", "last_seen_at", "observed_in_last_sync")
})

var _ = Service("slackDirectoryConnections", func() {
	Description("Manage organization-wide Slack workspace authorizations, independently of project runtime installations.")
	Security(security.Session)
	shared.DeclareErrorResponses()
	Error(string(oops.CodeUnavailable), func() { Description("Slack connections are unavailable or authorization is not configured."); Fault() })
	HTTP(func() {
		shared.DeclareHTTPErrorResponses()
		Response(string(oops.CodeUnavailable), StatusServiceUnavailable)
	})
	Method("list", func() {
		Payload(func() { security.SessionPayload() })
		Result(func() {
			Attribute("connections", ArrayOf(Connection), "Workspace connections in creation order.")
			Attribute("authorization_configured", Boolean, "Whether the deployment can start Slack OAuth.")
			Required("connections", "authorization_configured")
		})
		HTTP(func() { GET("/rpc/slackDirectoryConnections.list"); security.SessionHeader(); Response(StatusOK) })
		Meta("openapi:operationId", "listSlackDirectoryConnections")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"SlackDirectoryConnections"}`)
	})
	Method("sync", func() {
		Description("Request a complete Slack workspace directory sync. Concurrent requests join the running sync.")
		Payload(func() {
			security.SessionPayload()
			Meta("openapi:typename", "SyncSlackDirectoryRequestBody")
			Attribute("id", String, "Connection to sync.", func() { Format(FormatUUID) })
			Attribute("generation", String, "Generation last read by the administrator.", func() { Format(FormatUUID) })
			Required("id", "generation")
		})
		Result(func() {
			Attribute("accepted", Boolean, "The durable sync was started or already running.")
			Required("accepted")
		})
		HTTP(func() {
			POST("/rpc/slackDirectoryConnections.sync")
			security.SessionHeader()
			Response(StatusAccepted)
		})
		Meta("openapi:operationId", "syncSlackDirectory")
		Meta("openapi:extension:x-speakeasy-name-override", "sync")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"SyncSlackDirectory"}`)
	})
	Method("listMembers", func() {
		Description("Read observed Slack members across the organization or within one workspace. Does not create identity mappings.")
		Payload(func() {
			security.SessionPayload()
			Attribute("connection_id", String, "Filter to a workspace connection.", func() { Format(FormatUUID) })
			Attribute("search", String, "Literal case-insensitive name, email or Slack ID search.", func() { MaxLength(200) })
			Attribute("cursor", String, "Continue after the last membership ID.", func() { Format(FormatUUID) })
			Attribute("limit", Int, "Maximum returned rows.", func() { Default(50); Minimum(1); Maximum(100) })
		})
		Result(func() {
			Attribute("members", ArrayOf(Member))
			Attribute("total", Int64, "Matching retained membership rows.")
			Attribute("next_cursor", String, "Cursor for the next page, when present.")
			Required("members", "total")
		})
		HTTP(func() {
			GET("/rpc/slackDirectoryConnections.listMembers")
			security.SessionHeader()
			Param("connection_id")
			Param("search")
			Param("cursor")
			Param("limit")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listSlackDirectoryMembers")
		Meta("openapi:extension:x-speakeasy-name-override", "listMembers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"SlackDirectoryMembers"}`)
	})
	Method("begin", func() {
		Payload(func() {
			security.SessionPayload()
			Meta("openapi:typename", "BeginSlackDirectoryConnectionRequestBody")
			Attribute("connection_id", String, "Reconnect this workspace.", func() { Format(FormatUUID) })
		})
		Result(func() {
			Attribute("authorization_url", String, "Slack authorization URL bound to the current session.")
			Required("authorization_url")
		})
		HTTP(func() { POST("/rpc/slackDirectoryConnections.begin"); security.SessionHeader(); Response(StatusOK) })
		Meta("openapi:operationId", "beginSlackDirectoryConnection")
		Meta("openapi:extension:x-speakeasy-name-override", "begin")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"BeginSlackDirectoryConnection"}`)
	})
	Method("disconnect", func() {
		Payload(func() {
			security.SessionPayload()
			Meta("openapi:typename", "DisconnectSlackDirectoryConnectionRequestBody")
			Attribute("id", String, "Durable organization workspace connection ID.", func() { Format(FormatUUID) })
			Attribute("generation", String, "Generation last read by the administrator.", func() { Format(FormatUUID) })
			Required("id", "generation")
		})
		Result(Connection)
		HTTP(func() {
			POST("/rpc/slackDirectoryConnections.disconnect")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "disconnectSlackDirectoryConnection")
		Meta("openapi:extension:x-speakeasy-name-override", "disconnect")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"DisconnectSlackDirectoryConnection"}`)
	})
})
