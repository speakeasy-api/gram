package authz

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/schema"
)

// Proposal is one relationship a caller wants to write onto an agent.
type Proposal struct {
	ForType string          // the type that declares the grant, e.g. "mcp_server"
	Grant   string          // e.g. "connector"
	Anchor  Ref             // where it is anchored: the server, its project or the org
	Params  json.RawMessage // condition parameters; nil means unconditioned
}

// CanGrant checks delegation containment: every proposal must be Delegable,
// and both the caller and the agent's owner must hold it — at the same or a
// broader anchor, with a covering condition, and without an overlapping block.
// This is a write path, so loading the owner's snapshot here is fine.
func (e *Engine) CanGrant(ctx context.Context, ownerUserID string, proposals []Proposal) error {
	p, ok := PrincipalFrom(ctx)
	if !ok || len(p.Layers) != 1 || p.Actor.Type != "user" {
		return fmt.Errorf("%w: delegation is a human-session operation", ErrForbidden)
	}
	holders := []*Snapshot{p.Layers[0]}
	if ownerUserID != p.Actor.ID {
		tx, err := e.Store.Begin(ctx, p.Org)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Close(ctx) }()
		owner, err := e.snapshot(ctx, tx, p.Org, User(ownerUserID), LayerOwner, tx.Revision())
		if err != nil {
			return err
		}
		holders = append(holders, owner)
	}
	now := p.Now()
	for _, prop := range proposals {
		t, ok := e.Schema.Types[prop.ForType]
		if !ok {
			return fmt.Errorf("unknown type %q", prop.ForType)
		}
		g, ok := t.Grants[prop.Grant]
		if !ok || !g.Delegable {
			return fmt.Errorf("%w: %s.%s is not delegable to agents", ErrForbidden, prop.ForType, prop.Grant)
		}
		for _, h := range holders {
			if !holds(e.Schema, h, t, g, prop, now) {
				who := "your permissions"
				if h.Kind == LayerOwner {
					who = "the owner's permissions"
				}
				return fmt.Errorf("%w: %s.%s on %s:%s exceeds %s", ErrForbidden, prop.ForType, prop.Grant, prop.Anchor.Type, prop.Anchor.ID, who)
			}
		}
	}
	return nil
}

func holds(sc *schema.Schema, h *Snapshot, t *schema.Type, g *schema.GrantDef, prop Proposal, now time.Time) bool {
	anchors := append([]Ref{prop.Anchor}, prop.Anchor.Parents...)
	covered := false
	for _, a := range anchors {
		for _, e := range h.byAnchor[anchorKey{a.Type, a.ID, schema.StoredRelation(t.Name, g.Name, a.Type)}] {
			if !e.expiresAt.IsZero() && !now.Before(e.expiresAt) {
				continue
			}
			if e.cond == "" || (prop.Params != nil && sc.Conditions[e.cond].Covers != nil && sc.Conditions[e.cond].Covers(e.params, prop.Params)) {
				covered = true
			}
		}
	}
	if !covered {
		return false
	}
	// Unconditional blocks overlap: any on this type for a broad proposal, or
	// one at/above the object for a narrow proposal. Conditional blocks (e.g.
	// "no destructive tools") are left to runtime, where the owner layer
	// enforces them on every call. Spike limitation: a conditional block held
	// only by a non-owner configurer is not carried — see README.
	for _, bg := range t.Grants {
		if !bg.Block {
			continue
		}
		if prop.Anchor.Type != t.Name {
			for _, a := range bg.Anchors {
				at := a
				if a == schema.Self {
					at = t.Name
				}
				if hasUnconditional(h.byRelation[relKey{at, schema.StoredRelation(t.Name, bg.Name, at)}]) {
					return false
				}
			}
			continue
		}
		for _, a := range anchors {
			if hasUnconditional(h.byAnchor[anchorKey{a.Type, a.ID, schema.StoredRelation(t.Name, bg.Name, a.Type)}]) {
				return false
			}
		}
	}
	return true
}

func hasUnconditional(es []entry) bool {
	for _, e := range es {
		if e.cond == "" {
			return true
		}
	}
	return false
}
