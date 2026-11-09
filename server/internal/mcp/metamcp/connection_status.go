package metamcp

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"strings"
)

// ToolConnectionStatus is the gateway's sign-in status tool. It reports, per
// member, whether the caller holds a credential for it, and links unconnected
// members to the gateway's connect page. Signing in still happens in the
// browser; the tool never runs an OAuth flow itself.
const ToolConnectionStatus = "connection_status"

// ConnectionStatusResourceURI is the MCP Apps view rendered for a
// connection_status result by hosts that support the
// io.modelcontextprotocol/ui extension. It carries a hash of the page because
// hosts cache a resource by URI: an edited page under the same URI is never
// fetched again.
var ConnectionStatusResourceURI = ConnectionStatusResourcePrefix + contentHash(ConnectionStatusHTML)

// ConnectionStatusResourcePrefix matches every version of the view; reads of
// an earlier hash get the current page so cached tool lists keep rendering.
const ConnectionStatusResourcePrefix = "ui://gateway/connection-status/"

// IsConnectionStatusResourceURI accepts the current view URI or any earlier
// hashed version of it.
func IsConnectionStatusResourceURI(uri string) bool {
	hash, ok := strings.CutPrefix(uri, ConnectionStatusResourcePrefix)
	if !ok || len(hash) != len(contentHash("")) {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil && strings.ToLower(hash) == hash
}

func contentHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}

// MCPAppMimeType is the MCP Apps resource mime type.
const MCPAppMimeType = "text/html;profile=mcp-app"

//go:embed connection_status.html
var ConnectionStatusHTML string

// Member sign-in states surfaced by connection_status.
const (
	// SignInVerifiedAtConsent records successful discovery without a live probe.
	SignInVerifiedAtConsent = "verified_at_consent"
	// SignInNeedsConsent reports a configuration change requiring new approval.
	SignInNeedsConsent = "needs_consent"
	// SignInConnected: the caller holds a credential that routes to the member.
	SignInConnected = "connected"
	// SignInNotConnected: the member is bound to a sign-in provider and the
	// caller holds no credential for it.
	SignInNotConnected = "not_connected"
	// SignInNotRequired: the member is bound to no sign-in provider.
	SignInNotRequired = "not_required"
	// SignInError: a credential exists but cannot be used; Detail says why.
	SignInError = "error"
	// SignInUnavailable: an outage stops the sign-in being used; Detail names the fallback.
	SignInUnavailable = "unavailable"
	// SignInActionRequired: signing in here won't help; Detail says what to do.
	SignInActionRequired = "action_required"
	// SignInHidden: the caller hid the member, so its tools are not served.
	SignInHidden = "hidden"
)

// ConnectionStatusServer is one member entry in a connection_status result.
type ConnectionStatusServer struct {
	Slug   string `json:"slug"`
	Name   string `json:"name,omitempty"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`

	// Reconnect marks a row on an agent gateway that reconnecting at the
	// agent's sign-in step can fix.
	Reconnect bool `json:"reconnect,omitempty"`

	// Outage is what the view shows for SignInUnavailable; Detail stays the message for the AI app.
	Outage *ConnectionStatusOutage `json:"outage,omitempty"`
}

// ConnectionStatusOutage is an outage in words for the person reading the view.
type ConnectionStatusOutage struct {
	Message string `json:"message"`

	// ReconnectURL is set when reconnecting could get past the outage.
	ReconnectURL string `json:"reconnectUrl,omitempty"`
}

// ConnectionStatusResult is the structuredContent payload of a
// connection_status call. ConnectURL is the gateway's connect page; it is
// empty when the gateway has no sign-in.
type ConnectionStatusResult struct {
	ConnectURL string                   `json:"connectUrl,omitempty"`
	Servers    []ConnectionStatusServer `json:"servers"`

	// Reconnect is set on an agent gateway when a server needs signing in
	// again: the agent cannot, so someone who can edit it reconnects it.
	Reconnect *ConnectionStatusReconnect `json:"reconnect,omitempty"`

	// Agent reports an agent gateway, which the agent cannot sign in to
	// itself. Only the text hint reads it.
	Agent bool `json:"-"`
}

// ConnectionStatusReconnect is where an agent's servers are reconnected.
type ConnectionStatusReconnect struct {
	// URL opens the agent's MCP sign-in settings in the dashboard; empty
	// when the page could not be named.
	URL string `json:"url,omitempty"`
}

var connectionStatusSchema = []byte(`{
	"type": "object",
	"properties": {},
	"additionalProperties": false
}`)

// Text renders the result for hosts that show only text content, and for the
// model. It carries the same list and the same connect link as the view.
func (r ConnectionStatusResult) Text() string {
	var b strings.Builder
	connected, verified, total := 0, 0, 0
	consented, needsConsent := false, false
	for _, s := range r.Servers {
		if s.Status != SignInHidden {
			total++
		}
		if s.Status == SignInVerifiedAtConsent {
			verified++
			consented = true
		}
		if s.Status == SignInConnected || s.Status == SignInNotRequired {
			connected++
		}
	}
	noun := "servers"
	if total == 1 {
		noun = "server"
	}
	if consented {
		fmt.Fprintf(&b, "%d of %d %s verified at consent; current sign-in status has not been checked.\n\n", verified, total, noun)
	} else {
		fmt.Fprintf(&b, "%d of %d %s ready.\n\n", connected, total, noun)
	}
	needsConnect, hidden := false, false
	for _, s := range r.Servers {
		label := s.Name
		if label == "" {
			label = s.Slug
		}
		switch s.Status {
		case SignInVerifiedAtConsent:
			fmt.Fprintf(&b, "- %s: verified at consent (%s)\n", label, s.Detail)
		case SignInNeedsConsent:
			needsConsent = true
			fmt.Fprintf(&b, "- %s: new consent required (%s)\n", label, s.Detail)
		case SignInConnected:
			fmt.Fprintf(&b, "- %s: signed in\n", label)
		case SignInNotRequired:
			fmt.Fprintf(&b, "- %s: no sign-in needed\n", label)
		case SignInNotConnected:
			needsConnect = true
			fmt.Fprintf(&b, "- %s: not signed in\n", label)
		case SignInActionRequired:
			fmt.Fprintf(&b, "- %s: needs attention (%s)\n", label, s.Detail)
		case SignInHidden:
			hidden = true
			fmt.Fprintf(&b, "- %s: hidden by the user\n", label)
		case SignInUnavailable:
			fmt.Fprintf(&b, "- %s: temporarily unavailable (%s)\n", label, s.Detail)
		default:
			needsConnect = true
			fmt.Fprintf(&b, "- %s: needs attention (%s)\n", label, s.Detail)
		}
	}
	if needsConsent {
		b.WriteString("\nReconnect this gateway in your AI app to review access.\n")
	}
	if hidden && r.ConnectURL != "" {
		fmt.Fprintf(&b, "\nTo use a hidden server, show it again at %s.\n", r.ConnectURL)
	}
	if needsConnect {
		b.WriteString(r.signInHint())
	}
	return b.String()
}

// signInHint says who signs in the servers that need it, and where.
func (r ConnectionStatusResult) signInHint() string {
	if r.ConnectURL != "" {
		return fmt.Sprintf("\nTo sign in, open %s in a browser, then call %s again to refresh.\n", r.ConnectURL, ToolConnectionStatus)
	}
	if r.Reconnect == nil && r.Agent {
		return fmt.Sprintf("\nThis agent cannot fix these servers itself. Each server above says who can, then call %s again to refresh.\n", ToolConnectionStatus)
	}
	if r.Reconnect == nil {
		return "\nThis gateway has no sign-in page; ask the gateway's administrator to connect these servers.\n"
	}
	// The owner is not the only person who may reconnect an agent's servers,
	// so the hint names no one. Each server's line says what is wrong; a
	// server never signed in, or one that failed for another reason, is not
	// necessarily a broken sign-in.
	const who = "someone who can edit this agent"
	if r.Reconnect.URL == "" {
		return fmt.Sprintf("\nThese servers need attention that this agent cannot give itself. Ask %s to check them in Speakeasy, then call %s again to refresh.\n", who, ToolConnectionStatus)
	}
	return fmt.Sprintf("\nThese servers need attention that this agent cannot give itself. Ask %s to open %s and connect or reconnect them, then call %s again to refresh.\n", who, r.Reconnect.URL, ToolConnectionStatus)
}
