package admission

// DenialError reports that an issuer's policy refused a presented
// client_id. Callers map it onto their own wire format; this package
// deliberately owns only WHY the client was refused and what to tell the
// end user, never the OAuth error code or HTTP status, which are the
// transport's business.
type DenialError struct {
	Mode   Mode
	Reason DenialReason
}

func (e *DenialError) Error() string {
	return "cimd admission denied: " + string(e.Reason) + " (mode " + string(e.Mode) + ")"
}

// Description is the client-facing explanation for the denial.
//
// It is deliberately explicit rather than generic. An admission mode is
// customer-configured policy, not a rollout secret, and the client has
// nowhere else to learn why it was rejected: MCP clients commit to CIMD
// over dynamic client registration at metadata-discovery time and do not
// retry via registration when /authorize rejects them, so this text is the
// end user's only clue that an operator has to allow their client.
//
// It is also scoped to the CIMD mechanism on purpose. Dynamic client
// registration remains open, so claiming the client itself is barred would
// overstate what the policy actually does.
func (e *DenialError) Description() string {
	if e.Reason == DenialDisabled {
		return "this server does not accept client ID metadata documents"
	}
	return "this client ID metadata document URL is not permitted by the server's client policy; the server operator must allow it"
}
