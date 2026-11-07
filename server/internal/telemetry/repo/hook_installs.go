package repo

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Masterminds/semver/v3"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// HookInstall is one hooks install, keyed by project, user, hostname and
// harness. The version, generation and route are the latest non-empty values
// the install sent.
type HookInstall struct {
	ProjectID          string
	UserEmail          string
	Hostname           string
	Harness            string
	BinaryVersion      string
	EndpointGeneration string
	InstallRoute       string
	FirstSeen          time.Time
	LastSeen           time.Time
	Events             uint64
	Errors             uint64
}

const hookInstallsQuery = `
SELECT toString(gram_project_id) AS project_id, user_email, hostname, harness,
       argMaxIfMerge(binary_version) AS binary_version,
       argMaxIfMerge(endpoint_generation) AS endpoint_generation,
       argMaxIfMerge(install_route) AS install_route,
       min(first_seen) AS first_seen, max(last_seen) AS last_seen,
       sum(event_count) AS events, sum(error_count) AS errors
FROM hook_install_summaries
WHERE organization_id = ? AND day >= toDate(?)
GROUP BY gram_project_id, user_email, hostname, harness
ORDER BY last_seen DESC, user_email, hostname, harness
LIMIT ?`

// ListHookInstalls reads the installs of one organization seen since the
// given time. An empty organizationID reads the unattributed bucket: legacy
// unkeyed traffic that carries no organization.
func (q *Queries) ListHookInstalls(ctx context.Context, organizationID string, since time.Time, limit int) ([]HookInstall, error) {
	rows, err := q.conn.Query(ctx, hookInstallsQuery, organizationID, since.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("querying hook installs: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	out := []HookInstall{}
	for rows.Next() {
		var row HookInstall
		if err := rows.Scan(&row.ProjectID, &row.UserEmail, &row.Hostname, &row.Harness,
			&row.BinaryVersion, &row.EndpointGeneration, &row.InstallRoute,
			&row.FirstSeen, &row.LastSeen, &row.Events, &row.Errors); err != nil {
			return nil, fmt.Errorf("scanning hook install row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating hook install rows: %w", err)
	}
	return out, nil
}

// HookVersionCount is the number of installs on one binary version. An empty
// version means the install never sent one, such as the legacy curl hooks.
type HookVersionCount struct {
	Version  string
	Installs int
}

// HookFleetSummary answers the oncall questions for one organization's
// installs.
type HookFleetSummary struct {
	Installs int
	// Versions is sorted newest first, with unversioned installs last.
	Versions []HookVersionCount
	// Outdated counts installs below the minimum version, unversioned
	// installs included. It is zero when no minimum is given.
	Outdated int
	// EventsByGeneration is the event count per endpoint generation, so
	// legacy endpoint traffic is EventsByGeneration["legacy_keyed"] and
	// EventsByGeneration["legacy_unkeyed"].
	EventsByGeneration map[string]uint64
	// Quiet holds installs whose last event is older than the quiet cutoff.
	Quiet []HookInstall
}

// SummarizeHookInstalls builds the fleet summary. minVersion may be empty.
// Installs last seen before quietBefore are reported as quiet.
func SummarizeHookInstalls(installs []HookInstall, minVersion string, quietBefore time.Time) HookFleetSummary {
	var minV *semver.Version
	if minVersion != "" {
		if v, err := semver.NewVersion(minVersion); err == nil {
			minV = v
		}
	}

	summary := HookFleetSummary{
		Installs:           len(installs),
		Versions:           []HookVersionCount{},
		Outdated:           0,
		EventsByGeneration: map[string]uint64{},
		Quiet:              []HookInstall{},
	}
	counts := map[string]int{}
	for _, in := range installs {
		counts[in.BinaryVersion]++
		summary.EventsByGeneration[in.EndpointGeneration] += in.Events
		if minV != nil {
			v, err := semver.NewVersion(in.BinaryVersion)
			if err != nil || v.LessThan(minV) {
				summary.Outdated++
			}
		}
		if in.LastSeen.Before(quietBefore) {
			summary.Quiet = append(summary.Quiet, in)
		}
	}
	for version, n := range counts {
		summary.Versions = append(summary.Versions, HookVersionCount{Version: version, Installs: n})
	}
	sort.Slice(summary.Versions, func(i, j int) bool {
		return versionNewer(summary.Versions[i].Version, summary.Versions[j].Version)
	})
	return summary
}

func versionNewer(a, b string) bool {
	va, errA := semver.NewVersion(a)
	vb, errB := semver.NewVersion(b)
	switch {
	case errA == nil && errB == nil:
		if va.Equal(vb) {
			return a < b
		}
		return va.GreaterThan(vb)
	case errA == nil:
		return true
	case errB == nil:
		return false
	default:
		return a > b
	}
}
