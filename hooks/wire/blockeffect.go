package wire

// BlockEffectKey is the IngestHookResult.Effects key the structured
// requestable-block payload rides under.
const BlockEffectKey = "block"

// BlockEffectVersion is the current contract version stamped into V.
// Consumers ignore effects whose version they don't understand, so bump it
// only on a breaking reshape of the payload.
const BlockEffectVersion = 1

// BlockEffect is the structured mirror of the "Request access" prose the
// server appends to a requestable deny (today: a shadow-MCP block that minted
// a bypass-request link). It exists so the speakeasy-hooks binary — and
// through it the local device agent — can offer a native "request access"
// flow without parsing prose. Absence of the effect means the deny is not
// requestable (PII, secrets, spend, prompt policies carry nothing).
//
// The server emitter (server/internal/hooks) and the relay decoder
// (hooks/relay) both import this so the vocabulary cannot drift within the
// repo; deployed relays still lag the server, so each side validates
// independently (the relay guards on V, Requestable, and RequestToken).
type BlockEffect struct {
	V           int    `json:"v"`
	Category    string `json:"category"`
	Requestable bool   `json:"requestable"`
	// RequestToken is the control plane's opaque bypass-request token
	// (rpbr2.…) the device agent later spends on
	// risk.createPolicyBypassRequest.
	RequestToken string `json:"request_token"`
	// RequestURL is the browser fallback carrying the same token in its
	// fragment; it mirrors the link in the deny prose.
	RequestURL string `json:"request_url"`
	// RequestExpiresAt is RFC3339; empty when the token TTL is unknown.
	RequestExpiresAt string `json:"request_expires_at,omitempty"`
	ServerName       string `json:"server_name,omitempty"`
	ServerURL        string `json:"server_url,omitempty"`
	PolicyName       string `json:"policy_name,omitempty"`
	ToolName         string `json:"tool_name,omitempty"`
	// BlockURL is the dashboard's durable block-row page. Duplicate
	// deliveries re-mint the request link but never a second block row, so
	// the effect can exist without it.
	BlockURL string `json:"block_url,omitempty"`
}
