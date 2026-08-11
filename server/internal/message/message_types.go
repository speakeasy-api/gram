package message

type Type = string

const (
	User         Type = "user_message"
	ToolRequest  Type = "tool_request"
	ToolResponse Type = "tool_response"
	Assistant    Type = "assistant_message"
	// PromptAttachment is client-side context attached to a user prompt and
	// recorded with tool-like provenance after the turn.
	PromptAttachment Type = "prompt_attachment"
)

var allTypes = []Type{
	User,
	ToolRequest,
	ToolResponse,
	Assistant,
	PromptAttachment,
}

var validTypes = map[Type]struct{}{
	User:             {},
	ToolRequest:      {},
	ToolResponse:     {},
	Assistant:        {},
	PromptAttachment: {},
}

func AllTypes() []Type {
	return append([]Type(nil), allTypes...)
}

func IsTypeValid(messageType Type) bool {
	_, ok := validTypes[messageType]
	return ok
}

// TrustClass is the provenance tier a scanner should apply to some content.
type TrustClass = string

const (
	// TrustFirstParty is content the human authored.
	TrustFirstParty TrustClass = "first_party"
	// TrustModel is content the model produced.
	TrustModel TrustClass = "model"
	// TrustExternal is content from outside the user and the model, which is
	// never an instruction to follow.
	TrustExternal TrustClass = "external"
)

// TrustClassForKind maps a content part kind to its trust tier. Trust is
// derived rather than stored so a row cannot contradict itself, and an unknown
// kind fails safe as external.
func TrustClassForKind(kind string) TrustClass {
	switch kind {
	case PromptAttachment:
		return TrustExternal
	default:
		return TrustExternal
	}
}
