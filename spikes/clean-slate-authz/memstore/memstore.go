// Package memstore is an in-memory authz.Store with the same semantics as
// pgstore, so the scenarios run without a database.
package memstore

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/authz"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/model"
)

type Store struct {
	mu       sync.RWMutex
	rows     []authz.Relationship
	agents   map[string]authz.AgentRow
	keys     map[string]authz.APIKeyRow
	revision int64
	Clock    func() time.Time
}

func New(rows []authz.Relationship, agents []authz.AgentRow, keys []authz.APIKeyRow) *Store {
	s := &Store{rows: slices.Clone(rows), agents: map[string]authz.AgentRow{}, keys: map[string]authz.APIKeyRow{}, revision: 1, Clock: time.Now}
	for _, a := range agents {
		s.agents[a.ID] = a
	}
	for _, k := range keys {
		s.keys[k.ID] = k
	}
	return s
}

// Delete removes a relationship and bumps the revision, as the trigger would.
func (s *Store) Delete(id uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = slices.DeleteFunc(s.rows, func(r authz.Relationship) bool { return r.ID == id })
	s.revision++
}

func (s *Store) Revision(context.Context, string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revision, nil
}

func (s *Store) Begin(_ context.Context, org string) (authz.Tx, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &tx{rows: slices.Clone(s.rows), agents: s.agents, keys: s.keys, revision: s.revision, now: s.Clock()}, nil
}

type tx struct {
	rows     []authz.Relationship
	agents   map[string]authz.AgentRow
	keys     map[string]authz.APIKeyRow
	revision int64
	now      time.Time
}

func (t *tx) Revision() int64             { return t.revision }
func (t *tx) Now() time.Time              { return t.now }
func (t *tx) Close(context.Context) error { return nil }

func (t *tx) APIKey(_ context.Context, id string) (authz.APIKeyRow, error) {
	k, ok := t.keys[id]
	if !ok {
		return k, errors.New("not found")
	}
	return k, nil
}

func (t *tx) Agent(_ context.Context, id string) (authz.AgentRow, error) {
	a, ok := t.agents[id]
	if !ok {
		return a, errors.New("not found")
	}
	return a, nil
}

func (t *tx) live(r authz.Relationship) bool { return r.ExpiresAt == nil || t.now.Before(*r.ExpiresAt) }

// SubjectRelationships mirrors pgstore's recursive query: follow membership
// relations from the subject to a bounded depth, then return every live row
// whose subject is in the closure.
func (t *tx) SubjectRelationships(_ context.Context, s authz.Subject) ([]authz.Relationship, error) {
	closure := map[authz.Subject]bool{s: true}
	frontier := []authz.Subject{s}
	for depth := 0; depth < 3 && len(frontier) > 0; depth++ {
		var next []authz.Subject
		for _, r := range t.rows {
			if t.live(r) && model.IsMembership(r.Relation) && slices.Contains(frontier, r.Subject) {
				set := authz.Subject{Type: r.Object.Type, ID: r.Object.ID, Relation: r.Relation}
				if !closure[set] {
					closure[set] = true
					next = append(next, set)
				}
			}
		}
		frontier = next
	}
	var out []authz.Relationship
	for _, r := range t.rows {
		if t.live(r) && closure[r.Subject] {
			out = append(out, r)
		}
	}
	return out, nil
}

func (t *tx) OrgFacts(_ context.Context, markers []string, _ []string) (memberships, marked []authz.Relationship, err error) {
	for _, r := range t.rows {
		switch {
		case r.Object.Type == model.Group && r.Relation == "member" && r.Subject.Type == model.User && t.live(r):
			memberships = append(memberships, r)
		case slices.Contains(markers, r.Relation) && t.live(r):
			marked = append(marked, r)
		}
	}
	return memberships, marked, nil
}
