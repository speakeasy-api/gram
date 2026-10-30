package authz

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/schema"
)

var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrForbidden       = errors.New("forbidden")
)

// Store is the storage contract. Begin must return a consistent snapshot
// (REPEATABLE READ on the primary): the revision and every row read through
// the Tx come from the same moment.
type Store interface {
	Begin(ctx context.Context, org string) (Tx, error)
	Revision(ctx context.Context, org string) (int64, error)
}

type Tx interface {
	Revision() int64
	Now() time.Time
	APIKey(ctx context.Context, id string) (APIKeyRow, error)
	Agent(ctx context.Context, id string) (AgentRow, error)
	SubjectRelationships(ctx context.Context, s Subject) ([]Relationship, error)
	// OrgFacts loads target-side facts. scopes lists the role subject scopes
	// the caller's grants reference ("role:R"), so a store can load only
	// those memberships.
	OrgFacts(ctx context.Context, markers []string, scopes []string) (memberships, marked []Relationship, err error)
	Close(ctx context.Context) error
}

type APIKeyRow struct {
	ID        string
	AgentID   string
	RevokedAt *time.Time
	ExpiresAt *time.Time
}

type AgentRow struct {
	ID          string
	OwnerUserID string
	ProjectID   string
	Status      string
}

// Stats counts work so tests and benchmarks can show what admission cost.
type Stats struct {
	SnapshotLoads atomic.Int64
	SnapshotHits  atomic.Int64
	FactLoads     atomic.Int64
}

type Engine struct {
	Schema *schema.Schema
	Store  Store
	Cache  *Cache // nil disables caching; correctness never depends on it
	Stats  Stats
}

func New(sc *schema.Schema, store Store, cache *Cache) *Engine {
	return &Engine{Schema: sc, Store: store, Cache: cache}
}

// Admit runs once per request, in the auth middleware.
func (e *Engine) Admit(ctx context.Context, cred Credential) (*Principal, error) {
	tx, err := e.Store.Begin(ctx, cred.Org)
	if err != nil {
		return nil, fmt.Errorf("authz admission: %w", err) // fail closed
	}
	defer func() { _ = tx.Close(ctx) }()

	rev, now := tx.Revision(), tx.Now()
	p := &Principal{Org: cred.Org, Revision: rev, AdmittedAt: now, admittedMono: time.Now()}

	switch {
	case cred.KeyID != "":
		key, err := tx.APIKey(ctx, cred.KeyID)
		if err != nil || key.RevokedAt != nil || (key.ExpiresAt != nil && !now.Before(*key.ExpiresAt)) {
			return nil, ErrUnauthenticated
		}
		agent, err := tx.Agent(ctx, key.AgentID) // live read, never cached
		if err != nil || agent.Status != "active" {
			return nil, ErrUnauthenticated
		}
		p.Actor = Agent(agent.ID)
		for _, l := range []struct {
			kind LayerKind
			subj Subject
		}{
			{LayerCeiling, APIKey(key.ID)},
			{LayerSubject, Agent(agent.ID)},
			{LayerOwner, User(agent.OwnerUserID)},
		} {
			snap, err := e.snapshot(ctx, tx, cred.Org, l.subj, l.kind, rev)
			if err != nil {
				return nil, err
			}
			p.Layers = append(p.Layers, snap)
		}
	case cred.UserID != "":
		p.Actor = User(cred.UserID)
		snap, err := e.snapshot(ctx, tx, cred.Org, p.Actor, LayerSubject, rev)
		if err != nil {
			return nil, err
		}
		p.Layers = []*Snapshot{snap}
	default:
		return nil, ErrUnauthenticated
	}

	if p.Facts, err = e.orgFacts(ctx, tx, cred.Org, rev, referencedScopes(p.Layers)); err != nil {
		return nil, err
	}
	for _, l := range p.Layers {
		if !l.validUntil.IsZero() && (p.ValidUntil.IsZero() || l.validUntil.Before(p.ValidUntil)) {
			p.ValidUntil = l.validUntil
		}
	}
	return p, nil
}

func (e *Engine) snapshot(ctx context.Context, tx Tx, org string, s Subject, kind LayerKind, rev int64) (*Snapshot, error) {
	key := cacheKey{org: org, subject: s, kind: kind, revision: rev, schema: e.Schema.Hash}
	if snap, ok := e.Cache.get(key); ok {
		e.Stats.SnapshotHits.Add(1)
		return snap, nil
	}
	rows, err := tx.SubjectRelationships(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("load relationships for %s: %w", s, err)
	}
	e.Stats.SnapshotLoads.Add(1)
	snap := newSnapshot(kind, s, rev, rows)
	e.Cache.put(key, snap)
	return snap, nil
}

func referencedScopes(layers []*Snapshot) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range layers {
		for _, es := range l.byAnchor {
			for _, e := range es {
				if strings.HasPrefix(e.subject, "role:") && !seen[e.subject] {
					seen[e.subject] = true
					out = append(out, e.subject)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func (e *Engine) orgFacts(ctx context.Context, tx Tx, org string, rev int64, scopes []string) (*OrgFacts, error) {
	key := cacheKey{org: org, revision: rev, schema: e.Schema.Hash, facts: true, scopes: strings.Join(scopes, ",")}
	if f, ok := e.Cache.getFacts(key); ok {
		return f, nil
	}
	members, marked, err := tx.OrgFacts(ctx, e.Schema.Markers(), scopes)
	if err != nil {
		return nil, fmt.Errorf("load org facts: %w", err)
	}
	e.Stats.FactLoads.Add(1)
	f := NewOrgFacts(rev, members, marked)
	e.Cache.putFacts(key, f)
	return f, nil
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func PrincipalFrom(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(*Principal)
	return p, ok
}

type Request struct {
	Type     string
	Perm     string
	Object   Ref
	Context  any  // condition facts, e.g. model.MCPToolContext
	Eligible bool // "could any context satisfy this?"
	Explain  bool
}

// Check evaluates one request against every layer. No I/O.
func (e *Engine) Check(ctx context.Context, req Request) Decision {
	d := Decision{Perm: req.Type + "." + req.Perm}
	p, ok := PrincipalFrom(ctx)
	if !ok {
		d.Reason = "no admitted principal"
		return d
	}
	if p.Expired() {
		d.Outcome, d.Reason = Undetermined, "principal past ValidUntil; re-admit"
		return d
	}
	if req.Object.Type != req.Type {
		d.Reason = "permission and object type differ"
		return d
	}
	if req.Object.Org() != p.Org {
		d.Reason = "cross-tenant object"
		return d
	}
	now := p.Now()
	for _, layer := range p.Layers {
		c := evalCtx{sc: e.Schema, layer: layer, facts: p.Facts, now: now, reqCtx: req.Context, eligible: req.Eligible, explain: req.Explain}
		switch c.perm(req.Object, req.Perm) {
		case schema.True:
			if req.Explain {
				d.Path = append(d.Path, layer.Kind.String()+": "+fmt.Sprint(c.path))
			}
			continue
		case schema.Unknown:
			d.Outcome, d.Layer, d.Reason = Undetermined, layer.Kind, "a required fact was not supplied"
			return d
		default:
			d.Outcome, d.Layer, d.Reason = Denied, layer.Kind, "no matching allow, or a block applied"
			return d
		}
	}
	d.Outcome = Allowed
	return d
}

// Lookup returns a predicate for every object of typ the principal may perm.
func (e *Engine) Lookup(ctx context.Context, typ, perm string) (Filter, error) {
	p, ok := PrincipalFrom(ctx)
	if !ok {
		return Filter{}, ErrForbidden
	}
	var f Filter
	for _, layer := range p.Layers {
		c := lookupCtx{sc: e.Schema, layer: layer, facts: p.Facts, now: p.Now()}
		f.Layers = append(f.Layers, c.perm(typ, perm))
	}
	return f, nil
}

type Opt func(*Request)

// With supplies condition facts for a check.
func With(reqCtx any) Opt { return func(r *Request) { r.Context = reqCtx } }

func request[T Object](p Permission[T], obj T, opts []Opt) Request {
	r := Request{Type: p.Type, Perm: p.Name, Object: obj.AuthzRef()}
	for _, o := range opts {
		o(&r)
	}
	return r
}

// Require is the one-line handler gate.
func Require[T Object](ctx context.Context, e *Engine, p Permission[T], obj T, opts ...Opt) error {
	if d := e.Check(ctx, request(p, obj, opts)); !d.Allowed() {
		return fmt.Errorf("%w: %s %s (layer %s: %s)", ErrForbidden, d.Perm, d.Outcome, d.Layer, d.Reason)
	}
	return nil
}

func Can[T Object](ctx context.Context, e *Engine, p Permission[T], obj T, opts ...Opt) bool {
	return e.Check(ctx, request(p, obj, opts)).Allowed()
}

func Eligible[T Object](ctx context.Context, e *Engine, p Permission[T], obj T) bool {
	r := request(p, obj, nil)
	r.Eligible = true
	return e.Check(ctx, r).Allowed()
}

func Explain[T Object](ctx context.Context, e *Engine, p Permission[T], obj T, opts ...Opt) Decision {
	r := request(p, obj, opts)
	r.Explain = true
	return e.Check(ctx, r)
}

func List[T Object](ctx context.Context, e *Engine, p Permission[T]) (Filter, error) {
	return e.Lookup(ctx, p.Type, p.Name)
}

// Guard keeps a long-lived stream honest: Still re-admits when the revision
// moves or the principal passes ValidUntil, then re-checks.
type Guard[T Object] struct {
	e     *Engine
	cred  Credential
	perm  Permission[T]
	obj   T
	every time.Duration
	last  time.Time
	mu    sync.Mutex
	ctx   context.Context
}

func NewGuard[T Object](ctx context.Context, e *Engine, cred Credential, p Permission[T], obj T, every time.Duration) *Guard[T] {
	return &Guard[T]{e: e, cred: cred, perm: p, obj: obj, every: every, ctx: ctx, last: time.Now()}
}

func (g *Guard[T]) Still() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	p, _ := PrincipalFrom(g.ctx)
	due := time.Since(g.last) >= g.every || p == nil || p.Expired()
	if due {
		g.last = time.Now()
		rev, err := g.e.Store.Revision(g.ctx, g.cred.Org)
		if err != nil {
			return err
		}
		if p == nil || p.Expired() || rev != p.Revision {
			np, err := g.e.Admit(g.ctx, g.cred)
			if err != nil {
				return err
			}
			g.ctx = WithPrincipal(g.ctx, np)
		}
	}
	return Require(g.ctx, g.e, g.perm, g.obj)
}
