package dialect

import "strings"

// Sensitive attribute keys are shared by dialect extraction and relay redaction.
// Dialect keys stay in the exact set even when a prefix also covers them.
const (
	claudeCodeUserPromptKey  = "user_prompt"
	codexPromptKey           = "prompt"
	semconvInputMessagesKey  = "gen_ai.input.messages"
	semconvOutputMessagesKey = "gen_ai.output.messages"
	semconvUserIDKey         = "user.id"
	userEmailKey             = "user.email"
	vendorUserAccountIDKey   = "user.account_id"
)

var sensitiveDataExactKeys = map[string]struct{}{
	claudeCodeUserPromptKey:      {},
	codexPromptKey:               {},
	semconvInputMessagesKey:      {},
	semconvOutputMessagesKey:     {},
	semconvUserIDKey:             {},
	userEmailKey:                 {},
	vendorUserAccountIDKey:       {},
	"assistant":                  {},
	"content":                    {},
	"gen_ai.system_instructions": {},
	"tool.args":                  {},
	"tool_result":                {},

	// The column enrichers copy a record's words and its person onto the
	// canonical speakeasy.agent keys, next to the sensitive attributes they
	// were read from. The copies carry the same values, so a destination
	// that excludes sensitive data must see them redacted too. The enrich
	// package pins these spellings to its column keys in a test, since it
	// imports this package and so cannot be imported here.
	"speakeasy.agent.text":             {},
	"speakeasy.agent.user_email":       {},
	"speakeasy.agent.external_user_id": {},
}

var sensitiveDataPrefixes = [...]string{
	"gen_ai.input.",
	"gen_ai.output.",
	"gen_ai.tool.call.",
	"enduser.",
	"user.",
}

func IsSensitiveDataKey(key string) bool {
	if _, ok := sensitiveDataExactKeys[key]; ok {
		return true
	}
	for _, prefix := range sensitiveDataPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
