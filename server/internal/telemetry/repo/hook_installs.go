package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	EventsByGeneration map[string]uint64
	Errors             uint64
}

// HookInstallPage is a page of installs, most recently seen first. Truncated
// is set when more installs matched than the limit allowed.
type HookInstallPage struct {
	Installs  []HookInstall
	Truncated bool
}

const hookInstallsQuery = `
SELECT toString(gram_project_id) AS project_id, user_email, hostname, harness,
       argMaxIfMerge(binary_version) AS binary_version,
       argMaxIfMerge(endpoint_generation) AS endpoint_generation,
       argMaxIfMerge(install_route) AS install_route,
       min(first_seen) AS first_seen, max(last_seen) AS last_seen,
       sum(event_count) AS events, sumMap(events_by_generation) AS events_by_generation,
       sum(error_count) AS errors
FROM hook_install_summaries
WHERE organization_id = ? AND day >= toDate(?)
GROUP BY gram_project_id, user_email, hostname, harness
HAVING last_seen >= ?
ORDER BY last_seen DESC, user_email, hostname, harness
LIMIT ?`

var errHookInstallsNoOrganization = errors.New("hook installs: organization id is required")

// ListHookInstalls reads the installs of one organization seen since the
// given time.
func (q *Queries) ListHookInstalls(ctx context.Context, organizationID string, since time.Time, limit int) (HookInstallPage, error) {
	if organizationID == "" {
		return HookInstallPage{}, errHookInstallsNoOrganization
	}
	return q.listHookInstalls(ctx, organizationID, since, limit)
}

// ListUnattributedHookInstalls reads installs whose traffic carried no
// organization, such as legacy unkeyed hooks. Staff-only: the bucket spans
// every tenant and must not be reached from a customer-facing surface.
func (q *Queries) ListUnattributedHookInstalls(ctx context.Context, since time.Time, limit int) (HookInstallPage, error) {
	return q.listHookInstalls(ctx, "", since, limit)
}

func (q *Queries) listHookInstalls(ctx context.Context, organizationID string, since time.Time, limit int) (HookInstallPage, error) {
	since = since.UTC()
	rows, err := q.conn.Query(ctx, hookInstallsQuery, organizationID, since, since, limit+1)
	if err != nil {
		return HookInstallPage{}, fmt.Errorf("querying hook installs: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	page := HookInstallPage{Installs: []HookInstall{}, Truncated: false}
	for rows.Next() {
		var row HookInstall
		if err := rows.Scan(&row.ProjectID, &row.UserEmail, &row.Hostname, &row.Harness,
			&row.BinaryVersion, &row.EndpointGeneration, &row.InstallRoute,
			&row.FirstSeen, &row.LastSeen, &row.Events, &row.EventsByGeneration, &row.Errors); err != nil {
			return HookInstallPage{}, fmt.Errorf("scanning hook install row: %w", err)
		}
		page.Installs = append(page.Installs, row)
	}
	if err := rows.Err(); err != nil {
		return HookInstallPage{}, fmt.Errorf("iterating hook install rows: %w", err)
	}
	if len(page.Installs) > limit {
		page.Installs = page.Installs[:limit]
		page.Truncated = true
	}
	return page, nil
}
