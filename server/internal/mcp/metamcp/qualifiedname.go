package metamcp

import (
	"fmt"
	"strings"
)

// QualifiedNameSeparator joins a member server's slug and one of its tool
// names into the gateway-wide tool identifier. Server slugs can never contain
// a dash run: they are always computed server-side by conv.ToSlug, which
// collapses consecutive dashes, so splitting at the first occurrence is
// unambiguous against every addressable member.
const QualifiedNameSeparator = "--"

// SplitQualifiedName splits a gateway tool identifier into its member server
// slug and tool name. The tool-name half keeps any further separator
// occurrences — member tool names are upstream-controlled and may contain
// dash runs.
func SplitQualifiedName(name string) (serverSlug, toolName string, err error) {
	before, after, found := strings.Cut(name, QualifiedNameSeparator)
	if !found || before == "" || after == "" {
		return "", "", fmt.Errorf("tool name %q is not of the form serverslug%stoolname", name, QualifiedNameSeparator)
	}
	return before, after, nil
}

// QualifyName prefixes a member tool name with its server's slug.
func QualifyName(serverSlug, toolName string) string {
	return serverSlug + QualifiedNameSeparator + toolName
}
