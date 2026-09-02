package dialect

import "strings"

type SensitivityClass uint8

const (
	SensitivityNone SensitivityClass = iota
	SensitivityContent
	SensitivityIdentity
	SensitivitySecret
)

type sensitivityPrefix struct {
	prefix string
	class  SensitivityClass
}

var sensitiveDataExactKeys = map[string]SensitivityClass{
	"assistant":                  SensitivityContent,
	"content":                    SensitivityContent,
	"gen_ai.system_instructions": SensitivityContent,
	"prompt":                     SensitivityContent,
	"tool.args":                  SensitivityContent,
	"tool_result":                SensitivityContent,
	"user_prompt":                SensitivityContent,
}

var sensitiveDataPrefixes = [...]sensitivityPrefix{
	{prefix: "gen_ai.input.", class: SensitivityContent},
	{prefix: "gen_ai.output.", class: SensitivityContent},
	{prefix: "gen_ai.tool.call.", class: SensitivityContent},
	{prefix: "enduser.", class: SensitivityIdentity},
	{prefix: "user.", class: SensitivityIdentity},
}

func ClassifySensitiveDataKey(key string) SensitivityClass {
	if class, ok := sensitiveDataExactKeys[key]; ok {
		return class
	}
	for _, candidate := range sensitiveDataPrefixes {
		if strings.HasPrefix(key, candidate.prefix) {
			return candidate.class
		}
	}
	return SensitivityNone
}
