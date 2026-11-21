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

	// ClientVersion is the client version of the latest failure, empty when not recorded.
	ClientVersion string

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

// attemptFacts is one attempt reduced for episodes.
type attemptFacts struct {
	AttemptSummary

	// MemberID and EndingClientID are the gateway member and service connection on the
	// row End decides the attempt by: its earliest final row, else its latest row.
	MemberID       string `ch:"member"`
	EndingClientID string `ch:"ending_client"`

	// ClientVersion is the latest client version named.
	ClientVersion string `ch:"version"`

	// SignInMembers, SignInClients and SignInTimes are the member, service connection
	// and time of each row where the person's sign-in to a service worked.
	SignInMembers []string    `ch:"sign_in_members"`
	SignInClients []string    `ch:"sign_in_clients"`
	SignInTimes   []time.Time `ch:"sign_in_times"`
}

// serviceSignedIn selects the rows where a person's sign-in to a service worked: they
// signed in, the service renewed it, or their company issued one.
const serviceSignedIn = `(
	(step IN ('provider_sign_in', 'provider_token') AND step_result = 'signed_in')
	OR (step = 'provider_refresh' AND step_result IN ('refreshed', 'adopted_concurrent_winner'))
	OR (step = 'upstream_ema' AND step_result = 'assertion_issued'))`

// episodeColumns adds what episodes need to an attempt's summary.
const episodeColumns = attemptColumns + `,
	if(countIf(is_final) > 0,
		argMinIf(member_id, (event_time, step, outcome, reason, member_id), is_final),
		argMax(member_id, (event_time, step, reason, member_id))) AS member,
	if(countIf(is_final) > 0,
		argMinIf(remote_session_client_id, (event_time, step, outcome, reason, remote_session_client_id), is_final),
		argMax(remote_session_client_id, (event_time, step, reason, remote_session_client_id))) AS ending_client,
	argMaxIf(client_version, (event_time, client_version), client_version != '') AS version,
	groupArrayIf((member_id, remote_session_client_id, event_time), ` + serviceSignedIn + `) AS sign_ins,
	arrayMap(x -> toString(x.1), sign_ins) AS sign_in_members,
	arrayMap(x -> toString(x.2), sign_ins) AS sign_in_clients,
	arrayMap(x -> x.3, sign_ins) AS sign_in_times`

// episodeHaving keeps the attempts that can be a failure or show a part working: ones
// that connected, carry a reason, or hold a service sign-in.
const episodeHaving = ` AND ((has_final AND (final_outcome = 'connected' OR final_reason != ''))
	OR (NOT has_final AND last_reason != '') OR length(sign_ins) > 0)`

// readEpisodeAttempts reads the attempts episodes are derived from, as recorded by now:
// every attempt started from from by a person with an attempt that can read as failed
// since since. An unfinished one does once its live window has passed.
func readEpisodeAttempts(ctx context.Context, ch driver.Conn, organizationID string, from, since, now time.Time) ([]attemptFacts, error) {
	until := now.Add(time.Nanosecond)
	where, args, err := AttemptFilter{ //nolint:exhaustruct_v5 // one organization, every project and server
		StartedFrom: from, StartedBefore: until, OrganizationID: organizationID,
		PeopleFailingFrom: later(from, since.Add(-AttemptLiveWindow)), PeopleFailingBefore: until,
	}.where()
	if err != nil {
		return nil, err
	}
	rows, err := ch.Query(failingReadContext(ctx), `SELECT * EXCEPT (sign_ins) FROM (SELECT`+episodeColumns+` FROM `+eventsTable+
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
	case f.part == PartAppSignIn && namedClient(a.ClientFamily):
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

// lastServed is one connection's latest served times in lastServedTable, with max()
// taken over its unmerged rows. AnsweredAt is zero when the server never answered.
type lastServed struct {
	ServerKind            string    `ch:"server_kind"`
	ServerID              string    `ch:"server_id"`
	MemberID              string    `ch:"member_id"`
	SubjectURN            string    `ch:"subject_urn"`
	AppKey                string    `ch:"app_key"`
	ClientFamily          string    `ch:"client_family"`
	RemoteSessionClientID string    `ch:"remote_session_client_id"`
	ServedAt              time.Time `ch:"served_at"`
	AnsweredAt            time.Time `ch:"answered_at"`
}

// serverRef is a server, gateway or toolset by kind and id.
type serverRef struct{ kind, id string }

// evidence is everything recorded that shows part of a connection working.
type evidence struct {
	signIns map[string][]signIn
	served  map[serverRef][]lastServed
}

func indexEvidence(attempts []attemptFacts, served []lastServed, now time.Time) evidence {
	ev := evidence{signIns: map[string][]signIn{}, served: map[serverRef][]lastServed{}}
	for _, s := range served {
		ref := serverRef{kind: s.ServerKind, id: s.ServerID}
		ev.served[ref] = append(ev.served[ref], s)
	}
	for _, a := range attempts {
		s := signIn{
			part: PartAppSignIn, serverKind: a.ServerKind, server: a.ServerID, member: "", project: a.ProjectID,
			app: a.AppKey, clientFamily: a.ClientFamily, remoteClientID: a.RemoteSessionClientID, at: time.Time{},
		}
		// Connected on the token step means the app was handed a working sign-in, new or
		// renewed. A renewed company sign-in is the same for an attempt that reached no server.
		if e := a.End(now); e.Outcome == OutcomeConnected && (e.Step == StepInboundToken.name || e.Step == StepSignInRenewal.name) {
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
	a := f.a
	if f.part == PartServer {
		return ev.answered(a.ServerKind, a.ServerID, a.MemberID)
	}
	var at time.Time
	for _, s := range ev.signIns[a.SubjectURN] {
		if f.proves(s) {
			at = later(at, s.at)
		}
	}
	if a.ServerID == "" {
		return at
	}
	for _, s := range ev.served[serverRef{kind: a.ServerKind, id: a.ServerID}] {
		if s.SubjectURN != a.SubjectURN {
			continue
		}
		switch f.part {
		case PartAppSignIn:
			// Any request served got past the app's sign-in, whoever answered it.
			if f.sameApp(s.AppKey, s.ClientFamily) {
				at = later(at, s.ServedAt)
			}
		case PartServiceSignIn:
			// Only the server answering shows the service accepted the sign-in.
			if f.servedBySameService(s) {
				at = later(at, s.AnsweredAt)
			}
		case PartServer:
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
	for _, s := range ev.served[serverRef{kind: kind, id: server}] {
		if s.MemberID == member || (kind == serverKindMeta && member == "") {
			at = later(at, s.AnsweredAt)
		}
	}
	return at
}

// proves reports whether the same person's sign-in s shows the part f broke working.
func (f failure) proves(s signIn) bool {
	switch f.part {
	case PartAppSignIn:
		// A service sign-in does not count: one made while signing in comes before the app has its own.
		return s.part == PartAppSignIn && f.sameServer(s) && f.sameApp(s.app, s.clientFamily)
	case PartServiceSignIn:
		return s.part == PartServiceSignIn && f.sameService(s)
	case PartServer:
		return false
	default:
		return false
	}
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
	if f.a.AppKey == AppUnverified.key {
		return true
	}
	if app != f.a.AppKey {
		return false
	}
	return !namedClient(f.a.ClientFamily) || !namedClient(clientFamily) || f.a.ClientFamily == clientFamily
}

// servedBySameService reports whether the person's served connection s, on f's server,
// used the service sign-in that failed.
func (f failure) servedBySameService(s lastServed) bool {
	failed, used := f.a.RemoteSessionClientID, s.RemoteSessionClientID
	if s.MemberID != f.a.MemberID {
		// A failure on the gateway alone can be any member's service, so only the same service connection ties a member to it.
		return f.a.MemberID == "" && failed != "" && failed == used
	}
	// No service connection on the row is unknown, not another one: hosted paths never name one.
	return failed == "" || used == "" || failed == used
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

// openEpisodes returns, by connection key, the run of failures since the broken part
// last worked, for each part of a connection whose latest failure is not over.
func openEpisodes(failures []failure, ev evidence) map[string]Episode {
	byKey := map[string][]failure{}
	for _, f := range failures {
		byKey[f.connectionKey()] = append(byKey[f.connectionKey()], f)
	}
	episodes := map[string]Episode{}
	for key, fs := range byKey {
		latest := slices.MaxFunc(fs, func(x, y failure) int {
			return cmp.Or(x.end.At.Compare(y.end.At), slices.Compare(x.a.AttemptID[:], y.a.AttemptID[:]))
		})
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
		}
		opening := latest
		for _, f := range fs {
			if f.end.At.Before(worked) {
				continue
			}
			e.Failures += f.a.Weight
			if cmp.Or(f.end.At.Compare(opening.end.At), slices.Compare(f.a.AttemptID[:], opening.a.AttemptID[:])) < 0 {
				opening = f
			}
		}
		e.OpenedAt, e.OpeningReason = opening.end.At, opening.end.Reason
		episodes[key] = e
	}
	return episodes
}
