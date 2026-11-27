package demoseed

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
)

// The connection health dataset: one MCP authentication provider, five servers and a
// gateway, and a month of sign-ins by the people below. Its people, servers, apps and
// story times make the suspected problems the dashboard lists.

// healthPerson is one person signing in through the provider.
type healthPerson struct {
	key     string
	name    string
	family  string
	version string
	// servers are healthServers keys, or healthGatewayKey.
	servers []string
	// weight is how often the person signs in, in tenths.
	weight int
}

const healthGatewayKey = "gateway"

var healthPeople = []healthPerson{
	{"priya", "Priya Nair", mcpconnectionattempts.FamilyOpenAIConnector, "", []string{"linear", "notion", "wiki"}, 13},
	{"mei", "Mei Tan", mcpconnectionattempts.FamilyCodexCLI, "0.62.1", []string{healthGatewayKey, "github"}, 10},
	{"sam", "Sam Ortiz", mcpconnectionattempts.FamilyClaudeCode, "2.1.4", []string{"github", "linear", "support"}, 12},
	{"jonas", "Jonas Weber", mcpconnectionattempts.FamilyClaudeCode, "2.1.4", []string{healthGatewayKey, "notion"}, 10},
	{"omar", "Omar Haddad", mcpconnectionattempts.FamilyClaudeCode, "2.1.4", []string{"linear", "github"}, 10},
	{"ana", "Ana Costa", mcpconnectionattempts.FamilyVSCode, "1.105.1", []string{"github", "notion"}, 10},
	{"ken", "Ken Ito", mcpconnectionattempts.FamilyOpenAIConnector, "", []string{"wiki", "support"}, 10},
	{"rosa", "Rosa Mendes", mcpconnectionattempts.FamilyOpenAIConnector, "", []string{"support", "notion"}, 8},
	{"lena", "Lena Park", mcpconnectionattempts.FamilyCursor, "2.0.1", []string{"github", "linear"}, 10},
	{"hugo", "Hugo Lindqvist", mcpconnectionattempts.FamilyVSCode, "1.105.1", []string{healthGatewayKey}, 7},
	{"marcus", "Marcus Lee", mcpconnectionattempts.FamilyCodexCLI, "0.62.1", []string{"support", "github"}, 10},
	{"sofia", "Sofia Marquez", mcpconnectionattempts.FamilyCursor, "2.0.2", []string{"linear", "github"}, 12},
	{"dana", "Dana Reyes", mcpconnectionattempts.FamilyClaudeCode, "2.1.4", []string{"linear"}, 10},
	{"noor", "Noor Aziz", mcpconnectionattempts.FamilyClaudeCode, "2.0.14", []string{healthGatewayKey, "notion"}, 10},
	{"lars", "Lars Nyberg", mcpconnectionattempts.FamilyCursor, "2.0.1", []string{"github"}, 10},
	{"ivy", "Ivy Chen", mcpconnectionattempts.FamilyOpenAIConnector, "", []string{"wiki", "notion"}, 10},
	{"theo", "Theo Brandt", mcpconnectionattempts.FamilyOpenAIConnector, "", []string{"wiki", "support"}, 10},
	{"amara", "Amara Obi", mcpconnectionattempts.FamilyOpenAIConnector, "", []string{"wiki"}, 8},
	{"felix", "Felix Grant", mcpconnectionattempts.FamilyCursor, "2.0.1", []string{"github", "notion"}, 10},
}

// handle is the person's user handle: "Priya Nair" is "priya_nair".
func (p healthPerson) handle() string { return strings.ToLower(strings.ReplaceAll(p.name, " ", "_")) }

// healthKillswitchPerson is the roster member the seeded killswitch pauses, so her requests are blocked.
var healthKillswitchPerson = healthPerson{"okafor", "", mcpconnectionattempts.FamilyClaudeCode, "2.0.14", []string{"github", "notion"}, 0}

// healthMembers are the people as organization members, so their names resolve.
func healthMembers() []demoUser {
	members := make([]demoUser, len(healthPeople))
	for i, p := range healthPeople {
		members[i] = demoUser{handle: p.handle(), name: p.name, division: "", department: "", title: "", empType: "", costCenter: "", team: "", hostname: ""}
	}
	return members
}

// healthServer is one MCP server of the provider. service is the outside service it signs
// in to, empty for one that takes a saved key.
type healthServer struct {
	key, name, slug, url, service string
}

var healthServers = []healthServer{
	{"github", "GitHub", "eng-github", "https://github-mcp.example.com/mcp", "https://github.com"},
	{"linear", "Jira", "eng-jira", "https://jira-mcp.example.com/mcp", "https://jira.example.com"},
	{"notion", "Confluence", "eng-confluence", "https://confluence-mcp.example.com/mcp", "https://confluence.example.com"},
	{"wiki", "Internal Wiki", "internal-wiki", "https://wiki-mcp.example.com/mcp", ""},
	{"support", "Support Desk", "support-desk", "https://support-mcp.example.com/mcp", "https://support.example.com"},
}

// healthGatewayMembers are the Engineering Tools gateway's members, in order.
var healthGatewayMembers = []string{"github", "linear", "notion"}

func healthServerByKey(key string) healthServer {
	for _, srv := range healthServers {
		if srv.key == key {
			return srv
		}
	}
	panic("demo seed: unknown connection health server " + key)
}

// healthClient is the registration of one app family. Claude Code signs in with its
// catalog metadata document; the others registered dynamically.
type healthClient struct {
	family, name string
	metadata     bool
}

var healthClients = []healthClient{
	{mcpconnectionattempts.FamilyClaudeCode, "Claude Code", true},
	{mcpconnectionattempts.FamilyCursor, "Cursor", false},
	{mcpconnectionattempts.FamilyOpenAIConnector, "ChatGPT", false},
	{mcpconnectionattempts.FamilyVSCode, "Visual Studio Code", false},
	{mcpconnectionattempts.FamilyCodexCLI, "Codex", false},
}

func (s *seeder) healthIssuer() uuid.UUID             { return s.det("health-issuer") }
func (s *seeder) healthGateway() uuid.UUID            { return s.det("health-gateway") }
func (s *seeder) healthServerID(key string) uuid.UUID { return s.det("health-server-" + key) }
func (s *seeder) healthClientID(family string) uuid.UUID {
	return s.det("health-client-" + family)
}

// healthClock places the story: its day is today once this morning's Linear errors have
// recovered, past 11:45 local time, and yesterday before.
type healthClock struct {
	now time.Time
	day time.Time
}

func newHealthClock(now time.Time) healthClock {
	local := now.In(time.Local)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	if day.Add(11*time.Hour + 45*time.Minute).After(local) {
		day = day.AddDate(0, 0, -1)
	}
	return healthClock{now: now, day: day}
}

// at is a local time days from the story's day.
func (c healthClock) at(days, hour, minute, second int) time.Time {
	return time.Date(c.day.Year(), c.day.Month(), c.day.Day()+days, hour, minute, second, 0, time.Local).UTC()
}

// The story's fixed points.
func (c healthClock) linearDown() (time.Time, time.Time) { return c.at(0, 9, 42, 0), c.at(0, 11, 6, 0) }
func (c healthClock) cursorBrokenFrom() time.Time        { return c.at(-2, 10, 14, 0) }
func (c healthClock) cursorNotionFailure() time.Time     { return c.now.Add(-14 * time.Minute) }
func (c healthClock) wikiBrokenFrom() time.Time          { return c.at(-1, 16, 0, 0) }
func (c healthClock) danaFrom() time.Time                { return c.at(-2, 9, 12, 0) }

// seedRand draws deterministic variety from one name, in sequence.
type seedRand struct {
	name string
	n    uint64
}

func (r *seedRand) next() uint64 {
	r.n++
	return hash64(r.name, r.n)
}

// intn is in [0, n).
func (r *seedRand) intn(n int) int { return int(r.next() % uint64(n)) } //nolint:gosec // n is a small positive count

// between is in [lo, hi].
func (r *seedRand) between(lo, hi int) int { return lo + r.intn(hi-lo+1) }

// float is in [0, 1).
func (r *seedRand) float() float64 { return float64(r.next()>>11) / (1 << 53) }

// span is a duration in [lo, hi).
func (r *seedRand) span(lo, hi time.Duration) time.Duration {
	return lo + time.Duration(r.float()*float64(hi-lo))
}

// seedConnectionHealth writes the provider, its servers and gateway, the apps'
// registrations and the people's sessions. Everything sits in the demo project, so the
// project delete clears it on reseed.
func (s *seeder) seedConnectionHealth(ctx context.Context) error {
	org, proj, issuer := s.orgID(), s.projectID(), s.healthIssuer()
	if err := s.q.ServersInsertHealthIssuer(ctx, repo.ServersInsertHealthIssuerParams{ID: issuer, ProjectID: proj, OrgID: org}); err != nil {
		return fmt.Errorf("insert connection health provider: %w", err)
	}
	for _, srv := range healthServers {
		if err := s.q.ServersInsertHealthServer(ctx, repo.ServersInsertHealthServerParams{
			RemoteID: s.det("health-remote-" + srv.key), ProjectID: proj, Name: srv.name, Slug: srv.slug, Url: srv.url,
			ServerID: s.healthServerID(srv.key), IssuerID: issuer,
			EndpointID: s.det("health-endpoint-" + srv.key), EndpointSlug: s.spec.OrgSlug + "-" + srv.slug,
		}); err != nil {
			return fmt.Errorf("insert connection health server %s: %w", srv.name, err)
		}
	}
	if err := s.q.ServersInsertHealthGateway(ctx, repo.ServersInsertHealthGatewayParams{
		ID: s.healthGateway(), OrgID: org, ProjectID: proj, IssuerID: issuer,
		EndpointID: s.det("health-endpoint-gateway"), EndpointSlug: s.spec.OrgSlug + "-engineering-tools",
	}); err != nil {
		return fmt.Errorf("insert connection health gateway: %w", err)
	}
	for i, key := range healthGatewayMembers {
		if err := s.q.ServersInsertGatewayMember(ctx, repo.ServersInsertGatewayMemberParams{
			ID: s.det("health-member-" + key), ProjectID: proj, MetaID: s.healthGateway(), McpServerID: s.healthServerID(key), SortOrder: int32(i),
		}); err != nil {
			return fmt.Errorf("insert connection health gateway member %s: %w", key, err)
		}
	}
	for _, c := range healthClients {
		clientID, metadata := s.name("health-client-"+c.family), pgtype.Text{String: "", Valid: false}
		if c.metadata {
			clientID, metadata = demoConnectionApp, pgtype.Text{String: demoConnectionApp, Valid: true}
		}
		if err := s.q.ServersInsertHealthClient(ctx, repo.ServersInsertHealthClientParams{
			ID: s.healthClientID(c.family), ProjectID: proj, OrgID: org, IssuerID: issuer,
			ClientID: clientID, ClientName: c.name, MetadataUri: metadata,
		}); err != nil {
			return fmt.Errorf("insert connection health client %s: %w", c.name, err)
		}
	}
	// An organization sign-in reaches every server in the organization, these included.
	for i := 1; i <= 3; i++ {
		for _, srv := range healthServers {
			if err := s.q.ServersInsertUpstreamClientChoice(ctx, repo.ServersInsertUpstreamClientChoiceParams{
				ID: s.det("health-org-choice-" + strconv.Itoa(i) + "-" + srv.key), ProjectID: proj, OrgID: org,
				CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: s.det("org-gateway-session-" + strconv.Itoa(i)),
				McpServerID: s.healthServerID(srv.key), AuthClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, RemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Source: upstreamclients.SelectionSourceIssuance,
			}); err != nil {
				return fmt.Errorf("insert organization sign-in choice on %s: %w", srv.name, err)
			}
		}
	}
	return s.seedHealthSessions(ctx)
}

func (s *seeder) healthServerIDs() []uuid.UUID {
	ids := make([]uuid.UUID, len(healthServers))
	for i, srv := range healthServers {
		ids[i] = s.healthServerID(srv.key)
	}
	return ids
}

// healthSession is one sign-in of a person, ended when deleted is set.
type healthSession struct {
	userID, family                    string
	created, lastUsed, refreshExpires time.Time
	deleted                           *time.Time
}

// healthSessions are each person's past sign-ins over the month and the one they hold
// now. Lena Park and Dana Reyes hold sign-ins past their limit (they need to sign in
// again), and Hugo Lindqvist and Rosa Mendes have not used theirs for over a week.
func (s *seeder) healthSessions() []healthSession {
	clk := newHealthClock(s.now)
	now := s.now
	r := &seedRand{name: s.name("health-sessions"), n: 0}
	day := 24 * time.Hour
	people := append(append([]healthPerson{}, healthPeople...), healthKillswitchPerson)
	var out []healthSession
	for _, p := range people {
		userID := s.userID(p.handle())
		if p.key == healthKillswitchPerson.key {
			userID = s.userID("amara")
		}
		t := now.Add(-r.span(27*day, 30*day))
		for {
			end := t.Add(r.span(3*day, 11*day))
			if !end.Before(now.Add(-6 * time.Hour)) {
				break
			}
			ended := end
			out = append(out, healthSession{userID, p.family, t, end, end.Add(3 * day), &ended})
			t = end.Add(r.span(12*time.Minute, 30*time.Hour))
		}
		created := t
		if created.After(now.Add(-8 * time.Hour)) {
			created = now.Add(-8 * time.Hour)
		}
		lastUsed := now.Add(-time.Duration(r.between(30, 240)) * time.Minute)
		// The people renewing right now, and Marcus Lee's retry loop, used their sign-in minutes ago.
		if minutes, ok := map[string]int{"sam": 3, "priya": 7, "ana": 11, "mei": 15, "sofia": 19, "jonas": 23, "omar": 27, "marcus": 5}[p.key]; ok {
			lastUsed = now.Add(-time.Duration(minutes) * time.Minute)
		}
		refresh := lastUsed.Add(14 * day)
		switch p.key {
		case "lena":
			refresh, lastUsed = now.Add(-2*time.Hour), now.Add(-2*time.Hour-20*time.Minute)
		case "dana":
			refresh, lastUsed = clk.danaFrom().Add(-5*time.Minute), clk.at(-3, 17, 20, 0)
			if c := lastUsed.Add(-2 * day); c.Before(created) {
				created = c
			}
		case "hugo", "rosa":
			lastUsed = now.Add(-r.span(9*day, 11*day))
			refresh = lastUsed.Add(14 * day)
			if c := lastUsed.Add(-day); c.Before(created) {
				created = c
			}
		}
		out = append(out, healthSession{userID, p.family, created, lastUsed, refresh, nil})
		// Some people also connect a second app.
		second := map[string]string{
			"sam": mcpconnectionattempts.FamilyCursor, "priya": mcpconnectionattempts.FamilyClaudeCode,
			"ana": mcpconnectionattempts.FamilyClaudeCode, "omar": mcpconnectionattempts.FamilyCodexCLI,
		}[p.key]
		if second != "" {
			used := now.Add(-r.span(time.Hour, 30*time.Hour))
			out = append(out, healthSession{userID, second, now.Add(-r.span(2*day, 9*day)), used, used.Add(14 * day), nil})
		}
	}
	return out
}

func (s *seeder) seedHealthSessions(ctx context.Context) error {
	for n, session := range s.healthSessions() {
		expires := session.lastUsed.Add(time.Hour)
		if expires.After(session.refreshExpires) {
			expires = session.refreshExpires
		}
		deleted := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
		if session.deleted != nil {
			deleted = conv.ToPGTimestamptz(*session.deleted)
		}
		id := s.det("health-session-" + strconv.Itoa(n))
		if err := s.q.ServersInsertHealthSession(ctx, repo.ServersInsertHealthSessionParams{
			ID: id, ProjectID: s.projectID(), OrgID: s.orgID(),
			IssuerID: s.healthIssuer(), ClientID: s.healthClientID(session.family), UserID: session.userID,
			RefreshExpiresAt: conv.ToPGTimestamptz(session.refreshExpires), ExpiresAt: conv.ToPGTimestamptz(expires),
			LastUsedAt: conv.ToPGTimestamptz(session.lastUsed), CreatedAt: conv.ToPGTimestamptz(session.created),
			DeletedAt: deleted,
		}); err != nil {
			return fmt.Errorf("insert connection health session %d: %w", n, err)
		}
		if session.deleted != nil {
			continue
		}
		// The servers take no upstream sign-in of their own, as the consent page records it.
		for _, srv := range healthServers {
			if err := s.q.ServersInsertUpstreamClientChoice(ctx, repo.ServersInsertUpstreamClientChoiceParams{
				ID: s.det("health-choice-" + strconv.Itoa(n) + "-" + srv.key), ProjectID: s.projectID(), OrgID: s.orgID(),
				CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: id, McpServerID: s.healthServerID(srv.key),
				AuthClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, RemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Source: upstreamclients.SelectionSourceIssuance,
			}); err != nil {
				return fmt.Errorf("insert connection health session %d choice on %s: %w", n, srv.name, err)
			}
		}
	}
	return nil
}
