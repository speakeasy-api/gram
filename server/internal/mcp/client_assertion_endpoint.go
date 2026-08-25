package mcp

// clientAssertionEndpoint names which client-authenticated endpoint a request
// was posted to, so the audience an assertion may name is that endpoint's own
// URL and not a sibling's.
type clientAssertionEndpoint string

const (
	clientAssertionAtToken  clientAssertionEndpoint = "token"
	clientAssertionAtRevoke clientAssertionEndpoint = "revoke"
)
