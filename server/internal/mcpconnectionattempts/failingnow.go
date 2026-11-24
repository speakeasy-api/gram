package mcpconnectionattempts

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// EpisodeLookback is how far back failures are read, so an episode opens no earlier.
	EpisodeLookback = 7 * 24 * time.Hour

	// failingReadTimeout and failingReadMemoryBytes cap each ClickHouse read, so a very
	// large organization fails its own read instead of holding the caller.
	failingReadTimeout     = 60 * time.Second
	failingReadMemoryBytes = 2 << 30
)

func failingReadContext(ctx context.Context) context.Context {
	return clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"max_execution_time": int(failingReadTimeout / time.Second),
		"max_memory_usage":   failingReadMemoryBytes,
	}))
}

// ErrLogsOff means the organization has logs turned off, so its connections are not shown.
var ErrLogsOff = errors.New("logs are off for this organization")

// FailingNowFilter selects whose failures to read.
type FailingNowFilter struct {
	// OrganizationID is the organization. Required.
	OrganizationID string

	// ProviderHost keeps one provider, by its normalized host, or every one when empty.
	ProviderHost string

	// ServerID keeps one server, gateway, gateway member or toolset, or every one when empty.
	ServerID string

	// TriedSince keeps the connections whose latest failure became known since then, or
	// every one in EpisodeLookback when zero.
	TriedSince time.Time
}

// FailingPerson is a person who cannot use a server, gateway member or toolset right now.
type FailingPerson struct {
	Episode

	// ServerLastAnsweredAt is when the server, member or toolset last answered anyone,
	// zero for never. Later than LastFailureAt, it serves others while this person fails.
	ServerLastAnsweredAt time.Time
}

// ReadFailingNow returns the people failing at now: the part their latest failure on a
// connection broke has not worked since, and the grace of its reason has passed since
// the run opened. Rows naming no person are never counted. Served requests keep only
// their latest times, so ones later than now are read too: the read is exact only for
// now. Served writes are throttled per connection, about five minutes, so a person can
// still be listed for a while after they recovered. Failures from before served
// requests were recorded (before that deploy, or while the organization's logs were
// off) cannot be ended by them, so the caller must pass a TriedSince no earlier than
// that. The ServerID and ProviderHost filters apply after the read.
func ReadFailingNow(ctx context.Context, ch driver.Conn, logsEnabled func(context.Context, string) (bool, error), filter FailingNowFilter, now time.Time) ([]FailingPerson, error) {
	if filter.OrganizationID == "" {
		return nil, errors.New("failing now needs an organization")
	}
	enabled, err := logsEnabled(ctx, filter.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("check logs enabled: %w", err)
	}
	if !enabled {
		return nil, ErrLogsOff
	}

	from := now.Add(-EpisodeLookback)
	since := later(from, filter.TriedSince)
	attempts, err := readEpisodeAttempts(ctx, ch, filter.OrganizationID, from, since, now)
	if err != nil {
		return nil, err
	}
	failures := triedSince(failuresOf(attempts, now), since)
	if len(failures) == 0 {
		return nil, nil
	}
	served, answers, err := readServed(ctx, ch, filter.OrganizationID, failures)
	if err != nil {
		return nil, err
	}
	ev := indexEvidence(attempts, served, answers, now)

	var out []FailingPerson
	for _, e := range openEpisodes(failures, ev, since) {
		switch {
		case now.Sub(e.OpenedAt) < GraceOf(e.Reason),
			filter.ServerID != "" && e.ServerID != filter.ServerID && e.MemberID != filter.ServerID,
			filter.ProviderHost != "" && e.ProviderHost != filter.ProviderHost:
			continue
		}
		out = append(out, FailingPerson{Episode: e, ServerLastAnsweredAt: ev.answered(e.ServerKind, e.ServerID, e.MemberID)})
	}
	slices.SortFunc(out, func(a, b FailingPerson) int {
		return cmp.Or(a.OpenedAt.Compare(b.OpenedAt), cmp.Compare(a.SubjectURN, b.SubjectURN), cmp.Compare(a.ServerID, b.ServerID), cmp.Compare(a.MemberID, b.MemberID), cmp.Compare(a.AppKey, b.AppKey), cmp.Compare(a.Part, b.Part), cmp.Compare(a.ClientFamily, b.ClientFamily), cmp.Compare(a.Reason, b.Reason))
	})
	return out, nil
}

// triedSince keeps the failures of the connections whose latest failure became known
// since since. The caller decides how recently someone must have tried.
func triedSince(failures []failure, since time.Time) []failure {
	known := map[string]time.Time{}
	for _, f := range failures {
		known[f.connectionKey()] = later(known[f.connectionKey()], f.knownAt)
	}
	return slices.DeleteFunc(failures, func(f failure) bool { return known[f.connectionKey()].Before(since) })
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

// servedQuery reads the failing people's own connections on the failing servers and
// gateways. Arguments: organization, server kinds, server ids, subjects.
const servedQuery = `
SELECT
	server_kind, server_id, member_id, subject_urn, app_key, client_family, remote_session_client_id,
	max(last_served_at) AS served_at, max(last_answered_at) AS answered_at
FROM ` + lastServedTable + `
WHERE organization_id = ? AND server_kind IN ? AND server_id IN ? AND subject_urn IN ?
GROUP BY server_kind, server_id, member_id, subject_urn, app_key, client_family, remote_session_client_id`

// answersQuery reads anyone's latest answer per member on the failing servers and
// gateways: a server failure ends on anyone's answer. Arguments: organization, server
// kinds, server ids.
const answersQuery = `
SELECT
	server_kind, server_id, member_id, '' AS subject_urn, '' AS app_key, '' AS client_family, '' AS remote_session_client_id,
	max(last_served_at) AS served_at, max(last_answered_at) AS answered_at
FROM ` + lastServedTable + `
WHERE organization_id = ? AND server_kind IN ? AND server_id IN ?
GROUP BY server_kind, server_id, member_id`

// readServed reads the failing people's served connections, and anyone's latest
// answers, on the servers the failures reached.
func readServed(ctx context.Context, ch driver.Conn, organizationID string, failures []failure) (served, answers []lastServed, err error) {
	var kinds, ids, subjects []string
	for _, f := range failures {
		if f.a.ServerID == "" {
			continue
		}
		if !slices.Contains(kinds, f.a.ServerKind) {
			kinds = append(kinds, f.a.ServerKind)
		}
		if !slices.Contains(ids, f.a.ServerID) {
			ids = append(ids, f.a.ServerID)
		}
		if !slices.Contains(subjects, f.a.SubjectURN) {
			subjects = append(subjects, f.a.SubjectURN)
		}
	}
	if len(ids) == 0 {
		return nil, nil, nil
	}
	if served, err = queryServed(ctx, ch, servedQuery, organizationID, kinds, ids, subjects); err != nil {
		return nil, nil, err
	}
	if answers, err = queryServed(ctx, ch, answersQuery, organizationID, kinds, ids); err != nil {
		return nil, nil, err
	}
	return served, answers, nil
}

func queryServed(ctx context.Context, ch driver.Conn, query string, args ...any) ([]lastServed, error) {
	rows, err := ch.Query(failingReadContext(ctx), query, args...)
	if err != nil {
		return nil, fmt.Errorf("query served connections: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	var out []lastServed
	for rows.Next() {
		var s lastServed
		if err := rows.ScanStruct(&s); err != nil {
			return nil, fmt.Errorf("scan served connection: %w", err)
		}
		// The epoch is the column's "never".
		if s.AnsweredAt.Unix() <= 0 {
			s.AnsweredAt = time.Time{}
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read served connection rows: %w", err)
	}
	return out, nil
}
