package audit

// Saved queries were replaced by widgets. Nothing records these actions any
// more; they stay so audit rows already written keep their labels.
const (
	ActionQueryCreate Action = "query:create"
	ActionQueryUpdate Action = "query:update"
	ActionQueryDelete Action = "query:delete"
)
