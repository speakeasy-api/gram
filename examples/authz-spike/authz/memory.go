package authz

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// MemoryBackend stores policy without databases, caches, or implicit defaults.
// Its mutex makes each policy read or replacement one coherent snapshot.
type MemoryBackend struct {
	mu          sync.RWMutex
	policies    map[GrantSource][]Grant
	roles       map[Principal][]GrantSource
	credentials map[credentialKey]credentialBinding
	revision    uint64
}

var _ Backend = (*MemoryBackend)(nil)

// NewMemoryBackend constructs an empty store; absence grants no permission.
func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{
		mu: sync.RWMutex{}, policies: make(map[GrantSource][]Grant), roles: make(map[Principal][]GrantSource),
		credentials: make(map[credentialKey]credentialBinding), revision: 0,
	}
}

// BindCredential binds a credential to its principal and delegation atomically.
// An existing credential cannot be rebound to a different execution context.
func (b *MemoryBackend) BindCredential(ctx context.Context, principal Principal, credentialID string, delegation Delegation) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	key := credentialKey{organizationID: principal.OrganizationID, id: credentialID}
	binding := credentialBinding{principal: principal, delegation: delegation}
	if existing, exists := b.credentials[key]; exists && existing != binding {
		return fmt.Errorf("credential is already bound to another execution context")
	}
	b.credentials[key] = binding
	b.revision++
	return nil
}

// Authorize evaluates the validated question against freshly loaded policy.
func (b *MemoryBackend) Authorize(ctx context.Context, question Evaluation) (Decision, error) {
	snapshot, err := b.ResolvePolicies(ctx, question.Subject)
	if err != nil {
		return Decision{}, err
	}
	return snapshot.intersect(func(grants []ResolvedGrant) Decision {
		return evaluate(grants, question, snapshot.Revision)
	}), nil
}

// AuthorizeAny evaluates one subject's alternatives using one coherent grant read.
func (b *MemoryBackend) AuthorizeAny(ctx context.Context, questions []Evaluation, mode AlternativeMode) (Decision, error) {
	if len(questions) == 0 {
		return Decision{}, fmt.Errorf("authorization alternatives must not be empty")
	}
	if mode != IndependentAlternatives && mode != UnblockedAlternatives {
		return Decision{}, fmt.Errorf("unknown authorization alternative mode")
	}
	for _, question := range questions {
		if question.Subject != questions[0].Subject {
			return Decision{}, fmt.Errorf("authorization alternatives require the same subject")
		}
	}
	snapshot, err := b.ResolvePolicies(ctx, questions[0].Subject)
	if err != nil {
		return Decision{}, err
	}
	return snapshot.intersect(func(grants []ResolvedGrant) Decision {
		return evaluateAlternatives(grants, questions, mode, snapshot.Revision)
	}), nil
}

// AuthorizeGroups evaluates independent paths without flattening their exclusions.
// All groups and leaves use one subject's coherent policy snapshot.
func (b *MemoryBackend) AuthorizeGroups(ctx context.Context, subject Subject, groups []EvaluationGroup) (Decision, error) {
	if len(groups) == 0 || len(groups) > MaxGroupRequests {
		return Decision{}, fmt.Errorf("authorization groups require a bounded nonempty list")
	}
	total := 0
	for _, group := range groups {
		if group.Mode != IndependentAlternatives && group.Mode != UnblockedAlternatives {
			return Decision{}, fmt.Errorf("unknown authorization alternative mode")
		}
		total += len(group.Questions)
		if len(group.Questions) == 0 || total > MaxGroupRequests {
			return Decision{}, fmt.Errorf("authorization groups require 1 to %d total questions and no empty groups", MaxGroupRequests)
		}
		for _, question := range group.Questions {
			if question.Subject != subject {
				return Decision{}, fmt.Errorf("authorization groups require the same subject")
			}
		}
	}
	snapshot, err := b.ResolvePolicies(ctx, subject)
	if err != nil {
		return Decision{}, err
	}
	for range groups {
		if err := ctx.Err(); err != nil {
			return Decision{}, err
		}
	}
	return snapshot.intersect(func(grants []ResolvedGrant) Decision {
		decisions := make([]Decision, 0, len(groups))
		for _, group := range groups {
			decisions = append(decisions, evaluateAlternatives(grants, group.Questions, group.Mode, snapshot.Revision))
		}
		return combineAlternatives(decisions, IndependentAlternatives, snapshot.Revision)
	}), nil
}

// Filter keeps only explicit allows from a bounded batch under one policy revision.
// Missing permissions, opposing grants, and unresolved matches omit a candidate;
// binding and backend errors fail the batch without returning partial results.
func (b *MemoryBackend) Filter(ctx context.Context, subject Subject, questions []Evaluation) ([]ResourceRef, error) {
	if len(questions) > MaxFilterCandidates {
		return nil, fmt.Errorf("resource filtering exceeds %d candidates", MaxFilterCandidates)
	}
	for _, question := range questions {
		if question.Subject != subject || question.Form != ResourceOperation {
			return nil, fmt.Errorf("resource filtering requires one subject and resource operations")
		}
	}
	snapshot, err := b.ResolvePolicies(ctx, subject)
	if err != nil {
		return nil, err
	}
	allowed := make([]ResourceRef, 0, len(questions))
	for _, question := range questions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		decision := snapshot.intersect(func(grants []ResolvedGrant) Decision {
			return evaluate(grants, question, snapshot.Revision)
		})
		if decision.Outcome == Allowed {
			allowed = append(allowed, question.Target.Resource)
		}
	}
	return allowed, nil
}

// Discover proves a nonempty permitted region by partitioning tool constraints.
// This is a policy-existence proof, not a claim that a tool currently exists in
// the server catalog. It never supplies permission to execute the witness.
func (b *MemoryBackend) Discover(ctx context.Context, question Admission) (Decision, error) {
	snapshot, err := b.ResolvePolicies(ctx, question.Subject)
	if err != nil {
		return Decision{}, err
	}
	// Each named tool is a separate region; the final region represents any
	// other name, making subtraction complete for exact-name constraints.
	tools := []string{}
	for _, policy := range snapshot.Policies {
		for _, assignment := range policy.Grants {
			grant := assignment.Grant
			if relevantScope(question.Definition, grant.Permission) && grant.Constraints.Tool != "" {
				if !slices.Contains(tools, grant.Constraints.Tool) {
					tools = append(tools, grant.Constraints.Tool)
				}
			}
		}
	}
	other := "<other-tool>"
	for slices.Contains(tools, other) {
		other += "_"
	}
	tools = append(tools, other)
	dispositions := []Disposition{DispositionUnknown, ReadOnly, Destructive, Idempotent, OpenWorld}
	uncertain := false
	for _, tool := range tools {
		for _, disposition := range dispositions {
			if err := ctx.Err(); err != nil {
				return Decision{}, err
			}
			facts := ToolFacts{Name: tool, Disposition: disposition}
			decision := snapshot.intersect(func(grants []ResolvedGrant) Decision {
				return evaluate(grants, Evaluation{
					Subject: question.Subject, Definition: question.Definition,
					Target: question.Target, Form: ToolOperation, Tool: &facts,
				}, snapshot.Revision)
			})
			if decision.Outcome == Allowed {
				return Decision{Outcome: Allowed, Reason: "some shared tool-use policy remains after exclusions", PolicyRevision: snapshot.Revision}, nil
			}
			uncertain = uncertain || decision.Outcome == Unresolved
		}
	}
	if uncertain {
		return Decision{Outcome: Unresolved, Reason: "remaining access could not be established", PolicyRevision: snapshot.Revision}, nil
	}
	return Decision{Outcome: Denied, Reason: "no shared tool-use policy remains after exclusions", PolicyRevision: snapshot.Revision}, nil
}
