package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// ErrDenied means evaluation did not prove permission for protected work.
var ErrDenied = errors.New("authorization denied")

// Outcome keeps uncertainty distinct from both an allow and a definite denial.
type Outcome string

const (
	// Allowed means the backend proved the requested access.
	Allowed Outcome = "allowed"

	// Denied means policy does not permit the requested access.
	Denied Outcome = "denied"

	// Unresolved means the available facts cannot establish the answer.
	Unresolved Outcome = "unresolved"
)

// Decision describes the backend result, not a reusable authorization credential.
type Decision struct {
	// Outcome identifies whether access was established.
	Outcome Outcome

	// Reason explains the result without changing its policy interpretation.
	Reason string

	// PolicyRevision identifies the coherent in-memory snapshot used to decide.
	PolicyRevision uint64

	// Exclusion records opposing-scope evidence for concrete checks and their
	// alternatives, independently of missing allows. Discovery leaves it unset.
	Exclusion ExclusionEvidence
}

// Evaluation contains a validated question and a snapshot of its definition.
type Evaluation struct {
	// Subject identifies whose live policy the backend loads.
	Subject Subject

	// Definition describes the permission being evaluated.
	Definition PermissionDefinition

	// Target supplies the protected resource and trusted enclosing boundaries.
	Target Target

	// Form identifies resource, invocation, or creation semantics.
	Form OperationForm

	// Tool contains invocation facts only for tool operations.
	Tool *ToolFacts
}

// Admission contains a discovery question with no fabricated invocation facts.
type Admission struct {
	// Subject identifies whose effective access is being discovered.
	Subject Subject

	// Definition describes the permission whose remaining access is sought.
	Definition PermissionDefinition

	// Target identifies the resource being considered for admission.
	Target Target
}

// Backend evaluates concrete and discovery questions separately.
// Policy-driven lookup, authorized policy-editing workflows, richer evidence, and
// containment are later slices; persistence alone does not authorize a mutation.
type Backend interface {
	// Authorize evaluates a concrete operation against current policy.
	Authorize(context.Context, Evaluation) (Decision, error)

	// AuthorizeAny evaluates alternatives for one subject against one snapshot.
	AuthorizeAny(context.Context, []Evaluation, AlternativeMode) (Decision, error)

	// AuthorizeGroups evaluates independent domain paths under one snapshot.
	AuthorizeGroups(context.Context, Subject, []EvaluationGroup) (Decision, error)

	// Filter evaluates bounded resource candidates using one principal snapshot.
	Filter(context.Context, Subject, []Evaluation) ([]ResourceRef, error)

	// Discover proves whether any access remains after exclusions.
	Discover(context.Context, Admission) (Decision, error)
}

// Client validates application questions without becoming a second evaluator.
type Client struct {
	backend     Backend
	definitions map[Permission]PermissionDefinition
}

// NewClient snapshots ordered declarations into an index for per-call lookup.
// Duplicate names and unknown relationship references are configuration errors.
func NewClient(backend Backend, definitions []PermissionDefinition) (*Client, error) {
	if backend == nil {
		return nil, fmt.Errorf("authorization backend is required")
	}
	index := make(map[Permission]PermissionDefinition, len(definitions))
	for _, definition := range definitions {
		if definition.Name == "" || definition.Resource == "" || len(definition.AllowedAnchors) == 0 {
			return nil, fmt.Errorf("permission definition requires name, resource, and grant anchors")
		}
		if _, exists := index[definition.Name]; exists {
			return nil, fmt.Errorf("duplicate permission definition %q", definition.Name)
		}
		index[definition.Name] = copyDefinition(definition)
	}
	for _, definition := range index {
		seenConstraints := make(map[ConstraintKind]bool, len(definition.AllowedConstraints))
		for _, constraint := range definition.AllowedConstraints {
			switch constraint {
			case ToolConstraint, DispositionConstraint:
			default:
				return nil, fmt.Errorf("permission %q declares unsupported constraint %q", definition.Name, constraint)
			}
			if seenConstraints[constraint] {
				return nil, fmt.Errorf("permission %q declares duplicate constraint %q", definition.Name, constraint)
			}
			seenConstraints[constraint] = true
		}
		for _, related := range append(slices.Clone(definition.ImpliedBy), definition.ExcludedBy...) {
			other, exists := index[related]
			if !exists || other.Resource != definition.Resource {
				return nil, fmt.Errorf("permission %q has invalid relationship %q", definition.Name, related)
			}
		}
		for _, form := range definition.Forms {
			switch form {
			case ResourceOperation, ToolOperation:
			case CreationOperation:
				if definition.CreationParent == "" || !slices.Contains(definition.AllowedAnchors, definition.CreationParent) {
					return nil, fmt.Errorf("permission %q requires a supported creation parent", definition.Name)
				}
			default:
				return nil, fmt.Errorf("permission %q has an unsupported operation form", definition.Name)
			}
		}
	}
	return &Client{backend: backend, definitions: index}, nil
}

// Authorize validates the operation before asking the backend for a decision.
func (c *Client) Authorize(ctx context.Context, subject Subject, request Request) (Decision, error) {
	question, err := c.prepareEvaluation(subject, request)
	if err != nil {
		return Decision{}, err
	}
	return c.backend.Authorize(ctx, question)
}

func (c *Client) prepareEvaluation(subject Subject, request Request) (Evaluation, error) {
	target, err := snapshotTarget(request.Target)
	if err != nil {
		return Evaluation{}, err
	}
	definition, err := c.validate(subject, request.Permission, target)
	if err != nil {
		return Evaluation{}, err
	}
	form := ResourceOperation
	var tool *ToolFacts
	if request.Tool != nil {
		form = ToolOperation
		facts := *request.Tool
		if facts.Name == "" || facts.Name == "*" {
			return Evaluation{}, fmt.Errorf("tool invocation requires a concrete tool name")
		}
		switch facts.Disposition {
		case DispositionUnknown, ReadOnly, Destructive, Idempotent, OpenWorld:
		default:
			return Evaluation{}, fmt.Errorf("tool invocation has an unsupported disposition")
		}
		tool = &facts
	}
	if request.Creation {
		if tool != nil {
			return Evaluation{}, fmt.Errorf("creation cannot also be a tool invocation")
		}
		form = CreationOperation
	}
	if !slices.Contains(definition.Forms, form) {
		return Evaluation{}, fmt.Errorf("permission %q does not support %q questions", request.Permission, form)
	}
	if form == CreationOperation {
		if target.Resource.Kind != definition.CreationParent {
			return Evaluation{}, fmt.Errorf("creation requires a %q parent", definition.CreationParent)
		}
	} else if err := validatePermissionTarget(definition, target); err != nil {
		return Evaluation{}, err
	}
	return Evaluation{
		Subject: subject, Definition: definition, Target: target,
		Form: form, Tool: tool,
	}, nil
}

// Require permits protected work only after an explicit allowed result.
func (c *Client) Require(ctx context.Context, subject Subject, request Request) error {
	decision, err := c.Authorize(ctx, subject, request)
	if err != nil {
		return err
	}
	if decision.Outcome != Allowed {
		return fmt.Errorf("%w: %s", ErrDenied, decision.Reason)
	}
	return nil
}

// DiscoverAccess cannot be substituted for authorization of a concrete operation.
func (c *Client) DiscoverAccess(ctx context.Context, subject Subject, permission Permission, input Target) (Decision, error) {
	target, err := snapshotTarget(input)
	if err != nil {
		return Decision{}, err
	}
	definition, err := c.validate(subject, permission, target)
	if err != nil {
		return Decision{}, err
	}
	if !definition.Discoverable {
		return Decision{}, fmt.Errorf("permission %q does not support discovery", permission)
	}
	if err := validatePermissionTarget(definition, target); err != nil {
		return Decision{}, err
	}
	return c.backend.Discover(ctx, Admission{Subject: subject, Definition: definition, Target: target})
}

func (c *Client) validate(subject Subject, permission Permission, target Target) (PermissionDefinition, error) {
	if err := validateSubject(subject); err != nil {
		return PermissionDefinition{}, err
	}
	if err := validateRef(target.Resource); err != nil {
		return PermissionDefinition{}, fmt.Errorf("authorization target: %w", err)
	}
	if subject.Principal.OrganizationID != target.Resource.OrganizationID {
		return PermissionDefinition{}, fmt.Errorf("subject and target belong to different organizations")
	}
	definition, exists := c.definitions[permission]
	if !exists {
		return PermissionDefinition{}, fmt.Errorf("unknown permission %q", permission)
	}
	return copyDefinition(definition), nil
}

func validatePermissionTarget(definition PermissionDefinition, target Target) error {
	if target.Resource.Kind != definition.Resource {
		return fmt.Errorf("permission %q requires a %q target", definition.Name, definition.Resource)
	}
	for _, required := range definition.RequiredAncestors {
		count := 0
		for _, ancestor := range target.Ancestors {
			if ancestor.Kind == required {
				count++
			}
		}
		if count != 1 {
			return fmt.Errorf("permission %q requires exactly one %q ancestor", definition.Name, required)
		}
	}
	return nil
}

func copyDefinition(definition PermissionDefinition) PermissionDefinition {
	definition.AllowedAnchors = slices.Clone(definition.AllowedAnchors)
	definition.AllowedConstraints = slices.Clone(definition.AllowedConstraints)
	definition.ImpliedBy = slices.Clone(definition.ImpliedBy)
	definition.ExcludedBy = slices.Clone(definition.ExcludedBy)
	definition.Forms = slices.Clone(definition.Forms)
	definition.RequiredAncestors = slices.Clone(definition.RequiredAncestors)
	return definition
}

// ReplaceGrants validates policy persistence, not the editor's write authority.
// The calling domain must authorize the mutation; the final showcase will
// demonstrate that gate separately from this persistence boundary.
func (c *Client) ReplaceGrants(ctx context.Context, principal Principal, grants []Grant) error {
	if err := validatePrincipal(principal); err != nil {
		return err
	}
	return c.ReplaceSourceGrants(ctx, directGrantSource(principal), grants)
}

func (c *Client) validateGrants(organizationID string, grants []Grant) error {
	for _, grant := range grants {
		definition, exists := c.definitions[grant.Permission]
		if !exists {
			return fmt.Errorf("unknown grant scope %q", grant.Permission)
		}
		if err := validateRef(grant.Anchor); err != nil {
			return fmt.Errorf("grant anchor: %w", err)
		}
		if grant.Anchor.OrganizationID != organizationID {
			return fmt.Errorf("grant belongs to a different organization")
		}
		if !slices.Contains(definition.AllowedAnchors, grant.Anchor.Kind) {
			return fmt.Errorf("scope %q cannot be anchored to %q", grant.Permission, grant.Anchor.Kind)
		}
		if grant.Constraints.Tool != "" && !slices.Contains(definition.AllowedConstraints, ToolConstraint) {
			return fmt.Errorf("scope %q does not support tool constraints", grant.Permission)
		}
		if grant.Constraints.Disposition != DispositionUnknown && !slices.Contains(definition.AllowedConstraints, DispositionConstraint) {
			return fmt.Errorf("scope %q does not support disposition constraints", grant.Permission)
		}
		if grant.Constraints.Tool == "*" {
			return fmt.Errorf("use an empty tool constraint for unrestricted tool access")
		}
		switch grant.Constraints.Disposition {
		case DispositionUnknown, ReadOnly, Destructive, Idempotent, OpenWorld:
		default:
			return fmt.Errorf("grant has an unsupported disposition")
		}
	}
	return nil
}
