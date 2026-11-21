package problems_test

import (
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems"
)

var now = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

type episodeOption func(*mcpconnectionattempts.FailingPerson)

func app(family, version string) episodeOption {
	return func(e *mcpconnectionattempts.FailingPerson) { e.ClientFamily, e.ClientVersion = family, version }
}

func member(gateway, id string) episodeOption {
	return func(e *mcpconnectionattempts.FailingPerson) {
		e.ServerKind, e.ServerID, e.MemberID = mcpconnectionattempts.ServerKindMeta, gateway, id
	}
}

// episode is person failing on an MCP server, the run opened ago before now.
func episode(person, server string, part mcpconnectionattempts.Part, reason mcpconnectionattempts.Code, ago time.Duration, opts ...episodeOption) mcpconnectionattempts.FailingPerson {
	e := mcpconnectionattempts.FailingPerson{
		Episode: mcpconnectionattempts.Episode{
			SubjectURN:    "user:" + person,
			ServerKind:    mcpconnectionattempts.ServerKindMCP,
			ServerID:      server,
			ClientFamily:  mcpconnectionattempts.FamilyClaudeCode,
			Part:          part,
			OpeningReason: reason.String(),
			Reason:        reason.String(),
			OpenedAt:      now.Add(-ago),
			LastFailureAt: now.Add(-ago / 2),
		},
	}
	for _, o := range opts {
		o(&e)
	}
	return e
}

// down is a server not answering, which the provider fixes.
func down(person, server string, ago time.Duration, opts ...episodeOption) mcpconnectionattempts.FailingPerson {
	return episode(person, server, mcpconnectionattempts.PartServer, mcpconnectionattempts.CodeProviderServerError, ago, opts...)
}

// badKey is a service sign-in an administrator fixes.
func badKey(person, server string, ago time.Duration, opts ...episodeOption) mcpconnectionattempts.FailingPerson {
	return episode(person, server, mcpconnectionattempts.PartServiceSignIn, mcpconnectionattempts.CodeStaticKeyRejected, ago, opts...)
}

// signedOut is an app's sign-in to Speakeasy the person fixes.
func signedOut(person, server string, ago time.Duration, opts ...episodeOption) mcpconnectionattempts.FailingPerson {
	return episode(person, server, mcpconnectionattempts.PartAppSignIn, mcpconnectionattempts.CodeInvalidBearerToken, ago, opts...)
}

func served(server, family string, ago time.Duration) problems.ServedByFamily {
	return problems.ServedByFamily{ServerKind: mcpconnectionattempts.ServerKindMCP, ServerID: server, MemberID: "", ClientFamily: family, LastServedAt: now.Add(-ago)}
}

type summary struct {
	Rule            problems.Rule
	Scope           problems.Scope
	Part            mcpconnectionattempts.Part
	People          int
	Episodes        int
	SharedApp       problems.App
	OtherAppsServed bool
}

func summarize(ps []problems.Problem) []summary {
	out := []summary{}
	for _, p := range ps {
		out = append(out, summary{p.Rule, p.Scope, p.Part, p.People, len(p.Episodes), p.SharedApp, p.OtherAppsServed})
	}
	return out
}

func serverScope(id string) problems.Scope {
	return problems.Scope{Kind: problems.ScopeServer, Key: mcpconnectionattempts.ServerKindMCP + ":" + id}
}

func TestGroup(t *testing.T) {
	t.Parallel()

	cursor := app(mcpconnectionattempts.FamilyCursor, "2.0.1")
	cursorApp := problems.App{Family: mcpconnectionattempts.FamilyCursor, Version: "2.0.1"}
	linear := map[string]problems.Upstream{
		"s1": {URL: "https://API.Linear.app:8443/mcp", Issuer: ""},
		"s2": {URL: "https://api.linear.app/other", Issuer: ""},
	}
	sharedIssuer := map[string]problems.Upstream{
		"s1": {URL: "https://a.example.com/mcp", Issuer: "https://auth.example.com"},
		"s2": {URL: "https://b.example.com/mcp", Issuer: "https://auth.example.com/"},
	}

	cases := []struct {
		name      string
		episodes  []mcpconnectionattempts.FailingPerson
		upstreams map[string]problems.Upstream
		served    []problems.ServedByFamily
		want      []summary
	}{
		{
			name:     "three people on one server",
			episodes: []mcpconnectionattempts.FailingPerson{down("a", "s1", time.Hour), down("b", "s1", time.Hour), down("c", "s1", time.Hour)},
			want:     []summary{{problems.RuleThreePeopleSameServer, serverScope("s1"), mcpconnectionattempts.PartServer, 3, 3, problems.App{}, false}},
		},
		{
			name:     "two people on one server",
			episodes: []mcpconnectionattempts.FailingPerson{down("a", "s1", time.Hour), down("b", "s1", time.Hour)},
			want:     []summary{},
		},
		{
			name: "five people across two servers with one upstream",
			episodes: []mcpconnectionattempts.FailingPerson{
				down("a", "s1", time.Hour), down("b", "s1", time.Hour), down("c", "s1", time.Hour),
				down("d", "s2", time.Hour), down("e", "s2", time.Hour),
			},
			upstreams: linear,
			want:      []summary{{problems.RuleThreePeopleSameService, problems.Scope{Kind: problems.ScopeService, Key: "api.linear.app"}, mcpconnectionattempts.PartServer, 5, 5, problems.App{}, false}},
		},
		{
			name:      "an upstream on a single server",
			episodes:  []mcpconnectionattempts.FailingPerson{down("a", "s1", time.Hour), down("b", "s1", time.Hour), down("c", "s1", time.Hour)},
			upstreams: linear,
			want:      []summary{{problems.RuleThreePeopleSameServer, serverScope("s1"), mcpconnectionattempts.PartServer, 3, 3, problems.App{}, false}},
		},
		{
			name:      "a server with no known upstream is in no service",
			episodes:  []mcpconnectionattempts.FailingPerson{down("a", "s1", time.Hour), down("b", "s1", time.Hour), down("c", "s3", time.Hour)},
			upstreams: linear,
			want:      []summary{},
		},
		{
			name:      "a gateway member shares its upstream with a direct server",
			episodes:  []mcpconnectionattempts.FailingPerson{down("a", "s1", time.Hour), down("b", "s1", time.Hour), down("c", "gw", time.Hour, member("gw", "s2"))},
			upstreams: linear,
			want:      []summary{{problems.RuleThreePeopleSameService, problems.Scope{Kind: problems.ScopeService, Key: "api.linear.app"}, mcpconnectionattempts.PartServer, 3, 3, problems.App{}, false}},
		},
		{
			name:      "service sign-ins with one issuer are one service",
			episodes:  []mcpconnectionattempts.FailingPerson{badKey("a", "s1", time.Hour), badKey("b", "s1", time.Hour), badKey("c", "s2", time.Hour)},
			upstreams: sharedIssuer,
			want:      []summary{{problems.RuleThreePeopleSameService, problems.Scope{Kind: problems.ScopeService, Key: "auth.example.com"}, mcpconnectionattempts.PartServiceSignIn, 3, 3, problems.App{}, false}},
		},
		{
			name:      "server failures do not group by issuer",
			episodes:  []mcpconnectionattempts.FailingPerson{down("a", "s1", time.Hour), down("b", "s1", time.Hour), down("c", "s2", time.Hour)},
			upstreams: sharedIssuer,
			want:      []summary{},
		},
		{
			name:     "an app version while another app is served",
			episodes: []mcpconnectionattempts.FailingPerson{signedOut("a", "s1", time.Hour, cursor), signedOut("b", "s1", time.Hour, cursor), signedOut("c", "s1", time.Hour, cursor)},
			served:   []problems.ServedByFamily{served("s1", mcpconnectionattempts.FamilyClaudeCode, 10*time.Minute)},
			want:     []summary{{problems.RuleThreePeopleSameAppVersion, problems.Scope{Kind: problems.ScopeAppVersion, Key: "cursor@2.0.1"}, mcpconnectionattempts.PartAppSignIn, 3, 3, cursorApp, true}},
		},
		{
			name:     "an app version with nothing else served is a server problem",
			episodes: []mcpconnectionattempts.FailingPerson{signedOut("a", "s1", time.Hour, cursor), signedOut("b", "s1", time.Hour, cursor), signedOut("c", "s1", time.Hour, cursor)},
			served:   []problems.ServedByFamily{served("s1", mcpconnectionattempts.FamilyCursor, 10*time.Minute), served("s1", mcpconnectionattempts.FamilyClaudeCode, 2*time.Hour)},
			want:     []summary{{problems.RuleThreePeopleSameServer, serverScope("s1"), mcpconnectionattempts.PartAppSignIn, 3, 3, cursorApp, false}},
		},
		{
			name:     "only unknown or generic families served is a server problem",
			episodes: []mcpconnectionattempts.FailingPerson{signedOut("a", "s1", time.Hour, cursor), signedOut("b", "s1", time.Hour, cursor), signedOut("c", "s1", time.Hour, cursor)},
			served:   []problems.ServedByFamily{served("s1", "", 10*time.Minute), served("s1", mcpconnectionattempts.FamilyScript, 10*time.Minute), served("s1", mcpconnectionattempts.FamilyBrowser, 10*time.Minute)},
			want:     []summary{{problems.RuleThreePeopleSameServer, serverScope("s1"), mcpconnectionattempts.PartAppSignIn, 3, 3, cursorApp, false}},
		},
		{
			name:     "an app version across three servers",
			episodes: []mcpconnectionattempts.FailingPerson{signedOut("a", "s1", time.Hour, cursor), signedOut("b", "s2", time.Hour, cursor), signedOut("c", "s3", time.Hour, cursor)},
			served:   []problems.ServedByFamily{served("s2", mcpconnectionattempts.FamilyClaudeCode, 10*time.Minute)},
			want:     []summary{{problems.RuleThreePeopleSameAppVersion, problems.Scope{Kind: problems.ScopeAppVersion, Key: "cursor@2.0.1"}, mcpconnectionattempts.PartAppSignIn, 3, 3, cursorApp, true}},
		},
		{
			name: "two ways on one server stay two problems",
			episodes: []mcpconnectionattempts.FailingPerson{
				down("a", "s1", time.Hour), down("b", "s1", time.Hour), down("c", "s1", time.Hour),
				badKey("d", "s1", 2*time.Hour), badKey("e", "s1", 2*time.Hour), badKey("f", "s1", 2*time.Hour),
			},
			want: []summary{
				{problems.RuleThreePeopleSameServer, serverScope("s1"), mcpconnectionattempts.PartServiceSignIn, 3, 3, problems.App{}, false},
				{problems.RuleThreePeopleSameServer, serverScope("s1"), mcpconnectionattempts.PartServer, 3, 3, problems.App{}, false},
			},
		},
		{
			name:     "two and two people failing different ways",
			episodes: []mcpconnectionattempts.FailingPerson{down("a", "s1", time.Hour), down("b", "s1", time.Hour), badKey("c", "s1", time.Hour), badKey("d", "s1", time.Hour)},
			want:     []summary{},
		},
		{
			name:     "one person failing for 24 hours",
			episodes: []mcpconnectionattempts.FailingPerson{badKey("a", "s1", 24*time.Hour)},
			want:     []summary{{problems.RuleOnePerson24Hours, problems.Scope{Kind: problems.ScopePerson, Key: "user:a"}, mcpconnectionattempts.PartServiceSignIn, 1, 1, problems.App{}, false}},
		},
		{
			name:     "one person failing for 23 hours",
			episodes: []mcpconnectionattempts.FailingPerson{badKey("a", "s1", 23*time.Hour)},
			want:     []summary{},
		},
		{
			name:     "a person in a group problem is not also a person problem",
			episodes: []mcpconnectionattempts.FailingPerson{down("a", "s1", 30*time.Hour), down("b", "s1", time.Hour), down("c", "s1", time.Hour)},
			want:     []summary{{problems.RuleThreePeopleSameServer, serverScope("s1"), mcpconnectionattempts.PartServer, 3, 3, problems.App{}, false}},
		},
		{
			name:     "one person failing the same way on two servers",
			episodes: []mcpconnectionattempts.FailingPerson{badKey("a", "s1", 25*time.Hour), badKey("a", "s2", 30*time.Hour)},
			want:     []summary{{problems.RuleOnePerson24Hours, problems.Scope{Kind: problems.ScopePerson, Key: "user:a"}, mcpconnectionattempts.PartServiceSignIn, 1, 2, problems.App{}, false}},
		},
		{
			name:      "the same person on two servers counts once",
			episodes:  []mcpconnectionattempts.FailingPerson{down("a", "s1", time.Hour), down("a", "s2", time.Hour), down("b", "s2", time.Hour)},
			upstreams: linear,
			want:      []summary{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, summarize(problems.Group("org-a", tc.episodes, tc.upstreams, tc.served, now)))
		})
	}
}

func TestGroup_ProblemTimes(t *testing.T) {
	t.Parallel()
	got := problems.Group("org-a", []mcpconnectionattempts.FailingPerson{down("a", "s1", time.Hour), down("b", "s1", 3*time.Hour), down("c", "s1", 2*time.Hour)}, nil, nil, now)
	require.Len(t, got, 1)
	require.Equal(t, now.Add(-3*time.Hour), got[0].Since)
	require.Equal(t, now.Add(-30*time.Minute), got[0].LastFailedAt)
	require.Equal(t, mcpconnectionattempts.WhoProvider, got[0].WhoFixes)
	require.Equal(t, "org-a", got[0].OrganizationID)
}

func TestGroup_IDsAreStablePerOrganization(t *testing.T) {
	t.Parallel()
	eps := []mcpconnectionattempts.FailingPerson{down("a", "s1", time.Hour), down("b", "s1", time.Hour), down("c", "s1", time.Hour), badKey("d", "s2", 25*time.Hour)}

	first := problems.Group("org-a", eps, nil, nil, now)
	again := problems.Group("org-a", append(eps, down("e", "s1", time.Minute)), nil, nil, now.Add(time.Hour))
	other := problems.Group("org-b", eps, nil, nil, now)
	require.Len(t, first, 2)
	require.Len(t, again, 2)
	require.Len(t, other, 2)

	ids := func(ps []problems.Problem) []string {
		out := []string{}
		for _, p := range ps {
			out = append(out, p.ID)
		}
		slices.Sort(out)
		return out
	}
	require.Equal(t, ids(first), ids(again), "a problem keeps its id as people join it")
	require.NotEqual(t, first[0].ID, first[1].ID)
	for _, id := range ids(other) {
		require.NotContains(t, ids(first), id)
	}
}

func TestGroup_OrderIsDeterministic(t *testing.T) {
	t.Parallel()
	eps := []mcpconnectionattempts.FailingPerson{
		down("a", "s1", time.Hour), down("b", "s1", time.Hour), down("c", "s1", time.Hour), down("d", "s1", time.Hour),
		badKey("e", "s2", 2*time.Hour), badKey("f", "s2", 2*time.Hour), badKey("g", "s2", 2*time.Hour),
		badKey("h", "s3", 3*time.Hour), badKey("i", "s3", 3*time.Hour), badKey("j", "s3", 3*time.Hour),
		signedOut("k", "s4", 26*time.Hour), signedOut("l", "s4", 25*time.Hour), signedOut("l", "s5", 27*time.Hour),
	}
	want := problems.Group("org-a", eps, nil, nil, now)
	require.Len(t, want, 5)
	require.Equal(t, 4, want[0].People)
	require.Equal(t, serverScope("s3"), want[1].Scope, "equal people, earliest first")

	r := rand.New(rand.NewPCG(1, 2))
	for range 20 {
		shuffled := slices.Clone(eps)
		r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		require.Equal(t, want, problems.Group("org-a", shuffled, nil, nil, now))
	}
}
