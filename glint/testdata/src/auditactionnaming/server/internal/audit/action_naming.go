package audit

type Action string

const (
	ActionGood              Action = "api_key:create"
	ActionGoodUnderscore    Action = "remote_session_client:delete"
	ActionHyphenNamespace   Action = "api-key:create"    // want "name audit action \"api-key:create\" as <namespace>:<verb> with lowercase snake_case segments"
	ActionUpperNamespace    Action = "Api:create"        // want "name audit action \"Api:create\" as <namespace>:<verb> with lowercase snake_case segments"
	ActionMissingColon      Action = "project_create"    // want "name audit action \"project_create\" as <namespace>:<verb> with lowercase snake_case segments"
	ActionEmptyNamespace    Action = ":create"           // want "name audit action \":create\" as <namespace>:<verb> with lowercase snake_case segments"
	ActionEmptyVerb         Action = "project:"          // want "name audit action \"project:\" as <namespace>:<verb> with lowercase snake_case segments"
	ActionUpperVerb         Action = "project:Create"    // want "name audit action \"project:Create\" as <namespace>:<verb> with lowercase snake_case segments"
	ActionHyphenVerb        Action = "project:re-create" // want "name audit action \"project:re-create\" as <namespace>:<verb> with lowercase snake_case segments"
	ActionMultipleColons    Action = "project:role:set"  // want "name audit action \"project:role:set\" as <namespace>:<verb> with lowercase snake_case segments"
	NonActionStringConstant        = "api-key:create"
)
