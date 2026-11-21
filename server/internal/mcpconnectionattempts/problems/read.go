package problems

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// Read returns the organization's suspected problems at now, from the people who tried
// within TriedWithin. It returns mcpconnectionattempts.ErrLogsOff when the
// organization's logs are off.
func Read(ctx context.Context, ch driver.Conn, db repo.DBTX, logsEnabled func(context.Context, string) (bool, error), organizationID string, now time.Time) ([]Problem, error) {
	if organizationID == "" {
		return nil, errors.New("suspected problems need an organization")
	}
	episodes, err := mcpconnectionattempts.ReadFailingNow(ctx, ch, logsEnabled, mcpconnectionattempts.FailingNowFilter{
		OrganizationID: organizationID,
		ProviderHost:   "",
		ServerID:       "",
		TriedSince:     now.Add(-TriedWithin),
	}, now)
	if err != nil {
		return nil, fmt.Errorf("read failing now: %w", err)
	}
	if len(episodes) == 0 {
		return nil, nil
	}

	upstreams, err := readUpstreams(ctx, db, organizationID, episodes)
	if err != nil {
		return nil, err
	}
	served, err := readServedByFamily(ctx, ch, organizationID, episodes)
	if err != nil {
		return nil, err
	}
	return Group(organizationID, episodes, upstreams, served, now), nil
}

// readUpstreams reads the configured upstream of each episode's mcp_servers row: the
// server for an MCP server, the member for a gateway member.
func readUpstreams(ctx context.Context, db repo.DBTX, organizationID string, episodes []mcpconnectionattempts.FailingPerson) (map[string]Upstream, error) {
	var ids []uuid.UUID
	for _, e := range episodes {
		var raw string
		switch e.ServerKind {
		case mcpconnectionattempts.ServerKindMCP:
			raw = e.ServerID
		case mcpconnectionattempts.ServerKindMeta:
			raw = e.MemberID
		}
		id, err := uuid.Parse(raw)
		if err != nil || slices.Contains(ids, id) {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return map[string]Upstream{}, nil
	}

	rows, err := repo.New(db).ListServerUpstreams(ctx, repo.ListServerUpstreamsParams{OrganizationID: organizationID, ServerIds: ids})
	if err != nil {
		return nil, fmt.Errorf("list server upstreams: %w", err)
	}
	out := make(map[string]Upstream, len(rows))
	for _, r := range rows {
		out[r.ID.String()] = Upstream{URL: r.Url, Issuer: r.Issuer.String}
	}
	return out, nil
}

// servedByFamilyQuery reads the latest served request per server and client family.
// Arguments: organization, server kinds, server ids.
const servedByFamilyQuery = `
SELECT server_kind, server_id, member_id, client_family, max(last_served_at) AS last_served_at
FROM ` + mcpconnectionattempts.LastServedTable + `
WHERE organization_id = ? AND server_kind IN ? AND server_id IN ?
GROUP BY server_kind, server_id, member_id, client_family`

// readServedByFamily reads which client families were last served when on the
// episodes' servers.
func readServedByFamily(ctx context.Context, ch driver.Conn, organizationID string, episodes []mcpconnectionattempts.FailingPerson) ([]ServedByFamily, error) {
	var kinds, ids []string
	for _, e := range episodes {
		if e.ServerID == "" {
			continue
		}
		if !slices.Contains(kinds, e.ServerKind) {
			kinds = append(kinds, e.ServerKind)
		}
		if !slices.Contains(ids, e.ServerID) {
			ids = append(ids, e.ServerID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := ch.Query(mcpconnectionattempts.FailingReadContext(ctx), servedByFamilyQuery, organizationID, kinds, ids)
	if err != nil {
		return nil, fmt.Errorf("query served by family: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	var out []ServedByFamily
	for rows.Next() {
		var s ServedByFamily
		if err := rows.ScanStruct(&s); err != nil {
			return nil, fmt.Errorf("scan served by family: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read served by family rows: %w", err)
	}
	return out, nil
}
