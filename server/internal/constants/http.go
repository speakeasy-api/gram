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
	// HeaderContentEncoding names the coding applied to a request or response
	// body.
	HeaderContentEncoding = "Content-Encoding"

	// UserAgent identifies requests Speakeasy originates to third parties.
	UserAgent = "Speakeasy-Gram/1.0"
)
