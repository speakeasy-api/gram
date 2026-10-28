package xrbac

import (
	"context"
	"errors"
	"fmt"
)

var ErrInvalidRequest = errors.New("invalid authorization request")

// Client validates every question against the catalogue, then delegates to
// its configured Backend. It never evaluates policy itself.
type Client struct {
	backend  Backend
	maxBatch int
}

func NewClient(b Backend, maxBatch int) *Client {
	if b == nil || maxBatch <= 0 {
		panic("xrbac: backend and positive batch bound are required")
	}
	return &Client{backend: b, maxBatch: maxBatch}
}

func (c *Client) validate(s Subject, req Request) (Evaluation, error) {
	def, ok := byScope[req.Scope]
	if !ok || def.Lifecycle != LifecycleActive {
		return Evaluation{}, fmt.Errorf("%w: unknown or retired scope %q", ErrInvalidRequest, req.Scope)
	}
	if def.ExclusionPurpose == ExclusionNone && def.Visibility == "internal" {
		return Evaluation{}, fmt.Errorf("%w: %q is an exclusion, not a checkable permission", ErrInvalidRequest, req.Scope)
	}
	t := req.Target
	if t.Resource.Kind != def.ResourceKind || t.Resource.ID == "" || t.Resource.ID == "*" {
		return Evaluation{}, fmt.Errorf("%w: %s needs a concrete %s target", ErrInvalidRequest, req.Scope, def.ResourceKind)
	}
	if t.Resource.Org != s.Org {
		return Evaluation{}, fmt.Errorf("%w: cross-tenant target", ErrInvalidRequest)
	}
	for _, kind := range def.RequiredAncestors {
		n := 0
		for _, a := range t.Ancestors {
			if a.Kind == kind {
				if a.Org != s.Org {
					return Evaluation{}, fmt.Errorf("%w: cross-tenant ancestor", ErrInvalidRequest)
				}
				n++
			}
		}
		if n != 1 {
			return Evaluation{}, fmt.Errorf("%w: %s target needs exactly one %s ancestor", ErrInvalidRequest, def.ResourceKind, kind)
		}
	}
	if req.Kind == ToolCall && (req.Tool == nil || req.Tool.Name == "" || req.Tool.Classification == "" || !hasKey(def.SelectorKeys, "tool")) {
		return Evaluation{}, fmt.Errorf("%w: tool call needs tool facts with explicit classification", ErrInvalidRequest)
	}
	def, _ = LookupScopeDefinition(req.Scope)
	return Evaluation{Subject: s, Request: cloneRequest(req), Definition: def}, nil
}

func (c *Client) validateAll(s Subject, reqs []Request) ([]Evaluation, error) {
	if len(reqs) == 0 || len(reqs) > c.maxBatch {
		return nil, fmt.Errorf("%w: batch size %d outside 1..%d", ErrInvalidRequest, len(reqs), c.maxBatch)
	}
	evs := make([]Evaluation, len(reqs))
	for i, r := range reqs {
		ev, err := c.validate(s, r)
		if err != nil {
			return nil, err // a malformed alternative fails the whole request
		}
		evs[i] = ev
	}
	return evs, nil
}

func (c *Client) Authorize(ctx context.Context, s Subject, req Request) (Decision, error) {
	ev, err := c.validate(s, req)
	if err != nil {
		return Decision{}, err
	}
	return c.backend.Authorize(ctx, ev)
}

func (c *Client) Require(ctx context.Context, s Subject, req Request) error {
	d, err := c.Authorize(ctx, s, req)
	if err != nil {
		return err
	}
	return d.RequireAllowed()
}

func (c *Client) AuthorizeAny(ctx context.Context, s Subject, reqs []Request) (Decision, error) {
	evs, err := c.validateAll(s, reqs)
	if err != nil {
		return Decision{}, err
	}
	return c.backend.AuthorizeAny(ctx, evs)
}

func (c *Client) RequireAny(ctx context.Context, s Subject, reqs []Request) error {
	d, err := c.AuthorizeAny(ctx, s, reqs)
	if err != nil {
		return err
	}
	return d.RequireAllowed()
}

func (c *Client) AuthorizeAll(ctx context.Context, s Subject, reqs []Request) (Decision, error) {
	evs, err := c.validateAll(s, reqs)
	if err != nil {
		return Decision{}, err
	}
	return c.backend.AuthorizeAll(ctx, evs)
}

func (c *Client) Filter(ctx context.Context, s Subject, reqs []Request) ([]Target, error) {
	evs, err := c.validateAll(s, reqs)
	if err != nil {
		return nil, err
	}
	return c.backend.Filter(ctx, evs)
}

func (c *Client) DiscoverAccess(ctx context.Context, s Subject, q AccessQuery) (Decision, error) {
	if _, err := c.validate(s, ResourceCheck(q.Scope, q.Target)); err != nil {
		return Decision{}, err
	}
	return c.backend.DiscoverAccess(ctx, s, q)
}

func (c *Client) CheckDelegation(ctx context.Context, s Subject, d Delegation) (Decision, error) {
	if d.Agent.Resource.Kind != "agent" || d.Agent.Resource.Org != s.Org {
		return Decision{}, fmt.Errorf("%w: delegation needs an agent in the subject's organization", ErrInvalidRequest)
	}
	for _, r := range d.Proposed {
		if _, ok := byScope[r.Scope]; !ok {
			return Decision{}, fmt.Errorf("%w: unknown scope %q in proposal", ErrInvalidRequest, r.Scope)
		}
	}
	return c.backend.CheckDelegation(ctx, s, d)
}

func (c *Client) Explain(ctx context.Context, s Subject, req Request) (Explanation, error) {
	ev, err := c.validate(s, req)
	if err != nil {
		return Explanation{}, err
	}
	return c.backend.Explain(ctx, ev)
}
