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
			SubjectURN:    fmt.Sprintf("user:%d", r.IntN(people)),
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
			if r.IntN(10) == 0 {
				e.ServerKind, e.ServerID = "identity_provider", ""
			}
		}
		eps = append(eps, e)
	}

	var served []mcpconnectionattempts.ServedByFamily
	for s := range servers {
		if r.IntN(2) == 0 {
			served = append(served, mcpconnectionattempts.ServedByFamily{ServerKind: "mcp", ServerID: serverUUID(s), MemberID: "", ClientFamily: families[r.IntN(len(families))], LastServedAt: internalNow.Add(-time.Duration(r.IntN(48*60)) * time.Minute)})
		}
		if r.IntN(4) == 0 {
			served = append(served, mcpconnectionattempts.ServedByFamily{ServerKind: "meta", ServerID: fmt.Sprintf("gw-%d", s%7), MemberID: "", ClientFamily: families[r.IntN(len(families))], LastServedAt: internalNow.Add(-time.Duration(r.IntN(48*60)) * time.Minute)})
		}
	}
	return eps, upstreams, served
}

// referenceGroup applies the rule by rebuilding every candidate after each pick.
func referenceGroup(organizationID string, episodes []mcpconnectionattempts.FailingPerson, upstreams map[string]Upstream, served []mcpconnectionattempts.ServedByFamily, now time.Time) []Problem {
	g := newGrouper(organizationID, episodes, upstreams, served)
	taken := make([]bool, len(episodes))
	grouped := map[personWay]bool{}
	type key struct {
		w way
		s Scope
	}

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

		found, bestOther := false, false
		var bestKey key
		var bestIdx []int
		var bestPeople int
		for k, idx := range groups {
			subjects, servers := map[string]bool{}, map[string]bool{}
			for _, i := range idx {
				subjects[episodes[i].SubjectURN] = true
				servers[g.servers[i]] = true
			}
			if len(subjects) < MinPeople {
				continue
			}
			other := false
			switch k.s.Kind {
			case ScopeService:
				if len(servers) < MinServersPerService {
					continue
				}
			case ScopeAppVersion:
				if other = g.otherAppsServed(idx); !other {
					continue
				}
			case ScopeServer, ScopePerson:
			}
			if !found || cmp.Or(rank(k.s, len(subjects), len(idx), bestKey.s, bestPeople, len(bestIdx)), cmp.Compare(k.w.part, bestKey.w.part), cmp.Compare(k.w.who, bestKey.w.who)) < 0 {
				found, bestKey, bestIdx, bestPeople, bestOther = true, k, idx, len(subjects), other
			}
		}
		if !found {
			break
		}
		for _, i := range bestIdx {
			taken[i] = true
			grouped[personWay{subject: episodes[i].SubjectURN, way: bestKey.w}] = true
		}
		if bestKey.s.Kind != ScopeAppVersion {
			bestOther = g.otherAppsServed(bestIdx)
		}
		out = append(out, g.problem(bestKey.s, bestKey.w, bestIdx, bestPeople, bestOther))
	}
	out = append(out, g.longFailures(taken, grouped, now)...)
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
