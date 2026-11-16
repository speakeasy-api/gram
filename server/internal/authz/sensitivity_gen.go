// Code generated from registry/sensitivity.go by authzgen. DO NOT EDIT.
// Regenerate with `mise run gen:authz`.

package authz

// Sensitivity says why granting a scope deserves a warning. The empty value
// marks a scope that needs none.
type Sensitivity string

const (
	// SensitivityNone marks a scope that needs no warning before it is granted.
	SensitivityNone Sensitivity = ""

	// SensitivityConversations marks a scope that exposes other members'
	// agent session transcripts.
	SensitivityConversations Sensitivity = "conversations"

	// SensitivityToolLogs marks a scope that exposes tool arguments, results
	// and prompt text from every member of a project.
	SensitivityToolLogs Sensitivity = "tool_logs"

	// SensitivityDestructiveAdmin marks a scope that can remove members or
	// change what every role can do.
	SensitivityDestructiveAdmin Sensitivity = "destructive_admin"
)
