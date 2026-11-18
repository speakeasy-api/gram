package attemptlog

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// Facet names a filter that has counts.
type Facet string

const (
	FacetOutcomes    Facet = "outcomes"
	FacetSteps       Facet = "steps"
	FacetServers     Facet = "servers"
	FacetMembers     Facet = "members"
	FacetPeople      Facet = "people"
	FacetNetworks    Facet = "networks"
	FacetApps        Facet = "apps"
	FacetAppVersions Facet = "app_versions"
	FacetReasons     Facet = "reasons"
	FacetServices    Facet = "services"
)

// Facets are every Facet.
var Facets = []Facet{FacetOutcomes, FacetSteps, FacetServers, FacetMembers, FacetPeople, FacetNetworks, FacetApps, FacetAppVersions, FacetReasons, FacetServices}

// FacetValue is one filter value and its weighted attempts.
type FacetValue struct {
	// Value is the filter value.
	Value string

	// Attempts is the weighted attempts with that value.
	Attempts uint64
}

// Counts are the total and the count per filter value.
type Counts struct {
	// Total is weighted attempts matching every filter.
	Total uint64

	// Facets hold each filter's values; each ignores its own filter. Each keeps
	// its MaxFacetValues values with the most attempts, which Truncated does not report.
	Facets map[Facet][]FacetValue

	// Truncated says the combinations were cut at the cap, so counts are lower bounds.
	Truncated bool
}

// cubeRow is one combination of filter values and its weighted attempts.
type cubeRow struct {
	Outcome    string `ch:"c_outcome"`
	Step       string `ch:"c_step"`
	Server     string `ch:"c_server"`
	Member     string `ch:"c_member"`
	Subject    string `ch:"c_subject"`
	Network    string `ch:"c_network"`
	App        string `ch:"c_app"`
	AppVersion string `ch:"c_app_version"`
	Reason     string `ch:"c_reason"`
	Service    string `ch:"c_service"`
	Attempts   uint64 `ch:"c_attempts"`
}

func (r cubeRow) value(facet Facet) string {
	switch facet {
	case FacetOutcomes:
		return r.Outcome
	case FacetSteps:
		return r.Step
	case FacetServers:
		return r.Server
	case FacetMembers:
		return r.Member
	case FacetPeople:
		return r.Subject
	case FacetNetworks:
		return r.Network
	case FacetApps:
		return r.App
	case FacetAppVersions:
		return r.AppVersion
	case FacetReasons:
		return r.Reason
	case FacetServices:
		return r.Service
	default:
		return ""
	}
}

func (f Filters) values(facet Facet) []string {
	switch facet {
	case FacetOutcomes:
		return f.Outcomes
	case FacetSteps:
		return f.Steps
	case FacetServers:
		return f.ServerIDs
	case FacetMembers:
		return f.MemberIDs
	case FacetPeople:
		return f.People
	case FacetNetworks:
		return f.Networks
	case FacetApps:
		return f.Apps
	case FacetAppVersions:
		return f.AppVersions
	case FacetReasons:
		return f.Reasons
	case FacetServices:
		return f.Services
	default:
		return nil
	}
}

// matches applies every filter but skip, as filterSQL does.
func (f Filters) matches(r cubeRow, skip Facet) bool {
	for _, facet := range Facets {
		if facet == skip {
			continue
		}
		values := f.values(facet)
		if len(values) == 0 {
			continue
		}
		// A network stands for attempts with no known person, as in filterSQL.
		if facet == FacetNetworks && (r.Subject != "" || r.Network == "") {
			return false
		}
		if !slices.Contains(values, r.value(facet)) {
			return false
		}
	}
	return true
}

// Count reads the total and per-value counts in one bounded query, then applies filters in Go.
func Count(ctx context.Context, ch driver.Conn, s Scope, f Filters, now time.Time) (Counts, error) {
	attempts, args, err := attemptsSQL(s, now)
	if err != nil {
		return Counts{}, err
	}
	args = append(args, maxCubeRows+1)
	query := `
	SELECT d_outcome AS c_outcome, d_step AS c_step, a_server AS c_server, d_member AS c_member,
		a_subject AS c_subject, a_device AS c_network, d_app AS c_app, d_app_version AS c_app_version,
		d_reason AS c_reason, d_service AS c_service, sum(a_weight) AS c_attempts
	FROM (` + attempts + `)
	GROUP BY c_outcome, c_step, c_server, c_member, c_subject, c_network, c_app, c_app_version, c_reason, c_service
	ORDER BY c_attempts DESC
	LIMIT ?` + readSettings
	result, err := ch.Query(ctx, query, args...)
	if err != nil {
		return Counts{}, fmt.Errorf("query attempt counts: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return result.Close() })

	var cube []cubeRow
	for result.Next() {
		var row cubeRow
		if err := result.ScanStruct(&row); err != nil {
			return Counts{}, fmt.Errorf("scan attempt counts: %w", err)
		}
		cube = append(cube, row)
	}
	if err := result.Err(); err != nil {
		return Counts{}, fmt.Errorf("read attempt counts: %w", err)
	}
	truncated := len(cube) > maxCubeRows
	if truncated {
		cube = cube[:maxCubeRows]
	}
	counts := countCube(cube, f)
	counts.Truncated = truncated
	if truncated {
		total, err := countTotal(ctx, ch, s, f, now)
		if err != nil {
			return Counts{}, err
		}
		counts.Total = total
	}
	return counts, nil
}

// countTotal is the exact weighted total under every filter, for when the cube is cut.
func countTotal(ctx context.Context, ch driver.Conn, s Scope, f Filters, now time.Time) (uint64, error) {
	attempts, args, err := attemptsSQL(s, now)
	if err != nil {
		return 0, err
	}
	where, filterArgs := filterSQL(f)
	args = append(args, filterArgs...)
	var total uint64
	if err := ch.QueryRow(ctx, `SELECT toUInt64(sum(a_weight)) FROM (`+attempts+`) WHERE `+where+readSettings, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("query attempt total: %w", err)
	}
	return total, nil
}

func countCube(cube []cubeRow, f Filters) Counts {
	counts := Counts{Total: 0, Facets: make(map[Facet][]FacetValue, len(Facets)), Truncated: false}
	for _, row := range cube {
		if f.matches(row, "") {
			counts.Total += row.Attempts
		}
	}
	for _, facet := range Facets {
		byValue := map[string]uint64{}
		for _, row := range cube {
			if facet == FacetNetworks && row.Subject != "" {
				continue
			}
			// Only apps and people have a meaning for empty: unlisted, and no known person.
			if facet != FacetApps && facet != FacetPeople && row.value(facet) == "" {
				continue
			}
			if !f.matches(row, facet) {
				continue
			}
			byValue[row.value(facet)] += row.Attempts
		}
		values := make([]FacetValue, 0, len(byValue))
		for value, attempts := range byValue {
			values = append(values, FacetValue{Value: value, Attempts: attempts})
		}
		slices.SortFunc(values, func(a, b FacetValue) int {
			return cmp.Or(cmp.Compare(b.Attempts, a.Attempts), cmp.Compare(a.Value, b.Value))
		})
		if len(values) > MaxFacetValues {
			values = values[:MaxFacetValues]
		}
		counts.Facets[facet] = values
	}
	return counts
}
