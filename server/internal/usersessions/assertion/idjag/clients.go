package idjag

import "slices"

// Client is an OAuth client the token endpoint accepts an ID-JAG from.
type Client struct {
	// DisplayName names the client for administrators.
	DisplayName string

	// ClientID is the client ID metadata document URL the client presents,
	// matched exactly. It is the value an identity provider records as the
	// client at the resource.
	ClientID string
}

// allowedClients are the only clients that may exchange an ID-JAG. Each is
// resolved from its client ID metadata document, whose grant_types need not
// list the JWT bearer grant: the clients that use it publish one document for
// every grant.
var allowedClients = []Client{
	{DisplayName: "Claude Code", ClientID: "https://claude.ai/oauth/claude-code-client-metadata"},
	{DisplayName: "Codex", ClientID: "https://chatgpt.com/oauth/codex/client.json"},
}

// AllowedClients returns the clients that may exchange an ID-JAG.
func AllowedClients() []Client {
	return slices.Clone(allowedClients)
}

// ClientAllowed reports whether clientID may exchange an ID-JAG.
func ClientAllowed(clientID string) bool {
	return slices.ContainsFunc(allowedClients, func(c Client) bool { return c.ClientID == clientID })
}
