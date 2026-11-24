package problems_test

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems"
)

var now = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

const (
	s1 = "00000000-0000-0000-0000-000000000001"
	s2 = "00000000-0000-0000-0000-000000000002"
	s3 = "00000000-0000-0000-0000-000000000003"
	s4 = "00000000-0000-0000-0000-000000000004"
	s5 = "00000000-0000-0000-0000-000000000005"
	sa = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	sb = "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"
)

type episodeOption func(*mcpconnectionattempts.FailingPerson)

func app(family, version string) episodeOption {
	return func(e *mcpconnectionattempts.FailingPerson) { e.ClientFamily, e.ClientVersion = family, version }
}

// member puts the episode on a gateway, through member when it is not empty.
func member(gateway, id string) episodeOption {
	return func(e *mcpconnectionattempts.FailingPerson) {
		e.ServerKind, e.ServerID, e.MemberID = "meta", gateway, id
	}
}

func noServer(e *mcpconnectionattempts.FailingPerson) {
	e.ServerKind, e.ServerID = "identity_provider", ""
}

// episode is person failing on an MCP server, the run opened ago before now.
func episode(person, server string, part mcpconnectionattempts.Part, reason mcpconnectionattempts.Code, ago time.Duration, opts ...episodeOption) mcpconnectionattempts.FailingPerson {
	e := mcpconnectionattempts.FailingPerson{
		Episode: mcpconnectionattempts.Episode{
			SubjectURN:    "user:" + person,
			ServerKind:    "mcp",
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

// tunnelDown is a server not answering that an administrator fixes.
func tunnelDown(person, server string, ago time.Duration, opts ...episodeOption) mcpconnectionattempts.FailingPerson {
	return episode(person, server, mcpconnectionattempts.PartServer, mcpconnectionattempts.CodeTunnelUnavailable, ago, opts...)
}

// badKey is a service sign-in an administrator fixes.
func badKey(person, server string, ago time.Duration, opts ...episodeOption) mcpconnectionattempts.FailingPerson {
	return episode(person, server, mcpconnectionattempts.PartServiceSignIn, mcpconnectionattempts.CodeStaticKeyRejected, ago, opts...)
}

// serviceDown is a service sign-in the provider fixes.
func serviceDown(person, server string, ago time.Duration, opts ...episodeOption) mcpconnectionattempts.FailingPerson {
	return episode(person, server, mcpconnectionattempts.PartServiceSignIn, mcpconnectionattempts.CodeProviderServerError, ago, opts...)
}

// signedOut is an app's sign-in to Speakeasy the person fixes.
func signedOut(person, server string, ago time.Duration, opts ...episodeOption) mcpconnectionattempts.FailingPerson {
	return episode(person, server, mcpconnectionattempts.PartAppSignIn, mcpconnectionattempts.CodeInvalidBearerToken, ago, opts...)
}

// served is a request of family the server answered ago before now.
func served(kind, server, memberID, family string, ago time.Duration) mcpconnectionattempts.ServedByFamily {
	return mcpconnectionattempts.ServedByFamily{ServerKind: kind, ServerID: server, MemberID: memberID, ClientFamily: family, LastServedAt: now.Add(-ago), LastAnsweredAt: now.Add(-ago)}
}

// servedNotAnswered is a request of family that Speakeasy served without the server answering.
func servedNotAnswered(kind, server, family string, ago time.Duration) mcpconnectionattempts.ServedByFamily {
	return mcpconnectionattempts.ServedByFamily{ServerKind: kind, ServerID: server, MemberID: "", ClientFamily: family, LastServedAt: now.Add(-ago), LastAnsweredAt: time.Time{}}
}

func apiKey(e *mcpconnectionattempts.FailingPerson) {
	e.SubjectURN = "apikey:" + e.SubjectURN[len("user:"):]
}

func authorizationServer(e *mcpconnectionattempts.FailingPerson) {
	e.ServerKind, e.ServerID = "authorization_server", "https://auth.example.com"
}

type summary struct {
	Rule            problems.Rule
	Scope           problems.Scope
	Part            mcpconnectionattempts.Part
	WhoFixes        mcpconnectionattempts.WhoFixes
	People          int
	Episodes        int
	SharedApp       problems.App
	OtherAppsWorked bool
}

func summarize(ps []problems.Problem) []summary {
	out := []summary{}
	for _, p := range ps {
		out = append(out, summary{p.Rule, p.Scope, p.Part, p.WhoFixes, p.People, len(p.Episodes), p.SharedApp, p.OtherAppsWorked})
	}
	return out
}

func serverScope(key string) problems.Scope {
	return problems.Scope{Kind: problems.ScopeServer, Key: key}
}
func serviceScope(key string) problems.Scope {
	return problems.Scope{Kind: problems.ScopeService, Key: key}
}
func personScope(p string) problems.Scope {
	return problems.Scope{Kind: problems.ScopePerson, Key: "user:" + p}
}

var (
	noApp     = problems.App{}
	cursor    = app(mcpconnectionattempts.FamilyCursor, "2.0.1")
	cursorApp = problems.App{Family: mcpconnectionattempts.FamilyCursor, Version: "2.0.1"}
	cursorKey = problems.Scope{Kind: problems.ScopeAppVersion, Key: "cursor@2.0.1"}
)

func TestGroup(t *testing.T) {
	t.Parallel()

	apiExample := map[string]problems.Upstream{
		s1: {URL: "https://API.Example.com:8443/mcp", Issuer: ""},
		s2: {URL: "https://api.example.com/other", Issuer: ""},
		s3: {URL: "https://api.example.com/third", Issuer: ""},
	}
	sharedIssuer := map[string]problems.Upstream{
		s1: {URL: "https://a.example.com/mcp", Issuer: "https://auth.example.com"},
		s2: {URL: "https://b.example.com/mcp", Issuer: "https://auth.example.com/"},
	}
	tenantIssuers := map[string]problems.Upstream{
		s1: {URL: "https://a.example.com/mcp", Issuer: "https://idp.example.com/tenants/one"},
		s2: {URL: "https://b.example.com/mcp", Issuer: "https://idp.example.com/tenants/two"},
	}
	part := mcpconnectionattempts.PartServer
	signIn := mcpconnectionattempts.PartServiceSignIn
	appSignIn := mcpconnectionattempts.PartAppSignIn
	claude := mcpconnectionattempts.FamilyClaudeCode
	provider, admin, person := mcpconnectionattempts.WhoProvider, mcpconnectionattempts.WhoAdmin, mcpconnectionattempts.WhoPerson

	cases := []struct {
		name      string
		episodes  []mcpconnectionattempts.FailingPerson
		upstreams map[string]problems.Upstream
		served    []mcpconnectionattempts.ServedByFamily
		want      []summary
	}{
		{
			name:     "two people on one server",
			episodes: []mcpconnectionattempts.FailingPerson{down("a", s1, time.Hour), down("b", s1, time.Hour)},
			want:     []summary{{problems.RuleSameServer, serverScope(s1), part, provider, 2, 2, noApp, false}},
		},
		{
			name:     "one person on one server",
			episodes: []mcpconnectionattempts.FailingPerson{down("a", s1, time.Hour)},
			want:     []summary{},
		},
		{
			name: "five people across two servers with one upstream",
			episodes: []mcpconnectionattempts.FailingPerson{
				down("a", s1, time.Hour), down("b", s1, time.Hour), down("c", s1, time.Hour),
				down("d", s2, time.Hour), down("e", s2, time.Hour),
			},
			upstreams: apiExample,
			want:      []summary{{problems.RuleSameService, serviceScope("api.example.com"), part, provider, 5, 5, noApp, false}},
		},
		{
			name:      "an upstream on a single server",
			episodes:  []mcpconnectionattempts.FailingPerson{down("a", s1, time.Hour), down("b", s1, time.Hour)},
			upstreams: apiExample,
			want:      []summary{{problems.RuleSameServer, serverScope(s1), part, provider, 2, 2, noApp, false}},
		},
		{
			name:      "a server with no known upstream is in no service",
			episodes:  []mcpconnectionattempts.FailingPerson{down("a", s1, time.Hour), down("c", s4, time.Hour)},
			upstreams: apiExample,
			want:      []summary{},
		},
		{
			name:      "a gateway member shares its upstream with another direct server",
			episodes:  []mcpconnectionattempts.FailingPerson{down("a", s1, time.Hour), down("c", "gw", time.Hour, member("gw", s2))},
			upstreams: apiExample,
			want:      []summary{{problems.RuleSameService, serviceScope("api.example.com"), part, provider, 2, 2, noApp, false}},
		},
		{
			name:      "a server reached directly and through a gateway is one server",
			episodes:  []mcpconnectionattempts.FailingPerson{down("a", s1, time.Hour), down("c", "gw", time.Hour, member("gw", s1))},
			upstreams: apiExample,
			want:      []summary{{problems.RuleSameServer, serverScope(s1), part, provider, 2, 2, noApp, false}},
		},
		{
			name: "people directly and through a gateway with no upstream add up on one server",
			episodes: []mcpconnectionattempts.FailingPerson{
				down("a", s1, time.Hour), down("b", s1, time.Hour),
				down("d", "gw", time.Hour, member("gw", s1)), down("e", "gw", time.Hour, member("gw", s1)),
			},
			want: []summary{{problems.RuleSameServer, serverScope(s1), part, provider, 4, 4, noApp, false}},
		},
		{
			name:      "upper-case ids match their upstream",
			episodes:  []mcpconnectionattempts.FailingPerson{down("a", strings.ToUpper(sa), time.Hour), down("c", strings.ToUpper(sb), time.Hour)},
			upstreams: map[string]problems.Upstream{sa: apiExample[s1], sb: apiExample[s2]},
			want:      []summary{{problems.RuleSameService, serviceScope("api.example.com"), part, provider, 2, 2, noApp, false}},
		},
		{
			name:      "service sign-ins with one issuer are one service",
			episodes:  []mcpconnectionattempts.FailingPerson{serviceDown("a", s1, time.Hour), serviceDown("c", s2, time.Hour)},
			upstreams: sharedIssuer,
			want:      []summary{{problems.RuleSameService, serviceScope("https://auth.example.com"), signIn, provider, 2, 2, noApp, false}},
		},
		{
			name:      "failures an administrator fixes are not a service",
			episodes:  []mcpconnectionattempts.FailingPerson{badKey("a", s1, time.Hour), badKey("c", s2, time.Hour)},
			upstreams: sharedIssuer,
			want:      []summary{},
		},
		{
			name:      "app sign-ins are not a service",
			episodes:  []mcpconnectionattempts.FailingPerson{signedOut("a", s1, time.Hour), signedOut("c", s2, time.Hour)},
			upstreams: apiExample,
			want:      []summary{},
		},
		{
			name:      "issuers on one host with different paths are different services",
			episodes:  []mcpconnectionattempts.FailingPerson{serviceDown("a", s1, time.Hour), serviceDown("c", s2, time.Hour)},
			upstreams: tenantIssuers,
			want:      []summary{},
		},
		{
			name:      "issuers on one host with different ports are different services",
			episodes:  []mcpconnectionattempts.FailingPerson{serviceDown("a", s1, time.Hour), serviceDown("c", s2, time.Hour)},
			upstreams: map[string]problems.Upstream{s1: {URL: "https://a.example.com/mcp", Issuer: "https://idp.example.com:8443/a/b"}, s2: {URL: "https://b.example.com/mcp", Issuer: "https://idp.example.com/a/b"}},
			want:      []summary{},
		},
		{
			name:      "an escaped path is not the decoded path",
			episodes:  []mcpconnectionattempts.FailingPerson{serviceDown("a", s1, time.Hour), serviceDown("c", s2, time.Hour)},
			upstreams: map[string]problems.Upstream{s1: {URL: "https://a.example.com/mcp", Issuer: "https://idp.example.com/a%2Fb"}, s2: {URL: "https://b.example.com/mcp", Issuer: "https://idp.example.com/a/b"}},
			want:      []summary{},
		},
		{
			name:      "a malformed issuer falls back to the URL host",
			episodes:  []mcpconnectionattempts.FailingPerson{serviceDown("a", s1, time.Hour), serviceDown("c", s2, time.Hour)},
			upstreams: map[string]problems.Upstream{s1: {URL: "https://mcp.example.com/a", Issuer: "//idp.example.com/a"}, s2: {URL: "https://MCP.example.com/b", Issuer: "//idp.example.com/a"}},
			want:      []summary{{problems.RuleSameService, serviceScope("mcp.example.com"), signIn, provider, 2, 2, noApp, false}},
		},
		{
			name:      "server failures do not group by issuer",
			episodes:  []mcpconnectionattempts.FailingPerson{down("a", s1, time.Hour), down("c", s2, time.Hour)},
			upstreams: sharedIssuer,
			want:      []summary{},
		},
		{
			name:     "an app version while another app is served",
			episodes: []mcpconnectionattempts.FailingPerson{signedOut("a", s1, time.Hour, cursor), signedOut("b", s1, time.Hour, cursor)},
			served:   []mcpconnectionattempts.ServedByFamily{served("mcp", s1, "", claude, 10*time.Minute)},
			want:     []summary{{problems.RuleSameAppVersion, cursorKey, appSignIn, person, 2, 2, cursorApp, true}},
		},
		{
			name:     "another app served but not answered proves nothing about a server failure",
			episodes: []mcpconnectionattempts.FailingPerson{down("a", s1, time.Hour, cursor), down("b", s1, time.Hour, cursor)},
			served:   []mcpconnectionattempts.ServedByFamily{servedNotAnswered("mcp", s1, claude, 10*time.Minute)},
			want:     []summary{{problems.RuleSameServer, serverScope(s1), part, provider, 2, 2, cursorApp, false}},
		},
		{
			name: "another app working on a server before its first failure there does not count",
			episodes: []mcpconnectionattempts.FailingPerson{
				signedOut("a", s1, 3*time.Hour, cursor), signedOut("b", s2, time.Hour, cursor),
			},
			served: []mcpconnectionattempts.ServedByFamily{served("mcp", s2, "", claude, 2*time.Hour)},
			want:   []summary{},
		},
		{
			name: "an app version loses its only evidence when its earliest member is picked elsewhere",
			episodes: []mcpconnectionattempts.FailingPerson{
				signedOut("x", s1, 3*time.Hour, cursor), signedOut("p", s1, time.Hour), signedOut("q", s1, time.Hour), signedOut("r", s1, time.Hour),
				signedOut("d", s2, time.Hour, cursor), signedOut("e", s2, time.Hour, cursor),
			},
			served: []mcpconnectionattempts.ServedByFamily{served("mcp", s1, "", mcpconnectionattempts.FamilyCodexCLI, 2*time.Hour), served("mcp", s2, "", mcpconnectionattempts.FamilyCodexCLI, 2*time.Hour)},
			want: []summary{
				{problems.RuleSameServer, serverScope(s1), appSignIn, person, 4, 4, noApp, true},
				{problems.RuleSameServer, serverScope(s2), appSignIn, person, 2, 2, cursorApp, false},
			},
		},
		{
			name:     "an app version with evidence on a server takes people who reached no server",
			episodes: []mcpconnectionattempts.FailingPerson{signedOut("a", s1, time.Hour, cursor), signedOut("c", "", time.Hour, cursor, noServer)},
			served:   []mcpconnectionattempts.ServedByFamily{served("mcp", s1, "", claude, 10*time.Minute)},
			want:     []summary{{problems.RuleSameAppVersion, cursorKey, appSignIn, person, 2, 2, cursorApp, true}},
		},
		{
			name: "five people at the authorization server join one person on a server with evidence",
			episodes: []mcpconnectionattempts.FailingPerson{
				signedOut("a", "", time.Hour, cursor, authorizationServer), signedOut("b", "", time.Hour, cursor, authorizationServer),
				signedOut("c", "", time.Hour, cursor, authorizationServer), signedOut("d", "", time.Hour, cursor, authorizationServer),
				signedOut("e", "", time.Hour, cursor, authorizationServer), signedOut("f", s1, time.Hour, cursor),
			},
			served: []mcpconnectionattempts.ServedByFamily{served("mcp", s1, "", claude, 10*time.Minute)},
			want:   []summary{{problems.RuleSameAppVersion, cursorKey, appSignIn, person, 6, 6, cursorApp, true}},
		},
		{
			name: "people at the authorization server with no evidence on any server are not an app version",
			episodes: []mcpconnectionattempts.FailingPerson{
				signedOut("a", "", time.Hour, cursor, authorizationServer), signedOut("b", "", time.Hour, cursor, authorizationServer),
				signedOut("c", "", time.Hour, cursor, authorizationServer), signedOut("d", "", time.Hour, cursor, authorizationServer),
				signedOut("e", "", time.Hour, cursor, authorizationServer),
			},
			served: []mcpconnectionattempts.ServedByFamily{served("mcp", s1, "", claude, 10*time.Minute)},
			want:   []summary{},
		},
		{
			name:     "an authorization server is not a server",
			episodes: []mcpconnectionattempts.FailingPerson{down("a", s1, time.Hour, authorizationServer), down("b", s1, time.Hour, authorizationServer)},
			want:     []summary{},
		},
		{
			name:     "API keys are not people",
			episodes: []mcpconnectionattempts.FailingPerson{down("a", s1, time.Hour, apiKey), down("b", s1, time.Hour, apiKey), down("c", s1, 30*time.Hour, apiKey)},
			want:     []summary{},
		},
		{
			name:     "one person and an API key are not two people",
			episodes: []mcpconnectionattempts.FailingPerson{down("a", s1, time.Hour), down("c", s1, time.Hour, apiKey)},
			want:     []summary{},
		},
		{
			name:     "an app version with nothing else served is a server problem",
			episodes: []mcpconnectionattempts.FailingPerson{signedOut("a", s1, time.Hour, cursor), signedOut("b", s1, time.Hour, cursor)},
			served:   []mcpconnectionattempts.ServedByFamily{served("mcp", s1, "", mcpconnectionattempts.FamilyCursor, 10*time.Minute), served("mcp", s1, "", claude, 2*time.Hour)},
			want:     []summary{{problems.RuleSameServer, serverScope(s1), appSignIn, person, 2, 2, cursorApp, false}},
		},
		{
			name:     "only unknown or generic families served is a server problem",
			episodes: []mcpconnectionattempts.FailingPerson{signedOut("a", s1, time.Hour, cursor), signedOut("b", s1, time.Hour, cursor)},
			served: []mcpconnectionattempts.ServedByFamily{
				served("mcp", s1, "", "", 10*time.Minute), served("mcp", s1, "", mcpconnectionattempts.FamilyScript, 10*time.Minute), served("mcp", s1, "", mcpconnectionattempts.FamilyBrowser, 10*time.Minute),
			},
			want: []summary{{problems.RuleSameServer, serverScope(s1), appSignIn, person, 2, 2, cursorApp, false}},
		},
		{
			name:     "an app version across two servers with evidence on both",
			episodes: []mcpconnectionattempts.FailingPerson{signedOut("a", s1, time.Hour, cursor), signedOut("b", s2, time.Hour, cursor)},
			served:   []mcpconnectionattempts.ServedByFamily{served("mcp", s1, "", claude, 10*time.Minute), served("mcp", s2, "", claude, 10*time.Minute)},
			want:     []summary{{problems.RuleSameAppVersion, cursorKey, appSignIn, person, 2, 2, cursorApp, true}},
		},
		{
			name:     "evidence on one server does not take another server's failures",
			episodes: []mcpconnectionattempts.FailingPerson{signedOut("a", s1, time.Hour, cursor), signedOut("b", s2, time.Hour, cursor)},
			served:   []mcpconnectionattempts.ServedByFamily{served("mcp", s2, "", claude, 10*time.Minute)},
			want:     []summary{},
		},
		{
			name: "an app version takes only the failures on servers with evidence",
			episodes: []mcpconnectionattempts.FailingPerson{
				signedOut("a", s1, time.Hour, cursor), signedOut("b", s1, time.Hour, cursor),
				signedOut("e", s2, 2*time.Hour, cursor), signedOut("f", s2, 2*time.Hour),
			},
			served: []mcpconnectionattempts.ServedByFamily{served("mcp", s1, "", claude, 10*time.Minute)},
			want: []summary{
				{problems.RuleSameServer, serverScope(s2), appSignIn, person, 2, 2, noApp, false},
				{problems.RuleSameAppVersion, cursorKey, appSignIn, person, 2, 2, cursorApp, true},
			},
		},
		{
			name: "people on no server leave with the app version's last server evidence",
			episodes: []mcpconnectionattempts.FailingPerson{
				signedOut("a", "", time.Hour, cursor, authorizationServer), signedOut("b", "", time.Hour, cursor, authorizationServer),
				signedOut("f", s1, time.Hour, cursor), signedOut("g", s1, time.Hour), signedOut("h", s1, time.Hour), signedOut("i", s1, time.Hour),
			},
			served: []mcpconnectionattempts.ServedByFamily{served("mcp", s1, "", mcpconnectionattempts.FamilyCodexCLI, 10*time.Minute)},
			want:   []summary{{problems.RuleSameServer, serverScope(s1), appSignIn, person, 4, 4, noApp, true}},
		},
		{
			name: "one person with evidence is not an app version",
			episodes: []mcpconnectionattempts.FailingPerson{
				signedOut("a", s1, 2*time.Hour, cursor),
				signedOut("b", s2, time.Hour, cursor), signedOut("c", s2, time.Hour, cursor), signedOut("d", s2, time.Hour, cursor),
			},
			served: []mcpconnectionattempts.ServedByFamily{served("mcp", s1, "", claude, 10*time.Minute)},
			want:   []summary{{problems.RuleSameServer, serverScope(s2), appSignIn, person, 3, 3, cursorApp, false}},
		},
		{
			name: "one person with evidence left out of an app version is alone after 24 hours",
			episodes: []mcpconnectionattempts.FailingPerson{
				signedOut("a", s1, 25*time.Hour, cursor),
				signedOut("b", s2, time.Hour, cursor), signedOut("c", s2, time.Hour, cursor), signedOut("d", s2, time.Hour, cursor),
			},
			served: []mcpconnectionattempts.ServedByFamily{served("mcp", s1, "", claude, 10*time.Minute)},
			want: []summary{
				{problems.RuleSameServer, serverScope(s2), appSignIn, person, 3, 3, cursorApp, false},
				{problems.RuleOnePerson, personScope("a"), appSignIn, person, 1, 1, cursorApp, true},
			},
		},
		{
			name:     "a gateway-level served row counts for an app sign-in on the gateway",
			episodes: []mcpconnectionattempts.FailingPerson{signedOut("a", "gw", time.Hour, cursor, member("gw", "")), signedOut("b", "gw", time.Hour, cursor, member("gw", ""))},
			served:   []mcpconnectionattempts.ServedByFamily{served("meta", "gw", "", claude, 10*time.Minute)},
			want:     []summary{{problems.RuleSameAppVersion, cursorKey, appSignIn, person, 2, 2, cursorApp, true}},
		},
		{
			name:     "a member's answered row counts for a failure at the gateway itself",
			episodes: []mcpconnectionattempts.FailingPerson{down("a", "gw", time.Hour, cursor, member("gw", "")), down("b", "gw", time.Hour, cursor, member("gw", ""))},
			served:   []mcpconnectionattempts.ServedByFamily{served("meta", "gw", s1, claude, 10*time.Minute)},
			want:     []summary{{problems.RuleSameAppVersion, cursorKey, part, provider, 2, 2, cursorApp, true}},
		},
		{
			name:     "another app answered through the server directly counts for its gateway member's outbound failures",
			episodes: []mcpconnectionattempts.FailingPerson{down("a", "gw", time.Hour, cursor, member("gw", s1)), down("b", "gw", time.Hour, cursor, member("gw", s1))},
			served:   []mcpconnectionattempts.ServedByFamily{served("mcp", s1, "", claude, 10*time.Minute)},
			want:     []summary{{problems.RuleSameAppVersion, cursorKey, part, provider, 2, 2, cursorApp, true}},
		},
		{
			name:     "another app signed in at the server directly does not count for a sign-in at the gateway",
			episodes: []mcpconnectionattempts.FailingPerson{signedOut("a", "gw", time.Hour, cursor, member("gw", s1)), signedOut("b", "gw", time.Hour, cursor, member("gw", s1))},
			served:   []mcpconnectionattempts.ServedByFamily{served("mcp", s1, "", claude, 10*time.Minute)},
			want:     []summary{{problems.RuleSameServer, serverScope(s1), appSignIn, person, 2, 2, cursorApp, false}},
		},
		{
			name:     "another app served through any member counts for a sign-in at the gateway",
			episodes: []mcpconnectionattempts.FailingPerson{signedOut("a", "gw", time.Hour, cursor, member("gw", s1)), signedOut("b", "gw", time.Hour, cursor, member("gw", s1))},
			served:   []mcpconnectionattempts.ServedByFamily{served("meta", "gw", s2, claude, 10*time.Minute)},
			want:     []summary{{problems.RuleSameAppVersion, cursorKey, appSignIn, person, 2, 2, cursorApp, true}},
		},
		{
			name:     "a gateway-level served row does not count for a member",
			episodes: []mcpconnectionattempts.FailingPerson{badKey("a", "gw", time.Hour, cursor, member("gw", s1)), badKey("b", "gw", time.Hour, cursor, member("gw", s1))},
			served:   []mcpconnectionattempts.ServedByFamily{served("meta", "gw", "", claude, 10*time.Minute)},
			want:     []summary{{problems.RuleSameServer, serverScope(s1), signIn, admin, 2, 2, cursorApp, false}},
		},
		{
			name:     "mixed apps share no app",
			episodes: []mcpconnectionattempts.FailingPerson{down("a", s1, time.Hour, cursor), down("b", s1, time.Hour, app(mcpconnectionattempts.FamilyCursor, "2.0.2")), down("c", s1, time.Hour, app(claude, "2.0.1"))},
			want:     []summary{{problems.RuleSameServer, serverScope(s1), part, provider, 3, 3, noApp, false}},
		},
		{
			name: "the narrower scope wins a tie on people",
			episodes: []mcpconnectionattempts.FailingPerson{
				down("a", s1, time.Hour), down("b", s1, time.Hour), down("a", s2, time.Hour),
			},
			upstreams: apiExample,
			want:      []summary{{problems.RuleSameServer, serverScope(s1), part, provider, 2, 2, noApp, false}},
		},
		{
			name: "a server's outage is not listed again as its service on the servers left",
			episodes: []mcpconnectionattempts.FailingPerson{
				down("a", s1, time.Hour), down("b", s1, time.Hour), down("c", s1, time.Hour),
				down("a", s2, 2*time.Hour), down("b", s2, 2*time.Hour),
				down("a", s3, time.Hour), down("c", s3, time.Hour),
			},
			upstreams: apiExample,
			want: []summary{
				{problems.RuleSameServer, serverScope(s1), part, provider, 3, 3, noApp, false},
				{problems.RuleSameServer, serverScope(s2), part, provider, 2, 2, noApp, false},
				{problems.RuleSameServer, serverScope(s3), part, provider, 2, 2, noApp, false},
			},
		},
		{
			name:      "the same two people on two servers with one upstream are two server problems",
			episodes:  []mcpconnectionattempts.FailingPerson{down("a", s1, 2*time.Hour), down("b", s1, 2*time.Hour), down("a", s2, time.Hour), down("b", s2, time.Hour)},
			upstreams: apiExample,
			want: []summary{
				{problems.RuleSameServer, serverScope(s1), part, provider, 2, 2, noApp, false},
				{problems.RuleSameServer, serverScope(s2), part, provider, 2, 2, noApp, false},
			},
		},
		{
			name: "a service with more people than any server takes its servers",
			episodes: []mcpconnectionattempts.FailingPerson{
				down("a", s1, time.Hour), down("b", s1, time.Hour), down("c", s2, time.Hour), down("d", s2, time.Hour), down("e", s3, time.Hour),
			},
			upstreams: apiExample,
			want:      []summary{{problems.RuleSameService, serviceScope("api.example.com"), part, provider, 5, 5, noApp, false}},
		},
		{
			name:      "server failures an administrator fixes are not a service",
			episodes:  []mcpconnectionattempts.FailingPerson{tunnelDown("a", s1, time.Hour), tunnelDown("b", s2, time.Hour)},
			upstreams: apiExample,
			want:      []summary{},
		},
		{
			name: "two ways on one server stay two problems",
			episodes: []mcpconnectionattempts.FailingPerson{
				down("a", s1, time.Hour), down("b", s1, time.Hour),
				badKey("d", s1, 2*time.Hour), badKey("e", s1, 2*time.Hour),
			},
			want: []summary{
				{problems.RuleSameServer, serverScope(s1), signIn, admin, 2, 2, noApp, false},
				{problems.RuleSameServer, serverScope(s1), part, provider, 2, 2, noApp, false},
			},
		},
		{
			name: "one part with two who-fixes stays two problems",
			episodes: []mcpconnectionattempts.FailingPerson{
				serviceDown("a", s1, time.Hour), serviceDown("b", s1, time.Hour),
				badKey("d", s1, 2*time.Hour), badKey("e", s1, 2*time.Hour),
			},
			want: []summary{
				{problems.RuleSameServer, serverScope(s1), signIn, admin, 2, 2, noApp, false},
				{problems.RuleSameServer, serverScope(s1), signIn, provider, 2, 2, noApp, false},
			},
		},
		{
			name:     "one person in each of two ways",
			episodes: []mcpconnectionattempts.FailingPerson{down("a", s1, time.Hour), badKey("c", s1, time.Hour)},
			want:     []summary{},
		},
		{
			name:     "one person failing for 24 hours",
			episodes: []mcpconnectionattempts.FailingPerson{badKey("a", s1, 24*time.Hour)},
			want:     []summary{{problems.RuleOnePerson, personScope("a"), signIn, admin, 1, 1, noApp, false}},
		},
		{
			name:     "one person failing for 23 hours",
			episodes: []mcpconnectionattempts.FailingPerson{badKey("a", s1, 23*time.Hour)},
			want:     []summary{},
		},
		{
			name:     "a person in a group problem is not also a person problem",
			episodes: []mcpconnectionattempts.FailingPerson{down("a", s1, 30*time.Hour), down("b", s1, time.Hour)},
			want:     []summary{{problems.RuleSameServer, serverScope(s1), part, provider, 2, 2, noApp, false}},
		},
		{
			name:     "a person in a group problem can be a person problem on another server",
			episodes: []mcpconnectionattempts.FailingPerson{badKey("a", s1, time.Hour), badKey("b", s1, time.Hour), badKey("a", s2, 30*time.Hour)},
			want: []summary{
				{problems.RuleSameServer, serverScope(s1), signIn, admin, 2, 2, noApp, false},
				{problems.RuleOnePerson, personScope("a"), signIn, admin, 1, 1, noApp, false},
			},
		},
		{
			name:     "one person failing the same way on two servers",
			episodes: []mcpconnectionattempts.FailingPerson{badKey("a", s1, 25*time.Hour), badKey("a", s2, 30*time.Hour)},
			want:     []summary{{problems.RuleOnePerson, personScope("a"), signIn, admin, 1, 2, noApp, false}},
		},
		{
			name:      "the same person on two servers counts once",
			episodes:  []mcpconnectionattempts.FailingPerson{down("a", s1, time.Hour), down("a", s2, time.Hour)},
			upstreams: apiExample,
			want:      []summary{},
		},
		{
			name:     "episodes with no server are in no group",
			episodes: []mcpconnectionattempts.FailingPerson{badKey("a", "", time.Hour, noServer), badKey("b", "", time.Hour, noServer)},
			want:     []summary{},
		},
		{
			name:     "an episode with no server can be a person problem",
			episodes: []mcpconnectionattempts.FailingPerson{badKey("a", "", 25*time.Hour, noServer)},
			want:     []summary{{problems.RuleOnePerson, personScope("a"), signIn, admin, 1, 1, noApp, false}},
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
	got := problems.Group("org-a", []mcpconnectionattempts.FailingPerson{down("a", s1, time.Hour), down("b", s1, 3*time.Hour), down("c", s1, 2*time.Hour)}, nil, nil, now)
	require.Len(t, got, 1)
	require.Equal(t, now.Add(-3*time.Hour), got[0].Since)
	require.Equal(t, now.Add(-30*time.Minute), got[0].LastFailedAt)
	require.Equal(t, mcpconnectionattempts.WhoProvider, got[0].WhoFixes)
	require.Equal(t, "org-a", got[0].OrganizationID)
}

func TestGroup_IDsAreStablePerOrganization(t *testing.T) {
	t.Parallel()
	eps := []mcpconnectionattempts.FailingPerson{down("a", s1, time.Hour), down("b", s1, time.Hour), down("c", s1, time.Hour), badKey("d", s2, 25*time.Hour)}

	first := problems.Group("org-a", eps, nil, nil, now)
	again := problems.Group("org-a", append(eps, down("e", s1, time.Minute)), nil, nil, now.Add(time.Hour))
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
		down("a", s1, time.Hour), down("b", s1, time.Hour), down("c", s1, time.Hour), down("d", s1, time.Hour),
		badKey("e", s2, 2*time.Hour), badKey("f", s2, 2*time.Hour), badKey("g", s2, 2*time.Hour),
		badKey("h", s3, 3*time.Hour), badKey("i", s3, 3*time.Hour), badKey("j", s3, 3*time.Hour),
		signedOut("k", s4, 26*time.Hour), signedOut("l", s4, 25*time.Hour), signedOut("l", s5, 27*time.Hour),
		down("a", s1, time.Hour, app(mcpconnectionattempts.FamilyCursor, "9")),
	}
	want := problems.Group("org-a", eps, nil, nil, now)
	require.Len(t, want, 5)
	require.Equal(t, 4, want[0].People)
	require.Equal(t, serverScope(s3), want[1].Scope, "equal people, earliest first")

	r := rand.New(rand.NewPCG(1, 2))
	for range 20 {
		shuffled := slices.Clone(eps)
		r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		require.Equal(t, want, problems.Group("org-a", shuffled, nil, nil, now))
	}
}

// Episodes that tie on every field but the last compared keep a fixed order.
func TestGroup_EpisodeOrderIsTotal(t *testing.T) {
	t.Parallel()
	one := down("a", s1, time.Hour)
	two := one
	two.ServerLastAnsweredAt = now.Add(-time.Minute)
	eps := []mcpconnectionattempts.FailingPerson{one, two, down("b", s1, time.Hour), down("c", s1, time.Hour)}
	want := problems.Group("org-a", eps, nil, nil, now)
	require.Len(t, want, 1)

	r := rand.New(rand.NewPCG(4, 4))
	for range 20 {
		shuffled := slices.Clone(eps)
		r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		require.Equal(t, want[0].Episodes, problems.Group("org-a", shuffled, nil, nil, now)[0].Episodes)
	}
}
