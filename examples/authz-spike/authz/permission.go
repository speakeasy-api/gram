package authz

// Permission names a scope rather than an HTTP method.
type Permission string

// PermissionDefinition separates resource containment from permission inheritance.
type PermissionDefinition struct {
	// Name is the scope's stable name.
	Name Permission

	// Resource identifies the protected resource family.
	Resource ResourceKind

	// AllowedAnchors identifies boundaries at which this scope can be granted.
	AllowedAnchors []ResourceKind

	// AllowedConstraints lists supported grant attributes; empty permits none.
	AllowedConstraints []ConstraintKind

	// ImpliedBy lists scopes that directly satisfy this permission.
	ImpliedBy []Permission

	// ExcludedBy names opposing scopes that can remove this permission.
	ExcludedBy []Permission

	// Forms lists supported concrete questions; opposing scopes have no forms.
	Forms []OperationForm

	// RequiredAncestors identifies mandatory ancestry for existing resources.
	RequiredAncestors []ResourceKind

	// CreationParent identifies the parent target for creation, when supported.
	CreationParent ResourceKind

	// Discoverable permits discovery through the separate admission API.
	Discoverable bool
}

// ConstraintKind names a typed attribute understood by the evaluator.
type ConstraintKind string

const (
	// ToolConstraint permits narrowing a grant to one tool name.
	ToolConstraint ConstraintKind = "tool"

	// DispositionConstraint permits narrowing a grant by tool classification.
	DispositionConstraint ConstraintKind = "disposition"
)

// Constraints narrow a grant; empty fields impose no constraint.
type Constraints struct {
	// Tool narrows the scope to a named tool, when constrained.
	Tool string

	// Disposition requires this classification, when constrained.
	Disposition Disposition
}

// Grant binds a scope to one boundary, with no effect field.
type Grant struct {
	// Permission names the scope, including an opposing scope when applicable.
	Permission Permission

	// Anchor names the concrete resource or enclosing grant boundary.
	Anchor ResourceRef

	// Constraints narrow the grant within that boundary.
	Constraints Constraints
}
