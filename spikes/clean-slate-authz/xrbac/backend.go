package xrbac

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Backend is the plan's swappable evaluator contract. Every operation loads
// the authority it needs itself; the client only validates.
type Backend interface {
	Authorize(context.Context, Evaluation) (Decision, error)
	AuthorizeAny(context.Context, []Evaluation) (Decision, error)
	AuthorizeAll(context.Context, []Evaluation) (Decision, error)
	Filter(context.Context, []Evaluation) ([]Target, error)
	DiscoverAccess(context.Context, Subject, AccessQuery) (Decision, error)
	CheckDelegation(context.Context, Subject, Delegation) (Decision, error)
	Explain(context.Context, Evaluation) (Explanation, error)
}

// Authority is everything loaded for one operation: the separately required
// limits and the relationship-derived rules for each.
type Authority struct {
	Limits        []PolicyLimit
	Relationships map[LimitKind][]Rule
}

// Source is the domain-adapter boundary: credential, lifecycle, ownership,
// membership and stored grants. PostgreSQL and memory implement it.
type Source interface {
	Begin(ctx context.Context, org string) (SourceTx, error)
}

type SourceTx interface {
	Now() time.Time
	// UserPolicy returns the user's grants (direct, roles, directory-mapped
	// roles) and the agents they currently own.
	UserPolicy(ctx context.Context, userID string) (rules []Rule, ownedAgents []string, err error)
	// AgentPolicy returns the agent's own runtime grants (no owner, no user:all).
	AgentPolicy(ctx context.Context, agentID string) ([]Rule, error)
	Agent(ctx context.Context, id string) (AgentFacts, error)
	Credential(ctx context.Context, keyID string) (CredentialFacts, error)
	// SubjectPrincipals returns trusted attribution for activity targets:
	// the author and their roles, or the agent.
	SubjectPrincipals(ctx context.Context, targets []Ref) (map[Ref][]string, error)
	Close(ctx context.Context) error
}

type AgentFacts struct {
	ID, ProjectID, OwnerUserID, Status string
}

type CredentialFacts struct {
	ID, AgentID string
	RevokedAt   *time.Time
	ExpiresAt   *time.Time
	Policy      []Rule // stored delegated policy (decoded from its versioned envelope)
}

var ErrUnauthenticated = errors.New("unauthenticated")

// StoreBackend implements Backend over any Source.
type StoreBackend struct {
	Src Source
	// Offset shifts the evaluation clock. Test/demo helper only.
	Offset time.Duration
}

// relationships turns domain facts into explicit rules, subject to denial
// like any grant: self-access to your own activity, and management of (and
// activity for) agents you currently own.
func relationships(userID string, ownedAgents []string) []Rule {
	rules := []Rule{{Scope: ScopeChatRead, Selector: map[string]string{"resource_kind": "chat", "resource_id": "*", "subject_principal": "user:" + userID}, Source: "relationship:self"}}
	for _, a := range ownedAgents {
		rules = append(rules,
			Rule{Scope: ScopeAgentWrite, Selector: map[string]string{"resource_kind": "agent", "resource_id": a}, Source: "relationship:owner"},
			Rule{Scope: ScopeChatRead, Selector: map[string]string{"resource_kind": "chat", "resource_id": "*", "subject_principal": "agent:" + a}, Source: "relationship:owner"})
	}
	return rules
}

func (b *StoreBackend) authority(ctx context.Context, tx SourceTx, s Subject) (Authority, error) {
	switch s.Method {
	case AuthSession:
		uid, ok := strings.CutPrefix(s.Actor, "user:")
		if !ok {
			return Authority{}, ErrUnauthenticated
		}
		rules, owned, err := tx.UserPolicy(ctx, uid)
		if err != nil {
			return Authority{}, err
		}
		return Authority{Limits: []PolicyLimit{{Kind: LimitSubject, Rules: rules}}, Relationships: map[LimitKind][]Rule{LimitSubject: relationships(uid, owned)}}, nil
	case AuthAgentKey:
		now := tx.Now()
		cred, err := tx.Credential(ctx, s.CredentialID)
		if err != nil || cred.RevokedAt != nil || (cred.ExpiresAt != nil && !now.Before(*cred.ExpiresAt)) {
			return Authority{}, ErrUnauthenticated
		}
		agent, err := tx.Agent(ctx, cred.AgentID)
		if err != nil || agent.Status != "active" || s.Actor != "agent:"+agent.ID {
			return Authority{}, ErrUnauthenticated
		}
		agentRules, err := tx.AgentPolicy(ctx, agent.ID)
		if err != nil {
			return Authority{}, err
		}
		ownerRules, owned, err := tx.UserPolicy(ctx, agent.OwnerUserID)
		if err != nil {
			return Authority{}, err
		}
		return Authority{
			Limits: []PolicyLimit{
				{Kind: LimitCredential, Rules: cred.Policy},
				{Kind: LimitAgent, Rules: agentRules},
				{Kind: LimitOwner, Rules: ownerRules},
			},
			Relationships: map[LimitKind][]Rule{LimitOwner: relationships(agent.OwnerUserID, owned)},
		}, nil
	}
	return Authority{}, ErrUnauthenticated
}

// load opens one consistent read for one operation. A later operation
// reloads live authority: there is deliberately no cross-request cache.
func (b *StoreBackend) load(ctx context.Context, s Subject, evs []Evaluation) (SourceTx, Authority, map[Ref][]string, error) {
	tx, err := b.Src.Begin(ctx, s.Org)
	if err != nil {
		return nil, Authority{}, nil, err
	}
	a, err := b.authority(ctx, tx, s)
	if err != nil {
		_ = tx.Close(ctx)
		return nil, Authority{}, nil, err
	}
	var needs []Ref
	for _, ev := range evs {
		if hasKey(ev.Definition.SelectorKeys, "subject_principal") {
			needs = append(needs, ev.Request.Target.Resource)
		}
	}
	var sp map[Ref][]string
	if len(needs) > 0 {
		if sp, err = tx.SubjectPrincipals(ctx, needs); err != nil {
			_ = tx.Close(ctx)
			return nil, Authority{}, nil, err
		}
	}
	return tx, a, sp, nil
}

func hasKey(keys []string, k string) bool {
	return slices.Contains(keys, k)
}

func (b *StoreBackend) now(tx SourceTx) time.Time { return tx.Now().Add(b.Offset) }

func (b *StoreBackend) decide(tx SourceTx, a Authority, sp map[Ref][]string, ev Evaluation) Decision {
	var principals []string
	if hasKey(ev.Definition.SelectorKeys, "subject_principal") {
		principals = sp[ev.Request.Target.Resource]
		if principals == nil {
			principals = []string{} // known to have no trusted attribution
		}
	}
	return evaluate(a, ev.Request, requestFacts(ev.Request, principals), b.now(tx))
}

func (b *StoreBackend) Authorize(ctx context.Context, ev Evaluation) (Decision, error) {
	tx, a, sp, err := b.load(ctx, ev.Subject, []Evaluation{ev})
	if err != nil {
		return Decision{}, err
	}
	defer func() { _ = tx.Close(ctx) }()
	return b.decide(tx, a, sp, ev), nil
}

// AuthorizeAny: one same alternative must pass every limit.
func (b *StoreBackend) AuthorizeAny(ctx context.Context, evs []Evaluation) (Decision, error) {
	tx, a, sp, err := b.load(ctx, evs[0].Subject, evs)
	if err != nil {
		return Decision{}, err
	}
	defer func() { _ = tx.Close(ctx) }()
	var first Decision
	for i, ev := range evs {
		d := b.decide(tx, a, sp, ev)
		if d.Outcome == Allowed {
			return d, nil
		}
		if i == 0 {
			first = d
		}
	}
	return first, nil
}

func (b *StoreBackend) AuthorizeAll(ctx context.Context, evs []Evaluation) (Decision, error) {
	tx, a, sp, err := b.load(ctx, evs[0].Subject, evs)
	if err != nil {
		return Decision{}, err
	}
	defer func() { _ = tx.Close(ctx) }()
	var d Decision
	for _, ev := range evs {
		if d = b.decide(tx, a, sp, ev); d.Outcome != Allowed {
			return d, nil
		}
	}
	return d, nil
}

// Filter returns allowed targets in input order, for a bounded, already
// loaded candidate set. It is not a pagination substitute.
func (b *StoreBackend) Filter(ctx context.Context, evs []Evaluation) ([]Target, error) {
	tx, a, sp, err := b.load(ctx, evs[0].Subject, evs)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Close(ctx) }()
	var out []Target
	for _, ev := range evs {
		if b.decide(tx, a, sp, ev).Outcome == Allowed {
			out = append(out, ev.Request.Target)
		}
	}
	return out, nil
}

func (b *StoreBackend) DiscoverAccess(ctx context.Context, s Subject, q AccessQuery) (Decision, error) {
	tx, a, _, err := b.load(ctx, s, nil)
	if err != nil {
		return Decision{}, err
	}
	defer func() { _ = tx.Close(ctx) }()
	return discover(a, q, b.now(tx)), nil
}

// CheckDelegation loads the editor's and the current owner's authority itself;
// callers cannot supply a replacement parent policy.
func (b *StoreBackend) CheckDelegation(ctx context.Context, s Subject, d Delegation) (Decision, error) {
	if s.Method != AuthSession {
		return Decision{Outcome: Denied, Reason: ReasonNotDelegable}, nil
	}
	tx, editor, _, err := b.load(ctx, s, nil)
	if err != nil {
		return Decision{}, err
	}
	defer func() { _ = tx.Close(ctx) }()
	agent, err := tx.Agent(ctx, d.Agent.Resource.ID)
	if err != nil {
		return Decision{}, fmt.Errorf("load agent: %w", err)
	}
	ownerRules, owned, err := tx.UserPolicy(ctx, agent.OwnerUserID)
	if err != nil {
		return Decision{}, err
	}
	parents := []PolicyLimit{editor.Limits[0], {Kind: LimitOwner, Rules: ownerRules}}
	rels := map[LimitKind][]Rule{LimitSubject: editor.Relationships[LimitSubject], LimitOwner: relationships(agent.OwnerUserID, owned)}
	return contain(parents, rels, d.Proposed, b.now(tx)), nil
}

func (b *StoreBackend) Explain(ctx context.Context, ev Evaluation) (Explanation, error) {
	d, err := b.Authorize(ctx, ev)
	if err != nil {
		return Explanation{}, err
	}
	detail := []string{fmt.Sprintf("%s %s: %s", ev.Request.Scope, d.Outcome, d.Reason)}
	if d.Source != "" {
		detail = append(detail, "decided by "+d.Source)
	}
	return Explanation{Decision: d, Detail: detail}, nil
}
