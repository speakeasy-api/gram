package problems

import (
	"cmp"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
)

var internalNow = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

func serverUUID(n int) string { return fmt.Sprintf("00000000-0000-0000-0000-%012d", n) }

// randomInput builds n episodes over a mix of servers, gateways, upstreams, apps and ways.
func randomInput(r *rand.Rand, n, servers, people int) ([]mcpconnectionattempts.FailingPerson, map[string]Upstream, []mcpconnectionattempts.ServedByFamily) {
	reasons := []mcpconnectionattempts.Code{mcpconnectionattempts.CodeProviderServerError, mcpconnectionattempts.CodeStaticKeyRejected, mcpconnectionattempts.CodeInvalidBearerToken}
	parts := []mcpconnectionattempts.Part{mcpconnectionattempts.PartServer, mcpconnectionattempts.PartServiceSignIn, mcpconnectionattempts.PartAppSignIn}
	families := []string{mcpconnectionattempts.FamilyCursor, mcpconnectionattempts.FamilyClaudeCode, mcpconnectionattempts.FamilyCodexCLI, mcpconnectionattempts.FamilyBrowser, ""}
	versions := []string{"", "1.0", "2.0"}
	subjectKinds := []string{"user", "user", "user", "user", "apikey"}
	hosts := servers/5 + 1

	upstreams := map[string]Upstream{}
	for s := range servers {
		if s%3 == 0 {
			continue
		}
		upstreams[serverUUID(s)] = Upstream{URL: fmt.Sprintf("https://h%d.example.com/mcp", s%hosts), Issuer: fmt.Sprintf("https://idp%d.example.com/t%d", s%(hosts/2+1), s%2)}
	}

	eps := make([]mcpconnectionattempts.FailingPerson, 0, n)
	for range n {
		s := r.IntN(servers)
		w := r.IntN(len(reasons))
		e := mcpconnectionattempts.FailingPerson{Episode: mcpconnectionattempts.Episode{
			SubjectURN:    subjectKinds[r.IntN(len(subjectKinds))] + fmt.Sprintf(":%d", r.IntN(people)),
			ServerKind:    "mcp",
			ServerID:      serverUUID(s),
			ClientFamily:  families[r.IntN(len(families))],
			ClientVersion: versions[r.IntN(len(versions))],
			Part:          parts[w],
			Reason:        reasons[w].String(),
			OpenedAt:      internalNow.Add(-time.Duration(r.IntN(48*60)) * time.Minute),
		}}
		e.LastFailureAt = e.OpenedAt.Add(time.Duration(r.IntN(60)) * time.Minute)
		switch r.IntN(6) {
		case 0:
			e.ServerKind, e.ServerID, e.MemberID = "meta", fmt.Sprintf("gw-%d", s%7), serverUUID(s)
		case 1:
			e.ServerKind, e.ServerID = "meta", fmt.Sprintf("gw-%d", s%7)
		case 2:
			switch r.IntN(10) {
			case 0:
				e.ServerKind, e.ServerID = "identity_provider", ""
			case 1:
				e.ServerKind, e.ServerID = "authorization_server", "https://auth.example.com"
			}
		}
		eps = append(eps, e)
	}

	var served []mcpconnectionattempts.ServedByFamily
	for s := range servers {
		if r.IntN(2) == 0 {
			at := internalNow.Add(-time.Duration(r.IntN(48*60)) * time.Minute)
			answered := time.Time{}
			if r.IntN(2) == 0 {
				answered = at.Add(-time.Duration(r.IntN(60)) * time.Minute)
			}
			served = append(served, mcpconnectionattempts.ServedByFamily{ServerKind: "mcp", ServerID: serverUUID(s), MemberID: "", ClientFamily: families[r.IntN(len(families))], LastServedAt: at, LastAnsweredAt: answered})
		}
		if r.IntN(4) == 0 {
			at := internalNow.Add(-time.Duration(r.IntN(48*60)) * time.Minute)
			memberID := ""
			if r.IntN(2) == 0 {
				memberID = serverUUID(s)
			}
			served = append(served, mcpconnectionattempts.ServedByFamily{ServerKind: "meta", ServerID: fmt.Sprintf("gw-%d", s%7), MemberID: memberID, ClientFamily: families[r.IntN(len(families))], LastServedAt: at, LastAnsweredAt: at})
		}
	}
	return eps, upstreams, served
}

// referenceGroup applies the rule by rebuilding every candidate after each pick.
func referenceGroup(organizationID string, episodes []mcpconnectionattempts.FailingPerson, upstreams map[string]Upstream, served []mcpconnectionattempts.ServedByFamily, now time.Time) []Problem {
	g := newGrouper(organizationID, episodes, upstreams, served)
	taken := make([]bool, len(episodes))
	type key struct {
		w way
		s Scope
	}

	dropped := map[key]bool{}
	var out []Problem
	for {
		groups := map[key][]int{}
		for i := range episodes {
			if taken[i] {
				continue
			}
			for _, s := range g.scopes[i] {
				groups[key{g.ways[i], s}] = append(groups[key{g.ways[i], s}], i)
			}
		}

		found := false
		var bestKey key
		var bestIdx []int
		var bestPeople int
		for k, idx := range groups {
			if dropped[k] {
				continue
			}
			if k.s.Kind == ScopeAppVersion {
				idx = g.withEvidence(k.w.part, idx)
			}
			subjects, servers := map[string]bool{}, map[string]bool{}
			for _, i := range idx {
				subjects[episodes[i].SubjectURN] = true
				if episodes[i].OnMCPServer() {
					servers[g.servers[i]] = true
				}
			}
			if len(subjects) < minPeople {
				continue
			}
			switch k.s.Kind {
			case ScopeService:
				if len(servers) < minServersPerService {
					continue
				}
			case ScopeAppVersion, ScopeServer, ScopePerson:
			}
			if !found || cmp.Or(rank(k.s, len(subjects), bestKey.s, bestPeople), cmp.Compare(k.w.part, bestKey.w.part), cmp.Compare(k.w.who, bestKey.w.who)) < 0 {
				found, bestKey, bestIdx, bestPeople = true, k, idx, len(subjects)
			}
		}
		if !found {
			break
		}
		for _, i := range bestIdx {
			taken[i] = true
			for _, s := range g.scopes[i] {
				if bestKey.s.Kind == ScopeServer && s.Kind == ScopeService {
					dropped[key{bestKey.w, s}] = true
				}
			}
		}
		out = append(out, g.problem(bestKey.s, bestKey.w, bestIdx, bestPeople, bestKey.s.Kind == ScopeAppVersion || g.otherAppsWorked(bestKey.w.part, bestIdx)))
	}
	out = append(out, g.longFailures(taken, now)...)
	slices.SortFunc(out, func(a, b Problem) int {
		return cmp.Or(cmp.Compare(b.People, a.People), a.Since.Compare(b.Since), cmp.Compare(a.ID, b.ID))
	})
	return out
}

func TestGroup_MatchesTheReference(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(3, 5))
	grouped := 0
	for n := range 300 {
		eps, upstreams, served := randomInput(r, 20+n%80, 3+n%9, 4+n%15)
		want := referenceGroup("org", eps, upstreams, served, internalNow)
		require.Equal(t, want, Group("org", eps, upstreams, served, internalNow), "input %d", n)
		grouped += len(want)
	}
	require.Greater(t, grouped, 300, "the inputs make problems")
}

func BenchmarkGroup30k(b *testing.B) {
	eps, upstreams, served := randomInput(rand.New(rand.NewPCG(7, 7)), 30_000, 3_000, 8_000)
	for b.Loop() {
		Group("org", eps, upstreams, served, internalNow)
	}
}

func BenchmarkReferenceGroup30k(b *testing.B) {
	eps, upstreams, served := randomInput(rand.New(rand.NewPCG(7, 7)), 30_000, 3_000, 8_000)
	for b.Loop() {
		referenceGroup("org", eps, upstreams, served, internalNow)
	}
}

// BenchmarkGroup30kIndependentServers is 10k three-person servers on one app version, nothing else served.
func BenchmarkGroup30kIndependentServers(b *testing.B) {
	eps := make([]mcpconnectionattempts.FailingPerson, 0, 30_000)
	for s := range 10_000 {
		for p := range 3 {
			eps = append(eps, mcpconnectionattempts.FailingPerson{Episode: mcpconnectionattempts.Episode{
				SubjectURN: fmt.Sprintf("user:%d-%d", s, p), ServerKind: "mcp", ServerID: serverUUID(s),
				ClientFamily: mcpconnectionattempts.FamilyCursor, ClientVersion: "2.0.1",
				Part: mcpconnectionattempts.PartServer, Reason: mcpconnectionattempts.CodeProviderServerError.String(),
				OpenedAt: internalNow.Add(-time.Hour), LastFailureAt: internalNow.Add(-time.Minute),
			}})
		}
	}
	for b.Loop() {
		Group("org", eps, nil, nil, internalNow)
	}
}
