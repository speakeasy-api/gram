package mcpconnectionattempts

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// RefusalFilter selects whose managed sign-in refusals to read.
type RefusalFilter struct {
	// OrganizationID is the organization. Required.
	OrganizationID string

	// ProjectIDs are the servers' projects; organization gateways are always read.
	ProjectIDs []uuid.UUID

	// ServerIDs are the servers, reached directly or as a gateway member.
	ServerIDs []string

	// Since is the earliest refusal read.
	Since time.Time
}

// Refusals is the people one server's managed sign-in was refused for, by one party.
type Refusals struct {
	// ServerID is the MCP server, reached directly or as a gateway member.
	ServerID string

	// RefusedBy is who refused.
	RefusedBy RefusedBy

	// People counts distinct people refused since the filter's Since whose requests on
	// the server have not been answered since their latest refusal.
	People uint64

	// LastRefusedAt is the latest of their refusals.
	LastRefusedAt time.Time
}

// refusalRow is one row of refusalsQuery.
type refusalRow struct {
	Server        string    `ch:"server"`
	RefusedBy     string    `ch:"refused_by"`
	People        uint64    `ch:"people"`
	LastRefusedAt time.Time `ch:"last_refused_at"`
}

// refusalsQuery reads, per server and party, the people refused whose latest refusal is
// later than their latest answered request. Arguments: refusal codes, their parties,
// organization, projects, attempts from, refusals from, now, server ids twice, refusal
// codes, anonymous prefix, organization, projects, answers from, server ids twice.
const refusalsQuery = `
SELECT r.server AS server, r.refused_by AS refused_by, toUInt64(count()) AS people, max(r.refused_at) AS last_refused_at
FROM (
	SELECT
		if(member_id != '', member_id, server_id) AS server,
		transform(reason, ?, ?, '') AS refused_by,
		subject_urn AS subject,
		max(event_time) AS refused_at
	FROM ` + EventsTable + `
	WHERE organization_id = ? AND project_id IN ?
		AND attempt_started_at >= fromUnixTimestamp64Nano(?)
		AND event_time >= fromUnixTimestamp64Nano(?) AND event_time <= fromUnixTimestamp64Nano(?)
		AND server_kind IN ('` + serverKindMCP + `', '` + serverKindMeta + `')
		AND (server_id IN ? OR member_id IN ?)
		AND reason IN ?
		AND subject_urn != '' AND NOT startsWith(subject_urn, ?)
	GROUP BY server, refused_by, subject
) AS r
LEFT JOIN (
	SELECT if(member_id != '', member_id, server_id) AS server, subject_urn AS subject, max(last_answered_at) AS answered_at
	FROM ` + lastServedTable + `
	WHERE organization_id = ? AND project_id IN ? AND last_answered_at >= fromUnixTimestamp64Nano(?)
		AND server_kind IN ('` + serverKindMCP + `', '` + serverKindMeta + `') AND (server_id IN ? OR member_id IN ?)
	GROUP BY server, subject
) AS s ON r.server = s.server AND r.subject = s.subject
WHERE s.answered_at IS NULL OR s.answered_at < r.refused_at
GROUP BY r.server, r.refused_by
ORDER BY r.server, r.refused_by
SETTINGS join_use_nulls = 1`

// ReadManagedSignInRefusals counts people refused managed sign-in per server and party; ErrLogsOff when logs are off.
// Not ReadFailingNow: it counts failed endings only, and access_denied is blocked and consent-page refusals end nothing.
func ReadManagedSignInRefusals(ctx context.Context, ch driver.Conn, logsEnabled func(context.Context, string) (bool, error), filter RefusalFilter, now time.Time) ([]Refusals, error) {
	if filter.OrganizationID == "" {
		return nil, errors.New("refusals need an organization")
	}
	if len(filter.ServerIDs) == 0 {
		return nil, nil
	}
	enabled, err := logsEnabled(ctx, filter.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("check logs enabled: %w", err)
	}
	if !enabled {
		return nil, ErrLogsOff
	}

	codes, parties := refusalCodes()
	// Organization gateways have no project.
	projects := append(slices.Clone(filter.ProjectIDs), uuid.Nil)
	from := filter.Since
	rows, err := ch.Query(failingReadContext(ctx), refusalsQuery,
		codes, parties,
		filter.OrganizationID, projects, from.Add(-AttemptLiveWindow).UnixNano(), from.UnixNano(), now.UnixNano(),
		filter.ServerIDs, filter.ServerIDs,
		codes,
		string(urn.SessionSubjectKindAnonymous)+":",
		filter.OrganizationID, projects, from.UnixNano(), filter.ServerIDs, filter.ServerIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("query managed sign-in refusals: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	var out []Refusals
	for rows.Next() {
		var r refusalRow
		if err := rows.ScanStruct(&r); err != nil {
			return nil, fmt.Errorf("scan managed sign-in refusals: %w", err)
		}
		out = append(out, Refusals{ServerID: r.Server, RefusedBy: RefusedBy(r.RefusedBy), People: r.People, LastRefusedAt: r.LastRefusedAt})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read managed sign-in refusal rows: %w", err)
	}
	return out, nil
}

// refusalCodes are the stored codes refusingParty names and, in the same order, their parties.
func refusalCodes() (codes, parties []string) {
	for _, code := range slices.SortedFunc(maps.Keys(refusingParty), func(a, b Code) int { return strings.Compare(a.name, b.name) }) {
		codes = append(codes, code.name)
		parties = append(parties, string(refusingParty[code]))
	}
	return codes, parties
}
