// authzgen copies everything below the package line into package authz, so
// the model and the evaluator share these names without importing each other.
// Keep this file free of imports and of anything only the registry needs.
package registry

// Sensitivity says why granting a scope deserves a warning. The empty value
// marks a scope that needs none.
type Sensitivity string

const (
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

	// SensitivityCredentials marks a scope that can create, store, rotate or
	// read upstream credentials: API keys, IdP secrets, cloud IAM
	// credentials, encryption keys.
	SensitivityCredentials Sensitivity = "credentials"

	// SensitivityMemberIdentity marks a scope that exposes or changes who
	// members are: directory people and groups, and invitations.
	SensitivityMemberIdentity Sensitivity = "member_identity"

	// SensitivityMemberActivity marks a scope that exposes or acts on what
	// other members do: their sessions, risk findings, spend and the
	// security log.
	SensitivityMemberActivity Sensitivity = "member_activity"

	// SensitivityAccessControl marks a scope that decides what other members
	// can reach without changing a role, such as approving an MCP server
	// for them.
	SensitivityAccessControl Sensitivity = "access_control"
)

// Sensitivities is every reason a scope can carry, in presentation order.
// Validate refuses a permission whose sensitivity is not listed, and the API
// design must declare each one.
var Sensitivities = []Sensitivity{
	SensitivityConversations,
	SensitivityToolLogs,
	SensitivityDestructiveAdmin,
	SensitivityCredentials,
	SensitivityMemberIdentity,
	SensitivityMemberActivity,
	SensitivityAccessControl,
}
