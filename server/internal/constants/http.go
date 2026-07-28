package constants

const (
	HeaderProxiedResponse  = "X-Gram-Proxy-Response"
	HeaderFilteredResponse = "X-Gram-Proxy-ResponseFiltered"
	HeaderSource           = "X-Gram-Source"
	// HeaderAssistantID carries the assistant id on setup/onboarding
	// completions (X-Gram-Source: assistant) so the completion handler can
	// link the chat to the assistant via an assistant_threads row, making the
	// setup thread listable and URL-addressable like runtime assistant
	// threads.
	HeaderAssistantID = "Gram-Assistant-ID"
)
