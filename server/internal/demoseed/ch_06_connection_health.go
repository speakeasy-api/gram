package demoseed

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// healthGen builds the connection health attempts (seed_04_connection_health.go): a
// month of sign-ins with a weekday rhythm, and the stories the suspected problems tell.
type healthGen struct {
	s      *seeder
	clk    healthClock
	r      *seedRand
	rows   []mcpconnectionattempts.Row
	served map[servedKey]mcpconnectionattempts.Served
	n      int
	err    error
}

// servedKey is one connection as mcp_connection_last_served keys it.
type servedKey struct{ kind, server, member, subject, app, family string }

// servedRequest is a request of the person's app that the server answered. It has no event row.
func (g *healthGen) servedRequest(p healthPerson, server, member string, at time.Time) {
	// Moved back rather than dropped, so whether the row exists never depends on the seed's second.
	if latest := g.clk.now.Add(-5 * time.Second); at.After(latest) {
		at = latest
	}
	kind, id := "mcp", g.s.healthServerID(server).String()
	if server == healthGatewayKey {
		kind, id = "meta", g.s.healthGateway().String()
	}
	memberID := ""
	if member != "" {
		memberID = g.s.healthServerID(member).String()
	}
	k := servedKey{kind: kind, server: id, member: memberID, subject: urn.NewUserSubject(g.userID(p)).String(), app: healthApp(p.family).String(), family: p.family}
	if prev, ok := g.served[k]; ok && !at.After(prev.ServedAt) {
		return
	}
	g.served[k] = mcpconnectionattempts.Served{
		OrganizationID: g.s.orgID(), ProjectID: g.s.projectID(), ServerKind: k.kind, ServerID: k.server, MemberID: k.member,
		SubjectURN: k.subject, AppKey: k.app, ClientFamily: k.family, RemoteSessionClientID: "", ServedAt: at, AnsweredAt: at,
	}
}

// healthAttempt is one attempt being built.
type healthAttempt struct {
	g       *healthGen
	key     string
	attempt mcpconnectionattempts.Attempt
	steps   int
}

// healthRow is what a row says beyond its step and result.
type healthRow struct {
	userID, family, version, grant, service, device, detail string
	member                                                  uuid.UUID
	repeats                                                 uint32
}

// begin starts an attempt on a server key or the gateway.
func (g *healthGen) begin(started time.Time, server string, app mcpconnectionattempts.App) *healthAttempt {
	g.n++
	key := "health-attempt-" + strconv.Itoa(g.n)
	scope := mcpconnectionattempts.GatewayScope(g.s.orgID(), g.s.projectID(), g.s.healthGateway(), app)
	if server != healthGatewayKey {
		scope = mcpconnectionattempts.ServerScope(g.s.orgID(), g.s.projectID(), g.s.healthServerID(server), app)
	}
	return &healthAttempt{g: g, key: key, attempt: mcpconnectionattempts.AttemptAt(g.s.det(key), started, scope), steps: 0}
}

func (a *healthAttempt) row(step mcpconnectionattempts.Step, what mcpconnectionattempts.Happened, at time.Time, final bool, f healthRow) {
	if latest := a.g.clk.now.Add(-5 * time.Second); at.After(latest) {
		at = latest
	}
	opts := []mcpconnectionattempts.Option{mcpconnectionattempts.UserSessionIssuer(a.g.s.healthIssuer())}
	if f.userID != "" {
		subject := urn.NewUserSubject(f.userID)
		opts = append(opts, mcpconnectionattempts.Subject(&subject))
	}
	if f.grant != "" {
		opts = append(opts, mcpconnectionattempts.Grant(f.grant))
	}
	if f.member != uuid.Nil {
		opts = append(opts, mcpconnectionattempts.Member(f.member))
	}
	if f.service != "" {
		opts = append(opts, mcpconnectionattempts.Provider(f.service))
	}
	if f.detail != "" {
		opts = append(opts, mcpconnectionattempts.Detail(f.detail))
	}
	row, err := mcpconnectionattempts.RowAt(a.attempt, step, what, final, at, opts...)
	if err != nil {
		if a.g.err == nil {
			a.g.err = fmt.Errorf("build connection health event: %w", err)
		}
		return
	}
	a.steps++
	row.TraceID = md5Hex(a.g.s.name(a.key + "-" + strconv.Itoa(a.steps)))
	row.ClientFamily, row.ClientVersion, row.DeviceKey, row.Repeats = f.family, f.version, f.device, f.repeats
	a.g.rows = append(a.g.rows, row)
}

// healthApp is how an app family's sign-ins name their app: Claude Code by its catalog
// metadata document, the others by a dynamic registration.
func healthApp(family string) mcpconnectionattempts.App {
	if family == mcpconnectionattempts.FamilyClaudeCode {
		return mcpconnectionattempts.AppOf(aivendors.CompiledSnapshot(), demoConnectionApp, mcpconnectionattempts.ClientIDAdmitted)
	}
	return mcpconnectionattempts.AppDCR
}

func (g *healthGen) userID(p healthPerson) string {
	if p.key == healthKillswitchPerson.key {
		return g.s.userID("amara")
	}
	return g.s.userID(p.handle())
}

func (g *healthGen) device(key string) string { return md5Hex(g.s.name("health-device-" + key)) }

// service is the outside service of a server, or of a gateway member.
func (g *healthGen) service(server string, member string) string {
	if server == healthGatewayKey {
		if member == "" {
			return ""
		}
		server = member
	}
	return healthServerByKey(server).service
}

// person is the row of a person's app.
func (g *healthGen) person(p healthPerson) healthRow {
	return healthRow{userID: g.userID(p), family: p.family, version: p.version, grant: "", service: "", device: g.device(p.key), detail: "", member: uuid.Nil, repeats: 0}
}

// browser is a sign-in page row: opened in the person's browser.
func (g *healthGen) browser(p healthPerson) healthRow {
	f := g.person(p)
	f.family, f.version = mcpconnectionattempts.FamilyBrowser, ""
	return f
}

// signInEnd is how a sign-in ends: at a step with a refusal, or not at all.
type signInEnd struct {
	step       mcpconnectionattempts.Step
	what       mcpconnectionattempts.Happened
	unfinished bool
}

var healthConnected = signInEnd{step: mcpconnectionattempts.StepInboundToken, what: mcpconnectionattempts.ResultCodeIssued, unfinished: false}

// signIn is a full sign-in: the MCP sign-in, the service's for a server that has one, then the token.
func (g *healthGen) signIn(p healthPerson, server string, t time.Time, end signInEnd) {
	a := g.begin(t, server, healthApp(p.family))
	b := g.browser(p)
	if end.step == mcpconnectionattempts.StepInboundAuthorize {
		b.userID = ""
		a.row(end.step, end.what, t, true, b)
		return
	}
	authorize := b
	authorize.userID = ""
	a.row(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted, t, false, authorize)
	if end.step == mcpconnectionattempts.StepInboundSignIn {
		a.row(end.step, end.what, t.Add(25*time.Second), true, b)
		return
	}
	a.row(mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.ResultSignedIn, t.Add(20*time.Second), false, b)
	if end.step == mcpconnectionattempts.StepInboundConsent {
		a.row(end.step, end.what, t.Add(48*time.Second), true, b)
		return
	}
	if service := g.service(server, ""); service != "" && server != healthGatewayKey {
		b.service = service
		if end.step == mcpconnectionattempts.StepProviderSignIn {
			a.row(end.step, end.what, t.Add(55*time.Second), true, b)
			return
		}
		a.row(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.ResultAccepted, t.Add(30*time.Second), false, b)
		if end.step == mcpconnectionattempts.StepProviderToken {
			a.row(end.step, end.what, t.Add(62*time.Second), true, b)
			return
		}
		a.row(mcpconnectionattempts.StepProviderToken, mcpconnectionattempts.ResultSignedIn, t.Add(50*time.Second), false, b)
	}
	if end.unfinished {
		return
	}
	token := g.person(p)
	token.grant = oauthwire.GrantTypeAuthorizationCode
	a.row(mcpconnectionattempts.StepInboundToken, end.what, t.Add(58*time.Second), true, token)
}

// renewal is the app renewing its MCP sign-in.
func (g *healthGen) renewal(p healthPerson, server string, t time.Time, what mcpconnectionattempts.Happened) {
	f := g.person(p)
	f.grant = oauthwire.GrantTypeRefreshToken
	g.begin(t, server, healthApp(p.family)).row(mcpconnectionattempts.StepInboundToken, what, t.Add(time.Second), true, f)
}

// serviceRenewal is Speakeasy renewing the person's sign-in to the service.
func (g *healthGen) serviceRenewal(p healthPerson, server, member string, t time.Time, what mcpconnectionattempts.Happened) {
	f := g.person(p)
	f.service = g.service(server, member)
	if member != "" {
		f.member = g.s.healthServerID(member)
	}
	g.begin(t, server, healthApp(p.family)).row(mcpconnectionattempts.StepProviderRefresh, what, t.Add(2*time.Second), true, f)
}

// request is an MCP request that was refused or failed. A served request has no row.
func (g *healthGen) request(p healthPerson, server, member string, t time.Time, code mcpconnectionattempts.Code, repeats uint32) {
	f := g.person(p)
	f.service, f.repeats = g.service(server, member), repeats
	if member != "" {
		f.member = g.s.healthServerID(member)
	}
	g.begin(t, server, healthApp(p.family)).row(mcpconnectionattempts.StepMCPRequest, code, t, true, f)
}

// listing is the gateway listing its members' tools; missing is a member whose service needs a sign-in.
func (g *healthGen) listing(p healthPerson, t time.Time, missing string) {
	a := g.begin(t, healthGatewayKey, healthApp(p.family))
	for i, key := range healthGatewayMembers {
		f := g.person(p)
		f.member = g.s.healthServerID(key)
		var what mcpconnectionattempts.Happened = mcpconnectionattempts.ResultListed
		if key == missing {
			what = mcpconnectionattempts.CodeNeedsSignIn
		}
		a.row(mcpconnectionattempts.StepDiscoveryMember, what, t.Add(time.Duration(i+1)*time.Second), false, f)
	}
	var what mcpconnectionattempts.Happened = mcpconnectionattempts.ResultListed
	if missing != "" {
		what = mcpconnectionattempts.CodeMemberToolsMissing
	}
	a.row(mcpconnectionattempts.StepDiscoveryListing, what, t.Add(4*time.Second), true, g.person(p))
	if missing == "" {
		g.servedRequest(p, healthGatewayKey, "", t.Add(4*time.Second))
	}
}

// network is a row of nobody known yet: an app or browser on one of a few networks.
func (g *healthGen) network(n int, family, version string) healthRow {
	return healthRow{userID: "", family: family, version: version, grant: "", service: "", device: g.device("network-" + strconv.Itoa(n)), detail: "", member: uuid.Nil, repeats: 0}
}

// refusedApp is an app refused when it asks for its token: a blocked AI app, or an app
// this server's client access does not admit.
func (g *healthGen) refusedApp(server string, t time.Time, blocked bool, network int) {
	app, code, family, version := mcpconnectionattempts.AppOtherMetadataURL, mcpconnectionattempts.CodeCIMDAdmissionDenied, mcpconnectionattempts.FamilyOther, ""
	if blocked {
		app, code, family, version = mcpconnectionattempts.AppDCR, mcpconnectionattempts.CodeAIToolBlocked, mcpconnectionattempts.FamilyPerplexity, "1.4.0"
	}
	f := g.network(network, family, version)
	f.grant = oauthwire.GrantTypeAuthorizationCode
	g.begin(t, server, app).row(mcpconnectionattempts.StepInboundToken, code, t, true, f)
}

// strayAttempt is an attempt with no known person.
func (g *healthGen) strayAttempt(server string, t time.Time) {
	network := g.r.intn(4)
	switch x := g.r.float(); {
	case x < .45:
		g.refusedApp(server, t, true, network)
	case x < .75:
		g.refusedApp(server, t, false, network)
	case x < .9:
		a := g.begin(t, server, mcpconnectionattempts.AppDCR)
		f := g.network(network, mcpconnectionattempts.FamilyBrowser, "")
		a.row(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted, t, false, f)
		a.row(mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.CodeNotOrgMember, t.Add(25*time.Second), true, f)
	default:
		g.begin(t, server, mcpconnectionattempts.AppDCR).row(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted, t, false, g.network(network, mcpconnectionattempts.FamilyBrowser, ""))
	}
}

func (g *healthGen) personByKey(key string) healthPerson {
	for _, p := range healthPeople {
		if p.key == key {
			return p
		}
	}
	panic("demo seed: unknown connection health person " + key)
}

// healthHourWeight is the share of a day's attempts in each local hour, in tenths.
var healthHourWeight = [24]int{2, 1, 1, 1, 1, 2, 3, 8, 20, 36, 46, 44, 28, 36, 46, 44, 36, 26, 15, 10, 7, 5, 4, 2}

// healthPerHour scales the hourly weights to about 3,000 attempts over the month.
const healthPerHour = 0.3

// healthKinds are the baseline's attempt kinds, weighted in tenths of a percent.
var healthKinds = []struct {
	kind   string
	weight int
}{
	{"renewal", 420}, {"service_renewal", 170}, {"sign_in", 120}, {"listing", 50},
	{"renewal_fail", 8}, {"service_fail", 5}, {"request_fail", 14}, {"signin_fail", 11},
	{"declined", 8}, {"unfinished", 8}, {"partial", 4}, {"network", 6},
}

var (
	healthRenewalFails = []mcpconnectionattempts.Code{
		mcpconnectionattempts.CodeRefreshTokenExpired, mcpconnectionattempts.CodeRefreshTokenRevoked,
		mcpconnectionattempts.CodeRefreshTokenUsedByOtherApp, mcpconnectionattempts.CodeRefreshTokenExpired,
		mcpconnectionattempts.CodeSignInRevokedByAdmin,
	}
	healthServiceFails = []mcpconnectionattempts.Code{
		mcpconnectionattempts.CodeInvalidGrant, mcpconnectionattempts.CodeProviderRateLimited, mcpconnectionattempts.CodeInvalidGrant,
	}
	healthRequestFails = []mcpconnectionattempts.Code{
		mcpconnectionattempts.CodeInvalidBearerToken, mcpconnectionattempts.CodePolicyBlocked,
		mcpconnectionattempts.CodeToolPermissionDenied, mcpconnectionattempts.CodeInvalidRemoteSession,
		mcpconnectionattempts.CodeInvalidBearerToken, mcpconnectionattempts.CodeToolNotApproved,
	}
	healthSignInFails = []signInEnd{
		{step: mcpconnectionattempts.StepInboundToken, what: mcpconnectionattempts.CodePKCEMismatch, unfinished: false},
		{step: mcpconnectionattempts.StepInboundToken, what: mcpconnectionattempts.CodeCodeNotFoundOrExpired, unfinished: false},
		{step: mcpconnectionattempts.StepProviderSignIn, what: mcpconnectionattempts.CodeProviderOAuthInvalidScope, unfinished: false},
		{step: mcpconnectionattempts.StepProviderToken, what: mcpconnectionattempts.CodeProviderOAuthInvalidGrant, unfinished: false},
		{step: mcpconnectionattempts.StepInboundSignIn, what: mcpconnectionattempts.CodeNoServerAccess, unfinished: false},
	}
)

// busy keeps the baseline out of a story's way.
func (g *healthGen) busy(p healthPerson, server string, t time.Time) bool {
	downFrom, downTo := g.clk.linearDown()
	switch {
	case (p.key == "hugo" || p.key == "rosa") && t.After(g.clk.now.Add(-9*24*time.Hour)):
		return true
	case p.key == "lena" && !t.Before(g.clk.cursorBrokenFrom()):
		return true
	case p.key == "dana" && t.After(g.clk.danaFrom().Add(-16*time.Hour)):
		return true
	case (server == "linear" || server == healthGatewayKey) && !t.Before(downFrom) && !t.After(downTo):
		return true
	case p.family == mcpconnectionattempts.FamilyCursor && p.version == "2.0.1" && server == "github" && !t.Before(g.clk.cursorBrokenFrom()):
		return true
	case server == "wiki" && !t.Before(g.clk.wikiBrokenFrom()) && slices.Contains([]string{"ken", "ivy", "theo", "amara"}, p.key):
		return true
	}
	return false
}

// busyLater is busy for a row that follows one at a time busy has already allowed: the
// stories' fixed windows apply, the quiet days counted back from the seed's clock only
// through the first row, so the pair never splits on the second the seed ran.
func (g *healthGen) busyLater(p healthPerson, server string, t time.Time) bool {
	if p.key == "hugo" || p.key == "rosa" {
		return false
	}
	return g.busy(p, server, t)
}

func (g *healthGen) baseline() {
	totalWeight := 0
	for _, p := range healthPeople {
		totalWeight += p.weight
	}
	kindWeight := 0
	for _, k := range healthKinds {
		kindWeight += k.weight
	}
	downFrom, _ := g.clk.linearDown()
	start := g.clk.now.Add(-(29*24 + 18) * time.Hour).In(time.Local).Truncate(time.Hour)
	for hour := start; hour.Before(g.clk.now); hour = hour.Add(time.Hour) {
		local := hour.In(time.Local)
		rate := healthPerHour * float64(healthHourWeight[local.Hour()])
		if local.Weekday() == time.Saturday || local.Weekday() == time.Sunday {
			rate *= 0.22
		}
		if hour.After(g.clk.now.Add(-24 * time.Hour)) {
			rate *= 1.7
		}
		n := int(rate)
		if g.r.float() < rate-float64(n) {
			n++
		}
		for range n {
			when := hour.Add(time.Duration(g.r.intn(3600)) * time.Second).UTC()
			pick := g.r.intn(totalWeight)
			p := healthPeople[0]
			for _, candidate := range healthPeople {
				if pick < candidate.weight {
					p = candidate
					break
				}
				pick -= candidate.weight
			}
			server := p.servers[g.r.intn(len(p.servers))]
			kindPick, kind := g.r.intn(kindWeight), ""
			for _, k := range healthKinds {
				if kindPick < k.weight {
					kind = k.kind
					break
				}
				kindPick -= k.weight
			}
			if when.After(g.clk.now.Add(-2*time.Minute)) || g.busy(p, server, when) {
				continue
			}
			// Keep the Linear recovery clean: no other failures there after this morning's errors.
			if (server == "linear" || server == healthGatewayKey) && when.After(downFrom) && kind != "renewal" && kind != "service_renewal" && kind != "sign_in" && kind != "listing" {
				continue
			}
			// A baseline failure is a blip the person gets past, so it never shows as a problem.
			// The recovery follows its failure: one near the cutoff recovers sooner instead of
			// being dropped, so the pair is in or out with the failure's own time.
			recoverAt := when.Add(g.r.span(3*time.Minute, 25*time.Minute))
			if healthBlips[kind] && g.busyLater(p, server, recoverAt) {
				continue
			}
			if cutoff := g.clk.now.Add(-2 * time.Minute); recoverAt.After(cutoff) {
				recoverAt = when.Add(cutoff.Sub(when) / 2)
			}
			g.emit(kind, p, server, when, recoverAt)
			// After a sign-in or renewal the app usually makes requests the server answers.
			if requestAt := when.Add(g.r.span(time.Minute, 20*time.Minute)); (kind == "renewal" || kind == "service_renewal" || kind == "sign_in") &&
				g.r.float() < .6 && !g.busyLater(p, server, requestAt) {
				g.servedRequest(p, server, "", requestAt)
			}
		}
	}
}

// healthBlips are the baseline's failing kinds, each followed by the person getting past it.
var healthBlips = map[string]bool{"renewal_fail": true, "service_fail": true, "request_fail": true, "signin_fail": true, "partial": true}

// recovered is the person connecting again after a blip: signed in, renewed with the
// gateway member's service, and listing the gateway's tools.
func (g *healthGen) recovered(p healthPerson, server, member string, at time.Time) {
	g.signIn(p, server, at, healthConnected)
	g.servedRequest(p, server, member, at.Add(90*time.Second))
	if server == healthGatewayKey {
		g.serviceRenewal(p, server, member, at.Add(time.Minute), mcpconnectionattempts.ResultRefreshed)
		g.listing(p, at.Add(2*time.Minute), "")
	}
}

func (g *healthGen) emit(kind string, p healthPerson, server string, when, recoverAt time.Time) {
	oauth := g.service(server, "") != ""
	member := healthGatewayMembers[g.r.intn(len(healthGatewayMembers))]
	if healthBlips[kind] {
		// A direct server has no gateway member.
		recoveredMember := ""
		if server == healthGatewayKey {
			recoveredMember = member
		}
		defer g.recovered(p, server, recoveredMember, recoverAt)
	}
	switch {
	case kind == "renewal", kind == "listing" && server != healthGatewayKey, kind == "partial" && server != healthGatewayKey,
		kind == "service_renewal" && server != healthGatewayKey && !oauth:
		g.renewal(p, server, when, mcpconnectionattempts.ResultRefreshed)
	case kind == "service_renewal" && server == healthGatewayKey:
		g.serviceRenewal(p, server, member, when, mcpconnectionattempts.ResultRefreshed)
	case kind == "service_renewal":
		g.serviceRenewal(p, server, "", when, mcpconnectionattempts.ResultRefreshed)
	case kind == "sign_in":
		g.signIn(p, server, when, healthConnected)
	case kind == "listing":
		g.listing(p, when, "")
	case kind == "partial":
		g.listing(p, when, member)
	case kind == "renewal_fail":
		g.renewal(p, server, when, healthRenewalFails[g.r.intn(len(healthRenewalFails))])
	case kind == "service_fail" && oauth && server != healthGatewayKey:
		g.serviceRenewal(p, server, "", when, healthServiceFails[g.r.intn(len(healthServiceFails))])
	case kind == "service_fail":
		g.renewal(p, server, when, mcpconnectionattempts.CodeRefreshTokenExpired)
	case kind == "request_fail":
		m := ""
		if server == healthGatewayKey {
			m = member
		}
		g.request(p, server, m, when, healthRequestFails[g.r.intn(len(healthRequestFails))], []uint32{0, 0, 0, 0, 1, 2}[g.r.intn(6)])
	case kind == "signin_fail":
		end := healthSignInFails[g.r.intn(len(healthSignInFails))]
		if (end.step == mcpconnectionattempts.StepProviderSignIn || end.step == mcpconnectionattempts.StepProviderToken) && (!oauth || server == healthGatewayKey) {
			end = healthSignInFails[0]
		}
		g.signIn(p, server, when, end)
	case kind == "declined" && oauth && server != healthGatewayKey && g.r.float() < .5:
		g.signIn(p, server, when, signInEnd{step: mcpconnectionattempts.StepProviderSignIn, what: mcpconnectionattempts.CodeProviderDenied, unfinished: false})
	case kind == "declined":
		g.signIn(p, server, when, signInEnd{step: mcpconnectionattempts.StepInboundConsent, what: mcpconnectionattempts.CodeCanceled, unfinished: false})
	case kind == "unfinished":
		g.signIn(p, server, when, signInEnd{step: mcpconnectionattempts.StepInboundToken, what: mcpconnectionattempts.ResultCodeIssued, unfinished: true})
	case kind == "network":
		g.strayAttempt(server, when)
	}
}

// stillTrying moves a story's last failure to ago before now when that is later, so the
// people in it have tried within the last day whatever the time of the seed.
func (g *healthGen) stillTrying(last time.Time, ago time.Duration) time.Time {
	if recent := g.clk.now.Add(-ago); recent.After(last) {
		return recent
	}
	return last
}

// spread places n times from start to end, the inner ones jittered.
func (g *healthGen) spread(start, end time.Time, n int) []time.Time {
	if n == 1 {
		return []time.Time{start}
	}
	step := end.Sub(start) / time.Duration(n-1)
	out := make([]time.Time, n)
	for i := range n {
		jitter := time.Duration(0)
		if i > 0 && i < n-1 {
			jitter = time.Duration((g.r.float()*0.7 - 0.35) * float64(step))
		}
		out[i] = start.Add(step*time.Duration(i) + jitter)
	}
	return out
}

// storyLinear: the Linear service returns errors from 09:48 to 11:04 for five people, then recovers.
func (g *healthGen) storyLinear() {
	unavailable, serverError := mcpconnectionattempts.CodeProviderUnavailable, mcpconnectionattempts.CodeProviderServerError
	type part struct {
		refresh bool
		code    mcpconnectionattempts.Code
		count   int
	}
	at := g.clk.at
	plan := []struct {
		who, server, member string
		parts               []part
		first, last         time.Time
	}{
		{"priya", "linear", "", []part{{true, unavailable, 9}, {false, serverError, 5}}, at(0, 9, 48, 3), at(0, 11, 4, 12)},
		{"omar", "linear", "", []part{{true, unavailable, 4}, {false, serverError, 2}}, at(0, 9, 51, 31), at(0, 10, 57, 40)},
		{"sofia", "linear", "", []part{{true, unavailable, 5}, {false, serverError, 4}}, at(0, 9, 55, 12), at(0, 11, 2, 5)},
		{"mei", healthGatewayKey, "linear", []part{{true, unavailable, 1}, {false, serverError, 1}, {false, mcpconnectionattempts.CodeRemoteSessionUnavailable, 1}}, at(0, 10, 39, 20), at(0, 10, 54, 30)},
		{"jonas", healthGatewayKey, "linear", []part{{true, unavailable, 1}}, at(0, 11, 1, 17), at(0, 11, 1, 17)},
	}
	for _, who := range plan {
		p := g.personByKey(who.who)
		var kinds []part
		for _, pt := range who.parts {
			for range pt.count {
				kinds = append(kinds, pt)
			}
		}
		// The first failure is a service renewal; the rest are shuffled.
		for i := len(kinds) - 1; i > 1; i-- {
			j := 1 + g.r.intn(i)
			kinds[i], kinds[j] = kinds[j], kinds[i]
		}
		for i, t := range g.spread(who.first, who.last, len(kinds)) {
			if kinds[i].refresh {
				g.serviceRenewal(p, who.server, who.member, t, kinds[i].code)
			} else {
				g.request(p, who.server, who.member, t, kinds[i].code, 0)
			}
		}
	}
	// The last good renewal before the errors, and the recovery after.
	g.serviceRenewal(g.personByKey("sam"), "linear", "", at(0, 9, 41, 10), mcpconnectionattempts.ResultRefreshed)
	for _, r := range []struct {
		who          string
		hour, minute int
	}{{"priya", 11, 12}, {"sofia", 11, 19}, {"omar", 11, 26}, {"sam", 11, 41}, {"priya", 12, 8}} {
		if t := at(0, r.hour, r.minute, 0); t.Before(g.clk.now) {
			g.serviceRenewal(g.personByKey(r.who), "linear", "", t, mcpconnectionattempts.ResultRefreshed)
			g.servedRequest(g.personByKey(r.who), "linear", "", t.Add(time.Minute))
		}
	}
	for _, r := range []struct {
		who    string
		minute int
	}{{"mei", 33}, {"jonas", 36}} {
		if t := at(0, 11, r.minute, 0); t.Before(g.clk.now) {
			g.serviceRenewal(g.personByKey(r.who), healthGatewayKey, "linear", t, mcpconnectionattempts.ResultRefreshed)
			g.servedRequest(g.personByKey(r.who), healthGatewayKey, "linear", t.Add(time.Minute))
		}
	}
}

// storyCursor: Cursor 2.0.1 cannot renew its MCP sign-in on GitHub for three people since
// two days ago; Cursor 2.0.2 renews fine there, and Claude Code works there.
func (g *healthGen) storyCursor() {
	at := g.clk.at
	for i, who := range []struct {
		key   string
		count int
	}{{"lena", 38}, {"felix", 22}, {"lars", 27}} {
		p := g.personByKey(who.key)
		start := at(-2, 10, 20+i, 15)
		storyEnd, ago := at(0, 10, 5+g.r.intn(34), 0), time.Duration(6+2*i)*time.Minute
		if who.key == "lena" {
			storyEnd, ago = at(0, 10, 41, 26), 5*time.Minute
		}
		end := g.stillTrying(storyEnd, ago)
		// The app retries a few times minutes apart during working hours, then gives up for a while.
		// Retries are placed on the story's fixed clock, so the draws never depend on the seed's second.
		times := []time.Time{start, end}
		for len(times) < who.count {
			burst := start.Add(time.Duration(g.r.float() * float64(storyEnd.Sub(start))))
			if h := burst.In(time.Local).Hour(); h < 8 || h > 17 {
				continue
			}
			for k := range g.r.between(3, 6) {
				t := burst.Add(time.Duration(float64(k) * float64(g.r.span(5*time.Minute, 11*time.Minute))))
				if t.After(start) && t.Before(storyEnd) && len(times) < who.count {
					times = append(times, t)
				}
			}
		}
		slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
		for _, t := range times {
			g.renewal(p, "github", t, mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed)
		}
		g.renewal(p, "github", at(-2, 10, 12, 0).Add(-time.Duration(g.r.intn(30))*time.Minute), mcpconnectionattempts.ResultRefreshed)
	}
	for _, t := range g.spread(at(-2, 11, 0, 0), g.clk.now.Add(-time.Hour), 9) {
		g.renewal(g.personByKey("sofia"), "github", t, mcpconnectionattempts.ResultRefreshed)
	}
	g.servedRequest(g.personByKey("sam"), "github", "", g.clk.now.Add(-25*time.Minute))
}

// storyWiki: Internal Wiki rejects its saved key since yesterday afternoon.
func (g *healthGen) storyWiki() {
	at := g.clk.at
	g.signIn(g.personByKey("ivy"), "wiki", at(-1, 15, 58, 0), healthConnected)
	for _, who := range []struct {
		key         string
		count       int
		first, last time.Time
	}{
		{"ken", 4, at(0, 10, 41, 30), g.stillTrying(at(0, 10, 50, 10), 7*time.Minute)},
		{"ivy", 3, at(-1, 16, 5, 40), g.stillTrying(at(0, 10, 42+g.r.intn(8), 0), 11*time.Minute)},
		{"theo", 4, at(-1, 16, 6, 40), g.stillTrying(at(0, 10, 42+g.r.intn(8), 0), 9*time.Minute)},
		{"amara", 5, at(-1, 16, 7, 40), g.stillTrying(at(0, 10, 52, 8), 6*time.Minute)},
	} {
		for _, t := range g.spread(who.first, who.last, who.count) {
			g.request(g.personByKey(who.key), "wiki", "", t, mcpconnectionattempts.CodeStaticKeyRejected, 0)
		}
	}
}

// storyDana: Dana's MCP sign-in reached its time limit two days ago; her app still retries.
func (g *healthGen) storyDana() {
	p := g.personByKey("dana")
	for _, t := range g.spread(g.clk.danaFrom().Add(44*time.Second), g.clk.now.Add(-8*time.Minute), 41) {
		g.renewal(p, "linear", t, mcpconnectionattempts.CodeRefreshTokenExpired)
	}
	g.renewal(p, "linear", g.clk.at(-3, 17, 20, 0), mcpconnectionattempts.ResultRefreshed)
}

// storyLoop: Codex keeps sending an expired sign-in to Support Desk; the repeats fold into one row.
func (g *healthGen) storyLoop() {
	p := g.personByKey("marcus")
	start := g.clk.now.Add(-6*time.Hour - 10*time.Minute)
	for i := range 34 {
		t := start.Add(time.Duration(5*i)*time.Minute + time.Duration(g.r.intn(50))*time.Second)
		g.request(p, "support", "", t, mcpconnectionattempts.CodeInvalidBearerToken, uint32(g.r.between(1, 3))) //nolint:gosec // 1 to 3
	}
	g.signIn(p, "support", start.Add(3*time.Hour+5*time.Minute), healthConnected)
}

// storyKillswitch: the seeded killswitch pauses Amara Okafor's tool calls, so her requests are blocked.
func (g *healthGen) storyKillswitch() {
	p := healthKillswitchPerson
	for d := 1; d <= 6; d++ {
		for range g.r.between(1, 3) {
			local := g.clk.now.Add(-time.Duration(d) * 24 * time.Hour).In(time.Local)
			t := time.Date(local.Year(), local.Month(), local.Day(), g.r.between(9, 17), g.r.intn(60), 0, 0, time.Local).UTC()
			g.request(p, p.servers[g.r.intn(2)], "", t, mcpconnectionattempts.CodeMCPToolCallsPaused, []uint32{0, 0, 1}[g.r.intn(3)])
		}
	}
	g.renewal(p, "github", g.clk.now.Add(-12*24*time.Hour-3*time.Hour), mcpconnectionattempts.ResultRefreshed)
	g.renewal(p, "notion", g.clk.now.Add(-8*24*time.Hour-5*time.Hour), mcpconnectionattempts.ResultRefreshed)
}

// storyDenied: refused apps in the last week, a blocked AI app and an app a server does not admit.
func (g *healthGen) storyDenied() {
	localAt := func(daysAgo, hour, minute int) time.Time {
		local := g.clk.now.Add(-time.Duration(daysAgo) * 24 * time.Hour).In(time.Local)
		return time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, time.Local).UTC()
	}
	for i, d := range []struct{ daysAgo, hour int }{{1, 10}, {2, 15}, {4, 11}, {5, 9}} {
		g.refusedApp([]string{"notion", "github", "notion", "support"}[i], localAt(d.daysAgo, d.hour, 7*i+3), true, i%2)
	}
	for i, d := range []int{1, 3, 6} {
		g.refusedApp("github", localAt(d, 14, 11*i+5), false, 2)
	}
}

// storyNow: right now, two sign-ins in progress and recent traffic.
func (g *healthGen) storyNow() {
	now := g.clk.now
	g.signIn(g.personByKey("noor"), "notion", now.Add(-6*time.Minute), signInEnd{step: mcpconnectionattempts.StepInboundToken, what: mcpconnectionattempts.ResultCodeIssued, unfinished: true})
	g.begin(now.Add(-19*time.Minute), "notion", mcpconnectionattempts.AppDCR).row(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted, now.Add(-19*time.Minute), false, g.network(1, mcpconnectionattempts.FamilyBrowser, ""))
	g.renewal(g.personByKey("felix"), "notion", now.Add(-14*time.Minute), mcpconnectionattempts.CodeRefreshTokenUsedByOtherApp)
	// Claude Code works on Notion after it, so Felix's failure there joins the Cursor 2.0.1 problem.
	g.servedRequest(g.personByKey("jonas"), "notion", "", now.Add(-4*time.Minute))
	g.signIn(g.personByKey("theo"), "support", now.Add(-33*time.Minute), signInEnd{step: mcpconnectionattempts.StepInboundConsent, what: mcpconnectionattempts.CodeCanceled, unfinished: false})
	for i, key := range []string{"sam", "priya", "ana", "mei", "sofia", "jonas", "omar"} {
		p := g.personByKey(key)
		t := now.Add(-time.Duration(3+i*4) * time.Minute)
		if p.servers[0] == healthGatewayKey {
			g.listing(p, t, "")
		} else {
			g.renewal(p, p.servers[0], t, mcpconnectionattempts.ResultRefreshed)
		}
	}
}

// storyConnect: Ana and Mei's apps ask for Support Desk, where neither has connected an account.
// It draws nothing, so the stories before it keep their rows.
func (g *healthGen) storyConnect() {
	for i, key := range []string{"ana", "mei"} {
		p := g.personByKey(key)
		for k := range 4 {
			t := g.clk.now.Add(-3*time.Hour + time.Duration(i)*7*time.Minute + time.Duration(k)*50*time.Minute)
			f := g.person(p)
			f.grant, f.detail = oauthwire.GrantTypeJWTBearer, "no_credential_ready"
			g.begin(t, "support", healthApp(p.family)).row(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeInvalidRemoteSession, t.Add(time.Second), true, f)
		}
	}
}

// storyFirsts: each person's first sign-ins, about a month ago, so every server shows connections.
func (g *healthGen) storyFirsts() {
	for _, p := range healthPeople {
		for _, server := range p.servers {
			local := g.clk.now.Add(-29*24*time.Hour + time.Duration(g.r.between(2, 70))*time.Hour).In(time.Local)
			t := time.Date(local.Year(), local.Month(), local.Day(), g.r.between(9, 16), g.r.intn(60), 0, 0, time.Local).UTC()
			g.signIn(p, server, t, healthConnected)
		}
	}
}

// connectionHealthRows is the whole dataset, deterministic for the seed's clock: the attempts,
// and the answered requests that have no attempt rows.
func (s *seeder) connectionHealthRows() ([]mcpconnectionattempts.Row, []mcpconnectionattempts.Served, error) {
	g := &healthGen{s: s, clk: newHealthClock(s.now), r: &seedRand{name: s.name("health-attempts"), n: 0}, rows: nil, served: map[servedKey]mcpconnectionattempts.Served{}, n: 0, err: nil}
	g.baseline()
	g.storyFirsts()
	g.storyLinear()
	g.storyCursor()
	g.storyWiki()
	g.storyDana()
	g.storyLoop()
	g.storyKillswitch()
	g.storyDenied()
	g.storyNow()
	g.storyConnect()
	served := slices.SortedFunc(maps.Values(g.served), func(a, b mcpconnectionattempts.Served) int {
		return cmp.Or(cmp.Compare(a.ServerID, b.ServerID), cmp.Compare(a.MemberID, b.MemberID), cmp.Compare(a.SubjectURN, b.SubjectURN), cmp.Compare(a.ClientFamily, b.ClientFamily))
	})
	return g.rows, served, g.err
}
