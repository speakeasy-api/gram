package xrbac

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"
)

// World is the domain data a Source reads, in today's shapes: grants by
// principal URN, role assignments, directory groups mapped to roles, agents,
// keys with stored delegated policy, and chat attribution.
type World struct {
	Grants              []StoredGrant
	RoleAssignments     map[string][]string // user id → role URNs
	DirectoryMembership map[string][]string // user id → group ids
	DirectoryRoleMaps   map[string][]string // group id → role URNs
	Agents              map[string]AgentFacts
	Credentials         map[string]CredentialFacts
	Chats               map[string]ChatFacts
}

type ChatFacts struct{ ID, ProjectID, UserID, AgentID string }

// MemSource serves a World with the same semantics as PgSource.
type MemSource struct {
	mu    sync.RWMutex
	W     World
	Clock func() time.Time
}

func (m *MemSource) RemoveMembership(userID, groupID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.W.DirectoryMembership[userID] = slices.DeleteFunc(slices.Clone(m.W.DirectoryMembership[userID]), func(g string) bool { return g == groupID })
}

func (m *MemSource) Begin(context.Context, string) (SourceTx, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now()
	if m.Clock != nil {
		now = m.Clock()
	}
	w := m.W
	w.DirectoryMembership = map[string][]string{}
	for k, v := range m.W.DirectoryMembership {
		w.DirectoryMembership[k] = slices.Clone(v)
	}
	return &memTx{w: w, now: now}, nil
}

type memTx struct {
	w   World
	now time.Time
}

func (t *memTx) Now() time.Time              { return t.now }
func (t *memTx) Close(context.Context) error { return nil }

func (t *memTx) roles(userID string) []string {
	roles := slices.Clone(t.w.RoleAssignments[userID])
	for _, g := range t.w.DirectoryMembership[userID] {
		roles = append(roles, t.w.DirectoryRoleMaps[g]...)
	}
	slices.Sort(roles)
	return slices.Compact(roles)
}

func (t *memTx) grantsFor(principals []string) []StoredGrant {
	var out []StoredGrant
	for _, g := range t.w.Grants {
		if slices.Contains(principals, g.Principal) {
			out = append(out, g)
		}
	}
	return out
}

func (t *memTx) UserPolicy(_ context.Context, userID string) ([]Rule, []string, error) {
	principals := append([]string{"user:" + userID, "user:all"}, t.roles(userID)...)
	var owned []string
	for _, a := range t.w.Agents {
		if a.OwnerUserID == userID && a.Status == "active" {
			owned = append(owned, a.ID)
		}
	}
	return DecodeAll(t.grantsFor(principals)), owned, nil
}

func (t *memTx) AgentPolicy(_ context.Context, agentID string) ([]Rule, error) {
	return DecodeAll(t.grantsFor([]string{"agent:" + agentID})), nil
}

func (t *memTx) Agent(_ context.Context, id string) (AgentFacts, error) {
	a, ok := t.w.Agents[id]
	if !ok {
		return a, errors.New("agent not found")
	}
	return a, nil
}

func (t *memTx) Credential(_ context.Context, id string) (CredentialFacts, error) {
	c, ok := t.w.Credentials[id]
	if !ok {
		return c, errors.New("credential not found")
	}
	return c, nil
}

func (t *memTx) SubjectPrincipals(_ context.Context, targets []Ref) (map[Ref][]string, error) {
	out := map[Ref][]string{}
	for _, r := range targets {
		c, ok := t.w.Chats[r.ID]
		if !ok {
			continue
		}
		switch {
		case c.UserID != "":
			out[r] = append([]string{"user:" + c.UserID}, t.roles(c.UserID)...)
		case c.AgentID != "":
			out[r] = []string{"agent:" + c.AgentID}
		}
	}
	return out, nil
}
