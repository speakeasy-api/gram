package authz

// OperationForm identifies structural inputs, not individual business actions.
type OperationForm string

const (
	// ResourceOperation checks an existing resource.
	ResourceOperation OperationForm = "resource"

	// ToolOperation checks an invocation with concrete tool facts.
	ToolOperation OperationForm = "tool"

	// CreationOperation checks creation at the parent boundary.
	CreationOperation OperationForm = "creation"
)

// Request is the application-facing question, validated inside the client.
// The service owns the required permission and supplies trusted target data.
type Request struct {
	// Permission names the scope required by the calling service.
	Permission Permission

	// Target identifies the resource, or the parent for creation.
	Target Target

	// Tool supplies invocation facts; nil means a resource-level question.
	Tool *ToolFacts

	// Creation selects parent-boundary authorization, not an existing child.
	Creation bool
}
