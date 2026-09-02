package dialect

import "strings"

var sensitiveDataExactKeys = map[string]struct{}{
	"assistant":                  {},
	"content":                    {},
	"gen_ai.system_instructions": {},
	"prompt":                     {},
	"tool.args":                  {},
	"tool_result":                {},
	"user_prompt":                {},
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
