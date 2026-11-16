// Why granting a scope deserves a warning.
//
// authzgen copies everything below the package line into package authz, so
// the model and the evaluator share these names without importing each other.
// Keep this file free of imports and of anything only the registry needs.
package registry

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
