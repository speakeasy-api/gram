package authz

import (
	"errors"
	"fmt"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/authz/schema"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Policy composition keeps who is acting separate from whose grants limit the
// request. A request carries one actor and one or more independent policies,
// and a permission is allowed only when it is allowed in every policy:
//
//   - An ordinary human has one policy: their own grants.
//   - A principal credential has the credential ceiling, the live agent and
//     the agent's current owner as three policies; a workload has its
//     session ceiling, assigned agent and owner.
//   - A captured boundary, such as a job started under an earlier request,
//     adds the policies it captured. Refreshing the live policies can narrow
//     authority but never lift the captured limits.
//   - An auth-client permission also needs every policy of the represented
//     user, when admission bound one.
//
// Each policy must satisfy the same complete permission, though different
// grants may satisfy it in each. Facts are evaluated for the actor in every
// policy, so an owner's policy never lends a machine actor the owner's
// authorship. Composition is three-valued like the evaluator: any policy that
// denies denies, otherwise any undecided policy makes the request an error.

// ErrNoPolicies reports a decision requested with no admitted policy. Missing
// admission is an error, never an empty all-pass intersection.
var ErrNoPolicies = errors.New("authorization has no admitted policies")

// Policy is the grant set of one principal that independently limits a
// request.
type Policy struct {
	// Principal names whose grants these are, for diagnostics only.
	Principal string

	// Grants are the policy's loaded grants. An empty policy denies.
	Grants []Grant
}

// PolicySet is the admitted authorization of one request.
type PolicySet struct {
	// Actor is the authenticated principal making the request.
	Actor urn.Principal

	// Policies are the live policies; every one must allow.
	Policies []Policy

	// Captured are the policies of a captured boundary; every one must also
	// allow, whatever the live policies say after a refresh.
	Captured []Policy

	// RepresentedUser is the trusted user an agent acts for, bound only to
	// $represented_user selectors.
	RepresentedUser string

	// Represented are the represented user's own policies. They limit
	// auth-client permissions only.
	Represented []Policy
}

// PolicyDecision is the composed outcome of one request.
type PolicyDecision struct {
	// Allowed reports whether every applicable policy allowed.
	Allowed bool

	// Reason explains the deciding policy's outcome.
	Reason PermissionReason

	// Policy is the principal of the first policy that denied, or "" when
	// allowed.
	Policy string

	// Option is the index of the alternative that allowed, or of the option
	// whose block vetoed the request; -1 for a single permission.
	Option int
}

func (e *RegistryEvaluator) policiesFor(set PolicySet, ref schema.PermissionRef) []Policy {
	policies := slices.Concat(set.Policies, set.Captured)
	if e.kinds[ref.Type] == ResourceKindMCPAuthClient {
		policies = append(policies, set.Represented...)
	}
	return policies
}

// bind applies the set's actor and represented user to req, so a request
// cannot claim a different actor from the one admission established.
func (set PolicySet) bind(req PermissionRequest) PermissionRequest {
	req.Actor = set.Actor
	req.RepresentedUser = set.RepresentedUser
	return req
}

func (set PolicySet) validate() error {
	if len(set.Policies) == 0 {
		return ErrNoPolicies
	}
	if set.Actor.IsZero() || set.Actor.ID == "" {
		return ErrMissingActor
	}
	return nil
}

// Decide answers req in every applicable policy of set.
func (e *RegistryEvaluator) Decide(set PolicySet, req PermissionRequest) (PolicyDecision, error) {
	if err := set.validate(); err != nil {
		return PolicyDecision{}, err
	}
	req = set.bind(req)
	p, err := e.prepare(req)
	if err != nil {
		return PolicyDecision{}, err
	}
	decision, err := e.decide(set, req, p)
	if err != nil {
		return PolicyDecision{}, err
	}
	decision.Option = -1
	return decision, nil
}

func (e *RegistryEvaluator) decide(set PolicySet, req PermissionRequest, p evaluatorPermission) (PolicyDecision, error) {
	var firstErr error
	for _, policy := range e.policiesFor(set, req.Permission) {
		out := e.run(policy.Grants, req, p)
		switch {
		case out.err != nil:
			if firstErr == nil {
				firstErr = fmt.Errorf("policy %s: %w", policy.Principal, out.err)
			}
		case !out.allowed:
			return PolicyDecision{Allowed: false, Reason: out.reason, Policy: policy.Principal, Option: -1}, nil
		}
	}
	if firstErr != nil {
		return PolicyDecision{}, fmt.Errorf("evaluate %s.%s: %w", req.Permission.Type, req.Permission.Permission, firstErr)
	}
	return PolicyDecision{Allowed: true, Reason: PermissionReasonMatched, Policy: "", Option: -1}, nil
}

// DecideAlternatives answers named alternatives: options lists one request per
// option of the compiled set, in declaration order. The request is allowed
// when one complete option is allowed in every policy; an option allowed in
// one policy and another option allowed in a second does not count. With
// VetoBlocks, a block standing against any option in any policy denies
// first, whatever the other options allow.
func (e *RegistryEvaluator) DecideAlternatives(set PolicySet, ref schema.PermissionRef, options []PermissionRequest) (PolicyDecision, error) {
	alternatives, ok := e.alternates[ref]
	if !ok {
		return PolicyDecision{}, fmt.Errorf("%w: alternatives %s.%s", ErrUnknownPermission, ref.Type, ref.Permission)
	}
	if len(options) != len(alternatives.Options) {
		return PolicyDecision{}, fmt.Errorf("alternatives %s.%s need %d requests, got %d", ref.Type, ref.Permission, len(alternatives.Options), len(options))
	}
	if err := set.validate(); err != nil {
		return PolicyDecision{}, err
	}

	// Validate every option before deciding any, so an invalid later option
	// errors even when an earlier one allows.
	options = slices.Clone(options)
	prepared := make([]evaluatorPermission, len(options))
	for i, option := range options {
		if option.Permission != alternatives.Options[i] {
			return PolicyDecision{}, fmt.Errorf("alternatives %s.%s option %d is %s.%s, got %s.%s", ref.Type, ref.Permission, i,
				alternatives.Options[i].Type, alternatives.Options[i].Permission, option.Permission.Type, option.Permission.Permission)
		}
		options[i] = set.bind(option)
		p, err := e.prepare(options[i])
		if err != nil {
			return PolicyDecision{}, err
		}
		prepared[i] = p
	}

	if alternatives.VetoBlocks {
		for i, option := range options {
			for _, policy := range e.policiesFor(set, option.Permission) {
				run := e.evaluation(policy.Grants, option, option.Permission.Type, resourceInstance{id: option.ResourceID, dimensions: option.Dimensions, facts: option.Facts})
				stands, _, restored := run.blockStands(prepared[i].def, run.allowMode())
				if restored.err != nil {
					return PolicyDecision{}, fmt.Errorf("veto %s.%s: %w", option.Permission.Type, option.Permission.Permission, restored.err)
				}
				if stands {
					return PolicyDecision{Allowed: false, Reason: PermissionReasonExcluded, Policy: policy.Principal, Option: i}, nil
				}
			}
		}
	}

	var firstErr error
	denied := PolicyDecision{Allowed: false, Reason: PermissionReasonNoGrant, Policy: "", Option: -1}
	for i, option := range options {
		decision, err := e.decide(set, option, prepared[i])
		switch {
		case err != nil:
			if firstErr == nil {
				firstErr = err
			}
		case decision.Allowed:
			decision.Option = i
			return decision, nil
		case denied.Policy == "":
			denied = decision
			denied.Option = i
		}
	}
	if firstErr != nil {
		return PolicyDecision{}, firstErr
	}
	return denied, nil
}
