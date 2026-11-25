package assistants

// assistantChatSessionSurface names the product surface stamped on an
// assistant chat, or "" when the chat should keep the source inferred from its
// messages ("assistants"). It reads only the persisted thread source kind, so
// a request cannot claim the Speakeasy Slack surface. Customer Slack apps stay
// on the ordinary assistants surface.
func assistantChatSessionSurface(sourceKind string) string {
	if sourceKind == sourceKindSpeakeasySlack {
		return SessionSurfaceSpeakeasySlack
	}
	return ""
}
