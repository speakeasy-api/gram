//nolint:exhaustruct // Stub answers rely on zero-value fields.
package zedspike

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
)

// Permissionship is the answer to one check under one policy.
type Permissionship int

const (
	NoPermission Permissionship = iota
	HasPermission
	// ConditionalPermission means a caveat needed context the check did not
	// carry. Every Client method treats it as a deny, so a missing fact fails
	// closed.
	ConditionalPermission
)

// ErrDenied is returned by Require when any policy behind the caller denies.
var ErrDenied = errors.New("permission denied")

// ErrNotImplemented marks the stubbed evaluator.
var ErrNotImplemented = errors.New("not implemented in the spike")

// Subject is one principal whose grants make up a policy.
type Subject struct {
	Type string // user, agent, api_key
	ID   string
}

// Policy is one principal's grants. The schema is evaluated once per policy.
type Policy struct {
	Subject Subject
	// Grants are the rows written for the subject, its roles and, for a user,
	// user:all. Loaded once per request at admission.
	Grants []GrantRow
	// Roles are the role URNs the subject holds, read as role#member.
	Roles []string
}

// Caller is everyone standing behind a request. A person is one policy; an
// agent's credential is the credential ceiling, the live agent and its owner;
// a workload is the session ceiling, its assigned agent and that agent's
// owner. Every policy must allow: this is the agent intersection, and it
// stays here rather than in rbac.zed because SpiceDB's "&" intersects two
// permissions of one subject, not two subjects.
type Caller struct {
	OrganizationID string
	Policies       []Policy
	// RepresentedUser is the user an upstream auth client check acts for.
	RepresentedUser string
	// RepresentedPolicies are that user's own policies. An auth client check
	// must pass them too, so an agent cannot use a connection its represented
	// user could not use.
	RepresentedPolicies []Policy
}

// clone copies the caller deeply, so nothing holding the original can change
// the grants a later check reads.
func (c Caller) clone() Caller {
	out := c
	out.Policies = clonePolicies(c.Policies)
	out.RepresentedPolicies = clonePolicies(c.RepresentedPolicies)
	return out
}

func clonePolicies(policies []Policy) []Policy {
	if policies == nil {
		return nil
	}
	out := make([]Policy, len(policies))
	for i, p := range policies {
		out[i] = Policy{Subject: p.Subject, Roles: slices.Clone(p.Roles), Grants: make([]GrantRow, len(p.Grants))}
		for j, g := range p.Grants {
			g.Selector = maps.Clone(g.Selector)
			out[i].Grants[j] = g
		}
	}
	return out
}

type callerKey struct{}

// withCaller stores the admitted caller. Admission lives in this package, so
// no handler can replace the caller before a check.
func withCaller(ctx context.Context, caller Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, caller.clone())
}

// CallerFrom returns the admitted caller. Handlers never pass one: the
// client reads it, so no handler can check as someone else.
func CallerFrom(ctx context.Context) (Caller, bool) {
	caller, ok := ctx.Value(callerKey{}).(Caller)
	return caller.clone(), ok
}

// Evaluator answers checks over one policy. The production implementation
// walks the compiled Registry over the policy's rows; the conformance test
// runs the same checks through SpiceDB's own evaluator.
type Evaluator interface {
	Check(ctx context.Context, organizationID string, policy Policy, check Check) (Permissionship, error)
	LookupResources(ctx context.Context, organizationID string, policy Policy, lookup Lookup) (Predicate, error)
}

// Client is what handlers hold.
type Client struct {
	registry  *Registry
	evaluator Evaluator
}

func NewClient(registry *Registry, evaluator Evaluator) *Client {
	return &Client{registry: registry, evaluator: evaluator}
}

// Require returns ErrDenied unless every policy behind the caller allows.
func (c *Client) Require(ctx context.Context, check Check) error {
	ok, err := c.Allowed(ctx, check)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", ErrDenied, check)
	}
	return nil
}

// Allowed reports whether every policy behind the caller allows.
func (c *Client) Allowed(ctx context.Context, check Check) (bool, error) {
	caller, ok := CallerFrom(ctx)
	if !ok || len(caller.Policies) == 0 {
		return false, nil
	}
	// every organization edge is the caller's organization
	if check.organizationID != "" && check.organizationID != caller.OrganizationID {
		return false, nil
	}
	policies := caller.Policies
	if check.resourceType == "mcp_auth_client" {
		check.context = maps.Clone(check.context)
		check.context["represented_user"] = caller.RepresentedUser
		policies = append(slices.Clone(policies), caller.RepresentedPolicies...)
	}
	for _, policy := range policies {
		answer, err := c.evaluator.Check(ctx, caller.OrganizationID, policy, check)
		if err != nil {
			return false, fmt.Errorf("check %s: %w", check, err)
		}
		if answer != HasPermission {
			return false, nil
		}
	}
	return true, nil
}

// AllowedEach answers a bounded list of checks in order, for filtering a
// short list such as one server's tools. A paginated or counted list uses
// LookupResources instead: filtering a page gives short pages and wrong
// totals.
func (c *Client) AllowedEach(ctx context.Context, checks []Check) ([]bool, error) {
	out := make([]bool, len(checks))
	for i, check := range checks {
		ok, err := c.Allowed(ctx, check)
		if err != nil {
			return nil, err
		}
		out[i] = ok
	}
	return out, nil
}

// Lookup asks which resources of a kind in one project the caller may reach
// with one permission. Built only by the typed methods.
type Lookup struct {
	resourceType string
	permission   string
	projectID    string
}

// ReadableMCPServers looks up the servers of a project the caller may read.
func (p ProjectRef) ReadableMCPServers() Lookup {
	return Lookup{resourceType: "mcp_server", permission: "read", projectID: p.id}
}

// ReadableChats looks up the chats of a project the caller may read.
func (p ProjectRef) ReadableChats() Lookup {
	return Lookup{resourceType: "chat", permission: "read", projectID: p.id}
}

// Predicate is the answer to a lookup as a SQL condition rather than a list of
// IDs: most grants name every resource of a project, which only the database
// can enumerate. The same predicate drives the page and the count, so a total
// always equals what the caller can page through.
type Predicate struct {
	// All is true when a grant on the project or organization covers every
	// row and nothing subtracts from it.
	All bool
	// Included are IDs allowed by name.
	Included []string
	// Excluded are IDs a block removes; ExcludedAfter are removed even from
	// Included (the caller's own blocks).
	Excluded      []string
	ExcludedAfter []string
	// ByAuthor and ByAgentOwner add "the row is mine" terms for chats: the
	// caller's user ID matched against the author column, or against the
	// owner of the chat's agent.
	ByAuthor     string
	ByAgentOwner string
}

// LookupResources intersects the predicate of every policy behind the caller.
func (c *Client) LookupResources(ctx context.Context, lookup Lookup) (Predicate, error) {
	caller, ok := CallerFrom(ctx)
	if !ok || len(caller.Policies) == 0 {
		return Predicate{}, nil
	}
	var predicates []Predicate
	for _, policy := range caller.Policies {
		predicate, err := c.evaluator.LookupResources(ctx, caller.OrganizationID, policy, lookup)
		if err != nil {
			return Predicate{}, fmt.Errorf("lookup %s#%s: %w", lookup.resourceType, lookup.permission, err)
		}
		predicates = append(predicates, predicate)
	}
	return intersect(predicates), nil
}

// SQL renders the predicate against a table's ID column.
func (p Predicate) SQL(idColumn string) (string, []any) {
	_ = idColumn
	panic(ErrNotImplemented)
}

func intersect(predicates []Predicate) Predicate {
	_ = predicates
	panic(ErrNotImplemented)
}

// RegistryEvaluator is the in-process evaluator over the compiled registry.
type RegistryEvaluator struct{ Registry *Registry }

// Check lands the policy's rows (Registry.Land), adds the check's edges and
// facts, and evaluates the permission's expression, evaluating caveats with
// SpiceDB's caveats package.
func (e RegistryEvaluator) Check(context.Context, string, Policy, Check) (Permissionship, error) {
	return NoPermission, ErrNotImplemented
}

// LookupResources compiles the permission's expression into a Predicate:
// relations on the project or organization fold to All, relations on the
// resource become Included or Excluded ID sets, facts become column terms.
func (e RegistryEvaluator) LookupResources(context.Context, string, Policy, Lookup) (Predicate, error) {
	return Predicate{}, ErrNotImplemented
}
