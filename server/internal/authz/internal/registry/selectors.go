// The keys a grant selector can carry and the closed value sets of some of
// them.
//
// authzgen copies everything below the package line into package authz, so
// the model and the evaluator share these names without importing each other.
// Keep this file free of imports and of anything only the registry needs.
package registry

// Selector keys. resource_kind and resource_id are on every grant; the rest
// are dimensions a resource type or a permission declares.
const (
	SelectorKeyResourceKind   = "resource_kind"
	SelectorKeyResourceID     = "resource_id"
	SelectorKeyTool           = "tool"
	SelectorKeyDisposition    = "disposition"
	SelectorKeyProjectID      = "project_id"
	SelectorKeyServerURL      = "server_url"
	SelectorKeyServerIdentity = "server_identity"
	// SelectorKeyClientType and SelectorKeyConnectionOwner narrow mcp:auth and
	// mcp:blocked_auth grants to an auth client type and to the owner of a
	// personal connection.
	SelectorKeyClientType      = "client_type"
	SelectorKeyConnectionOwner = "connection_owner"
	// SelectorKeyUpstreamClient narrows mcp:auth and mcp:blocked_auth grants
	// on personal clients to one upstream OAuth client, the
	// remote_session_clients id behind it. With a connection owner it names one
	// logical connection across every auth client sharing that upstream
	// client, which is what a connection owner's block names.
	SelectorKeyUpstreamClient = "upstream_client"
)

// Auth client types an mcp:auth check can name. Only remote_session clients
// are personal: their checks carry the connection owner.
const (
	AuthClientTypeRemoteSession    = "remote_session"
	AuthClientTypeServiceAccount   = "service_account"
	AuthClientTypeStaticCredential = "static_credential" //nolint:gosec // client type name, not a credential
	AuthClientTypePassThrough      = "pass_through"
	AuthClientTypeEMA              = "ema"
)

// Disposition values matching MCP tool annotation hint names (snake_case, no _hint suffix).
const (
	DispositionReadOnly    = "read_only"
	DispositionDestructive = "destructive"
	DispositionIdempotent  = "idempotent"
	DispositionOpenWorld   = "open_world"
	// DispositionUnannotated is a tool that sets none of the hints above to
	// true. It is a value of its own, so a grant limited to read-only tools
	// never reaches it and an admin can allow or block it by name.
	DispositionUnannotated = "unannotated"
)
