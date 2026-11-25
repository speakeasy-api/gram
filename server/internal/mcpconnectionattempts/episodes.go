package mcpconnectionattempts

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Episode is a person's open run of failures of one part of a connection (a server,
// gateway member and app): every such failure since that part last worked. Repeats and
// retries stay in the same run. Two parts broken at once are two episodes.
type Episode struct {
	// SubjectURN is the person.
	SubjectURN string

	// ProjectID is the project of the server, uuid.Nil for an organization gateway.
	ProjectID uuid.UUID

	// ServerKind is the kind of server, or provider_session or identity_provider for none.
	ServerKind string

	// ServerID is the server, gateway or toolset, empty when the attempts reached none.
	ServerID string

	// MemberID is the gateway member, empty for none.
	MemberID string

	// AppKey is the app.
	AppKey string

	// ClientFamily is the client of the latest failure.
	ClientFamily string

	// ClientVersion is the version on the row that named ClientFamily, empty when not recorded.
	ClientVersion string

	// Apps are the distinct apps of the run's failures, latest first, at most MaxEpisodeApps.
	Apps []EpisodeApp

	// AppsTotal counts the distinct apps of the run's failures, without the MaxEpisodeApps cap.
	AppsTotal int

	// ProviderHost is the provider of the latest failure.
	ProviderHost string

	// Part is the part of the connection that is broken.
	Part Part

	// OpeningReason is the reason of the first failure.
	OpeningReason string

	// Reason is the reason of the latest failure.
	Reason string

	// Step is the step of the latest failure.
	Step string

	// OpenedAt is when the first failure ended.
	OpenedAt time.Time

	// LastFailureAt is when the latest failure ended.
	LastFailureAt time.Time

	// Failures counts failed attempts, limiter repeats included.
	Failures uint64

	// LastWorkedAt is when the broken part last worked before the run, zero when it was
	// not seen working in the time read.
	LastWorkedAt time.Time
}

// MaxEpisodeApps bounds the apps an episode keeps.
const MaxEpisodeApps = 5

// EpisodeApp is one app a run's failures came from.
type EpisodeApp struct {
	// Key is the app key.
	Key string

	// Family is the client family, empty when not named.
	Family string

	// Version is the client version, empty when not recorded.
	Version string
}

// BackingServerID is the mcp_servers id reached (server or gateway member), else empty.
func (e Episode) BackingServerID() string {
	var raw string
	switch e.ServerKind {
	case serverKindMCP:
		raw = e.ServerID
	case serverKindMeta:
		raw = e.MemberID
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return ""
	}
	return id.String()
}

// OnMCPServer reports whether the episode's server is an MCP server, gateway or toolset, by its kind and id only.
func (e Episode) OnMCPServer() bool {
	switch e.ServerKind {
	case serverKindMCP, serverKindMeta, serverKindToolset:
		return e.ServerID != ""
	}
	return false
}

// attemptFacts is one attempt reduced for episodes.
type attemptFacts struct {
	AttemptSummary

	// MemberID and EndingClientID are the gateway member and service connection on the
	// row End decides the attempt by: its earliest final row, else its latest row.
	MemberID       string `ch:"member"`
	EndingClientID string `ch:"ending_client"`

	// ClientVersion is the version on the row that named the client, empty when it had none.
	ClientVersion string `ch:"version"`

	// SignInMembers, SignInClients and SignInTimes are the member, service connection
	// and time of each row where the person's sign-in to a service worked.
	SignInMembers []string    `ch:"sign_in_members"`
	SignInClients []string    `ch:"sign_in_clients"`
	SignInTimes   []time.Time `ch:"sign_in_times"`

	// Issuers are the providers recorded on the attempt's rows.
	Issuers []string `ch:"issuers"`
}

// serviceSignInRows selects the rows where a person's sign-in to a service worked: they
// signed in, the service renewed it, or their company issued one.
var serviceSignInRows = `(
	(step IN ('` + StepProviderSignIn.name + `', '` + StepProviderToken.name + `') AND step_result = '` + ResultSignedIn.name + `')
	OR (step = '` + StepProviderRefresh.name + `' AND step_result IN ('` + ResultRefreshed.name + `', '` + ResultAdoptedConcurrentWinner.name + `'))
	OR (step = '` + StepUpstreamEMA.name + `' AND step_result = '` + ResultAssertionIssued.name + `'))`

// episodeColumns adds what episodes need to an attempt's summary.
var episodeColumns = attemptColumns + `,
	if(countIf(is_final) > 0,
		argMinIf(member_id, (event_time, step, outcome, reason, member_id), is_final),
		argMax(member_id, (event_time, step, reason, member_id))) AS member,
	if(countIf(is_final) > 0,
		argMinIf(remote_session_client_id, (event_time, step, outcome, reason, remote_session_client_id), is_final),
		argMax(remote_session_client_id, (event_time, step, reason, remote_session_client_id))) AS ending_client,
	if(countIf(client_family NOT IN ('', 'browser')) > 0,
		argMinIf(client_version, (event_time, client_family, client_version), client_family NOT IN ('', 'browser')),
		argMinIf(client_version, (event_time, client_version), client_family = 'browser')) AS version,
	groupArrayIf((member_id, remote_session_client_id, event_time), ` + serviceSignInRows + `) AS sign_ins,
	arrayMap(x -> toString(x.1), sign_ins) AS sign_in_members,
	arrayMap(x -> toString(x.2), sign_ins) AS sign_in_clients,
	arrayMap(x -> x.3, sign_ins) AS sign_in_times,
	groupUniqArrayIf(user_session_issuer_id, user_session_issuer_id != '') AS issuers`

// episodeHaving keeps the attempts that can be a failure or show a part working: ones
// that connected, carry a reason, or hold a service sign-in.
const episodeHaving = ` AND ((` + fixCandidates + `) OR length(sign_ins) > 0)`

// readEpisodeAttempts reads the attempts episodes are derived from, as recorded by now:
// every attempt started from from by a person with an attempt that can read as failed
// since since. An unfinished one does once its live window has passed.
func readEpisodeAttempts(ctx context.Context, ch driver.Conn, filter FailingNowFilter, from, since, now time.Time) ([]attemptFacts, error) {
	organizationID := filter.OrganizationID
	until := now.Add(time.Nanosecond)
	where, args, err := AttemptFilter{
		StartedFrom: from, StartedBefore: until, OrganizationID: organizationID, ExcludeOrganizationIDs: nil, ProjectID: uuid.Nil, ServerID: "",
		PeopleFailingFrom: later(from, since.Add(-AttemptLiveWindow)), PeopleFailingBefore: until,
		PeopleProjectID: filter.ProjectID, PeopleIssuerID: filter.IssuerID, PeopleIssuerServerIDs: filter.IssuerServerIDs, FixCandidatesOnly: false,
	}.where()
	if err != nil {
		return nil, err
	}
	rows, err := ch.Query(failingReadContext(ctx), `SELECT * EXCEPT (sign_ins) FROM (SELECT`+episodeColumns+` FROM `+EventsTable+
		` WHERE organization_id = ? AND `+where+` AND event_time <= fromUnixTimestamp64Nano(?) GROUP BY organization_id, attempt_id HAVING subject != ''`+episodeHaving+`)`,
		append(append([]any{organizationID}, args...), now.UnixNano())...)
	if err != nil {
		return nil, fmt.Errorf("query episode attempts: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	var out []attemptFacts
	for rows.Next() {
		var a attemptFacts
		if err := rows.ScanStruct(&a); err != nil {
			return nil, fmt.Errorf("scan episode attempt: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read episode attempt rows: %w", err)
	}
	return out, nil
}

// failure is one failed attempt that can make a person failing.
type failure struct {
	a    attemptFacts
	end  Ending
	part Part

	// knownAt is when the attempt could first be read as failed: an attempt with no
	// final row is only failed once its live window has passed.
	knownAt time.Time
}

// failuresOf keeps the attempts that ended as a broken connection of a known person.
// An anonymous session is not one: the recorder cannot tell such callers apart.
func failuresOf(attempts []attemptFacts, now time.Time) []failure {
	var out []failure
	for _, a := range attempts {
		e := a.End(now)
		if !e.CountsAsFailing() || strings.HasPrefix(a.SubjectURN, string(urn.SessionSubjectKindAnonymous)+":") {
			continue
		}
		f := failure{a: a, end: e, part: PartOf(e.Step, e.Reason), knownAt: e.At}
		if !a.HasFinal {
			f.knownAt = later(e.At, a.StartedAt.Add(AttemptLiveWindow))
		}
		// The service connection that failed is the one on the row that ended the attempt.
		f.a.RemoteSessionClientID = a.EndingClientID
		// An app signs in to the gateway, not to the member a call was for.
		if f.part == PartAppSignIn {
			f.a.MemberID = ""
		}
		out = append(out, f)
	}
	return out
}

// connectionKey is the part of a connection a failure broke, told apart as far as its
// evidence is: an app sign-in by the client when one is named, a service sign-in by its
// service connection, and an attempt that reached no server by its project. Failures
// under one key are over together, so two that different successes end never share one.
func (f failure) connectionKey() string {
	a := f.a
	key := string(f.part) + "|" + a.SubjectURN + "|" + a.ServerKind + "|" + a.ServerID + "|" + a.MemberID + "|" + a.AppKey
	switch {
	case f.part == PartAppSignIn && NamedClient(a.ClientFamily):
		key += "|" + a.ClientFamily
	case f.part == PartServiceSignIn:
		key += "|" + a.RemoteSessionClientID
	}
	if a.ServerID == "" {
		key += "|" + a.ProjectID.String() + "|" + a.RemoteSessionClientID
	}
	return key
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// signIn is one recorded sign-in that worked: the app's to Speakeasy, or the person's
// to a service.
type signIn struct {
	part                              Part
	serverKind, server, member        string
	project                           uuid.UUID
	app, clientFamily, remoteClientID string
	at                                time.Time
}

// serverRef is a server, gateway or toolset by kind and id.
type serverRef struct{ kind, id string }

// evidence is everything recorded that shows part of a connection working.
type evidence struct {
	signIns map[string][]signIn

	// served are the failing people's own connections, and answers anyone's latest
	// answer per member, on the failing servers.
	served  map[serverRef][]lastServed
	answers map[serverRef][]lastServed
}

func indexEvidence(attempts []attemptFacts, served, answers []lastServed, now time.Time) evidence {
	ev := evidence{signIns: map[string][]signIn{}, served: map[serverRef][]lastServed{}, answers: map[serverRef][]lastServed{}}
	for _, s := range served {
		ref := serverRef{kind: s.ServerKind, id: s.ServerID}
		ev.served[ref] = append(ev.served[ref], s)
	}
	for _, s := range answers {
		ref := serverRef{kind: s.ServerKind, id: s.ServerID}
		ev.answers[ref] = append(ev.answers[ref], s)
	}
	for _, a := range attempts {
		s := signIn{
			part: PartAppSignIn, serverKind: a.ServerKind, server: a.ServerID, member: "", project: a.ProjectID,
			app: a.AppKey, clientFamily: a.ClientFamily, remoteClientID: a.RemoteSessionClientID, at: time.Time{},
		}
		// Connected on the token step means the app was handed a working sign-in, new or
		// renewed. A renewed company sign-in is the same for an attempt that reached no server.
		// First-party sign-in has no token step and connects on consent.
		e := a.End(now)
		if e.Outcome == OutcomeConnected && (e.Step == StepInboundToken.name || e.Step == StepSignInRenewal.name ||
			(e.Step == StepInboundConsent.name && a.AppKey == AppFirstParty.key)) {
			s.at = e.At
			ev.signIns[a.SubjectURN] = append(ev.signIns[a.SubjectURN], s)
		}
		s.part = PartServiceSignIn
		for i, at := range a.SignInTimes {
			s.member, s.remoteClientID, s.at = a.SignInMembers[i], a.SignInClients[i], at
			ev.signIns[a.SubjectURN] = append(ev.signIns[a.SubjectURN], s)
		}
	}
	return ev
}

// lastWorked is the latest time the part f broke is known to have worked for f's
// connection, zero for never. Later than the failure, it means the failure is over.
func (ev evidence) lastWorked(f failure) time.Time {
	switch f.part {
	case PartAppSignIn:
		return ev.appSignedIn(f)
	case PartServiceSignIn:
		return ev.serviceSignedIn(f)
	case PartServer:
		return ev.answered(f.a.ServerKind, f.a.ServerID, f.a.MemberID)
	default:
		return time.Time{}
	}
}

// appSignedIn is when the person's app last signed in on f's server, or last had a
// request there served, whoever answered it. A service sign-in does not count: one made
// while signing in comes before the app has its own.
func (ev evidence) appSignedIn(f failure) time.Time {
	a := f.a
	var at time.Time
	for _, s := range ev.signIns[a.SubjectURN] {
		if s.part == PartAppSignIn && f.sameServer(s) && f.sameApp(s.app, s.clientFamily) {
			at = later(at, s.at)
		}
	}
	for _, s := range ev.served[serverRef{kind: a.ServerKind, id: a.ServerID}] {
		if s.SubjectURN == a.SubjectURN && f.sameApp(s.AppKey, s.ClientFamily) {
			at = later(at, s.ServedAt)
		}
	}
	return at
}

// serviceSignedIn is when the person last signed in to f's service, or the server last
// answered them over it. Speakeasy serving a request does not count, except for a
// failure at a gateway's own gate.
func (ev evidence) serviceSignedIn(f failure) time.Time {
	a := f.a
	var at time.Time
	for _, s := range ev.signIns[a.SubjectURN] {
		if s.part == PartServiceSignIn && f.sameService(s) {
			at = later(at, s.at)
		}
	}
	atGate := a.ServerKind == serverKindMeta && a.MemberID == "" && a.RemoteSessionClientID == ""
	for _, s := range ev.served[serverRef{kind: a.ServerKind, id: a.ServerID}] {
		if s.SubjectURN != a.SubjectURN {
			continue
		}
		switch {
		case atGate && s.MemberID == "":
			// The gateway serving the person again shows its gate let them through.
			at = later(at, s.ServedAt)
		case f.servedBySameService(s):
			at = later(at, s.AnsweredAt)
		}
	}
	return at
}

// answered is when the server, or the gateway member, last answered anyone. Any member
// answering through a gateway shows the gateway's own checks work.
func (ev evidence) answered(kind, server, member string) time.Time {
	var at time.Time
	if server == "" {
		return at
	}
	for _, s := range ev.answers[serverRef{kind: kind, id: server}] {
		if s.MemberID == member || (kind == serverKindMeta && member == "") {
			at = later(at, s.AnsweredAt)
		}
	}
	return at
}

func (f failure) sameServer(s signIn) bool {
	a := f.a
	if s.serverKind != a.ServerKind || s.server != a.ServerID {
		return false
	}
	return a.ServerID != "" || (s.project == a.ProjectID && s.remoteClientID == a.RemoteSessionClientID)
}

// sameApp treats a failure filed under no verified app as any app's, and compares the
// client only when both rows name a real one: one registration kind covers many apps.
func (f failure) sameApp(app, clientFamily string) bool {
	if f.a.AppKey != AppUnverified.key && app != f.a.AppKey {
		return false
	}
	return !NamedClient(f.a.ClientFamily) || !NamedClient(clientFamily) || f.a.ClientFamily == clientFamily
}

// servedBySameService reports whether the person's served connection s, on f's server,
// used the service sign-in that failed.
func (f failure) servedBySameService(s lastServed) bool {
	failed, used := f.a.RemoteSessionClientID, s.RemoteSessionClientID
	if s.MemberID != f.a.MemberID {
		// A failure on the gateway alone can be any member's service, so only the same service connection ties a member to it.
		return f.a.MemberID == "" && failed != "" && failed == used
	}
	// A failure naming a service connection ends only over that one. Hosted failures name none.
	return failed == "" || failed == used
}

// sameService compares the service connection when both rows name one. Otherwise the
// server or member the sign-in is for decides, which a gateway alone does not name.
func (f failure) sameService(s signIn) bool {
	a := f.a
	if a.RemoteSessionClientID != "" && s.remoteClientID != "" {
		return a.RemoteSessionClientID == s.remoteClientID
	}
	endpoint := serviceEndpoint(a.ServerKind, a.ServerID, a.MemberID)
	return endpoint != "" && endpoint == serviceEndpoint(s.serverKind, s.server, s.member)
}

// serviceEndpoint is the server a service sign-in is for: the gateway member, or the
// server itself. A sign-in for a direct server names it as the member too.
func serviceEndpoint(kind, server, member string) string {
	switch {
	case member != "":
		return member
	case kind == serverKindMCP || kind == serverKindToolset:
		return server
	default:
		return ""
	}
}

// openEpisodes returns the run of failures since the broken part last worked, for each
// part of a connection whose latest failure is not over and became known since since.
// Evidence at exactly the failure's time does not end it.
func openEpisodes(failures []failure, ev evidence, since time.Time) []Episode {
	byKey := map[string][]failure{}
	for _, f := range failures {
		byKey[f.connectionKey()] = append(byKey[f.connectionKey()], f)
	}
	var episodes []Episode
	for _, fs := range byKey {
		latest := slices.MaxFunc(fs, func(x, y failure) int {
			return cmp.Or(x.end.At.Compare(y.end.At), slices.Compare(x.a.AttemptID[:], y.a.AttemptID[:]))
		})
		if latest.knownAt.Before(since) {
			continue
		}
		worked := ev.lastWorked(latest)
		if worked.After(latest.end.At) {
			continue
		}
		a := latest.a
		e := Episode{
			SubjectURN: a.SubjectURN, ProjectID: a.ProjectID, ServerKind: a.ServerKind, ServerID: a.ServerID, MemberID: a.MemberID,
			AppKey: a.AppKey, ClientFamily: a.ClientFamily, ClientVersion: a.ClientVersion, ProviderHost: a.ProviderHost,
			Part: latest.part, OpeningReason: latest.end.Reason, Reason: latest.end.Reason, Step: latest.end.Step,
			OpenedAt: latest.end.At, LastFailureAt: latest.end.At, Failures: 0, LastWorkedAt: worked,
			Apps:      []EpisodeApp{{Key: a.AppKey, Family: a.ClientFamily, Version: a.ClientVersion}},
			AppsTotal: 1,
		}
		apps := map[EpisodeApp]bool{e.Apps[0]: true}
		opening := latest
		// Newest first, so the kept apps are the latest when there are more than MaxEpisodeApps.
		slices.SortFunc(fs, func(x, y failure) int { return y.end.At.Compare(x.end.At) })
		for _, f := range fs {
			if f.end.At.Before(worked) {
				continue
			}
			e.Failures += f.a.Weight
			if app := (EpisodeApp{Key: f.a.AppKey, Family: f.a.ClientFamily, Version: f.a.ClientVersion}); !apps[app] {
				apps[app] = true
				e.AppsTotal++
				if len(e.Apps) < MaxEpisodeApps {
					e.Apps = append(e.Apps, app)
				}
			}
			if cmp.Or(f.end.At.Compare(opening.end.At), slices.Compare(f.a.AttemptID[:], opening.a.AttemptID[:])) < 0 {
				opening = f
			}
		}
		e.OpenedAt, e.OpeningReason = opening.end.At, opening.end.Reason
		episodes = append(episodes, e)
	}
	return episodes
}
