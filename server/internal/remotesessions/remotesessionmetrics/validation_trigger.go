package remotesessionmetrics

// ValidationTrigger names which caller presented a stored credential to its upstream.
type ValidationTrigger string

const (
	// ValidationTriggerVerify: a person clicked Verify on the consent page.
	ValidationTriggerVerify ValidationTrigger = "verify"

	// ValidationTriggerConnect: the remote login callback probed a grant it just committed.
	ValidationTriggerConnect ValidationTrigger = "connect"

	// ValidationTriggerKeepalive: the server's idle re-check of a grant with no refresh token.
	ValidationTriggerKeepalive ValidationTrigger = "keepalive"
)
